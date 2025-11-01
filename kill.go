package main

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
)

// protectedPID reports whether mtop must never signal pid. PID 1 is launchd
// or init; PID 0 is kernel_task on macOS, and kill(0, sig) would signal our
// own process group; negative PIDs address whole process groups.
func protectedPID(pid, self int) bool {
	return pid <= 1 || pid == self
}

type killResult struct {
	sent    int   // signal delivered
	gone    int   // process had already exited
	skipped int   // protected PIDs that were left alone
	failed  int   // kill returned some other error
	err     error // first such error
}

// signalPIDs sends sig to every pid that is not protected. kill is
// syscall.Kill outside of tests.
func signalPIDs(pids []int, sig syscall.Signal, self int, kill func(int, syscall.Signal) error) killResult {
	var r killResult
	for _, pid := range pids {
		if protectedPID(pid, self) {
			r.skipped++
			continue
		}
		switch err := kill(pid, sig); {
		case err == nil:
			r.sent++
		case errors.Is(err, syscall.ESRCH):
			r.gone++
		default:
			r.failed++
			if r.err == nil {
				r.err = err
			}
		}
	}
	return r
}

// message is the status line shown after a kill, e.g.
// "sent SIGTERM to 14 processes of Google Chrome".
func (r killResult) message(sig syscall.Signal, app string) string {
	if r.sent+r.gone+r.failed == 0 && r.skipped > 0 {
		return fmt.Sprintf("refusing to signal %s: PID 0/1 and mtop itself are off limits", app)
	}
	msg := fmt.Sprintf("sent %s to %d %s of %s", signalName(sig), r.sent, plural(r.sent, "process", "processes"), app)
	var notes []string
	if r.failed > 0 {
		notes = append(notes, fmt.Sprintf("%d failed: %v", r.failed, r.err))
	}
	if r.gone > 0 {
		notes = append(notes, fmt.Sprintf("%d already gone", r.gone))
	}
	if r.skipped > 0 {
		notes = append(notes, fmt.Sprintf("skipped %d protected", r.skipped))
	}
	if len(notes) > 0 {
		msg += " (" + strings.Join(notes, ", ") + ")"
	}
	return msg
}

func signalName(sig syscall.Signal) string {
	switch sig {
	case syscall.SIGTERM:
		return "SIGTERM"
	case syscall.SIGKILL:
		return "SIGKILL"
	}
	return sig.String()
}
