package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"tethon/internal/protocol"
)

type ClaudeConfig struct {
	Binary               string
	AllowAnthropicAPIKey bool
	KillDelay            time.Duration
	Logger               *slog.Logger
	Runner               CommandRunner
	// ExtraEnv is added to every spawned agent process (KEY=VALUE), overriding
	// any inherited value. The host uses it to point build/dependency caches at
	// a shared directory so worktrees do not each duplicate them.
	ExtraEnv []string
}

type CommandSpec struct {
	Binary string
	Args   []string
	Dir    string
	// Env entries are added to the inherited environment, overriding inherited
	// keys. Empty means inherit the daemon environment unchanged.
	Env []string
}

type CommandRunner interface {
	Start(context.Context, CommandSpec) (CommandProcess, error)
}

type CommandProcess interface {
	Stdout() io.Reader
	Stderr() io.Reader
	Wait() error
	Interrupt() error
	Kill() error
}

type execRunner struct{}

func (execRunner) Start(ctx context.Context, spec CommandSpec) (CommandProcess, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmd := exec.Command(spec.Binary, spec.Args...)
	cmd.Dir = spec.Dir
	if len(spec.Env) > 0 {
		cmd.Env = mergeEnv(os.Environ(), spec.Env)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &execProcess{cmd: cmd, stdout: stdout, stderr: stderr}, nil
}

type execProcess struct {
	cmd    *exec.Cmd
	stdout io.Reader
	stderr io.Reader
}

func (p *execProcess) Stdout() io.Reader {
	return p.stdout
}

func (p *execProcess) Stderr() io.Reader {
	return p.stderr
}

func (p *execProcess) Wait() error {
	return p.cmd.Wait()
}

func (p *execProcess) Interrupt() error {
	if p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Signal(os.Interrupt)
}

func (p *execProcess) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}

type claudeAdapter struct {
	binary               string
	allowAnthropicAPIKey bool
	killDelay            time.Duration
	logger               *slog.Logger
	runner               CommandRunner
	extraEnv             []string
}

func NewClaude(config ClaudeConfig) Adapter {
	binary := config.Binary
	if binary == "" {
		binary = "claude"
	}
	runner := config.Runner
	if runner == nil {
		runner = execRunner{}
	}
	killDelay := config.KillDelay
	if killDelay <= 0 {
		killDelay = 5 * time.Second
	}
	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &claudeAdapter{
		binary:               binary,
		allowAnthropicAPIKey: config.AllowAnthropicAPIKey,
		killDelay:            killDelay,
		logger:               logger,
		runner:               runner,
		extraEnv:             config.ExtraEnv,
	}
}

func (*claudeAdapter) Name() string {
	return "claude-cli"
}

func (*claudeAdapter) Capabilities() Capabilities {
	return Capabilities{
		Stream:           true,
		InteractiveInput: true,
		Approvals:        true,
		Resume:           true,
	}
}

func (a *claudeAdapter) Start(ctx context.Context, req StartRequest, sink Sink) (RunningSession, error) {
	if !a.allowAnthropicAPIKey && os.Getenv("ANTHROPIC_API_KEY") != "" {
		err := errors.New("claude-cli refuses to run while ANTHROPIC_API_KEY is set")
		a.logger.Warn("agent_start_rejected", "agent", "claude-cli", "session_id", req.Session.ID, "reason", err.Error())
		return nil, err
	}
	prompt := strings.TrimSpace(req.Create.Prompt)
	if prompt == "" {
		err := errors.New("prompt is required for claude-cli sessions")
		a.logger.Warn("agent_start_rejected", "agent", "claude-cli", "session_id", req.Session.ID, "reason", err.Error())
		return nil, err
	}

	session := &claudeSession{
		adapter:   a,
		sink:      sink,
		sessionID: req.Session.ID,
		workDir:   resolveWorkDir(req),
	}
	if _, err := sink.AppendEvent(ctx, protocol.EventSessionStarted, req.Create); err != nil {
		return nil, err
	}
	if err := session.startTurn(ctx, prompt, ""); err != nil {
		return nil, err
	}
	return session, nil
}

