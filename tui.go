package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

// ui is the interactive view. It knows nothing about the terminal itself:
// render returns a frame as a string and handleKey updates state, which
// keeps it easy to test.
type ui struct {
	interval time.Duration
	top      int
	snapshot func() ([]Process, error)
	now      func() time.Time

	nprocs int
	groups []Group // sorted
	err    error   // last snapshot error, shown until the next good one

	cursor int // index into visible()
	offset int // first body row on screen
	bodyH  int // body rows in the last frame, for paging
	status string
}

func newUI(opts options) *ui {
	return &ui{
		interval: opts.interval,
		top:      opts.top,
		snapshot: listProcesses,
		now:      time.Now,
		bodyH:    20,
	}
}

func runTUI(opts options) error {
	t, err := openTerminal()
	if err != nil {
		return err
	}
	// Deferred calls also run while a panic unwinds, so this covers
	// crashes in the UI loop as well as normal exits.
	defer t.restore()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGWINCH)
	defer signal.Stop(sigs)

	keys := make(chan key, 32)
	go readKeys(t.in, keys)

	tick := time.NewTicker(opts.interval)
	defer tick.Stop()

	u := newUI(opts)
	u.refresh()
	for {
		t.write(u.render(t.size()))
		select {
		case k, ok := <-keys:
			if !ok || u.handleKey(k) {
				return nil
			}
		case <-tick.C:
			u.refresh()
		case sig := <-sigs:
			if sig != syscall.SIGWINCH {
				return nil
			}
		}
	}
}

// refresh takes a new snapshot. On failure the previous groups stay on
// screen and the error is shown in the status line.
func (u *ui) refresh() {
	procs, err := u.snapshot()
	if err != nil {
		u.err = err
		return
	}
	u.err = nil
	u.nprocs = len(procs)
	u.groups = groupProcesses(procs)
	sortByCPU(u.groups)
	u.clampCursor()
}

// visible returns the groups currently on screen.
func (u *ui) visible() []Group {
	g := u.groups
	if u.top > 0 && len(g) > u.top {
		g = g[:u.top]
	}
	return g
}

func (u *ui) selected() *Group {
	vis := u.visible()
	if u.cursor < 0 || u.cursor >= len(vis) {
		return nil
	}
	return &vis[u.cursor]
}

func (u *ui) clampCursor() {
	n := len(u.visible())
	if u.cursor >= n {
		u.cursor = n - 1
	}
	if u.cursor < 0 {
		u.cursor = 0
	}
}

// handleKey applies one key press and reports whether to quit.
func (u *ui) handleKey(k key) (quit bool) {
	switch k.kind {
	case keyCtrlC:
		return true
	case keyUp:
		u.cursor--
	case keyDown:
		u.cursor++
	case keyPageUp:
		u.cursor -= u.bodyH
	case keyPageDown:
		u.cursor += u.bodyH
	case keyHome:
		u.cursor = 0
	case keyEnd:
		u.cursor = len(u.visible()) - 1
	case keyRune:
		switch k.r {
		case 'q':
			return true
		case 'j':
			u.cursor++
		case 'k':
			u.cursor--
		case 'g':
			u.cursor = 0
		case 'G':
			u.cursor = len(u.visible()) - 1
		}
	}
	u.clampCursor()
	return false
}

// Column widths for the numeric columns.
const (
	countW = 5
	cpuW   = 6
	memW   = 5
)

type layout struct {
	width, nameW, pidsW int
}

func (u *ui) layout(width int, groups []Group) layout {
	fixed := 2 + 1 + countW + 1 + cpuW + 1 + memW + 2
	nameW := 12
	for _, g := range groups {
		if n := utf8.RuneCountInString(g.Name); n > nameW {
			nameW = n
		}
	}
	nameW = min(nameW, 40)
	if fixed+nameW > width {
		nameW = max(4, width-fixed)
	}
	return layout{width: width, nameW: nameW, pidsW: max(0, width-fixed-nameW)}
}

// render draws a full frame for a width x height terminal.
func (u *ui) render(width, height int) string {
	lines := u.lines(width, height)
	var b strings.Builder
	b.WriteString(escHome)
	for i, line := range lines {
		// Clear first: clearing after a full-width line would eat its
		// last character on terminals that defer the wrap.
		b.WriteString(escClearLine)
		b.WriteString(line)
		if i < len(lines)-1 {
			b.WriteString("\r\n")
		}
	}
	return b.String()
}

func (u *ui) lines(width, height int) []string {
	if width < 20 || height < 4 {
		return []string{truncate("mtop: terminal too small", width)}
	}
	vis := u.visible()
	lay := u.layout(width, vis)

	out := make([]string, 0, height)
	out = append(out, u.titleLine(width, len(vis)))
	header := fmt.Sprintf("  %s %*s %*s %*s  %s",
		pad("APP", lay.nameW), countW, "PROCS", cpuW, "CPU%", memW, "MEM", "PIDS")
	out = append(out, escReverse+pad(header, width)+escReset)

	u.bodyH = height - 3
	u.scrollTo(u.cursor)
	for i := u.offset; i < len(vis) && i < u.offset+u.bodyH; i++ {
		out = append(out, u.groupLine(&vis[i], lay, i == u.cursor))
	}
	if len(vis) == 0 && u.err == nil {
		out = append(out, "  no processes")
	}
	for len(out) < height-1 {
		out = append(out, "")
	}
	// Keep the last line off the final column so the cursor never wraps
	// past the bottom of the screen.
	out = append(out, truncate(u.statusLine(), width-1))
	return out
}

func (u *ui) scrollTo(row int) {
	if row < u.offset {
		u.offset = row
	}
	if row >= u.offset+u.bodyH {
		u.offset = row - u.bodyH + 1
	}
	if u.offset < 0 {
		u.offset = 0
	}
}

func (u *ui) titleLine(width, shown int) string {
	left := fmt.Sprintf("mtop  %d processes in %d groups  every %s", u.nprocs, shown, u.interval)
	right := u.now().Format("15:04:05")
	gap := width - utf8.RuneCountInString(left) - len(right)
	if gap < 1 {
		return escBold + truncate(left, width) + escReset
	}
	return escBold + left + escReset + strings.Repeat(" ", gap) + right
}

func (u *ui) groupLine(g *Group, lay layout, selected bool) string {
	marker := "  "
	if selected {
		marker = "> "
	}
	line := fmt.Sprintf("%s%s %*d %*.1f %*s  %s",
		marker, pad(g.Name, lay.nameW),
		countW, len(g.Procs),
		cpuW, g.CPU,
		memW, formatBytes(g.RSS),
		truncate(joinPIDs(g.PIDs(), " "), lay.pidsW))
	if selected {
		return escReverse + pad(line, lay.width) + escReset
	}
	return truncate(line, lay.width)
}

func (u *ui) statusLine() string {
	switch {
	case u.err != nil:
		return "error: " + u.err.Error()
	case u.status != "":
		return u.status
	}
	return "j/k move  q quit"
}
