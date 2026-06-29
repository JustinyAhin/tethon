package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"tethon/internal/protocol"
)

func TestClaudeStartMapsAssistantOutput(t *testing.T) {
	runner := &fakeCommandRunner{outputs: []string{
		`{"type":"system","subtype":"init","session_id":"claude_1"}` + "\n" +
			`{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]},"session_id":"claude_1"}` + "\n" +
			`{"type":"result","is_error":false,"session_id":"claude_1","result":"hello"}` + "\n",
	}}
	sink := newMemorySink()
	adapter := NewClaude(ClaudeConfig{Binary: "claude", Runner: runner})

	_, err := adapter.Start(context.Background(), StartRequest{
		Session: protocol.Session{ID: "ses_1", Repo: "."},
		Create:  protocol.CreateSessionRequest{Repo: ".", Agent: "claude-cli", Prompt: "say hello"},
	}, sink)
	if err != nil {
		t.Fatal(err)
	}

	event := sink.waitForType(t, protocol.EventOutput)
	var payload protocol.OutputPayload
	decodePayload(t, event, &payload)
	if payload.Text != "hello" {
		t.Fatalf("output text = %q, want hello", payload.Text)
	}

	specs := runner.specs()
	if len(specs) != 1 {
		t.Fatalf("expected 1 command, got %d", len(specs))
	}
	assertArgsContain(t, specs[0].Args, "--permission-mode", "default")
	assertArgsContain(t, specs[0].Args, "--settings")
	if got := specs[0].Args[len(specs[0].Args)-1]; got != "say hello" {
		t.Fatalf("last arg = %q, want prompt", got)
	}
}

func TestClaudeStartPassesExplicitPermissionMode(t *testing.T) {
	runner := &fakeCommandRunner{outputs: []string{
		`{"type":"result","is_error":false,"session_id":"claude_1"}` + "\n",
	}}
	adapter := NewClaude(ClaudeConfig{Binary: "claude", Runner: runner})

	_, err := adapter.Start(context.Background(), StartRequest{
		Session: protocol.Session{ID: "ses_1", Repo: "."},
		Create: protocol.CreateSessionRequest{
			Repo:           ".",
			Agent:          "claude-cli",
			Prompt:         "hi",
			PermissionMode: "plan",
		},
	}, newMemorySink())
	if err != nil {
		t.Fatal(err)
	}

	specs := runner.specs()
	if len(specs) != 1 {
		t.Fatalf("expected 1 command, got %d", len(specs))
	}
	got := strings.Join(specs[0].Args, " ")
	if !strings.Contains(got, "--permission-mode plan") {
		t.Fatalf("args = %#v, want permission mode plan", specs[0].Args)
	}
}

func TestClaudeStartConfiguresPreToolUseDeferHook(t *testing.T) {
	runner := &fakeCommandRunner{outputs: []string{
		`{"type":"result","is_error":false,"session_id":"claude_1"}` + "\n",
	}}
	adapter := NewClaude(ClaudeConfig{Binary: "claude", Runner: runner, HookHelperBinary: "/tmp/tethon-hostd"})

	_, err := adapter.Start(context.Background(), StartRequest{
		Session: protocol.Session{ID: "ses_1", Repo: "."},
		Create:  protocol.CreateSessionRequest{Repo: ".", Agent: "claude-cli", Prompt: "hi"},
	}, newMemorySink())
	if err != nil {
		t.Fatal(err)
	}

	specs := runner.specs()
	settings := argAfter(t, specs[0].Args, "--settings")
	if !strings.Contains(settings, `"PreToolUse"`) ||
		!strings.Contains(settings, `TETHON_CLAUDE_HOOK_MODE='defer'`) ||
		!strings.Contains(settings, `/tmp/tethon-hostd`) {
		t.Fatalf("settings = %s", settings)
	}
}