type claudeSession struct {
	adapter *claudeAdapter
	sink    Sink

	sessionID string
	workDir   string

	mu              sync.Mutex
	active          bool
	cancelling      bool
	terminal        bool
	doneEmitted     bool
	process         CommandProcess
	turnDone        chan struct{}
	claudeSessionID string
	model           string
	pending         map[string]claudeApproval
}

type claudeApproval struct {
	RequestID string
	ToolName  string
	ToolInput json.RawMessage
}

func (s *claudeSession) Input(ctx context.Context, text string) ([]protocol.Event, error) {
	if err := s.checkCanStart(); err != nil {
		s.adapter.logger.Warn("agent_input_rejected", "agent", "claude-cli", "session_id", s.sessionID, "text_len", len(text), "reason", err.Error())
		return nil, err
	}

	event, err := s.sink.AppendEvent(ctx, protocol.EventInputReceived, protocol.InputReceivedPayload{Text: text})
	if err != nil {
		return nil, err
	}
	if err := s.startTurn(ctx, text, ""); err != nil {
		return nil, err
	}
	return []protocol.Event{event}, nil
}

func (s *claudeSession) ResolveApproval(ctx context.Context, requestID, action, text string) ([]protocol.Event, error) {
	if err := s.checkCanStart(); err != nil {
		s.adapter.logger.Warn("agent_approval_rejected", "agent", "claude-cli", "session_id", s.sessionID, "request_id", requestID, "action", action, "reason", err.Error())
		return nil, err
	}
	if err := s.sink.ResolveApproval(ctx, requestID, action); err != nil {
		return nil, err
	}
	resolved, err := s.sink.AppendEvent(ctx, protocol.EventApprovalResolved, protocol.ApprovalResolvedPayload{
		RequestID: requestID,
		Action:    action,
		Text:      text,
	})
	if err != nil {
		return nil, err
	}

	prompt := s.approvalPrompt(requestID, action, text)
	if err := s.startTurn(ctx, prompt, requestID); err != nil {
		return nil, err
	}
	return []protocol.Event{resolved}, nil
}

func (s *claudeSession) Cancel(ctx context.Context) ([]protocol.Event, error) {
	s.mu.Lock()
	if s.terminal {
		s.mu.Unlock()
		s.adapter.logger.Warn("agent_cancel_rejected", "agent", "claude-cli", "session_id", s.sessionID, "reason", ErrSessionDone.Error())
		return nil, ErrSessionDone
	}
	process := s.process
	done := s.turnDone
	active := s.active
	if active {
		s.cancelling = true
	}
	s.mu.Unlock()
	s.adapter.logger.Info("agent_cancel_requested", "agent", "claude-cli", "session_id", s.sessionID, "active", active)

	if active && process != nil {
		interruptErr := process.Interrupt()
		s.adapter.logger.Info("agent_interrupt_sent", "agent", "claude-cli", "session_id", s.sessionID, "error", errorString(interruptErr))
		select {
		case <-done:
		case <-time.After(s.adapter.killDelay):
			killErr := process.Kill()
			s.adapter.logger.Warn("agent_hard_kill_sent", "agent", "claude-cli", "session_id", s.sessionID, "error", errorString(killErr))
			select {
			case <-done:
			case <-time.After(time.Second):
			}
		}
	}
	events, err := s.markCancelled(ctx)
	if err == nil {
		s.adapter.logger.Info("agent_cancel_complete", "agent", "claude-cli", "session_id", s.sessionID)
	}
	return events, err
}

func (s *claudeSession) Terminal() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.terminal
}

