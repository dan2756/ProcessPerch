package main

import (
	"os"
	"strings"
	"testing"
)

func loadFixture(t *testing.T, name string) []Process {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	procs, err := parsePS(f)
	if err != nil {
		t.Fatalf("parsePS(%s): %v", name, err)
	}
	return procs
}

func TestParsePSDarwinFixture(t *testing.T) {
	procs := loadFixture(t, "ps_darwin.txt")
	if got, want := len(procs), 63; got != want {
		t.Fatalf("parsed %d processes, want %d", got, want)
	}

	byPID := make(map[int]Process)
	for _, p := range procs {
		byPID[p.PID] = p
	}

	kernel := byPID[0]
	if kernel.Comm != "kernel_task" || kernel.PPID != 0 || kernel.CPU != 6.1 {
		t.Errorf("pid 0 = %+v", kernel)
	}

	renderer := byPID[706]
	want := "/Applications/Google Chrome.app/Contents/Frameworks/Google Chrome Framework.framework/" +
		"Versions/141.0.7390.108/Helpers/Google Chrome Helper (Renderer).app/Contents/MacOS/" +
		"Google Chrome Helper (Renderer)"
	if renderer.Comm != want {
		t.Errorf("pid 706 comm = %q, want %q", renderer.Comm, want)
	}
	if renderer.PPID != 612 || renderer.CPU != 21.7 || renderer.RSS != 734208*1024 {
		t.Errorf("pid 706 = %+v", renderer)
	}

	// RSS wider than its column must not shift the command.
	xcode := byPID[1900]
	if xcode.RSS != 1543210*1024 || xcode.Comm != "/Applications/Xcode.app/Contents/MacOS/Xcode" {
		t.Errorf("pid 1900 = %+v", xcode)
	}

	if got := byPID[1602].Comm; got != "-zsh" {
		t.Errorf("login shell comm = %q", got)
	}
	if got := byPID[2600].Comm; got != "<defunct>" {
		t.Errorf("zombie comm = %q", got)
	}
}

func TestParsePSLinuxFixture(t *testing.T) {
	procs := loadFixture(t, "ps_linux.txt")
	if got, want := len(procs), 25; got != want {
		t.Fatalf("parsed %d processes, want %d", got, want)
	}
	comms := make(map[int]string)
	for _, p := range procs {
		comms[p.PID] = p.Comm
	}
	for pid, want := range map[int]string{
		112:  "kworker/0:1H-kblockd",
		1402: "tmux: server",
		2120: "Isolated Web Co",
		2302: "sh <defunct>",
	} {
		if comms[pid] != want {
			t.Errorf("pid %d comm = %q, want %q", pid, comms[pid], want)
		}
	}
}

func TestParsePSLine(t *testing.T) {
	tests := []struct {
		line string
		want Process
	}{
		{"    1     0   0.0  14880 /sbin/launchd",
			Process{PID: 1, PPID: 0, CPU: 0, RSS: 14880 * 1024, Comm: "/sbin/launchd"}},
		{"81234 81200 102.5 2097152 /Applications/Foo Bar.app/Contents/MacOS/Foo Bar",
			Process{PID: 81234, PPID: 81200, CPU: 102.5, RSS: 2097152 * 1024, Comm: "/Applications/Foo Bar.app/Contents/MacOS/Foo Bar"}},
		{"12\t1\t0.3\t100\ttab separated",
			Process{PID: 12, PPID: 1, CPU: 0.3, RSS: 100 * 1024, Comm: "tab separated"}},
		{"  7  1  0.0  0  two  spaces",
			Process{PID: 7, PPID: 1, Comm: "two  spaces"}},
	}
	for _, tt := range tests {
		got, err := parsePSLine(tt.line)
		if err != nil {
			t.Errorf("parsePSLine(%q): %v", tt.line, err)
			continue
		}
		if got != tt.want {
			t.Errorf("parsePSLine(%q) = %+v, want %+v", tt.line, got, tt.want)
		}
	}
}

func TestParsePSLineErrors(t *testing.T) {
	for _, line := range []string{
		"1 0 0.0",
		"1 0 0.0 100",
		"x 0 0.0 100 foo",
		"1 y 0.0 100 foo",
		"1 0 abc 100 foo",
		"1 0 0.0 -5 foo",
		"-1 0 0.0 5 foo",
	} {
		if p, err := parsePSLine(line); err == nil {
			t.Errorf("parsePSLine(%q) = %+v, want error", line, p)
		}
	}
}

func TestParsePSSkipsHeaderAndJunk(t *testing.T) {
	in := "  PID  PPID  %CPU    RSS COMM\n" +
		"\n" +
		"    1     0   0.1  14880 /sbin/launchd\n" +
		"garbage line\n" +
		"   86     1   0.4  12784 /usr/libexec/logd\r\n"
	procs, err := parsePS(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(procs) != 2 || procs[1].Comm != "/usr/libexec/logd" {
		t.Fatalf("got %+v", procs)
	}
}

func TestParsePSAllBad(t *testing.T) {
	if _, err := parsePS(strings.NewReader("ps: illegal option -- w\n")); err == nil {
		t.Fatal("want error when nothing parses")
	}
	procs, err := parsePS(strings.NewReader(""))
	if err != nil || len(procs) != 0 {
		t.Fatalf("empty input: %v, %v", procs, err)
	}
}
