package agent

import (
	"context"
	"strings"

	"tethon/internal/protocol"
)

type fakeAdapter struct{}

func NewFake() Adapter {
	return fakeAdapter{}
}

func (fakeAdapter) Name() string {
	return "fake"
}

func (fakeAdapter) Capabilities() Capabilities {
	return Capabilities{
		Stream:           true,
		InteractiveInput: true,
		Approvals:        true,
		Resume:           true,
	}
}

func (fakeAdapter) Start(ctx context.Context, req StartRequest, sink Sink) (RunningSession, error) {
	running := &fakeSession{sink: sink}
	approval := protocol.ApprovalPayload{
		RequestID: "approval_bootstrap",
		Command:   "fake risky command",
		Reason:    "proves pending approval state and replay",
	}
	if err := sink.CreatePendingApproval(ctx, approval.RequestID, approval); err != nil {
		return nil, err
	}
	if _, err := sink.AppendEvent(ctx, protocol.EventSessionStarted, req.Create); err != nil {
		return nil, err
	}
	if _, err := sink.AppendEvent(ctx, protocol.EventOutput, protocol.OutputPayload{Text: "fake agent ready"}); err != nil {
		return nil, err
	}
	if _, err := sink.AppendEvent(ctx, protocol.EventAwaitingApproval, approval); err != nil {
		return nil, err
	}
	return running, nil
}

type fakeSession struct {
	sink Sink
}

func (s *fakeSession) Input(ctx context.Context, text string) ([]protocol.Event, error) {
	events := make([]protocol.Event, 0, 3)
	event, err := s.sink.AppendEvent(ctx, protocol.EventInputReceived, protocol.InputReceivedPayload{Text: text})
	if err != nil {
		return nil, err
	}
	events = append(events, event)

	event, err = s.sink.AppendEvent(ctx, protocol.EventOutput, protocol.OutputPayload{Text: "fake agent received: " + text})
	if err != nil {
		return nil, err
	}
	events = append(events, event)

	if strings.EqualFold(text, "done") || strings.EqualFold(text, "finish") {
		event, err = s.sink.AppendEvent(ctx, protocol.EventDone, map[string]string{"reason": "fake agent finished"})
		if err != nil {
			return nil, err
		}
		events = append(events, event)
		if err := s.sink.SetSessionStatus(ctx, "done"); err != nil {
			return nil, err
		}
	}
	return events, nil
}

func (s *fakeSession) ResolveApproval(ctx context.Context, requestID, action, text string) ([]protocol.Event, error) {
	if err := s.sink.ResolveApproval(ctx, requestID, action); err != nil {
		return nil, err
	}
	events := make([]protocol.Event, 0, 2)
	event, err := s.sink.AppendEvent(ctx, protocol.EventApprovalResolved, protocol.ApprovalResolvedPayload{
		RequestID: requestID,
		Action:    action,
		Text:      text,
	})
	if err != nil {
		return nil, err
	}
	events = append(events, event)

	event, err = s.sink.AppendEvent(ctx, protocol.EventOutput, protocol.OutputPayload{Text: "fake approval resolved: " + action})
	if err != nil {
		return nil, err
	}
	events = append(events, event)
	return events, nil
}

func (*fakeSession) Cancel(context.Context) error {
	return nil
}
