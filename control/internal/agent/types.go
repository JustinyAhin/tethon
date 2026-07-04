package agent

import (
	"context"
	"errors"
	"os/exec"
	"sort"
	"time"

	"tethon/internal/protocol"
)

var ErrBusy = errors.New("agent turn is already running")
var ErrSessionDone = errors.New("session is no longer active")

type Capabilities struct {
	Stream           bool
	InteractiveInput bool
	Approvals        bool
	Resume           bool
}

type StartRequest struct {
	Session protocol.Session
	Create  protocol.CreateSessionRequest
	// WorkDir is the resolved directory the agent process should run in
	// (the session's worktree, plus any cwd). Empty means the adapter falls
	// back to deriving a directory from the session repo/cwd.
	WorkDir string
}

type Adapter interface {
	Name() string
	Capabilities() Capabilities
	Start(context.Context, StartRequest, Sink) (RunningSession, error)
}

type RunningSession interface {
	Input(context.Context, string) ([]protocol.Event, error)
	ResolveApproval(context.Context, string, string, string) ([]protocol.Event, error)
	Cancel(context.Context) ([]protocol.Event, error)
	Terminal() bool
	ProcessIdentity() ProcessIdentity
}

type ProcessIdentity struct {
	PID              int
	PGID             int
	StartedAt        time.Time
	Executable       string
	OwnsProcessGroup bool
}

type Sink interface {
	AppendEvent(context.Context, string, any) (protocol.Event, error)
	CreatePendingApproval(context.Context, string, any) error
	ResolveApproval(context.Context, string, string) error
	SetSessionStatus(context.Context, string) error
	SetSessionModel(context.Context, string) error
}

type Registry struct {
	adapters map[string]Adapter
}

func NewRegistry(adapters ...Adapter) *Registry {
	registry := &Registry{adapters: make(map[string]Adapter, len(adapters))}
	for _, adapter := range adapters {
		if adapter == nil || adapter.Name() == "" {
			continue
		}
		registry.adapters[adapter.Name()] = adapter
	}
	return registry
}

func DefaultRegistry() *Registry {
	adapters := []Adapter{NewFake()}
	if _, err := exec.LookPath("claude"); err == nil {
		adapters = append(adapters, NewClaude(ClaudeConfig{}))
	}
	return NewRegistry(adapters...)
}

func (r *Registry) Get(name string) (Adapter, bool) {
	if r == nil {
		return nil, false
	}
	adapter, ok := r.adapters[name]
	return adapter, ok
}

func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.adapters))
	for name := range r.adapters {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
