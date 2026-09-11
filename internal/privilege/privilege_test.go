package privilege

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// completeRequest is a valid request used as the base for the negative cases.
func completeRequest() Request {
	return Request{
		BinaryPath: "/data/bin/sing-box",
		ConfigPath: "/data/config/active.json",
		WorkDir:    "/data/runtime",
		LogPath:    "/data/log/sing-box.log",
		PIDPath:    "/data/runtime/sing-box.pid",
		StatusPath: "/data/runtime/sing-box.status",
		Elevate:    true,
		Reason:     "TUN device required",
	}
}

func TestValidateAcceptsCompleteRequests(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Request)
	}{
		{name: "fully populated", mutate: func(*Request) {}},
		{name: "without a working directory", mutate: func(r *Request) { r.WorkDir = "" }},
		{name: "without elevation", mutate: func(r *Request) { r.Elevate = false }},
		{name: "without a reason", mutate: func(r *Request) { r.Reason = "" }},
		{name: "without elevation or reason", mutate: func(r *Request) {
			r.Elevate = false
			r.Reason = ""
			r.WorkDir = ""
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := completeRequest()
			tc.mutate(&req)
			if err := req.Validate(); err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestValidateRequiresEveryMandatoryPath(t *testing.T) {
	tests := []struct {
		field string
		clear func(*Request)
	}{
		{field: "binaryPath", clear: func(r *Request) { r.BinaryPath = "" }},
		{field: "configPath", clear: func(r *Request) { r.ConfigPath = "" }},
		{field: "logPath", clear: func(r *Request) { r.LogPath = "" }},
		{field: "pidPath", clear: func(r *Request) { r.PIDPath = "" }},
		{field: "statusPath", clear: func(r *Request) { r.StatusPath = "" }},
	}

	for _, tc := range tests {
		t.Run(tc.field, func(t *testing.T) {
			req := completeRequest()
			tc.clear(&req)
			err := req.Validate()
			if err == nil {
				t.Fatalf("Validate() = nil with %s cleared, want a refusal", tc.field)
			}
			want := "privilege: " + tc.field + " is required"
			if err.Error() != want {
				t.Fatalf("Validate() = %q, want %q", err, want)
			}
		})
	}
}

func TestValidateRefusesTheZeroRequest(t *testing.T) {
	err := Request{}.Validate()
	if err == nil {
		t.Fatal("Validate() = nil for the zero request, want a refusal")
	}
	if !strings.HasPrefix(err.Error(), "privilege: ") || !strings.HasSuffix(err.Error(), " is required") {
		t.Fatalf("Validate() = %q, want a privilege requirement error", err)
	}
}

func TestValidateAcceptsTheZeroWorkDirAndDoesNotCheckContainment(t *testing.T) {
	// Validate is the shared presence check; containment (absolute paths inside
	// the app data directory, no traversal) is enforced by the platform runner
	// (internal/platform/privrun.newSpec) and re-checked by the privileged
	// helper. Pinning the gap here keeps the two layers from being confused.
	tests := []struct {
		name  string
		value string
	}{
		{name: "relative path", value: "config/active.json"},
		{name: "parent traversal", value: "../../etc/passwd"},
		{name: "absolute outside the data dir", value: "/etc/passwd"},
		{name: "windows style", value: `C:\Windows\config.json`},
		{name: "whitespace only", value: " "},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := completeRequest()
			req.ConfigPath = tc.value
			if err := req.Validate(); err != nil {
				t.Fatalf("Validate() = %v for configPath %q, want nil: containment belongs to the runner", err, tc.value)
			}
		})
	}
}

func TestValidateReportsOnlyTheFirstMissingFieldWhenSeveralAreEmpty(t *testing.T) {
	// The check iterates a map, so the reported field is unspecified; what must
	// hold is that it always reports exactly one of the mandatory fields and
	// never silently accepts the request.
	allEmpty := []string{"binaryPath", "configPath", "logPath", "pidPath", "statusPath"}
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		err := Request{}.Validate()
		if err == nil {
			t.Fatal("Validate() = nil, want a refusal")
		}
		message := err.Error()
		if !strings.HasPrefix(message, "privilege: ") || !strings.HasSuffix(message, " is required") {
			t.Fatalf("Validate() = %q, want a privilege requirement error", message)
		}
		field := strings.TrimSuffix(strings.TrimPrefix(message, "privilege: "), " is required")
		if !contains(allEmpty, field) {
			t.Fatalf("Validate() named %q, want one of %v", field, allEmpty)
		}
		seen[field] = true
	}
	if len(seen) == 0 {
		t.Fatal("no field was reported")
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestRequestJSONContract(t *testing.T) {
	req := completeRequest()
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	for _, key := range []string{"binaryPath", "configPath", "workDir", "logPath", "pidPath", "statusPath", "elevate", "reason"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("request JSON is missing %q", key)
		}
	}
	if len(fields) != 8 {
		t.Errorf("request JSON has %d fields, want 8: %v", len(fields), raw)
	}

	var decoded Request
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if decoded != req {
		t.Fatalf("round-trip = %+v, want %+v", decoded, req)
	}

	// Unknown fields must not break decoding; missing fields become empty.
	var partial Request
	if err := json.Unmarshal([]byte(`{"binaryPath":"/x","elevate":false,"futureField":42}`), &partial); err != nil {
		t.Fatalf("unmarshal partial request: %v", err)
	}
	if partial.BinaryPath != "/x" || partial.ConfigPath != "" || partial.Elevate {
		t.Fatalf("partial request = %+v, want only binaryPath set", partial)
	}
	if err := partial.Validate(); err == nil {
		t.Fatal("a request decoded from partial JSON must not validate")
	}
}

func TestElevationSentinelsAreDistinctAndWrappable(t *testing.T) {
	if ErrCancelled == nil || ErrUnsupported == nil {
		t.Fatal("the sentinel errors must not be nil")
	}
	if errors.Is(ErrCancelled, ErrUnsupported) || errors.Is(ErrUnsupported, ErrCancelled) {
		t.Fatal("ErrCancelled and ErrUnsupported must stay distinguishable")
	}
	if !strings.Contains(ErrCancelled.Error(), "cancel") {
		t.Errorf("ErrCancelled = %q, want a cancellation message", ErrCancelled)
	}
	if !strings.Contains(ErrUnsupported.Error(), "not supported") {
		t.Errorf("ErrUnsupported = %q, want an unsupported-platform message", ErrUnsupported)
	}

	wrapped := fmt.Errorf("privilege.RunnerStart: %w", ErrCancelled)
	if !errors.Is(wrapped, ErrCancelled) {
		t.Error("ErrCancelled must survive wrapping")
	}
	if errors.Is(wrapped, ErrUnsupported) {
		t.Error("a wrapped ErrCancelled must not match ErrUnsupported")
	}
}

// fakeProcess is a supervised process that stays alive until it is terminated.
type fakeProcess struct {
	mu        sync.Mutex
	pid       int
	elevated  bool
	logs      *strings.Reader
	logsOpen  bool
	code      int
	waited    chan struct{}
	exited    bool
	terminate int
	kill      int
}

func newFakeProcess(pid int, elevated bool) *fakeProcess {
	return &fakeProcess{
		pid:      pid,
		elevated: elevated,
		logs:     strings.NewReader("sing-box serving\n"),
		logsOpen: true,
		waited:   make(chan struct{}),
	}
}

func (p *fakeProcess) PID() int       { return p.pid }
func (p *fakeProcess) Elevated() bool { return p.elevated }

func (p *fakeProcess) Logs() io.ReadCloser { return io.NopCloser(p.logs) }

func (p *fakeProcess) Wait() (int, error) {
	<-p.waited
	p.mu.Lock()
	defer p.mu.Unlock()
	p.exited = true
	return p.code, nil
}

func (p *fakeProcess) Terminate() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.terminate++
	select {
	case <-p.waited:
	default:
		close(p.waited)
	}
	return nil
}

