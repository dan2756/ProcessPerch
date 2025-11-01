package main

import (
	"os"
	"os/exec"
	"reflect"
	"syscall"
	"testing"
)

type fakeKiller struct {
	got  []int
	errs map[int]error
}

func (f *fakeKiller) kill(pid int, sig syscall.Signal) error {
	f.got = append(f.got, pid)
	return f.errs[pid]
}

func TestSignalPIDsNeverTouchesProtected(t *testing.T) {
	const self = 4242
	f := &fakeKiller{}
	r := signalPIDs([]int{0, 1, -1, self, 500, 501}, syscall.SIGTERM, self, f.kill)
	if !reflect.DeepEqual(f.got, []int{500, 501}) {
		t.Errorf("signalled %v, want [500 501]", f.got)
	}
	if r.sent != 2 || r.skipped != 4 {
		t.Errorf("result = %+v", r)
	}
}

func TestSignalPIDsErrors(t *testing.T) {
	f := &fakeKiller{errs: map[int]error{
		11: syscall.ESRCH,
		12: syscall.EPERM,
		13: syscall.EPERM,
	}}
	r := signalPIDs([]int{10, 11, 12, 13}, syscall.SIGKILL, 1, f.kill)
	if r.sent != 1 || r.gone != 1 || r.failed != 2 || r.err != syscall.EPERM {
		t.Errorf("result = %+v", r)
	}
	want := "sent SIGKILL to 1 process of Slack (2 failed: operation not permitted, 1 already gone)"
	if got := r.message(syscall.SIGKILL, "Slack"); got != want {
		t.Errorf("message = %q\nwant      %q", got, want)
	}
}

func TestKillMessages(t *testing.T) {
	r := killResult{sent: 14}
	if got := r.message(syscall.SIGTERM, "Google Chrome"); got != "sent SIGTERM to 14 processes of Google Chrome" {
		t.Errorf("message = %q", got)
	}
	r = killResult{skipped: 1}
	if got := r.message(syscall.SIGTERM, "launchd"); got != "refusing to signal launchd: PID 0/1 and mtop itself are off limits" {
		t.Errorf("message = %q", got)
	}
	r = killResult{sent: 3, skipped: 1}
	if got := r.message(syscall.SIGTERM, "mtop"); got != "sent SIGTERM to 3 processes of mtop (skipped 1 protected)" {
		t.Errorf("message = %q", got)
	}
}

func TestSignalPIDsRealProcess(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skip("cannot start sleep:", err)
	}
	r := signalPIDs([]int{cmd.Process.Pid, os.Getpid()}, syscall.SIGTERM, os.Getpid(), syscall.Kill)
	if r.sent != 1 || r.skipped != 1 {
		t.Errorf("result = %+v", r)
		cmd.Process.Kill()
	}
	err := cmd.Wait()
	if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGTERM {
		t.Errorf("sleep did not die from SIGTERM: %v", err)
	}
}