func TestClaudeStartPassesExtraEnvToCommand(t *testing.T) {
	runner := &fakeCommandRunner{outputs: []string{
		`{"type":"result","is_error":false,"session_id":"claude_1"}` + "\n",
	}}
	extra := []string{"GOCACHE=/cache/go-build", "GOMODCACHE=/cache/go-mod"}
	adapter := NewClaude(ClaudeConfig{Binary: "claude", Runner: runner, ExtraEnv: extra})

	_, err := adapter.Start(context.Background(), StartRequest{
		Session: protocol.Session{ID: "ses_1", Repo: "."},
		Create:  protocol.CreateSessionRequest{Repo: ".", Agent: "claude-cli", Prompt: "hi"},
	}, newMemorySink())
	if err != nil {
		t.Fatal(err)
	}

	specs := runner.specs()
	if len(specs) != 1 {
		t.Fatalf("expected 1 command, got %d", len(specs))
	}
	if strings.Join(specs[0].Env, "\x00") != strings.Join(extra, "\x00") {
		t.Fatalf("command env = %#v, want %#v", specs[0].Env, extra)
	}
}

func TestMergeEnvOverridesInheritedKeys(t *testing.T) {
	base := []string{"PATH=/usr/bin", "GOCACHE=/old", "HOME=/home/me"}
	override := []string{"GOCACHE=/shared/go-build", "GOMODCACHE=/shared/go-mod"}
	merged := mergeEnv(base, override)

	got := map[string]int{}
	for _, kv := range merged {
		key, _, _ := strings.Cut(kv, "=")
		got[key]++
	}
	for _, key := range []string{"GOCACHE", "GOMODCACHE", "PATH", "HOME"} {
		if got[key] != 1 {
			t.Fatalf("key %q appears %d times, want 1: %#v", key, got[key], merged)
		}
	}
	// The override value must be the one that survives.
	last := map[string]string{}
	for _, kv := range merged {
		key, val, _ := strings.Cut(kv, "=")
		last[key] = val
	}
	if last["GOCACHE"] != "/shared/go-build" {
		t.Fatalf("GOCACHE = %q, want /shared/go-build", last["GOCACHE"])
	}
}

func TestClaudeInputResumesCapturedSession(t *testing.T) {
	runner := &fakeCommandRunner{outputs: []string{
		`{"type":"system","subtype":"init","session_id":"claude_1"}` + "\n" +
			`{"type":"result","is_error":false,"session_id":"claude_1"}` + "\n",
		`{"type":"assistant","message":{"content":[{"type":"text","text":"second"}]},"session_id":"claude_1"}` + "\n" +
			`{"type":"result","is_error":false,"session_id":"claude_1"}` + "\n",
	}}
	sink := newMemorySink()
	adapter := NewClaude(ClaudeConfig{Binary: "claude", Runner: runner})

	running, err := adapter.Start(context.Background(), StartRequest{
		Session: protocol.Session{ID: "ses_1", Repo: "."},
		Create:  protocol.CreateSessionRequest{Repo: ".", Agent: "claude-cli", Prompt: "first"},
	}, sink)
	if err != nil {
		t.Fatal(err)
	}
	runner.waitForSpecs(t, 1)
	sink.waitForInactive(t, running)

	if _, err := running.Input(context.Background(), "next"); err != nil {
		t.Fatal(err)
	}
	sink.waitForType(t, protocol.EventOutput)

	specs := runner.specs()
	if len(specs) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(specs))
	}
	got := strings.Join(specs[1].Args, " ")
	if !strings.Contains(got, "--resume claude_1") {
		t.Fatalf("second command did not resume captured session: %#v", specs[1].Args)
	}
	if !strings.Contains(got, "--permission-mode default") {
		t.Fatalf("second command did not include permission mode: %#v", specs[1].Args)
	}
}

