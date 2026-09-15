// Package traffic implements the traffic monitoring subsystem (spec §45–§46).
//
// The collector is bound to the runtime lifecycle: it starts when the runtime
// enters RUNNING and is cancelled when the runtime stops or fails. It never
// starts as a side effect of opening a screen in the UI.
package traffic

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/larffxx/singboxui/internal/events"
)

// Rate limits: the UI is updated at most once per eventTick, and the connection
// list is capped so a long-running session cannot grow memory without bound.
const (
	eventTick      = time.Second
	snapshotTopN   = 200
	maxTokenLength = 512
)

// Target identifies the local Clash API of the running runtime.
type Target struct {
	// BaseURL is the control endpoint, for example "http://127.0.0.1:9090".
	BaseURL string
	// Secret is the Clash API secret, when the configuration sets one.
	Secret string
	// ProfileID and RevisionID are reported with every snapshot so the UI can
	// tell a stale collector apart from a current one.
	ProfileID  string
	RevisionID string
}

// Deps wires the collector.
type Deps struct {
	Emitter events.Emitter
	Logger  *slog.Logger
	// HTTPClient is used for the control endpoints; it is owned by the caller so
	// the composition root controls timeouts and proxy behaviour.
	HTTPClient *http.Client
	// Interval overrides the update tick; tests use a short one.
	Interval time.Duration
}

// OutboundTraffic is the traffic attributed to one outbound.
type OutboundTraffic struct {
	Outbound    string `json:"outbound"`
	Upload      int64  `json:"upload"`
	Download    int64  `json:"download"`
	Connections int    `json:"connections"`
}

// Connection is one active connection, trimmed for the UI.
type Connection struct {
	ID       string `json:"id"`
	Host     string `json:"host,omitempty"`
	Network  string `json:"network,omitempty"`
	Rule     string `json:"rule,omitempty"`
	Outbound string `json:"outbound,omitempty"`
	Upload   int64  `json:"upload"`
	Download int64  `json:"download"`
	Started  string `json:"started,omitempty"`
}

// Snapshot is the latest traffic state (spec §46).
type Snapshot struct {
	Timestamp     time.Time         `json:"timestamp"`
	ProfileID     string            `json:"profileId,omitempty"`
	RevisionID    string            `json:"revisionId,omitempty"`
	Connections   int               `json:"connections"`
	UploadTotal   int64             `json:"uploadTotal"`
	DownloadTotal int64             `json:"downloadTotal"`
	UploadRate    int64             `json:"uploadRate"`
	DownloadRate  int64             `json:"downloadRate"`
	ByOutbound    []OutboundTraffic `json:"byOutbound"`
	Active        []Connection      `json:"active"`
	Available     bool              `json:"available"`
	Error         string            `json:"error,omitempty"`
}

// Service runs at most one collector.
type Service struct {
	deps   Deps
	logger *slog.Logger

	mu      sync.RWMutex
	latest  Snapshot
	running bool
	target  Target
	cancel  context.CancelFunc
	done    chan struct{}
}

// New builds the collector.
func New(deps Deps) *Service {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.Interval <= 0 {
		deps.Interval = eventTick
	}
	if deps.HTTPClient == nil {
		deps.HTTPClient = &http.Client{Timeout: 5 * time.Second}
	}
	return &Service{deps: deps, logger: deps.Logger}
}

// Start begins collecting for a target. Starting an already running collector
// with the same target is a no-op; a different target replaces the old
// collector atomically.
func (s *Service) Start(parent context.Context, target Target) {
	if strings.TrimSpace(target.BaseURL) == "" {
		s.Stop()
		return
	}
	s.mu.Lock()
	if s.running && sameTarget(s.target, target) {
		s.mu.Unlock()
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	s.target = target
	s.running = true
	s.done = make(chan struct{})
	done := s.done
	s.mu.Unlock()

	go func() {
		defer close(done)
		s.run(ctx, target)
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()
}

// Stop cancels the collector and waits for it to finish. It is safe to call it
// when nothing is running.
func (s *Service) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	done := s.done
	s.running = false
	s.cancel = nil
	s.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	if done != nil {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			s.logger.Warn("the traffic collector did not stop in time")
		}
	}
	s.mu.Lock()
	s.latest = Snapshot{Timestamp: time.Now().UTC()}
	s.mu.Unlock()
}

