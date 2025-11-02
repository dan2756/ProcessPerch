package main

import (
	"errors"
	"reflect"
	"regexp"
	"strings"
	"syscall"
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
	u.kill = func(int, syscall.Signal) error {
		t.Fatal("test tried to send a real signal")
		return nil
	}
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
	if !strings.HasPrefix(lines[2], ">▸ Google Chrome ") || !strings.Contains(lines[2], "   14   52.5  2.9G ") {
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
		if strings.HasPrefix(l, ">") {
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
	if !strings.HasPrefix(lines[2], ">▸ Google Chrome") {
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

// press feeds raw terminal input to the ui.
func press(t *testing.T, u *ui, s string) {
	t.Helper()
	for _, k := range parseKeys([]byte(s)) {
		if u.handleKey(k) {
			t.Fatalf("unexpected quit on %q", s)
		}
	}
}

func TestExpandGroup(t *testing.T) {
	u := testUI(t, loadFixture(t, "ps_darwin.txt"))
	press(t, u, "\r")
	lines := screen(u, 120, 30)
	if !strings.HasPrefix(lines[2], ">▾ Google Chrome ") {
		t.Fatalf("group row = %q", lines[2])
	}
	if !strings.Contains(lines[3], "  Google Chrome Helper (Renderer)") || !strings.HasSuffix(lines[3], "  706") {
		t.Errorf("first child = %q", lines[3])
	}
	if !strings.Contains(lines[16], "chrome_crashpad_handler") {
		t.Errorf("last child = %q", lines[16])
	}
	if !strings.HasPrefix(lines[17], " ▸ Docker ") {
		t.Errorf("row after children = %q", lines[17])
	}

	// The cursor stays on group rows: j skips over the children.
	press(t, u, "j")
	if g := u.selected(); g == nil || g.Name != "Docker" {
		t.Errorf("selected after j = %+v", g)
	}
	press(t, u, "ke")
	if lines := screen(u, 120, 30); !strings.HasPrefix(lines[3], " ▸ Docker") {
		t.Errorf("after collapse row 3 = %q", lines[3])
	}
}

func TestFilter(t *testing.T) {
	u := testUI(t, loadFixture(t, "ps_darwin.txt"))
	press(t, u, "/studio")
	if !u.filtering {
		t.Fatal("not in filter mode")
	}
	if got := groupNames(u.visible()); !reflect.DeepEqual(got, []string{"Visual Studio Code"}) {
		t.Errorf("visible = %v", got)
	}
	lines := screen(u, 100, 12)
	if last := lines[len(lines)-1]; last != "/studio " {
		t.Errorf("prompt = %q", last)
	}

	// Typed letters go to the prompt, not to the key bindings.
	press(t, u, strings.Repeat("\x7f", 6)+"xq")
	if u.filter != "xq" {
		t.Errorf("filter = %q", u.filter)
	}
	press(t, u, "\x7f\x7fCODE\r")
	if u.filtering || u.filter != "CODE" {
		t.Errorf("after enter: filtering=%v filter=%q", u.filtering, u.filter)
	}
	if got := groupNames(u.visible()); !reflect.DeepEqual(got, []string{"Visual Studio Code", "Xcode"}) {
		t.Errorf("case-insensitive match = %v", got)
	}
	if !strings.Contains(screen(u, 120, 12)[0], `filter "CODE"`) {
		t.Error("title does not show the active filter")
	}
	press(t, u, "\x1b")
	if u.filter != "" || len(u.visible()) != len(u.groups) {
		t.Errorf("esc did not clear the filter")
	}

	press(t, u, "/zzz")
	if lines := screen(u, 100, 12); !strings.Contains(lines[2], `no app matches "zzz"`) {
		t.Errorf("empty filter row = %q", lines[2])
	}
	press(t, u, "\x1b")
	if u.filtering || u.filter != "" {
		t.Error("esc in prompt should cancel")
	}
}

func TestSortCycle(t *testing.T) {
	u := testUI(t, loadFixture(t, "ps_darwin.txt"))
	press(t, u, "s")
	if u.sort != sortMem || u.visible()[0].Name != "Google Chrome" || u.visible()[1].Name != "Docker" {
		t.Errorf("sort %s: %v", u.sort, groupNames(u.visible()[:3]))
	}
	press(t, u, "s")
	if u.sort != sortName || u.visible()[0].Name != "1Password" {
		t.Errorf("sort %s: %v", u.sort, groupNames(u.visible()[:3]))
	}
	press(t, u, "s")
	if u.sort != sortCount || u.visible()[0].Name != "Google Chrome" || u.visible()[1].Name != "Visual Studio Code" {
		t.Errorf("sort %s: %v", u.sort, groupNames(u.visible()[:3]))
	}
	press(t, u, "s")
	if u.sort != sortCPU {
		t.Errorf("sort did not wrap around: %s", u.sort)
	}
	if last := screen(u, 100, 12)[11]; last != "sorted by cpu" {
		t.Errorf("status = %q", last)
	}
}

func TestKillSelectedGroup(t *testing.T) {
	u := testUI(t, loadFixture(t, "ps_darwin.txt"))
	var sent []int
	var sig syscall.Signal
	u.kill = func(pid int, s syscall.Signal) error {
		sent = append(sent, pid)
		sig = s
		return nil
	}
	press(t, u, "x")
	if len(sent) != 14 || sig != syscall.SIGTERM {
		t.Fatalf("sent %v to %v", sig, sent)
	}
	if !u.refreshSoon {
		t.Error("kill should ask for an early refresh")
	}
	lines := screen(u, 100, 12)
	if last := lines[len(lines)-1]; last != "sent SIGTERM to 14 processes of Google Chrome" {
		t.Errorf("status = %q", last)
	}

	// The status line goes back to the key help after a while.
	u.now = func() time.Time { return time.Date(2025, 10, 25, 19, 38, 30, 0, time.UTC) }
	if last := screen(u, 100, 12)[11]; last != keyHelp {
		t.Errorf("status after timeout = %q", last)
	}
}

func TestKillRefusesProtected(t *testing.T) {
	u := testUI(t, loadFixture(t, "ps_darwin.txt"))
	u.kill = func(pid int, _ syscall.Signal) error {
		t.Errorf("signalled protected pid %d", pid)
		return nil
	}
	for _, name := range []string{"launchd", "kernel_task"} {
		press(t, u, "/"+name+"\r")
		if g := u.selected(); g == nil || g.Name != name {
			t.Fatalf("selected %+v, want %s", g, name)
		}
		press(t, u, "X")
		if !strings.HasPrefix(u.status, "refusing to signal "+name) {
			t.Errorf("status = %q", u.status)
		}
		press(t, u, "\x1b")
	}

	u.self = 1650 // pretend we are the mtop process in the fixture
	press(t, u, "/mtop\rx")
	if !strings.HasPrefix(u.status, "refusing to signal mtop") {
		t.Errorf("status = %q", u.status)
	}
}

func TestHooksFireFromTUI(t *testing.T) {
	procs := loadFixture(t, "ps_darwin.txt")
	u := newUI(options{interval: 2 * time.Second, hooks: []hook{mustHook(t, "mem>2G:say big")}, cooldown: time.Minute})
	u.snapshot = func() ([]Process, error) { return procs, nil }
	u.now = func() time.Time { return time.Date(2025, 11, 2, 22, 32, 27, 0, time.UTC) }
	var fired []string
	u.runHook = func(f firing) error {
		fired = append(fired, f.group.Name)
		return nil
	}
	u.refresh()
	u.refresh()
	if want := []string{"Google Chrome", "Docker"}; !reflect.DeepEqual(fired, want) {
		t.Errorf("fired %v, want %v", fired, want)
	}
	lines := screen(u, 120, 12)
	if !strings.Contains(lines[0], "  1 hook") {
		t.Errorf("title = %q", lines[0])
	}
	if last := lines[11]; last != "hook mem>2G fired for Docker (cpu 16.8%, mem 2.4G, 4 processes)" {
		t.Errorf("status = %q", last)
	}
}
