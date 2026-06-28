package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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
	wantArgs := []string{"-p", "--verbose", "--output-format", "stream-json", "--include-hook-events", "say hello"}
	if strings.Join(specs[0].Args, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("args = %#v, want %#v", specs[0].Args, wantArgs)
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
}

func TestClaudeMalformedStreamEmitsError(t *testing.T) {
	runner := &fakeCommandRunner{outputs: []string{"not json\n"}}
	sink := newMemorySink()
	adapter := NewClaude(ClaudeConfig{Binary: "claude", Runner: runner})

	_, err := adapter.Start(context.Background(), StartRequest{
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

type memorySink struct {
	mu        sync.Mutex
	events    []protocol.Event
	approvals map[string]string
	state     string
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
