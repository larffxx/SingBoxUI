package traffic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/larffxx/singboxui/internal/events"
)

// quietLogger keeps the throttling paths (which log every failure up to three
// times) from polluting the test output.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// fakeEmitter records every event so the published snapshots can be asserted
// exactly instead of being inferred from the collector's internal state.
type fakeEmitter struct {
	mu     sync.Mutex
	events []emittedEvent
}

type emittedEvent struct {
	name    string
	payload any
}

func (e *fakeEmitter) Emit(name string, payload any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, emittedEvent{name: name, payload: payload})
}

func (e *fakeEmitter) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.events)
}

func (e *fakeEmitter) snapshots(t *testing.T) []Snapshot {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Snapshot, 0, len(e.events))
	for _, ev := range e.events {
		if ev.name != events.TrafficSnapshot {
			t.Errorf("emitted event %q, want %q", ev.name, events.TrafficSnapshot)
			continue
		}
		snap, ok := ev.payload.(Snapshot)
		if !ok {
			t.Fatalf("traffic payload is %T, want traffic.Snapshot", ev.payload)
		}
		out = append(out, snap)
	}
	return out
}

func newService(emitter events.Emitter, client *http.Client, interval time.Duration) *Service {
	return New(Deps{Emitter: emitter, Logger: quietLogger(), HTTPClient: client, Interval: interval})
}

// connectionPayload builds one Clash API connection with the metadata shape the
// real endpoint returns.
func makeConnection(id string, chains []string, up, down int64, host, dest, network, rule string) connectionPayload {
	c := connectionPayload{
		ID:       id,
		Upload:   up,
		Download: down,
		Start:    "2026-09-11T10:00:00Z",
		Rule:     rule,
		Chains:   chains,
	}
	c.Metadata.Host = host
	c.Metadata.Dest = dest
	c.Metadata.Network = network
	c.Metadata.Type = "HTTP"
	return c
}

// connectionsServer serves a fixed /connections payload.
func connectionsServer(t *testing.T, payload connectionsPayload) *httptest.Server {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/connections" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func waitForSnapshot(t *testing.T, svc *Service, want string, pred func(Snapshot) bool) Snapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if sn := svc.Latest(); pred(sn) {
			return sn
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no snapshot matching %q within the deadline; latest = %+v", want, svc.Latest())
	return Snapshot{}
}

func TestTargetEndpointJoinsTheControlPath(t *testing.T) {
	tests := []struct {
		name string
		base string
		want string
		// wantErrSubstring is checked with strings.Contains; an empty string
		// means the call must succeed.
		wantErrSubstring string
	}{
		{name: "plain", base: "http://127.0.0.1:9090", want: "http://127.0.0.1:9090/connections"},
		{name: "trailing slash", base: "http://127.0.0.1:9090/", want: "http://127.0.0.1:9090/connections"},
		{name: "path prefix", base: "http://127.0.0.1:9090/clash", want: "http://127.0.0.1:9090/clash/connections"},
		{name: "path prefix with slash", base: "http://127.0.0.1:9090/clash/", want: "http://127.0.0.1:9090/clash/connections"},
		{name: "surrounding spaces", base: "  http://127.0.0.1:9090  ", want: "http://127.0.0.1:9090/connections"},
		{name: "https", base: "https://box.local:9090", want: "https://box.local:9090/connections"},
		{name: "empty", base: "", wantErrSubstring: "no control endpoint"},
		{name: "spaces only", base: "   ", wantErrSubstring: "no control endpoint"},
		{name: "no scheme", base: "127.0.0.1:9090", wantErrSubstring: "is not a URL"},
		{name: "unterminated host", base: "http://[::1", wantErrSubstring: "is not a URL"},
		{name: "scheme only", base: "http://", wantErrSubstring: "is not a URL"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Target{BaseURL: tc.base}.endpoint("/connections")
			if tc.wantErrSubstring != "" {
				if err == nil {
					t.Fatalf("endpoint(%q) = %q, want an error containing %q", tc.base, got, tc.wantErrSubstring)
				}
				if !strings.Contains(err.Error(), tc.wantErrSubstring) {
					t.Fatalf("endpoint(%q) error = %q, want it to contain %q", tc.base, err, tc.wantErrSubstring)
				}
				return
			}
			if err != nil {
				t.Fatalf("endpoint(%q) = %v, want nil", tc.base, err)
			}
			if got != tc.want {
				t.Fatalf("endpoint(%q) = %q, want %q", tc.base, got, tc.want)
			}
		})
	}
}

