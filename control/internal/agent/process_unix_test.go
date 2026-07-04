//go:build darwin || linux

package agent

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"testing"
	"time"
)

func TestExecRunnerStartRecordsOwnedProcessIdentityAndInterruptsGroup(t *testing.T) {
	process := startExecRunnerHelper(t)
	identity := process.Identity()
	if identity.PID <= 0 {
		t.Fatalf("PID = %d, want > 0", identity.PID)
	}
	if identity.PGID != identity.PID {
		t.Fatalf("PGID = %d, want owned process group %d", identity.PGID, identity.PID)
	}
	if identity.StartedAt.IsZero() {
		t.Fatal("StartedAt is zero")
	}
	if identity.Executable == "" {
		t.Fatal("Executable is empty")
	}
	if !identity.OwnsProcessGroup {
		t.Fatal("OwnsProcessGroup = false, want true")
	}

	if err := process.Interrupt(); err != nil {
		t.Fatal(err)
	}
	if err := waitForProcess(process, time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestExecRunnerKillTerminatesOwnedProcessGroup(t *testing.T) {
	process := startExecRunnerHelper(t)
	if !process.Identity().OwnsProcessGroup {
		t.Fatalf("OwnsProcessGroup = false for identity %+v", process.Identity())
	}
	if err := process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := waitForProcessExit(process, time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestExecRunnerHelperProcess(t *testing.T) {
	if os.Getenv("TETHON_EXEC_RUNNER_HELPER") != "1" {
		return
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	fmt.Println("ready")
	select {
	case <-signals:
		os.Exit(0)
	case <-time.After(30 * time.Second):
		os.Exit(2)
	}
}

func startExecRunnerHelper(t *testing.T) CommandProcess {
	t.Helper()
	process, err := execRunner{}.Start(context.Background(), CommandSpec{
		Binary: os.Args[0],
		Args:   []string{"-test.run=TestExecRunnerHelperProcess"},
		Env:    []string{"TETHON_EXEC_RUNNER_HELPER=1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	ready, err := bufio.NewReader(process.Stdout()).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(ready) != "ready" {
		t.Fatalf("helper output = %q, want ready", ready)
	}
	return process
}

func waitForProcess(process CommandProcess, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() {
		done <- process.Wait()
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("timed out waiting for process")
	}
}

func waitForProcessExit(process CommandProcess, timeout time.Duration) error {
	done := make(chan struct{}, 1)
	go func() {
		_ = process.Wait()
		done <- struct{}{}
	}()
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("timed out waiting for process")
	}
}