// Running reports whether a collector is active.
func (s *Service) Running() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// Latest returns the most recent snapshot.
func (s *Service) Latest() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latest
}

// run reads the rate stream and refreshes the connection list on the same tick.
func (s *Service) run(ctx context.Context, target Target) {
	rates := make(chan rateUpdate, 4)
	// The rate stream is cancelled through ctx, so the reader cannot outlive the
	// collector (spec §45, §62).
	go s.consumeRates(ctx, target, rates)

	ticker := time.NewTicker(s.deps.Interval)
	defer ticker.Stop()
	var (
		uploadRate   int64
		downloadRate int64
		failures     int
	)
	for {
		select {
		case <-ctx.Done():
			return
		case update, ok := <-rates:
			if !ok {
				rates = nil
				continue
			}
			uploadRate = update.Up
			downloadRate = update.Down
		case <-ticker.C:
			snapshot, err := s.collect(ctx, target, uploadRate, downloadRate)
			if err != nil {
				failures++
				if ctx.Err() != nil {
					return
				}
				// A control endpoint that never answers is reported once and then
				// throttled: monitoring must not flood the log (spec §46).
				if failures <= 3 || failures%30 == 0 {
					s.logger.Debug("traffic snapshot unavailable", "error", err)
				}
				s.publish(Snapshot{
					Timestamp:  time.Now().UTC(),
					ProfileID:  target.ProfileID,
					RevisionID: target.RevisionID,
					Available:  false,
					Error:      err.Error(),
				})
				continue
			}
			failures = 0
			s.publish(snapshot)
		}
	}
}

type rateUpdate struct {
	Up   int64 `json:"up"`
	Down int64 `json:"down"`
}

// consumeRates streams the Clash API rate endpoint. Each line is one update;
// the UI never polls it.
func (s *Service) consumeRates(ctx context.Context, target Target, out chan<- rateUpdate) {
	defer close(out)
	endpoint, err := target.endpoint("/traffic")
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return
	}
	target.authorize(req)
	resp, err := s.deps.HTTPClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 8*1024), 256*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var update rateUpdate
		if err := json.Unmarshal([]byte(line), &update); err != nil {
			continue
		}
		select {
		case out <- update:
		case <-ctx.Done():
			return
		}
	}
}

// collect reads the connection list once and builds a snapshot.
func (s *Service) collect(ctx context.Context, target Target, uploadRate, downloadRate int64) (Snapshot, error) {
	body, err := s.get(ctx, target, "/connections")
	if err != nil {
		return Snapshot{}, err
	}
	var payload connectionsPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return Snapshot{}, fmt.Errorf("decode connections: %w", err)
	}
	snapshot := Snapshot{
		Timestamp:     time.Now().UTC(),
		ProfileID:     target.ProfileID,
		RevisionID:    target.RevisionID,
		Connections:   len(payload.Connections),
		UploadTotal:   payload.UploadTotal,
		DownloadTotal: payload.DownloadTotal,
		UploadRate:    uploadRate,
		DownloadRate:  downloadRate,
		Available:     true,
	}
	byOutbound := map[string]*OutboundTraffic{}
	for _, connection := range payload.Connections {
		outbound := connection.outboundName()
		bucket, ok := byOutbound[outbound]
		if !ok {
			bucket = &OutboundTraffic{Outbound: outbound}
			byOutbound[outbound] = bucket
		}
		bucket.Upload += connection.Upload
		bucket.Download += connection.Download
		bucket.Connections++
	}
	snapshot.ByOutbound = make([]OutboundTraffic, 0, len(byOutbound))
	for _, bucket := range byOutbound {
		snapshot.ByOutbound = append(snapshot.ByOutbound, *bucket)
	}
	sort.Slice(snapshot.ByOutbound, func(i, j int) bool {
		left := snapshot.ByOutbound[i].Upload + snapshot.ByOutbound[i].Download
		right := snapshot.ByOutbound[j].Upload + snapshot.ByOutbound[j].Download
		if left == right {
			return snapshot.ByOutbound[i].Outbound < snapshot.ByOutbound[j].Outbound
		}
		return left > right
	})
	if len(snapshot.ByOutbound) > snapshotTopN {
		snapshot.ByOutbound = snapshot.ByOutbound[:snapshotTopN]
	}
	snapshot.Active = make([]Connection, 0, min(len(payload.Connections), snapshotTopN))
	sorted := append([]connectionPayload(nil), payload.Connections...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Upload+sorted[i].Download > sorted[j].Upload+sorted[j].Download
	})
	for _, connection := range sorted {
		if len(snapshot.Active) >= snapshotTopN {
			break
		}
		snapshot.Active = append(snapshot.Active, connection.view())
	}
	return snapshot, nil
}

