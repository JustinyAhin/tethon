package resource

import (
	"context"
	"testing"
	"time"

	"tethon/internal/protocol"
)

func TestSnapshotCollectorShapesNoProcessSession(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 10, 0, time.UTC)
	created := now.Add(-10 * time.Second)
	collector := &SnapshotCollector{Now: func() time.Time { return now }}

	snapshots, warnings := collector.Collect(context.Background(), []SessionTarget{{
		Session: protocol.Session{
			ID:        "sess_1",
			Repo:      "/repo",
			Agent:     "fake",
			Status:    "running",
			CreatedAt: created,
		},
	}})

	if len(warnings) != 0 {
		t.Fatalf("top-level warnings = %+v, want none", warnings)
	}
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %+v", snapshots)
	}
	got := snapshots[0]
	if got.SessionID != "sess_1" || got.RuntimeSeconds != 10 || got.Aggregate.ProcessCount != 0 {
		t.Fatalf("unexpected snapshot: %+v", got)
	}
	if len(got.Processes) != 0 || len(got.ListeningPorts) != 0 {
		t.Fatalf("process data should be empty without identity: %+v", got)
	}
	if len(got.Warnings) != 1 || got.Warnings[0].Code != "no_active_process" {
		t.Fatalf("warnings = %+v, want no_active_process", got.Warnings)
	}
}

func TestAggregateProcesses(t *testing.T) {
	got := aggregateProcesses([]protocol.ResourceProcess{
		{PID: 1, RSSBytes: 100, CPUTimeMS: 10},
		{PID: 2, RSSBytes: 200, CPUTimeMS: 20},
	})
	if got.ProcessCount != 2 || got.RSSBytes != 300 || got.CPUTimeMS != 30 {
		t.Fatalf("aggregate = %+v, want summed values", got)
	}
}

func TestDiskUsageCanceledContextReturnsWarning(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	usage, warning := collectWorktreeDiskUsage(ctx, "/tmp/missing")
	if usage != nil {
		t.Fatalf("usage = %+v, want nil", usage)
	}
	if warning == nil || warning.Code != "disk_usage_timeout" {
		t.Fatalf("warning = %+v, want disk_usage_timeout", warning)
	}
}

func TestParseLinuxProcStat(t *testing.T) {
	stat, err := parseLinuxProcStat("1234 (claude code) S 1 1234 1234 0 -1 4194560 100 0 0 0 5 7 0 0 20 0 1 0 12345 100000 42 0 0 0")
	if err != nil {
		t.Fatal(err)
	}
	if stat.PID != 1234 || stat.PPID != 1 || stat.PGID != 1234 || stat.UTime != 5 || stat.STime != 7 || stat.RSSPages != 42 {
		t.Fatalf("stat = %+v", stat)
	}
	if got := linuxCPUTimeMS(stat); got != 120 {
		t.Fatalf("cpu time = %d, want 120", got)
	}
}

func TestParseLinuxListenSockets(t *testing.T) {
	raw := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:994D 00000000:0000 0A 00000000:00000000 00:00000000 00000000  501        0 99999 1 0000000000000000 100 0 0 10 0`
	sockets, err := parseLinuxListenSockets(raw, "tcp")
	if err != nil {
		t.Fatal(err)
	}
	if len(sockets) != 1 {
		t.Fatalf("sockets = %+v, want one", sockets)
	}
	if sockets[0].Address != "127.0.0.1" || sockets[0].Port != 39245 || sockets[0].Inode != "99999" {
		t.Fatalf("socket = %+v", sockets[0])
	}
}