func TestTargetAuthorizeSendsTheSecretOnlyWhenSet(t *testing.T) {
	tests := []struct {
		name   string
		secret string
		want   string
	}{
		{name: "empty", secret: "", want: ""},
		{name: "spaces", secret: "   ", want: ""},
		{name: "plain", secret: "s3cr3t", want: "Bearer s3cr3t"},
		{name: "trimmed", secret: "  s3cr3t  ", want: "Bearer s3cr3t"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:9090/connections", nil)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			Target{Secret: tc.secret}.authorize(req)
			if got := req.Header.Get("Authorization"); got != tc.want {
				t.Fatalf("Authorization = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOutboundNameUsesTheLastNonEmptyChainEntry(t *testing.T) {
	long := strings.Repeat("n", maxTokenLength+40)

	tests := []struct {
		name   string
		chains []string
		want   string
	}{
		{name: "no chains", chains: nil, want: "unknown"},
		{name: "empty chain list", chains: []string{}, want: "unknown"},
		{name: "only empty entries", chains: []string{"", "   "}, want: "unknown"},
		{name: "single", chains: []string{"proxy"}, want: "proxy"},
		{name: "last wins", chains: []string{"select", "proxy"}, want: "proxy"},
		{name: "trailing empty is skipped", chains: []string{"select", "proxy", ""}, want: "proxy"},
		{name: "leading empty is skipped", chains: []string{"", "proxy", ""}, want: "proxy"},
		{name: "trimmed", chains: []string{"  proxy  "}, want: "proxy"},
		{name: "truncated at the token limit", chains: []string{long}, want: long[:maxTokenLength]},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := connectionPayload{Chains: tc.chains}.outboundName()
			if got != tc.want {
				t.Fatalf("outboundName() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestConnectionViewPrefersHostAndTruncatesTokens(t *testing.T) {
	long := strings.Repeat("r", maxTokenLength+10)

	tests := []struct {
		name          string
		connection    connectionPayload
		wantHost      string
		wantRule      string
		wantNetwork   string
		wantOutbound  string
		wantUpload    int64
		wantDownload  int64
		wantStartedAt string
	}{
		{
			name:          "host wins over the destination",
			connection:    makeConnection("c1", []string{"proxy"}, 10, 20, "example.com", "93.184.216.34", "tcp", "MATCH"),
			wantHost:      "example.com",
			wantRule:      "MATCH",
			wantNetwork:   "tcp",
			wantOutbound:  "proxy",
			wantUpload:    10,
			wantDownload:  20,
			wantStartedAt: "2026-09-11T10:00:00Z",
		},
		{
			name:          "destination is the fallback",
			connection:    makeConnection("c2", []string{"direct"}, 1, 2, "", "1.1.1.1", "udp", "GeoIP,CN"),
			wantHost:      "1.1.1.1",
			wantRule:      "GeoIP,CN",
			wantNetwork:   "udp",
			wantOutbound:  "direct",
			wantUpload:    1,
			wantDownload:  2,
			wantStartedAt: "2026-09-11T10:00:00Z",
		},
		{
			name:         "no host at all",
			connection:   connectionPayload{ID: "c3", Chains: []string{"", ""}},
			wantHost:     "",
			wantOutbound: "unknown",
		},
		{
			name:          "long rule is truncated",
			connection:    makeConnection("c4", []string{"proxy"}, 0, 0, "h", "", "tcp", long),
			wantHost:      "h",
			wantRule:      long[:maxTokenLength],
			wantNetwork:   "tcp",
			wantOutbound:  "proxy",
			wantStartedAt: "2026-09-11T10:00:00Z",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.connection.view()
			if got.ID != tc.connection.ID {
				t.Errorf("ID = %q, want %q", got.ID, tc.connection.ID)
			}
			if got.Host != tc.wantHost {
				t.Errorf("Host = %q, want %q", got.Host, tc.wantHost)
			}
			if got.Rule != tc.wantRule {
				t.Errorf("Rule = %q, want %q", got.Rule, tc.wantRule)
			}
			if got.Network != tc.wantNetwork {
				t.Errorf("Network = %q, want %q", got.Network, tc.wantNetwork)
			}
			if got.Outbound != tc.wantOutbound {
				t.Errorf("Outbound = %q, want %q", got.Outbound, tc.wantOutbound)
			}
			if got.Upload != tc.wantUpload || got.Download != tc.wantDownload {
				t.Errorf("bytes = %d/%d, want %d/%d", got.Upload, got.Download, tc.wantUpload, tc.wantDownload)
			}
			if got.Started != tc.wantStartedAt {
				t.Errorf("Started = %q, want %q", got.Started, tc.wantStartedAt)
			}
		})
	}
}

func TestTruncateKeepsExactlyTheLimit(t *testing.T) {
	if got := truncate(strings.Repeat("x", maxTokenLength)); got != strings.Repeat("x", maxTokenLength) {
		t.Fatalf("truncate() shortened a value of exactly %d bytes", maxTokenLength)
	}
	if got := truncate(strings.Repeat("x", maxTokenLength+1)); len(got) != maxTokenLength {
		t.Fatalf("truncate() length = %d, want %d", len(got), maxTokenLength)
	}
}

func TestCollectAggregatesByOutboundAndSortsByVolume(t *testing.T) {
	payload := connectionsPayload{
		UploadTotal:   5000,
		DownloadTotal: 9000,
		Connections: []connectionPayload{
			makeConnection("c1", []string{"select", "proxy"}, 100, 200, "example.com", "93.184.216.34", "tcp", "MATCH"),
			makeConnection("c2", []string{"proxy"}, 50, 50, "cdn.example.com", "93.184.216.35", "tcp", "MATCH"),
			makeConnection("c3", []string{"direct"}, 10, 5, "local", "", "udp", "GeoIP,CN"),
			makeConnection("c4", nil, 0, 0, "", "10.0.0.1", "tcp", "MATCH"),
		},
	}
	srv := connectionsServer(t, payload)

	before := time.Now().UTC().Add(-time.Second)
	sn, err := newService(nil, srv.Client(), time.Second).collect(context.Background(),
		Target{BaseURL: srv.URL, Secret: "s", ProfileID: "p1", RevisionID: "r1"}, 111, 222)
	if err != nil {
		t.Fatalf("collect() = %v, want nil", err)
	}

	if sn.Timestamp.IsZero() || sn.Timestamp.Before(before) {
		t.Errorf("Timestamp = %v, want a fresh UTC timestamp", sn.Timestamp)
	}
	if sn.Timestamp.Location() != time.UTC {
		t.Errorf("Timestamp location = %v, want UTC", sn.Timestamp.Location())
	}
	if !sn.Available {
		t.Errorf("Available = false, want true (error = %q)", sn.Error)
	}
	if sn.Error != "" {
		t.Errorf("Error = %q, want empty", sn.Error)
	}
	if sn.ProfileID != "p1" || sn.RevisionID != "r1" {
		t.Errorf("profile/revision = %q/%q, want p1/r1", sn.ProfileID, sn.RevisionID)
	}
	if sn.Connections != 4 {
		t.Errorf("Connections = %d, want 4", sn.Connections)
	}
	if sn.UploadTotal != 5000 || sn.DownloadTotal != 9000 {
		t.Errorf("totals = %d/%d, want 5000/9000", sn.UploadTotal, sn.DownloadTotal)
	}
	if sn.UploadRate != 111 || sn.DownloadRate != 222 {
		t.Errorf("rates = %d/%d, want 111/222", sn.UploadRate, sn.DownloadRate)
	}

	wantByOutbound := []OutboundTraffic{
		{Outbound: "proxy", Upload: 150, Download: 250, Connections: 2},
		{Outbound: "direct", Upload: 10, Download: 5, Connections: 1},
		{Outbound: "unknown", Upload: 0, Download: 0, Connections: 1},
	}
	if !reflect.DeepEqual(sn.ByOutbound, wantByOutbound) {
		t.Errorf("ByOutbound = %+v, want %+v", sn.ByOutbound, wantByOutbound)
	}

	wantActiveIDs := []string{"c1", "c2", "c3", "c4"}
	gotActiveIDs := make([]string, 0, len(sn.Active))
	for _, c := range sn.Active {
		gotActiveIDs = append(gotActiveIDs, c.ID)
	}
	if !reflect.DeepEqual(gotActiveIDs, wantActiveIDs) {
		t.Errorf("Active order = %v, want %v (highest volume first)", gotActiveIDs, wantActiveIDs)
	}
	if sn.Active[0].Host != "example.com" || sn.Active[0].Outbound != "proxy" || sn.Active[0].Upload != 100 {
		t.Errorf("Active[0] = %+v, want the c1 view", sn.Active[0])
	}
	if sn.Active[3].Outbound != "unknown" {
		t.Errorf("Active[3].Outbound = %q, want unknown", sn.Active[3].Outbound)
	}
}

func TestCollectTieBreaksByOutboundName(t *testing.T) {
	payload := connectionsPayload{
		Connections: []connectionPayload{
			makeConnection("c1", []string{"zzz"}, 100, 0, "a", "", "tcp", ""),
			makeConnection("c2", []string{"aaa"}, 60, 40, "b", "", "tcp", ""),
			makeConnection("c3", []string{"mmm"}, 100, 0, "c", "", "tcp", ""),
		},
	}
	srv := connectionsServer(t, payload)

	sn, err := newService(nil, srv.Client(), time.Second).collect(context.Background(), Target{BaseURL: srv.URL}, 0, 0)
	if err != nil {
		t.Fatalf("collect() = %v, want nil", err)
	}
	want := []string{"aaa", "mmm", "zzz"}
	got := make([]string, 0, len(sn.ByOutbound))
	for _, bucket := range sn.ByOutbound {
		got = append(got, bucket.Outbound)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ByOutbound = %v, want %v (equal totals are ordered by name)", got, want)
	}
}

func TestCollectPrunesToTheTopN(t *testing.T) {
	const total = snapshotTopN + 50

	build := func(reverse bool) connectionsPayload {
		payload := connectionsPayload{UploadTotal: 1}
		payload.Connections = make([]connectionPayload, 0, total)
		for i := 0; i < total; i++ {
			index := i
			if reverse {
				index = total - 1 - i
			}
			payload.Connections = append(payload.Connections,
				makeConnection(fmt.Sprintf("c-%03d", index), []string{fmt.Sprintf("ob-%03d", index)},
					int64(1000-index), 0, "h", "", "tcp", ""))
		}
		return payload
	}

	for _, reverse := range []bool{false, true} {
		name := "insertion order"
		if reverse {
			name = "reversed insertion order"
		}
		t.Run(name, func(t *testing.T) {
			srv := connectionsServer(t, build(reverse))
			sn, err := newService(nil, srv.Client(), time.Second).collect(context.Background(), Target{BaseURL: srv.URL}, 0, 0)
			if err != nil {
				t.Fatalf("collect() = %v, want nil", err)
			}
			if sn.Connections != total {
				t.Fatalf("Connections = %d, want %d", sn.Connections, total)
			}
			if len(sn.ByOutbound) != snapshotTopN {
				t.Fatalf("len(ByOutbound) = %d, want %d", len(sn.ByOutbound), snapshotTopN)
			}
			if sn.ByOutbound[0].Outbound != "ob-000" {
				t.Errorf("highest-volume outbound = %q, want ob-000", sn.ByOutbound[0].Outbound)
			}
			if sn.ByOutbound[snapshotTopN-1].Outbound != fmt.Sprintf("ob-%03d", snapshotTopN-1) {
				t.Errorf("last kept outbound = %q, want ob-%03d", sn.ByOutbound[snapshotTopN-1].Outbound, snapshotTopN-1)
			}
			if len(sn.Active) != snapshotTopN {
				t.Fatalf("len(Active) = %d, want %d", len(sn.Active), snapshotTopN)
			}
			if sn.Active[0].ID != "c-000" {
				t.Errorf("Active[0].ID = %q, want c-000", sn.Active[0].ID)
			}
		})
	}
}

func TestCollectReportsEndpointFailures(t *testing.T) {
	t.Run("non-200 answer", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		t.Cleanup(srv.Close)

		_, err := newService(nil, srv.Client(), time.Second).collect(context.Background(), Target{BaseURL: srv.URL}, 0, 0)
		if err == nil {
			t.Fatal("collect() = nil, want an error")
		}
		if !strings.Contains(err.Error(), "500") {
			t.Fatalf("collect() error = %q, want it to mention the status code", err)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, "{not json")
		}))
		t.Cleanup(srv.Close)

		_, err := newService(nil, srv.Client(), time.Second).collect(context.Background(), Target{BaseURL: srv.URL}, 0, 0)
		if err == nil || !strings.Contains(err.Error(), "decode connections") {
			t.Fatalf("collect() error = %v, want a decode failure", err)
		}
	})

	t.Run("no endpoint", func(t *testing.T) {
		_, err := newService(nil, http.DefaultClient, time.Second).collect(context.Background(), Target{}, 0, 0)
		if err == nil || !strings.Contains(err.Error(), "no control endpoint") {
			t.Fatalf("collect() error = %v, want the missing-endpoint error", err)
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}))
		t.Cleanup(srv.Close)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		if _, err := newService(nil, srv.Client(), time.Second).collect(ctx, Target{BaseURL: srv.URL}, 0, 0); err == nil {
			t.Fatal("collect() = nil, want a transport error for the cancelled request")
		}
	})
}

