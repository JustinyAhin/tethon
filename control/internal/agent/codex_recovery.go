package agent

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"tethon/internal/codex"
	"tethon/internal/protocol"
)

func (s *codexSession) reconnect() bool {
	s.mu.Lock()
	s.state.ConnectionState = protocol.CodexReconnecting
	if s.state.ConversationState == protocol.CodexConversationUnavailable {
		s.state.ConnectionState = protocol.CodexDisconnected
	}

	err := s.sink.InvalidateCodexRequests(context.Background(), s.state.ConnectionID)
	s.pending = make(map[string]protocol.CodexRequestPayload)
	s.responded = make(map[string]bool)
	if err == nil {
		err = s.saveStateLocked()
	}
	s.mu.Unlock()

	if err != nil {
		return false
	}

	backoff := time.Second
	for {
		s.actions.Lock()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if _, err := s.sink.LastEventSeq(ctx); err != nil {
			cancel()
			s.actions.Unlock()
			return false
		}

		s.mu.Lock()
		unavailable := s.state.ConversationState == protocol.CodexConversationUnavailable
		s.mu.Unlock()
		if unavailable {
			cancel()
			s.actions.Unlock()
			return false
		}

		client, info, err := s.adapter.connect(ctx)
		if err == nil {
			err = s.resumeConnection(ctx, client, info)
			if err != nil {
				client.Close()
			}
		}
		cancel()
		s.actions.Unlock()

		if err == nil {
			return true
		}

		time.Sleep(backoff)
		backoff = min(backoff*2, 30*time.Second)
	}
}

// Resume only attaches to the existing native thread. It must never resend a
// prompt or rebuild an approval from the Tethon delivery log.
func (s *codexSession) resumeConnection(ctx context.Context, client *codex.Client, info codex.DaemonInfo) error {
	s.mu.Lock()
	oldConnection := s.state.ConnectionID
	threadID := s.state.ThreadID
	conversationState := s.state.ConversationState
	s.client = client
	s.state.ConnectionID = uuid.NewString()
	s.state.ConnectionState = protocol.CodexReconnecting
	s.state.SocketPath = info.SocketPath
	s.state.ServerVersion = info.AppServerVersion
	s.pending = make(map[string]protocol.CodexRequestPayload)
	s.responded = make(map[string]bool)
	err := s.sink.InvalidateCodexRequests(ctx, oldConnection)
	s.mu.Unlock()

	if err != nil {
		return err
	}

	if conversationState != protocol.CodexConversationReady {
		var response struct {
			Thread NativeThread `json:"thread"`
		}
		if err := client.Call(ctx, "thread/read", map[string]string{"threadId": threadID}, &response); err != nil {
			s.observeMissingConversation(err)
			return err
		}

		s.mu.Lock()
		s.state.CWD = response.Thread.CWD
		s.state.ConnectionState = protocol.CodexConnected
		err := s.saveStateLocked()
		s.mu.Unlock()
		return err
	}

	var resumed struct {
		Thread NativeThread `json:"thread"`
		Model  string       `json:"model"`
		CWD    string       `json:"cwd"`
	}
	if err := client.Call(ctx, "thread/resume", map[string]any{"threadId": threadID, "excludeTurns": true}, &resumed); err != nil {
		s.observeMissingConversation(err)
		return err
	}
	if resumed.Thread.ID != threadID {
		return errors.New("native resume returned a different conversation")
	}
	if resumed.CWD == "" {
		resumed.CWD = resumed.Thread.CWD
	}

	s.mu.Lock()
	s.state.CWD = resumed.CWD
	s.mu.Unlock()

	return s.reconcileConnection(ctx, client, threadID, resumed.Model)
}

func (s *codexSession) observeMissingConversation(err error) {
	var rpcError *codex.RPCError
	if !errors.As(err, &rpcError) {
		return
	}

	message := strings.ToLower(rpcError.Message)
	if !strings.Contains(message, "thread not found") && !strings.Contains(message, "thread does not exist") {
		return
	}

	s.mu.Lock()
	s.state.ConversationState = protocol.CodexConversationUnavailable
	s.state.ConnectionState = protocol.CodexDisconnected
	_ = s.saveStateLocked()
	s.mu.Unlock()
}

func (s *codexSession) reconcileConnection(ctx context.Context, client *codex.Client, threadID, model string) error {
	var history protocol.CodexHistoryResponse
	if err := s.historyWithClient(ctx, client, threadID, "", 20, &history); err != nil {
		return err
	}

	s.mu.Lock()
	s.state.ConversationState = protocol.CodexConversationReady
	s.state.TurnID = ""
	s.state.TurnState = protocol.CodexTurnIdle
	if len(history.Turns) > 0 {
		turn := history.Turns[0]
		s.state.TurnID = turn.ID
		s.state.TurnState = nativeTurnState(turn.Status)
	}
	s.mu.Unlock()

	if err := s.sink.SetSessionModel(ctx, model); err != nil {
		return err
	}

	// A native read is a transport barrier. Request reissues received before
	// its reply are already queued by the reader and can be recorded now.
	if err := client.Call(ctx, "thread/read", map[string]string{"threadId": threadID}, nil); err != nil {
		return err
	}
	for {
		select {
		case message, ok := <-client.Messages():
			if !ok {
				return errors.New("native connection closed during reconciliation")
			}
			if err := s.handleMessage(message); err != nil {
				return err
			}
		default:
			s.mu.Lock()
			s.state.ConnectionState = protocol.CodexConnected
			err := s.saveStateLocked()
			s.mu.Unlock()
			return err
		}
	}
}

func nativeTurnState(status string) protocol.CodexTurnState {
	switch status {
	case "inProgress":
		return protocol.CodexTurnInProgress
	case "completed":
		return protocol.CodexTurnCompleted
	case "interrupted":
		return protocol.CodexTurnInterrupted
	case "failed":
		return protocol.CodexTurnFailed
	default:
		return protocol.CodexTurnIdle
	}
}
