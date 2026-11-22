//go:build unix

// Package proctree is the last-resort process-tree safety net: kill a
// whole process group, and detect when a child has been reparented to
// init. Graceful drain lives in shared/lifecycle; this package is only
// for supervised children that must not outlive their owner.
package proctree

import (
	"fmt"
	"os"
	"syscall"
)

// GroupedSysProcAttr puts a child in its own process group so the whole
// tree can be signalled at once.
func GroupedSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// KillGroup sends a signal to the process group led by pid. It is a
// hard kill only when sig is SIGKILL.
func KillGroup(pid int, sig syscall.Signal) error {
	if err := syscall.Kill(-pid, sig); err != nil {
		return fmt.Errorf("kill -%d: %w", pid, err)
	}
	return nil
}

// Orphaned reports whether this process was reparented to init, i.e.
// its owner died.
func Orphaned() bool { return os.Getppid() == 1 }