func TestClaudeDeferredToolMapsToApprovalAndResolutionResumes(t *testing.T) {
	runner := &fakeCommandRunner{outputs: []string{
		`{"type":"system","subtype":"init","session_id":"claude_1"}` + "\n" +
			`{"type":"result","is_error":false,"stop_reason":"tool_deferred","session_id":"claude_1","deferred_tool_use":{"id":"tool_1","name":"Bash","input":{"command":"git status"}}}` + "\n",
		`{"type":"assistant","message":{"content":[{"type":"text","text":"continued"}]},"session_id":"claude_1"}` + "\n" +
			`{"type":"result","is_error":false,"session_id":"claude_1"}` + "\n",
	}}
	sink := newMemorySink()
	adapter := NewClaude(ClaudeConfig{Binary: "claude", Runner: runner})

	running, err := adapter.Start(context.Background(), StartRequest{
		Session: protocol.Session{ID: "ses_1", Repo: "."},
		Create:  protocol.CreateSessionRequest{Repo: ".", Agent: "claude-cli", Prompt: "run status"},
	}, sink)
	if err != nil {
		t.Fatal(err)
	}

	event := sink.waitForType(t, protocol.EventAwaitingApproval)
	var payload protocol.ApprovalPayload
	decodePayload(t, event, &payload)
	if payload.RequestID != "tool_1" || payload.ToolName != "Bash" {
		t.Fatalf("approval payload = %+v", payload)
	}
	if sink.status() != "awaiting_approval" {
		t.Fatalf("status = %q, want awaiting_approval", sink.status())
	}

	if _, err := running.ResolveApproval(context.Background(), "tool_1", "approve", ""); err != nil {
		t.Fatal(err)
	}
	sink.waitForType(t, protocol.EventApprovalResolved)
	sink.waitForText(t, "continued")

	specs := runner.specs()
	if len(specs) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(specs))
	}
	if !strings.Contains(strings.Join(specs[1].Args, " "), "--resume claude_1") {
		t.Fatalf("approval command did not resume captured session: %#v", specs[1].Args)
	}
	if !strings.Contains(strings.Join(specs[1].Args, " "), "--permission-mode default") {
		t.Fatalf("approval command did not include permission mode: %#v", specs[1].Args)
	}
	if got := specs[1].Args[len(specs[1].Args)-1]; got != "claude_1" {
		t.Fatalf("approval command should end at resumed session id, got last arg %q in %#v", got, specs[1].Args)
	}
	settings := argAfter(t, specs[1].Args, "--settings")
	if !strings.Contains(settings, `TETHON_CLAUDE_HOOK_MODE='approve'`) ||
		!strings.Contains(settings, `TETHON_CLAUDE_HOOK_REQUEST_ID='tool_1'`) {
		t.Fatalf("approval settings = %s", settings)
	}
}

func TestClaudeResultUsageEmitsUsageEventAndUpdatesModel(t *testing.T) {
	runner := &fakeCommandRunner{outputs: []string{
		`{"type":"system","subtype":"init","session_id":"claude_1"}` + "\n" +
			`{"type":"result","is_error":false,"session_id":"claude_1","model":"claude-sonnet-4-5-20250929","usage":{"input_tokens":1000,"output_tokens":2000,"cache_read_input_tokens":3000,"cache_creation_input_tokens":4000}}` + "\n",
	}}
	sink := newMemorySink()
	adapter := NewClaude(ClaudeConfig{Binary: "claude", Runner: runner})

	_, err := adapter.Start(context.Background(), StartRequest{
		Session: protocol.Session{ID: "ses_1", Repo: "."},
		Create:  protocol.CreateSessionRequest{Repo: ".", Agent: "claude-cli", Prompt: "usage"},
	}, sink)
	if err != nil {
		t.Fatal(err)
	}

	event := sink.waitForType(t, protocol.EventUsage)
	var payload protocol.UsagePayload
	decodePayload(t, event, &payload)
	if payload.Model != "claude-sonnet-4-5-20250929" || sink.model != payload.Model {
		t.Fatalf("model = payload %q sink %q", payload.Model, sink.model)
	}
	if payload.Tokens.Input != 1000 || payload.Tokens.Output != 2000 || payload.Tokens.CacheRead != 3000 || payload.Tokens.CacheWrite != 4000 {
		t.Fatalf("unexpected tokens: %+v", payload.Tokens)
	}
	if payload.Cost == nil || payload.Cost.EstimateStatus != "estimated" {
		t.Fatalf("missing estimated cost: %+v", payload.Cost)
	}
	want := 1000*3.0/1_000_000 + 2000*15.0/1_000_000 + 3000*0.30/1_000_000 + 4000*3.75/1_000_000
	if payload.Cost.TotalUSD != want {
		t.Fatalf("cost = %.12f, want %.12f", payload.Cost.TotalUSD, want)
	}
}