func (p *fakeProcess) Kill() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.kill++
	select {
	case <-p.waited:
	default:
		close(p.waited)
	}
	return nil
}

func (p *fakeProcess) Exited() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exited
}

// fakeRunner records what the supervisor asked for.
type fakeRunner struct {
	supported bool
	reason    string
	startErr  error
	process   *fakeProcess

	mu       sync.Mutex
	started  []Request
	contexts []context.Context
}

func (r *fakeRunner) Start(ctx context.Context, req Request) (Process, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.started = append(r.started, req)
	r.contexts = append(r.contexts, ctx)
	if r.startErr != nil {
		return nil, r.startErr
	}
	if r.process == nil {
		return nil, ErrUnsupported
	}
	return r.process, nil
}

func (r *fakeRunner) Supported() (bool, string) { return r.supported, r.reason }

func (r *fakeRunner) requests() []Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Request(nil), r.started...)
}

// Both interfaces are consumed by the runtime supervisor, the desktop facade and
// the platform packages; the compile-time assertions keep the shapes stable.
var (
	_ Process = (*fakeProcess)(nil)
	_ Runner  = (*fakeRunner)(nil)
)

func TestProcessContract(t *testing.T) {
	proc := newFakeProcess(4242, true)
	if got := proc.PID(); got != 4242 {
		t.Errorf("PID() = %d, want 4242", got)
	}
	if !proc.Elevated() {
		t.Error("Elevated() = false, want true")
	}
	if proc.Exited() {
		t.Error("Exited() = true before the process was stopped")
	}

	logs := proc.Logs()
	defer logs.Close()
	raw, err := io.ReadAll(logs)
	if err != nil {
		t.Fatalf("read logs: %v", err)
	}
	if !strings.Contains(string(raw), "sing-box serving") {
		t.Errorf("logs = %q, want the child output", raw)
	}

	finished := make(chan struct{})
	var code int
	go func() {
		defer close(finished)
		code, _ = proc.Wait()
	}()

	select {
	case <-finished:
		t.Fatal("Wait returned before the process was terminated")
	case <-time.After(50 * time.Millisecond):
	}

	if err := proc.Terminate(); err != nil {
		t.Fatalf("Terminate() = %v, want nil", err)
	}
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return after Terminate")
	}
	if !proc.Exited() {
		t.Error("Exited() = false after Wait returned")
	}
	if code != 0 {
		t.Errorf("Wait() code = %d, want 0", code)
	}
	if proc.terminate != 1 || proc.kill != 0 {
		t.Errorf("terminate/kill calls = %d/%d, want 1/0", proc.terminate, proc.kill)
	}
}