func (s *claudeSession) checkCanStart() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.terminal:
		return ErrSessionDone
	case s.active || s.cancelling:
		return ErrBusy
	default:
		return nil
	}
}

func (s *claudeSession) startTurn(ctx context.Context, prompt, approvalID string) error {
	s.mu.Lock()
	if s.terminal {
		s.mu.Unlock()
		return ErrSessionDone
	}
	if s.active || s.cancelling {
		s.mu.Unlock()
		return ErrBusy
	}
	claudeSessionID := s.claudeSessionID
	runCtx := context.Background()
	s.active = true
	s.cancelling = false
	s.mu.Unlock()

	if err := s.sink.SetSessionStatus(ctx, "running"); err != nil {
		s.finishTurn()
		return err
	}

	args := []string{"-p", "--verbose", "--output-format", "stream-json", "--include-hook-events"}
	if claudeSessionID != "" {
		args = append(args, "--resume", claudeSessionID)
	}
	args = append(args, prompt)
	s.adapter.logger.Info("agent_turn_start", "agent", "claude-cli", "session_id", s.sessionID, "cwd", s.workDir, "resume", claudeSessionID != "", "prompt_len", len(prompt))

	process, err := s.adapter.runner.Start(runCtx, CommandSpec{
		Binary: s.adapter.binary,
		Args:   args,
		Dir:    s.workDir,
		Env:    s.adapter.extraEnv,
	})
	if err != nil {
		s.finishTurn()
		s.adapter.logger.Error("agent_process_start_failed", "agent", "claude-cli", "session_id", s.sessionID, "cwd", s.workDir, "error", err.Error())
		return err
	}

	done := make(chan struct{})
	s.mu.Lock()
	s.process = process
	s.turnDone = done
	s.mu.Unlock()

	go s.runTurn(runCtx, process, approvalID, done)
	return nil
}

func (s *claudeSession) finishTurn() {
	s.mu.Lock()
	s.active = false
	s.cancelling = false
	s.process = nil
	s.turnDone = nil
	s.mu.Unlock()
}

func (s *claudeSession) runTurn(ctx context.Context, process CommandProcess, approvalID string, done chan struct{}) {
	defer func() {
		s.finishTurn()
		close(done)
	}()

	var stderr bytes.Buffer
	stderrDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&stderr, process.Stderr())
		close(stderrDone)
	}()

	streamErr := s.consumeStream(ctx, process.Stdout())
	if streamErr != nil {
		if !s.isCancellingOrTerminal() {
			s.adapter.logger.Error("agent_stream_error", "agent", "claude-cli", "session_id", s.sessionID, "error", streamErr.Error())
			s.appendTerminalError(ctx, streamErr.Error())
		}
	}
	waitErr := process.Wait()
	<-stderrDone
	if waitErr != nil && !s.isCancellingOrTerminal() {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = waitErr.Error()
		}
		s.adapter.logger.Error("agent_turn_exit", "agent", "claude-cli", "session_id", s.sessionID, "success", false, "stderr_len", stderr.Len(), "error", waitErr.Error())
		s.appendTerminalError(ctx, message)
	} else if waitErr == nil && streamErr == nil {
		s.adapter.logger.Info("agent_turn_exit", "agent", "claude-cli", "session_id", s.sessionID, "success", true, "stderr_len", stderr.Len())
	}

	if approvalID != "" {
		s.mu.Lock()
		delete(s.pending, approvalID)
		s.mu.Unlock()
	}
}

func (s *claudeSession) isCancellingOrTerminal() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancelling || s.terminal
}

func (s *claudeSession) consumeStream(ctx context.Context, r io.Reader) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	emittedOutput := false
	for scanner.Scan() {
		var event claudeStreamEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return fmt.Errorf("parse claude stream: %w", err)
		}
		result, err := s.handleClaudeEvent(ctx, event, &emittedOutput)
		if err != nil {
			return err
		}
		if result == claudeTurnDeferred {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read claude stream: %w", err)
	}
	return nil
}