func TestClaudeUsageUnknownModelOmitsEstimate(t *testing.T) {
	runner := &fakeCommandRunner{outputs: []string{
		`{"type":"result","is_error":false,"session_id":"claude_1","model":"claude-unknown-1","usage":{"input_tokens":1000,"output_tokens":2000}}` + "\n",
	}}
	sink := newMemorySink()
	adapter := NewClaude(ClaudeConfig{Binary: "claude", Runner: runner})

	_, err := adapter.Start(context.Background(), StartRequest{
		Session: protocol.Session{ID: "ses_1", Repo: "."},
		Create:  protocol.CreateSessionRequest{Repo: ".", Agent: "claude-cli", Prompt: "usage"},
	}, sink)
	if err != nil {
		t.Fatal(err)
	}

	event := sink.waitForType(t, protocol.EventUsage)
	var payload protocol.UsagePayload
	decodePayload(t, event, &payload)
	if payload.Cost == nil || payload.Cost.EstimateStatus != "unknown_model" || payload.Cost.TotalUSD != 0 {
		t.Fatalf("unexpected unknown-model cost: %+v", payload.Cost)
	}
}

func TestClaudeResultWithoutUsageEmitsNoUsageEvent(t *testing.T) {
	runner := &fakeCommandRunner{outputs: []string{
		`{"type":"result","is_error":false,"session_id":"claude_1","model":"claude-sonnet-4-5-20250929"}` + "\n",
	}}
	sink := newMemorySink()
	adapter := NewClaude(ClaudeConfig{Binary: "claude", Runner: runner})

	running, err := adapter.Start(context.Background(), StartRequest{
		Session: protocol.Session{ID: "ses_1", Repo: "."},
		Create:  protocol.CreateSessionRequest{Repo: ".", Agent: "claude-cli", Prompt: "no usage"},
	}, sink)
	if err != nil {
		t.Fatal(err)
	}
	sink.waitForInactive(t, running)
	if event, ok := sink.eventOfType(protocol.EventUsage); ok {
		t.Fatalf("unexpected usage event: %+v", event)
	}
}

func TestClaudeMalformedStreamEmitsError(t *testing.T) {
	runner := &fakeCommandRunner{outputs: []string{"not json\n"}}
	sink := newMemorySink()
	adapter := NewClaude(ClaudeConfig{Binary: "claude", Runner: runner})

	running, err := adapter.Start(context.Background(), StartRequest{
		Session: protocol.Session{ID: "ses_1", Repo: "."},
		Create:  protocol.CreateSessionRequest{Repo: ".", Agent: "claude-cli", Prompt: "bad"},
	}, sink)
	if err != nil {
		t.Fatal(err)
	}

	event := sink.waitForType(t, protocol.EventError)
	var payload protocol.ErrorPayload
	decodePayload(t, event, &payload)
	if !strings.Contains(payload.Message, "parse claude stream") {
		t.Fatalf("error message = %q", payload.Message)
	}
	if !running.Terminal() {
		t.Fatal("expected malformed stream to terminate session")
	}
}

func TestClaudeLogsMalformedStreamWithoutPromptBody(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	runner := &fakeCommandRunner{outputs: []string{"not json\n"}}
	sink := newMemorySink()
	adapter := NewClaude(ClaudeConfig{Binary: "claude", Runner: runner, Logger: logger})

	_, err := adapter.Start(context.Background(), StartRequest{
		Session: protocol.Session{ID: "ses_1", Repo: "."},
		Create:  protocol.CreateSessionRequest{Repo: ".", Agent: "claude-cli", Prompt: "secret prompt"},
	}, sink)
	if err != nil {
		t.Fatal(err)
	}
	sink.waitForType(t, protocol.EventError)

	raw := logs.String()
	if !strings.Contains(raw, "agent_turn_start") || !strings.Contains(raw, "agent_stream_error") {
		t.Fatalf("expected turn start and stream error logs, got %s", raw)
	}
	if strings.Contains(raw, "secret prompt") {
		t.Fatalf("logs contain prompt body: %s", raw)
	}
	if !strings.Contains(raw, `"prompt_len":13`) {
		t.Fatalf("logs missing prompt length: %s", raw)
	}
}