func TestServicePublishesSnapshotsWhileRunning(t *testing.T) {
	payload := connectionsPayload{
		UploadTotal:   300,
		DownloadTotal: 400,
		Connections: []connectionPayload{
			makeConnection("c1", []string{"proxy"}, 100, 200, "example.com", "", "tcp", "MATCH"),
			makeConnection("c2", []string{"direct"}, 5, 5, "local", "", "udp", "GeoIP,CN"),
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	authSeen := make(chan string, 4)
	mux := http.NewServeMux()
	mux.HandleFunc("/connections", func(w http.ResponseWriter, r *http.Request) {
		select {
		case authSeen <- r.Header.Get("Authorization"):
		default:
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
	mux.HandleFunc("/traffic", func(w http.ResponseWriter, r *http.Request) {
		flusher, _ := w.(http.Flusher)
		_, _ = io.WriteString(w, "{\"up\":1024,\"down\":2048}\n")
		if flusher != nil {
			flusher.Flush()
		}
		<-r.Context().Done()
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	emitter := &fakeEmitter{}
	svc := newService(emitter, srv.Client(), 20*time.Millisecond)
	svc.Start(context.Background(), Target{BaseURL: srv.URL, Secret: "topsecret", ProfileID: "p1", RevisionID: "r1"})
	t.Cleanup(svc.Stop)

	if !svc.Running() {
		t.Fatal("Running() = false right after Start")
	}

	sn := waitForSnapshot(t, svc, "an available snapshot with the rate stream applied", func(s Snapshot) bool {
		return s.Available && s.UploadRate == 1024 && s.DownloadRate == 2048
	})
	if sn.ProfileID != "p1" || sn.RevisionID != "r1" {
		t.Errorf("snapshot profile/revision = %q/%q, want p1/r1", sn.ProfileID, sn.RevisionID)
	}
	if sn.Connections != 2 || sn.UploadTotal != 300 || sn.DownloadTotal != 400 {
		t.Errorf("snapshot = %d connections, %d/%d bytes, want 2 and 300/400", sn.Connections, sn.UploadTotal, sn.DownloadTotal)
	}

	emitted := emitter.snapshots(t)
	if len(emitted) == 0 {
		t.Fatal("no traffic:snapshot event was emitted")
	}
	last := emitted[len(emitted)-1]
	if !last.Available || last.UploadTotal != 300 {
		t.Errorf("last emitted snapshot = %+v, want an available one carrying the payload", last)
	}

	select {
	case got := <-authSeen:
		if got != "Bearer topsecret" {
			t.Errorf("Authorization sent to /connections = %q, want %q", got, "Bearer topsecret")
		}
	case <-time.After(time.Second):
		t.Fatal("the collector never called /connections")
	}

	svc.Stop()
	if svc.Running() {
		t.Error("Running() = true after Stop")
	}
	after := svc.Latest()
	if after.Available || after.Connections != 0 || after.UploadTotal != 0 || after.ProfileID != "" {
		t.Errorf("Latest() after Stop = %+v, want the reset snapshot", after)
	}
	if after.Timestamp.IsZero() {
		t.Error("Latest() after Stop must still carry a timestamp")
	}
}

func TestServiceReportsAnUnavailableSnapshotWhenTheEndpointFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)

	emitter := &fakeEmitter{}
	svc := newService(emitter, srv.Client(), 20*time.Millisecond)
	defer svc.Stop()

	svc.Start(context.Background(), Target{BaseURL: srv.URL, ProfileID: "p1", RevisionID: "r1"})
	sn := waitForSnapshot(t, svc, "an unavailable snapshot", func(s Snapshot) bool {
		return !s.Available && s.Error != ""
	})
	if !strings.Contains(sn.Error, "502") {
		t.Errorf("snapshot error = %q, want the endpoint status", sn.Error)
	}
	if sn.UploadTotal != 0 || sn.Connections != 0 {
		t.Errorf("unavailable snapshot carries numbers: %+v", sn)
	}
	if sn.ProfileID != "p1" || sn.RevisionID != "r1" {
		t.Errorf("unavailable snapshot profile/revision = %q/%q, want p1/r1", sn.ProfileID, sn.RevisionID)
	}
	emitted := emitter.snapshots(t)
	if len(emitted) == 0 {
		t.Fatal("no traffic:snapshot event was emitted for the failure")
	}
	if emitted[len(emitted)-1].Available {
		t.Error("the emitted failure snapshot reported Available = true")
	}
}

func TestServiceSameTargetKeepsTheSameCollector(t *testing.T) {
	srv := connectionsServer(t, connectionsPayload{Connections: []connectionPayload{
		makeConnection("c1", []string{"proxy"}, 1, 1, "h", "", "tcp", ""),
	}})
	svc := newService(nil, srv.Client(), 20*time.Millisecond)
	defer svc.Stop()

	target := Target{BaseURL: srv.URL, Secret: "s", ProfileID: "p1", RevisionID: "r1"}
	svc.Start(context.Background(), target)
	waitForSnapshot(t, svc, "the first snapshot", func(s Snapshot) bool { return s.Available })

	svc.mu.RLock()
	first := svc.done
	svc.mu.RUnlock()

	svc.Start(context.Background(), target)
	svc.mu.RLock()
	second := svc.done
	svc.mu.RUnlock()

	if first != second {
		t.Error("Start with an identical target restarted the collector; it must be a no-op")
	}

	other := target
	other.ProfileID = "p2"
	svc.Start(context.Background(), other)
	svc.mu.RLock()
	third := svc.done
	svc.mu.RUnlock()
	if third == second {
		t.Error("Start with a different target reused the old collector")
	}
	if got := waitForSnapshot(t, svc, "the snapshot of the replaced collector", func(s Snapshot) bool {
		return s.ProfileID == "p2"
	}); got.RevisionID != "r1" {
		t.Errorf("replaced collector revision = %q, want r1", got.RevisionID)
	}
}

func TestServiceStartWithABlankTargetStopsAnExistingCollector(t *testing.T) {
	srv := connectionsServer(t, connectionsPayload{Connections: []connectionPayload{
		makeConnection("c1", []string{"proxy"}, 1, 1, "h", "", "tcp", ""),
	}})
	emitter := &fakeEmitter{}
	svc := newService(emitter, srv.Client(), 20*time.Millisecond)
	defer svc.Stop()

	svc.Start(context.Background(), Target{BaseURL: srv.URL})
	waitForSnapshot(t, svc, "the first snapshot", func(s Snapshot) bool { return s.Available })

	svc.Start(context.Background(), Target{BaseURL: "   "})
	if svc.Running() {
		t.Error("Running() = true after Start with a blank target")
	}
	if svc.Latest().Available {
		t.Error("Latest() still reports an available snapshot after the collector was stopped")
	}

	// A stopped collector must go quiet: the poller is gone, not merely
	// reporting stale numbers.
	published := emitter.count()
	time.Sleep(5 * 20 * time.Millisecond)
	if after := emitter.count(); after != published {
		t.Errorf("%d snapshots were published after the collector stopped, want none", after-published)
	}
}

func TestConsumeRatesStreamsUpdatesAndSkipsGarbage(t *testing.T) {
	authSeen := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/traffic" {
			http.NotFound(w, r)
			return
		}
		select {
		case authSeen <- r.Header.Get("Authorization"):
		default:
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = io.WriteString(w, "{\"up\":1,\"down\":2}\n")
		_, _ = io.WriteString(w, "this is not json\n")
		_, _ = io.WriteString(w, "\n")
		_, _ = io.WriteString(w, "{\"up\":3,\"down\":4}\n")
		_, _ = io.WriteString(w, "{\"up\":5,\"down\":6}\n")
	}))
	t.Cleanup(srv.Close)

	svc := newService(nil, srv.Client(), time.Second)
	out := make(chan rateUpdate, 8)
	svc.consumeRates(context.Background(), Target{BaseURL: srv.URL, Secret: "sk"}, out)

	var got []rateUpdate
	for update := range out {
		got = append(got, update)
	}
	want := []rateUpdate{{Up: 1, Down: 2}, {Up: 3, Down: 4}, {Up: 5, Down: 6}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rate updates = %+v, want %+v", got, want)
	}
	if auth := <-authSeen; auth != "Bearer sk" {
		t.Fatalf("Authorization = %q, want %q", auth, "Bearer sk")
	}
}

func TestConsumeRatesStopsOnANonOKAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	svc := newService(nil, srv.Client(), time.Second)
	out := make(chan rateUpdate, 4)

	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.consumeRates(context.Background(), Target{BaseURL: srv.URL}, out)
	}()

	select {
	case _, ok := <-out:
		if ok {
			t.Fatal("a rate update arrived from a non-OK answer")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("consumeRates did not give up on a non-OK answer")
	}
	<-done
}

func TestConsumeRatesStopsWithTheContext(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, _ := w.(http.Flusher)
		_, _ = io.WriteString(w, "{\"up\":1,\"down\":1}\n")
		if flusher != nil {
			flusher.Flush()
		}
		close(started)
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	svc := newService(nil, srv.Client(), time.Second)
	out := make(chan rateUpdate, 4)
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.consumeRates(ctx, Target{BaseURL: srv.URL}, out)
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the rate stream never started")
	}
	if update := <-out; update.Up != 1 || update.Down != 1 {
		t.Fatalf("first update = %+v, want 1/1", update)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("consumeRates outlived its context")
	}
}