type claudeTurnResult int

const (
	claudeTurnContinue claudeTurnResult = iota
	claudeTurnDeferred
)

func (s *claudeSession) handleClaudeEvent(ctx context.Context, event claudeStreamEvent, emittedOutput *bool) (claudeTurnResult, error) {
	if event.SessionID != "" {
		s.setClaudeSessionID(event.SessionID)
	}
	if model := event.model(); model != "" {
		if err := s.setModel(ctx, model); err != nil {
			return claudeTurnContinue, err
		}
	}
	switch event.Type {
	case "system":
		return claudeTurnContinue, nil
	case "assistant":
		for _, text := range event.assistantText() {
			if strings.TrimSpace(text) == "" {
				continue
			}
			if _, err := s.sink.AppendEvent(ctx, protocol.EventOutput, protocol.OutputPayload{Text: text}); err != nil {
				return claudeTurnContinue, err
			}
			*emittedOutput = true
		}
	case "result":
		if event.SessionID != "" {
			s.setClaudeSessionID(event.SessionID)
		}
		if event.Usage != nil {
			if err := s.emitUsage(ctx, event.Usage); err != nil {
				return claudeTurnContinue, err
			}
		}
		if approval, ok := event.approval(); ok {
			if err := s.createApproval(ctx, approval); err != nil {
				return claudeTurnContinue, err
			}
			return claudeTurnDeferred, nil
		}
		if event.IsError {
			message := strings.TrimSpace(event.Result)
			if message == "" {
				message = "claude turn failed"
			}
			s.appendTerminalError(ctx, message)
			return claudeTurnContinue, nil
		}
		if !*emittedOutput && strings.TrimSpace(event.Result) != "" {
			if _, err := s.sink.AppendEvent(ctx, protocol.EventOutput, protocol.OutputPayload{Text: event.Result}); err != nil {
				return claudeTurnContinue, err
			}
			*emittedOutput = true
		}
	case "rate_limit_event":
		if _, err := s.sink.AppendEvent(ctx, protocol.EventOutput, protocol.OutputPayload{Text: "claude rate limit event received"}); err != nil {
			return claudeTurnContinue, err
		}
		*emittedOutput = true
	}
	return claudeTurnContinue, nil
}

func (s *claudeSession) setClaudeSessionID(id string) {
	s.mu.Lock()
	s.claudeSessionID = id
	s.mu.Unlock()
}

func (s *claudeSession) setModel(ctx context.Context, model string) error {
	s.mu.Lock()
	if s.model == model {
		s.mu.Unlock()
		return nil
	}
	s.model = model
	s.mu.Unlock()
	return s.sink.SetSessionModel(ctx, model)
}

func (s *claudeSession) currentModel() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.model
}

func (s *claudeSession) emitUsage(ctx context.Context, usage *claudeUsage) error {
	tokens := usage.tokens()
	if tokens == (protocol.UsageTokens{}) {
		return nil
	}
	model := s.currentModel()
	payload := protocol.UsagePayload{
		Agent:   "claude-cli",
		Backend: "anthropic",
		CLI:     "claude",
		Model:   model,
		Tokens:  tokens,
		Cost:    estimateAnthropicCost(model, tokens),
		Raw:     &protocol.UsageRawInfo{ProviderUsagePresent: true},
	}
	_, err := s.sink.AppendEvent(ctx, protocol.EventUsage, payload)
	return err
}