func (s *Service) publish(snapshot Snapshot) {
	s.mu.Lock()
	s.latest = snapshot
	s.mu.Unlock()
	if s.deps.Emitter != nil {
		s.deps.Emitter.Emit(events.TrafficSnapshot, snapshot)
	}
}

func (s *Service) get(ctx context.Context, target Target, path string) ([]byte, error) {
	endpoint, err := target.endpoint(path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	target.authorize(req)
	resp, err := s.deps.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4*1024))
		return nil, fmt.Errorf("the control endpoint answered %s", resp.Status)
	}
	limited := io.LimitReader(resp.Body, 8<<20)
	return io.ReadAll(limited)
}

func (t Target) endpoint(path string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(t.BaseURL), "/")
	if base == "" {
		return "", errors.New("no control endpoint is configured")
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("the control endpoint %q is not a URL", t.BaseURL)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + path
	return parsed.String(), nil
}

func (t Target) authorize(req *http.Request) {
	if secret := strings.TrimSpace(t.Secret); secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
}

func sameTarget(left, right Target) bool {
	return left.BaseURL == right.BaseURL && left.Secret == right.Secret &&
		left.ProfileID == right.ProfileID && left.RevisionID == right.RevisionID
}

func truncate(value string) string {
	if len(value) <= maxTokenLength {
		return value
	}
	return value[:maxTokenLength]
}

type connectionsPayload struct {
	UploadTotal   int64               `json:"uploadTotal"`
	DownloadTotal int64               `json:"downloadTotal"`
	Connections   []connectionPayload `json:"connections"`
}

type connectionPayload struct {
	ID       string   `json:"id"`
	Upload   int64    `json:"upload"`
	Download int64    `json:"download"`
	Start    string   `json:"start"`
	Rule     string   `json:"rule"`
	Chains   []string `json:"chains"`
	Metadata struct {
		Host    string `json:"host"`
		Dest    string `json:"destinationIP"`
		Network string `json:"network"`
		Type    string `json:"type"`
	} `json:"metadata"`
}

func (c connectionPayload) outboundName() string {
	for i := len(c.Chains) - 1; i >= 0; i-- {
		if name := strings.TrimSpace(c.Chains[i]); name != "" {
			return truncate(name)
		}
	}
	return "unknown"
}

func (c connectionPayload) view() Connection {
	host := c.Metadata.Host
	if host == "" {
		host = c.Metadata.Dest
	}
	return Connection{
		ID:       c.ID,
		Host:     truncate(host),
		Network:  truncate(c.Metadata.Network),
		Rule:     truncate(c.Rule),
		Outbound: c.outboundName(),
		Upload:   c.Upload,
		Download: c.Download,
		Started:  c.Start,
	}
}