func TestNewAppliesDefaults(t *testing.T) {
	svc := New(Deps{})
	if svc.deps.Interval != eventTick {
		t.Errorf("Interval = %v, want %v", svc.deps.Interval, eventTick)
	}
	if svc.deps.HTTPClient == nil {
		t.Error("HTTPClient = nil, want a default client")
	}
	if svc.logger == nil {
		t.Error("logger = nil, want a default logger")
	}
	if svc.Running() {
		t.Error("a fresh collector reports Running() = true")
	}
	if svc.deps.Interval = time.Millisecond; svc.deps.Interval != time.Millisecond {
		t.Error("Interval is not honoured")
	}

	// publish must survive a missing emitter.
	svc.publish(Snapshot{Available: true, Connections: 1})
	if got := svc.Latest(); !got.Available || got.Connections != 1 {
		t.Errorf("Latest() = %+v, want the published snapshot", got)
	}
	if svc.Stop(); svc.Running() {
		t.Error("Stop on a collector that never started left Running() true")
	}
}

func TestSnapshotJSONContract(t *testing.T) {
	sn := Snapshot{
		Timestamp:     time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC),
		Connections:   3,
		UploadTotal:   1000,
		DownloadTotal: 2000,
		UploadRate:    10,
		DownloadRate:  20,
		ByOutbound:    []OutboundTraffic{{Outbound: "direct", Upload: 1, Download: 2, Connections: 3}},
		Active:        []Connection{{ID: "c1", Host: "example.com", Outbound: "direct"}},
		Available:     true,
	}
	raw, err := json.Marshal(sn)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	for _, key := range []string{
		"timestamp", "connections", "uploadTotal", "downloadTotal",
		"uploadRate", "downloadRate", "byOutbound", "active", "available",
	} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("snapshot JSON is missing %q", key)
		}
	}
	for _, key := range []string{"profileId", "revisionId", "error"} {
		if _, ok := decoded[key]; ok {
			t.Errorf("snapshot JSON contains %q although the value is empty", key)
		}
	}

	full := sn
	full.ProfileID = "p1"
	full.RevisionID = "r1"
	full.Error = "boom"
	raw, err = json.Marshal(full)
	if err != nil {
		t.Fatalf("marshal full snapshot: %v", err)
	}
	decoded = nil
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal full snapshot: %v", err)
	}
	for _, key := range []string{"profileId", "revisionId", "error"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("full snapshot JSON is missing %q", key)
		}
	}

	// The connection and outbound shapes are part of the same contract.
	raw, err = json.Marshal(sn.Active[0])
	if err != nil {
		t.Fatalf("marshal connection: %v", err)
	}
	decoded = nil
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal connection: %v", err)
	}
	for _, key := range []string{"id", "upload", "download"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("connection JSON is missing %q", key)
		}
	}
}
