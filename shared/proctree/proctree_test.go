//go:build unix

package proctree

import (
	"syscall"
	"testing"
	"time"
)

func TestKillGroup(t *testing.T) {
	t.Parallel()
	attr := &syscall.ProcAttr{
		Files: []uintptr{0, 1, 2},
		Sys:   GroupedSysProcAttr(),
	}
	pid, err := syscall.ForkExec("/bin/sleep", []string{"sleep", "60"}, attr)
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	if err := KillGroup(pid, syscall.SIGKILL); err != nil {
		t.Fatalf("kill group: %v", err)
	}
	var ws syscall.WaitStatus
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		wpid, err := syscall.Wait4(pid, &ws, 0, nil)
		if err == nil && wpid == pid && ws.Signaled() {
			return
		}
		if err != nil {
			t.Fatalf("wait: %v", err)
		}
	}
	t.Fatal("child still alive")
}

func TestOrphanedBool(t *testing.T) {
	t.Parallel()
	_ = Orphaned() // smoke: must not panic
}
