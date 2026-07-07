package resource

import (
	"context"
	"sort"
	"time"

	"tethon/internal/agent"
	"tethon/internal/protocol"
)

type Collector interface {
	Collect(context.Context, []SessionTarget) ([]protocol.SessionResourceSnapshot, []protocol.ResourceWarning)
}

type SessionTarget struct {
	Session  protocol.Session
	Identity agent.ProcessIdentity
}

type SnapshotCollector struct {
	Now func() time.Time
}

func NewCollector() *SnapshotCollector {
	return &SnapshotCollector{}
}

func (c *SnapshotCollector) Collect(ctx context.Context, targets []SessionTarget) ([]protocol.SessionResourceSnapshot, []protocol.ResourceWarning) {
	now := time.Now().UTC()
	if c != nil && c.Now != nil {
		now = c.Now().UTC()
	}
	snapshots := make([]protocol.SessionResourceSnapshot, 0, len(targets))
	for _, target := range targets {
		snapshots = append(snapshots, c.collectOne(ctx, target, now))
	}
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].SessionID < snapshots[j].SessionID
	})
	return snapshots, nil
}

func (c *SnapshotCollector) collectOne(ctx context.Context, target SessionTarget, now time.Time) protocol.SessionResourceSnapshot {
	roots := pathRoots(target)
	snapshot := protocol.SessionResourceSnapshot{
		SessionID:    target.Session.ID,
		Status:       target.Session.Status,
		Agent:        target.Session.Agent,
		RepoID:       target.Session.RepoID,
		Repo:         target.Session.Repo,
		CWD:          target.Session.CWD,
		WorktreePath: target.Session.WorktreePath,
		ProcessRoot: protocol.ResourceProcessIdentity{
			PID:              target.Identity.PID,
			PGID:             target.Identity.PGID,
			StartedAt:        target.Identity.StartedAt,
			Executable:       target.Identity.Executable,
			OwnsProcessGroup: target.Identity.OwnsProcessGroup,
		},
		Processes:      []protocol.ResourceProcess{},
		ListeningPorts: []protocol.ResourceListeningPort{},
	}
	if !target.Identity.StartedAt.IsZero() {
		snapshot.RuntimeSeconds = int64(now.Sub(target.Identity.StartedAt).Seconds())
	} else if !target.Session.CreatedAt.IsZero() {
		snapshot.RuntimeSeconds = int64(now.Sub(target.Session.CreatedAt).Seconds())
	}
	if snapshot.RuntimeSeconds < 0 {
		snapshot.RuntimeSeconds = 0
	}

	if target.Identity.PID <= 0 {
		snapshot.Warnings = append(snapshot.Warnings, warning("no_active_process", "session is active but has no live process identity"))
	} else {
		processes, ports, warnings := collectSessionProcesses(ctx, target, roots)
		snapshot.Processes = processes
		snapshot.ListeningPorts = ports
		snapshot.Warnings = append(snapshot.Warnings, warnings...)
		if len(processes) == 0 {
			snapshot.Warnings = append(snapshot.Warnings, warning("process_not_found", "root process was not found during resource collection"))
		}
	}
	sort.Slice(snapshot.Processes, func(i, j int) bool {
		return snapshot.Processes[i].PID < snapshot.Processes[j].PID
	})
	sort.Slice(snapshot.ListeningPorts, func(i, j int) bool {
		if snapshot.ListeningPorts[i].Port != snapshot.ListeningPorts[j].Port {
			return snapshot.ListeningPorts[i].Port < snapshot.ListeningPorts[j].Port
		}
		return snapshot.ListeningPorts[i].PID < snapshot.ListeningPorts[j].PID
	})
	snapshot.Aggregate = aggregateProcesses(snapshot.Processes)

	if target.Session.WorktreePath != "" {
		usage, diskWarning := collectWorktreeDiskUsage(ctx, target.Session.WorktreePath)
		if usage != nil {
			redacted := RedactPath(usage.Path, roots, DefaultPathMaxBytes)
			usage.Path = redacted.Value
			snapshot.WorktreeDiskUsage = usage
		}
		if diskWarning != nil {
			snapshot.Warnings = append(snapshot.Warnings, *diskWarning)
		}
	}
	return snapshot
}

func pathRoots(target SessionTarget) []PathRoot {
	roots := []PathRoot{}
	if target.Session.WorktreePath != "" {
		roots = append(roots, PathRoot{Label: "worktree", Path: target.Session.WorktreePath})
	}
	if target.Session.Repo != "" {
		roots = append(roots, PathRoot{Label: "repo", Path: target.Session.Repo})
	}
	return roots
}

func redactCommand(raw string, roots []PathRoot) protocol.ResourceProcess {
	redacted := RedactCommandLine(raw, RedactionOptions{
		MaxBytes:  DefaultCommandMaxBytes,
		PathRoots: roots,
	})
	return protocol.ResourceProcess{
		Command:          redacted.Value,
		CommandRedacted:  redacted.Redacted,
		CommandTruncated: redacted.Truncated,
	}
}

func aggregateProcesses(processes []protocol.ResourceProcess) protocol.ResourceAggregate {
	aggregate := protocol.ResourceAggregate{ProcessCount: len(processes)}
	for _, process := range processes {
		aggregate.RSSBytes += process.RSSBytes
		aggregate.CPUTimeMS += process.CPUTimeMS
	}
	return aggregate
}

func warning(code, message string) protocol.ResourceWarning {
	return protocol.ResourceWarning{Code: code, Message: message}
}