func TestProcessKillAlsoReleasesWait(t *testing.T) {
	proc := newFakeProcess(7, false)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_, _ = proc.Wait()
	}()
	if err := proc.Kill(); err != nil {
		t.Fatalf("Kill() = %v, want nil", err)
	}
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("Kill did not release Wait")
	}
	if proc.kill != 1 {
		t.Errorf("Kill calls = %d, want 1", proc.kill)
	}
}

func TestRunnerStartPassesTheRequestThroughUnchanged(t *testing.T) {
	proc := newFakeProcess(99, true)
	runner := &fakeRunner{supported: true, process: proc}
	req := completeRequest()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got, err := runner.Start(ctx, req)
	if err != nil {
		t.Fatalf("Start() = %v, want nil", err)
	}
	if got != Process(proc) {
		t.Fatalf("Start() returned %T, want the injected process", got)
	}
	requests := runner.requests()
	if len(requests) != 1 || requests[0] != req {
		t.Fatalf("Start() saw %+v, want the request unchanged", requests)
	}
	if runner.contexts[0] != ctx {
		t.Error("Start() did not forward the caller context")
	}

	// Start must not block until the child exits: the process is alive here and
	// only the caller decides when to stop it.
	if proc.Exited() {
		t.Error("the process already exited, so Start blocked until exit")
	}
	if err := proc.Terminate(); err != nil {
		t.Fatalf("Terminate() = %v, want nil", err)
	}
}

func TestRunnerRefusalPaths(t *testing.T) {
	tests := []struct {
		name      string
		runner    *fakeRunner
		wantErr   error
		wantNoPid bool
	}{
		{
			name:      "unsupported platform",
			runner:    &fakeRunner{supported: false, reason: "no pkexec on this system", startErr: ErrUnsupported},
			wantErr:   ErrUnsupported,
			wantNoPid: true,
		},
		{
			name:      "user dismissed the prompt",
			runner:    &fakeRunner{supported: true, startErr: ErrCancelled},
			wantErr:   ErrCancelled,
			wantNoPid: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			proc, err := tc.runner.Start(context.Background(), completeRequest())
			if err == nil {
				t.Fatalf("Start() = %v, want %v", proc, tc.wantErr)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Start() error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantNoPid && proc != nil {
				t.Fatalf("Start() returned a process (%v) alongside the refusal", proc)
			}
			if supported, reason := tc.runner.Supported(); supported && reason != "" {
				t.Errorf("Supported() = (%v, %q), want a consistent answer", supported, reason)
			}
		})
	}
}

func TestRunnerReportsWhyEscalationIsUnavailable(t *testing.T) {
	runner := &fakeRunner{supported: false, reason: "no pkexec on this system"}
	supported, reason := runner.Supported()
	if supported {
		t.Fatal("Supported() = true, want false")
	}
	if strings.TrimSpace(reason) == "" {
		t.Fatal("Supported() returned an empty reason for an unsupported platform")
	}
}