func TestClaudeNonZeroExitEmitsStderrError(t *testing.T) {
	process := newBlockingProcess()
	process.stdout = strings.NewReader("")
	process.stderr = strings.NewReader("boom\n")
	process.waitErr = errors.New("exit status 1")
	process.release()
	runner := &singleProcessRunner{process: process}
	sink := newMemorySink()
	adapter := NewClaude(ClaudeConfig{Binary: "claude", Runner: runner})

	running, err := adapter.Start(context.Background(), StartRequest{
		Session: protocol.Session{ID: "ses_1", Repo: "."},
		Create:  protocol.CreateSessionRequest{Repo: ".", Agent: "claude-cli", Prompt: "fail"},
	}, sink)
	if err != nil {
		t.Fatal(err)
	}

	event := sink.waitForType(t, protocol.EventError)
	var payload protocol.ErrorPayload
	decodePayload(t, event, &payload)
	if payload.Message != "boom" {
		t.Fatalf("error message = %q, want boom", payload.Message)
	}
	if !running.Terminal() {
		t.Fatal("expected non-zero exit to terminate session")
	}
}

func TestClaudeCancelInterruptsRunningProcess(t *testing.T) {
	process := newBlockingProcess()
	process.interruptReleases = true
	runner := &singleProcessRunner{process: process}
	sink := newMemorySink()
	adapter := NewClaude(ClaudeConfig{Binary: "claude", Runner: runner, KillDelay: 20 * time.Millisecond})

	running, err := adapter.Start(context.Background(), StartRequest{
		Session: protocol.Session{ID: "ses_1", Repo: "."},
		Create:  protocol.CreateSessionRequest{Repo: ".", Agent: "claude-cli", Prompt: "wait"},
	}, sink)
	if err != nil {
		t.Fatal(err)
	}

	events, err := running.Cancel(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != protocol.EventDone {
		t.Fatalf("cancel events = %+v", events)
	}
	if process.interrupts() != 1 {
		t.Fatalf("interrupts = %d, want 1", process.interrupts())
	}
	if process.kills() != 0 {
		t.Fatalf("kills = %d, want 0", process.kills())
	}
	if !running.Terminal() {
		t.Fatal("expected cancel to terminate session")
	}
}

func TestClaudeCancelHardKillsIgnoredInterrupt(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	process := newBlockingProcess()
	process.killReleases = true
	runner := &singleProcessRunner{process: process}
	sink := newMemorySink()
	adapter := NewClaude(ClaudeConfig{Binary: "claude", Runner: runner, KillDelay: time.Millisecond, Logger: logger})

	running, err := adapter.Start(context.Background(), StartRequest{
		Session: protocol.Session{ID: "ses_1", Repo: "."},
		Create:  protocol.CreateSessionRequest{Repo: ".", Agent: "claude-cli", Prompt: "wait"},
	}, sink)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := running.Cancel(context.Background()); err != nil {
		t.Fatal(err)
	}
	if process.interrupts() != 1 {
		t.Fatalf("interrupts = %d, want 1", process.interrupts())
	}
	if process.kills() != 1 {
		t.Fatalf("kills = %d, want 1", process.kills())
	}
	raw := logs.String()
	for _, want := range []string{"agent_cancel_requested", "agent_interrupt_sent", "agent_hard_kill_sent", "agent_cancel_complete"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("logs missing %q: %s", want, raw)
		}
	}
}

