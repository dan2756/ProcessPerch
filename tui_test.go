package main

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

// testUI returns a ui fed from a fixed snapshot.
func testUI(t *testing.T, procs []Process) *ui {
	t.Helper()
	u := newUI(options{interval: 2 * time.Second})
	u.snapshot = func() ([]Process, error) { return procs, nil }
	u.now = func() time.Time { return time.Date(2025, 10, 25, 19, 38, 13, 0, time.UTC) }
	u.refresh()
	return u
}

func screen(u *ui, w, h int) []string {
	lines := u.lines(w, h)
	for i := range lines {
		lines[i] = stripANSI(lines[i])
	}
	return lines
}

func TestRenderDarwin(t *testing.T) {
	u := testUI(t, loadFixture(t, "ps_darwin.txt"))
	lines := screen(u, 100, 12)
	if len(lines) != 12 {
		t.Fatalf("got %d lines, want 12", len(lines))
	}
	for i, l := range lines {
		if n := utf8.RuneCountInString(l); n > 100 {
			t.Errorf("line %d is %d wide: %q", i, n, l)
		}
	}
	if !strings.HasPrefix(lines[0], "mtop  63 processes in ") || !strings.HasSuffix(lines[0], "19:38:13") {
		t.Errorf("title = %q", lines[0])
	}
	if !strings.Contains(lines[1], "PROCS") || !strings.Contains(lines[1], "PIDS") {
		t.Errorf("header = %q", lines[1])
	}
	if !strings.HasPrefix(lines[2], "> Google Chrome ") || !strings.Contains(lines[2], "   14   52.5 ") {
		t.Errorf("first row = %q", lines[2])
	}
	if !strings.Contains(lines[2], " 706 701 640 612 ") {
		t.Errorf("first row pids = %q", lines[2])
	}
}

func TestCursorMovement(t *testing.T) {
	u := testUI(t, loadFixture(t, "ps_darwin.txt"))
	u.lines(100, 12)
	press := func(s string) {
		for _, k := range parseKeys([]byte(s)) {
			if u.handleKey(k) {
				t.Fatalf("unexpected quit on %q", s)
			}
		}
	}

	press("jj")
	if u.cursor != 2 {
		t.Errorf("cursor after jj = %d", u.cursor)
	}
	press("\x1b[A")
	if u.cursor != 1 {
		t.Errorf("cursor after up = %d", u.cursor)
	}
	press("kkkk")
	if u.cursor != 0 {
		t.Errorf("cursor clamps at top, got %d", u.cursor)
	}
	press("G")
	if want := len(u.visible()) - 1; u.cursor != want {
		t.Errorf("cursor after G = %d, want %d", u.cursor, want)
	}
	// The selected row must be on screen after scrolling.
	lines := screen(u, 100, 12)
	found := false
	for _, l := range lines {
		if strings.HasPrefix(l, "> ") {
			found = true
		}
	}
	if !found {
		t.Error("selected row scrolled off screen")
	}

	if !u.handleKey(key{kind: keyRune, r: 'q'}) || !u.handleKey(key{kind: keyCtrlC}) {
		t.Error("q and ctrl-c should quit")
	}
}

func TestTopLimitsGroups(t *testing.T) {
	u := testUI(t, loadFixture(t, "ps_darwin.txt"))
	u.top = 3
	if n := len(u.visible()); n != 3 {
		t.Fatalf("visible = %d groups, want 3", n)
	}
	lines := screen(u, 100, 12)
	if !strings.Contains(lines[0], "in 3 groups") {
		t.Errorf("title = %q", lines[0])
	}
}

func TestSnapshotErrorKeepsLastGroups(t *testing.T) {
	u := testUI(t, loadFixture(t, "ps_darwin.txt"))
	u.snapshot = func() ([]Process, error) { return nil, errors.New("ps: exit status 1") }
	u.refresh()
	lines := screen(u, 100, 12)
	if !strings.HasPrefix(lines[2], "> Google Chrome") {
		t.Errorf("groups dropped after error: %q", lines[2])
	}
	if last := lines[len(lines)-1]; last != "error: ps: exit status 1" {
		t.Errorf("status = %q", last)
	}
}

func TestRenderTinyTerminal(t *testing.T) {
	u := testUI(t, loadFixture(t, "ps_linux.txt"))
	for _, size := range [][2]int{{10, 3}, {20, 4}, {30, 5}} {
		for _, l := range screen(u, size[0], size[1]) {
			if utf8.RuneCountInString(l) > size[0] {
				t.Errorf("%dx%d: line too wide: %q", size[0], size[1], l)
			}
		}
	}
}
