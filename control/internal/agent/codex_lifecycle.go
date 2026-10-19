package agent

import (
	"context"
	"errors"

	"tethon/internal/protocol"
)

func (s *codexSession) ChangeConversation(ctx context.Context, action string) error {
	s.actions.Lock()
	defer s.actions.Unlock()

	state := s.NativeState()
	if state.ConnectionState != protocol.CodexConnected {
		return errors.New("reconnect Codex before changing this conversation")
	}
	if state.TurnState == protocol.CodexTurnInProgress {
		return errors.New("stop the active turn before changing this conversation")
	}

	var method string
	switch action {
	case "archive":
		method = "thread/archive"
	case "unarchive":
		method = "thread/unarchive"
	case "delete":
		method = "thread/delete"
	default:
		return errors.New("unsupported native conversation action")
	}

	if err := s.client.Call(ctx, method, map[string]string{"threadId": state.ThreadID}, nil); err != nil {
		return err
	}

	if action == "unarchive" {
		return s.client.Call(ctx, "thread/resume", map[string]any{
			"threadId":     state.ThreadID,
			"excludeTurns": true,
		}, nil)
	}

	return nil
}