func TestClaudeInputWhileRunningReturnsBusy(t *testing.T) {
	process := newBlockingProcess()
	process.interruptReleases = true
	runner := &singleProcessRunner{process: process}
	sink := newMemorySink()
	adapter := NewClaude(ClaudeConfig{Binary: "claude", Runner: runner})

	running, err := adapter.Start(context.Background(), StartRequest{
		Session: protocol.Session{ID: "ses_1", Repo: "."},
		Create:  protocol.CreateSessionRequest{Repo: ".", Agent: "claude-cli", Prompt: "wait"},
	}, sink)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := running.Input(context.Background(), "next"); !errors.Is(err, ErrBusy) {
		t.Fatalf("input error = %v, want ErrBusy", err)
	}
	if _, err := running.Cancel(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRunClaudeHookDecisions(t *testing.T) {
	cases := []struct {
		name       string
		env        map[string]string
		want       string
		wantInput  string
		wantReason string
	}{
		{name: "defer", env: map[string]string{"TETHON_CLAUDE_HOOK_MODE": "defer"}, want: "defer"},
		{
			name:      "approve matching tool",
			env:       map[string]string{"TETHON_CLAUDE_HOOK_MODE": "approve", "TETHON_CLAUDE_HOOK_REQUEST_ID": "tool_1"},
			want:      "allow",
			wantInput: `{"command":"git rm AGENTS.md"}`,
		},
		{
			name:       "deny matching tool",
			env:        map[string]string{"TETHON_CLAUDE_HOOK_MODE": "deny", "TETHON_CLAUDE_HOOK_REQUEST_ID": "tool_1", "TETHON_CLAUDE_HOOK_TEXT": "no"},
			want:       "deny",
			wantReason: "no",
		},
		{
			name: "mismatched tool stays deferred",
			env:  map[string]string{"TETHON_CLAUDE_HOOK_MODE": "approve", "TETHON_CLAUDE_HOOK_REQUEST_ID": "other"},
			want: "defer",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := strings.NewReader(`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_use_id":"tool_1","tool_input":{"command":"git rm AGENTS.md"}}`)
			var out bytes.Buffer
			if err := RunClaudeHook(in, &out, func(key string) string { return tc.env[key] }); err != nil {
				t.Fatal(err)
			}
			var got claudeHookOutput
			if err := json.Unmarshal(out.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.HookSpecificOutput.PermissionDecision != tc.want {
				t.Fatalf("decision = %q, want %q; output %s", got.HookSpecificOutput.PermissionDecision, tc.want, out.String())
			}
			if tc.wantInput != "" && string(got.HookSpecificOutput.UpdatedInput) != tc.wantInput {
				t.Fatalf("updatedInput = %s, want %s", got.HookSpecificOutput.UpdatedInput, tc.wantInput)
			}
			if got.HookSpecificOutput.PermissionDecisionReason != tc.wantReason {
				t.Fatalf("reason = %q, want %q", got.HookSpecificOutput.PermissionDecisionReason, tc.wantReason)
			}
		})
	}
}

type fakeCommandRunner struct {
	mu      sync.Mutex
	outputs []string
	calls   []CommandSpec
}

func (r *fakeCommandRunner) Start(_ context.Context, spec CommandSpec) (CommandProcess, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.outputs) == 0 {
		return nil, errors.New("unexpected command")
	}
	output := r.outputs[0]
	r.outputs = r.outputs[1:]
	r.calls = append(r.calls, spec)
	return fakeProcess{stdout: strings.NewReader(output), stderr: strings.NewReader("")}, nil
}

func (r *fakeCommandRunner) specs() []CommandSpec {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]CommandSpec, len(r.calls))
	copy(out, r.calls)
	return out
}