func (s *claudeSession) createApproval(ctx context.Context, approval claudeApproval) error {
	if approval.RequestID == "" {
		approval.RequestID = "claude_permission"
	}
	if len(approval.ToolInput) == 0 {
		approval.ToolInput = json.RawMessage(`{}`)
	}
	command := approval.ToolName
	if command == "" {
		command = "Claude tool request"
	}
	payload := protocol.ApprovalPayload{
		RequestID: approval.RequestID,
		Command:   command,
		Reason:    "Claude requested permission to continue",
		ToolName:  approval.ToolName,
		ToolInput: approval.ToolInput,
		Agent:     "claude-cli",
	}
	if err := s.sink.CreatePendingApproval(ctx, payload.RequestID, payload); err != nil {
		return err
	}
	if _, err := s.sink.AppendEvent(ctx, protocol.EventAwaitingApproval, payload); err != nil {
		return err
	}
	if err := s.sink.SetSessionStatus(ctx, "awaiting_approval"); err != nil {
		return err
	}
	s.adapter.logger.Info("agent_awaiting_approval", "agent", "claude-cli", "session_id", s.sessionID, "request_id", payload.RequestID, "tool_name", payload.ToolName)

	s.mu.Lock()
	if s.pending == nil {
		s.pending = make(map[string]claudeApproval)
	}
	s.pending[payload.RequestID] = approval
	s.mu.Unlock()
	return nil
}

func (s *claudeSession) approvalPrompt(requestID, action, text string) string {
	s.mu.Lock()
	approval := s.pending[requestID]
	s.mu.Unlock()

	var b strings.Builder
	fmt.Fprintf(&b, "Tethon resolved the pending Claude tool request %q with action %q.", requestID, action)
	if approval.ToolName != "" {
		fmt.Fprintf(&b, "\nTool: %s", approval.ToolName)
	}
	if len(approval.ToolInput) > 0 {
		fmt.Fprintf(&b, "\nTool input: %s", string(approval.ToolInput))
	}
	if text != "" {
		fmt.Fprintf(&b, "\nUser reply: %s", text)
	}
	switch action {
	case "approve":
		b.WriteString("\nContinue from the pending request if possible.")
	case "deny":
		b.WriteString("\nDo not run the pending tool. Explain the denial and continue with a safe alternative if possible.")
	case "reply":
		b.WriteString("\nUse the user reply as guidance before continuing.")
	}
	return b.String()
}

func (s *claudeSession) markCancelled(ctx context.Context) ([]protocol.Event, error) {
	s.mu.Lock()
	if s.doneEmitted {
		s.mu.Unlock()
		return nil, ErrSessionDone
	}
	s.terminal = true
	s.doneEmitted = true
	s.mu.Unlock()

	event, err := s.sink.AppendEvent(ctx, protocol.EventDone, protocol.DonePayload{Reason: "cancelled"})
	if err != nil {
		return nil, err
	}
	if err := s.sink.SetSessionStatus(ctx, "done"); err != nil {
		return nil, err
	}
	return []protocol.Event{event}, nil
}

