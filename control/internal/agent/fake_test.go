package agent

import (
	"context"
	"testing"

	"tethon/internal/protocol"
)

func TestFakeProcessIdentityIsEmpty(t *testing.T) {
	running, err := NewFake().Start(context.Background(), StartRequest{
		Session: protocol.Session{ID: "ses_1", Repo: "."},
		Create:  protocol.CreateSessionRequest{Repo: ".", Agent: "fake"},
	}, newMemorySink())
	if err != nil {
		t.Fatal(err)
	}
	if got := running.ProcessIdentity(); got != (ProcessIdentity{}) {
		t.Fatalf("process identity = %+v, want empty", got)
	}
}