func (r *fakeCommandRunner) waitForSpecs(t *testing.T, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(r.specs()) >= count {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d command specs", count)
}

func assertArgsContain(t *testing.T, args []string, want ...string) {
	t.Helper()
	for i := 0; i <= len(args)-len(want); i++ {
		matched := true
		for j := range want {
			if args[i+j] != want[j] {
				matched = false
				break
			}
		}
		if matched {
			return
		}
	}
	t.Fatalf("args = %#v, want sequence %#v", args, want)
}

func argAfter(t *testing.T, args []string, name string) string {
	t.Helper()
	for i, arg := range args {
		if arg == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	t.Fatalf("args = %#v, missing %s", args, name)
	return ""
}

type fakeProcess struct {
	stdout io.Reader
	stderr io.Reader
}

func (p fakeProcess) Stdout() io.Reader {
	return p.stdout
}

func (p fakeProcess) Stderr() io.Reader {
	return p.stderr
}

func (fakeProcess) Wait() error {
	return nil
}

func (fakeProcess) Interrupt() error {
	return nil
}

func (fakeProcess) Kill() error {
	return nil
}

type singleProcessRunner struct {
	process *blockingProcess
}

func (r *singleProcessRunner) Start(context.Context, CommandSpec) (CommandProcess, error) {
	return r.process, nil
}

type blockingProcess struct {
	stdout io.Reader
	stderr io.Reader

	waitErr           error
	interruptReleases bool
	killReleases      bool

	mu             sync.Mutex
	interruptCount int
	killCount      int
	done           chan struct{}
	once           sync.Once
}

func newBlockingProcess() *blockingProcess {
	return &blockingProcess{
		stdout: strings.NewReader(""),
		stderr: strings.NewReader(""),
		done:   make(chan struct{}),
	}
}

func (p *blockingProcess) Stdout() io.Reader {
	return p.stdout
}

func (p *blockingProcess) Stderr() io.Reader {
	return p.stderr
}

func (p *blockingProcess) Wait() error {
	<-p.done
	return p.waitErr
}

func (p *blockingProcess) Interrupt() error {
	p.mu.Lock()
	p.interruptCount++
	release := p.interruptReleases
	p.mu.Unlock()
	if release {
		p.release()
	}
	return nil
}

func (p *blockingProcess) Kill() error {
	p.mu.Lock()
	p.killCount++
	release := p.killReleases
	p.mu.Unlock()
	if release {
		p.release()
	}
	return nil
}

func (p *blockingProcess) release() {
	p.once.Do(func() {
		close(p.done)
	})
}

func (p *blockingProcess) interrupts() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.interruptCount
}

func (p *blockingProcess) kills() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.killCount
}

type memorySink struct {
	mu        sync.Mutex
	events    []protocol.Event
	approvals map[string]string
	state     string
	model     string
}

func newMemorySink() *memorySink {
	return &memorySink{approvals: make(map[string]string)}
}

func (s *memorySink) AppendEvent(_ context.Context, eventType string, payload any) (protocol.Event, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return protocol.Event{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	event := protocol.Event{
		SessionID: "ses_1",
		Seq:       int64(len(s.events) + 1),
		Type:      eventType,
		Payload:   raw,
	}
	s.events = append(s.events, event)
	return event, nil
}

func (s *memorySink) CreatePendingApproval(_ context.Context, requestID string, _ any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.approvals[requestID] = "pending"
	return nil
}

func (s *memorySink) ResolveApproval(_ context.Context, requestID, action string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.approvals[requestID] != "pending" {
		return errors.New("missing pending approval")
	}
	s.approvals[requestID] = action
	return nil
}

func (s *memorySink) SetSessionStatus(_ context.Context, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = status
	return nil
}

func (s *memorySink) SetSessionModel(_ context.Context, model string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.model = model
	return nil
}

func (s *memorySink) waitForType(t *testing.T, eventType string) protocol.Event {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		for _, event := range s.events {
			if event.Type == eventType {
				s.mu.Unlock()
				return event
			}
		}
		s.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for event type %s", eventType)
	return protocol.Event{}
}

func (s *memorySink) eventOfType(eventType string) (protocol.Event, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, event := range s.events {
		if event.Type == eventType {
			return event, true
		}
	}
	return protocol.Event{}, false
}

func (s *memorySink) waitForText(t *testing.T, text string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		for _, event := range s.events {
			if event.Type != protocol.EventOutput {
				continue
			}
			var payload protocol.OutputPayload
			if err := json.Unmarshal(event.Payload, &payload); err == nil && payload.Text == text {
				s.mu.Unlock()
				return
			}
		}
		s.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for output %q", text)
}

func (s *memorySink) waitForInactive(t *testing.T, running RunningSession) {
	t.Helper()
	session, ok := running.(*claudeSession)
	if !ok {
		t.Fatal("running session is not claudeSession")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		session.mu.Lock()
		active := session.active
		session.mu.Unlock()
		if !active {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for inactive claude session")
}

func (s *memorySink) status() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func decodePayload(t *testing.T, event protocol.Event, out any) {
	t.Helper()
	if err := json.Unmarshal(event.Payload, out); err != nil {
		t.Fatal(err)
	}
}