func (s *claudeSession) appendTerminalError(ctx context.Context, message string) {
	s.mu.Lock()
	if s.terminal {
		s.mu.Unlock()
		return
	}
	s.terminal = true
	s.mu.Unlock()
	_, _ = s.sink.AppendEvent(ctx, protocol.EventError, protocol.ErrorPayload{Message: message})
	_ = s.sink.SetSessionStatus(ctx, "error")
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

type claudeStreamEvent struct {
	Type              string              `json:"type"`
	Subtype           string              `json:"subtype"`
	SessionID         string              `json:"session_id"`
	RequestID         string              `json:"request_id"`
	Model             string              `json:"model"`
	Message           claudeMessage       `json:"message"`
	Result            string              `json:"result"`
	IsError           bool                `json:"is_error"`
	StopReason        string              `json:"stop_reason"`
	Usage             *claudeUsage        `json:"usage"`
	PermissionDenials []claudeToolRequest `json:"permission_denials"`
	DeferredToolUse   *claudeToolRequest  `json:"deferred_tool_use"`
}

type claudeMessage struct {
	Content []claudeContentBlock `json:"content"`
	Model   string               `json:"model"`
}

type claudeContentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type claudeToolRequest struct {
	ID      string          `json:"id"`
	ToolID  string          `json:"tool_use_id"`
	Name    string          `json:"name"`
	Tool    string          `json:"tool_name"`
	Input   json.RawMessage `json:"input"`
	Command string          `json:"command"`
	Reason  string          `json:"reason"`
}

type claudeUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreation            struct {
		Ephemeral5mInputTokens int64 `json:"ephemeral_5m_input_tokens"`
		Ephemeral1hInputTokens int64 `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation"`
}

func (u claudeUsage) tokens() protocol.UsageTokens {
	cacheWrite5m := u.CacheCreation.Ephemeral5mInputTokens
	if cacheWrite5m == 0 {
		cacheWrite5m = u.CacheCreationInputTokens
	}
	cacheWrite1h := u.CacheCreation.Ephemeral1hInputTokens
	tokens := protocol.UsageTokens{
		Input:        u.InputTokens,
		Output:       u.OutputTokens,
		CacheRead:    u.CacheReadInputTokens,
		CacheWrite:   u.CacheCreationInputTokens,
		CacheWrite5m: cacheWrite5m,
		CacheWrite1h: cacheWrite1h,
	}
	tokens.TotalBillable = tokens.Input + tokens.Output + tokens.CacheRead + tokens.CacheWrite5m + tokens.CacheWrite1h
	return tokens
}

func (e claudeStreamEvent) model() string {
	if e.Model != "" {
		return e.Model
	}
	return e.Message.Model
}

func (e claudeStreamEvent) assistantText() []string {
	var texts []string
	for _, block := range e.Message.Content {
		if block.Type == "text" && block.Text != "" {
			texts = append(texts, block.Text)
		}
	}
	return texts
}

func (e claudeStreamEvent) approval() (claudeApproval, bool) {
	if e.StopReason != "tool_deferred" && e.DeferredToolUse == nil && len(e.PermissionDenials) == 0 {
		return claudeApproval{}, false
	}
	if e.DeferredToolUse != nil {
		return approvalFromTool(*e.DeferredToolUse, e.RequestID), true
	}
	if len(e.PermissionDenials) > 0 {
		return approvalFromTool(e.PermissionDenials[0], e.RequestID), true
	}
	return claudeApproval{RequestID: firstNonEmpty(e.RequestID, "claude_permission")}, true
}

func approvalFromTool(tool claudeToolRequest, fallbackID string) claudeApproval {
	input := tool.Input
	if len(input) == 0 && tool.Command != "" {
		input, _ = json.Marshal(map[string]string{"command": tool.Command})
	}
	return claudeApproval{
		RequestID: firstNonEmpty(tool.ID, tool.ToolID, fallbackID, "claude_permission"),
		ToolName:  firstNonEmpty(tool.Name, tool.Tool),
		ToolInput: input,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func resolveWorkDir(req StartRequest) string {
	if req.WorkDir != "" {
		return req.WorkDir
	}
	return workingDir(req.Session.Repo, req.Session.CWD)
}

func workingDir(repo, cwd string) string {
	if cwd == "" {
		return repo
	}
	if filepath.IsAbs(cwd) {
		return cwd
	}
	if repo == "" {
		return cwd
	}
	return filepath.Join(repo, cwd)
}

// mergeEnv returns base with override applied: any base entry whose KEY also
// appears in override is dropped, then override is appended. This makes the
// override values authoritative regardless of platform env-precedence rules.
func mergeEnv(base, override []string) []string {
	overridden := make(map[string]struct{}, len(override))
	for _, kv := range override {
		if key, _, ok := strings.Cut(kv, "="); ok {
			overridden[key] = struct{}{}
		}
	}
	merged := make([]string, 0, len(base)+len(override))
	for _, kv := range base {
		key, _, _ := strings.Cut(kv, "=")
		if _, ok := overridden[key]; ok {
			continue
		}
		merged = append(merged, kv)
	}
	return append(merged, override...)
}
