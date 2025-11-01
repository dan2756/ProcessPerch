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

// statusTTL is how long a status message stays up before the key help
// comes back.
const statusTTL = 5 * time.Second

// ui is the interactive view. It knows nothing about the terminal itself:
// render returns a frame as a string and handleKey updates state, which
// keeps it easy to test.
type ui struct {
	interval time.Duration
	top      int
	sort     sortKey
	snapshot func() ([]Process, error)
	now      func() time.Time
	kill     func(pid int, sig syscall.Signal) error
	self     int // our own PID, never signalled

	nprocs int
	groups []Group // sorted
	err    error   // last snapshot error, shown until the next good one

	cursor    int // index into visible()
	offset    int // first body row on screen
	bodyH     int // body rows in the last frame, for paging
	expanded  map[string]bool
	filter    string
	filtering bool // typing into the filter prompt

	status   string
	statusAt time.Time

	// refreshSoon asks the main loop for an early snapshot, so killed
	// processes disappear without waiting a full interval.
	refreshSoon bool
}

func newUI(opts options) *ui {
	return &ui{
		interval: opts.interval,
		top:      opts.top,
		sort:     opts.sort,
		snapshot: listProcesses,
		now:      time.Now,
		kill:     syscall.Kill,
		self:     os.Getpid(),
		bodyH:    20,
		expanded: make(map[string]bool),
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
	var soon <-chan time.Time
	for {
		t.write(u.render(t.size()))
		select {
		case k, ok := <-keys:
			if !ok || u.handleKey(k) {
				return nil
			}
			if u.refreshSoon {
				u.refreshSoon = false
				soon = time.After(300 * time.Millisecond)
			}
		case <-soon:
			soon = nil
			u.refresh()
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
	sortGroups(u.groups, u.sort)
	u.clampCursor()
}

// visible returns the groups currently on screen: filtered by name, then
// limited by --top.
func (u *ui) visible() []Group {
	g := u.groups
	if u.filter != "" {
		needle := strings.ToLower(u.filter)
		var matched []Group
		for _, grp := range g {
			if strings.Contains(strings.ToLower(grp.Name), needle) {
				matched = append(matched, grp)
			}
		}
		g = matched
	}
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

func (u *ui) setStatus(format string, args ...any) {
	u.status = fmt.Sprintf(format, args...)
	u.statusAt = u.now()
}

// handleKey applies one key press and reports whether to quit.
func (u *ui) handleKey(k key) (quit bool) {
	if u.filtering && u.handleFilterKey(k) {
		return false
	}
	switch k.kind {
	case keyCtrlC:
		return true
	case keyEnter:
		u.toggleExpanded()
	case keyEsc:
		u.filter = ""
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
		case 'e':
			u.toggleExpanded()
		case 'x':
			u.killSelected(syscall.SIGTERM)
		case 'X':
			u.killSelected(syscall.SIGKILL)
		case 's':
			u.sort = u.sort.next()
			sortGroups(u.groups, u.sort)
			u.setStatus("sorted by %s", u.sort)
		case '/':
			u.filtering = true
		}
	}
	u.clampCursor()
	return false
}

// handleFilterKey edits the filter prompt. It reports false for keys the
// prompt does not consume (arrows, ctrl-c), which keep their usual meaning.
func (u *ui) handleFilterKey(k key) bool {
	switch k.kind {
	case keyRune:
		u.filter += string(k.r)
	case keyBackspace:
		if u.filter == "" {
			u.filtering = false
			return true
		}
		r := []rune(u.filter)
		u.filter = string(r[:len(r)-1])
	case keyEnter:
		u.filtering = false
		return true
	case keyEsc:
		u.filtering = false
		u.filter = ""
	default:
		return false
	}
	u.cursor, u.offset = 0, 0
	return true
}

func (u *ui) toggleExpanded() {
	if g := u.selected(); g != nil {
		u.expanded[g.Key] = !u.expanded[g.Key]
	}
}

// killSelected signals every process in the selected group. There is no
// confirmation on purpose: the status line says what happened.
func (u *ui) killSelected(sig syscall.Signal) {
	g := u.selected()
	if g == nil {
		return
	}
	r := signalPIDs(g.PIDs(), sig, u.self, u.kill)
	u.setStatus("%s", r.message(sig, g.Name))
	u.refreshSoon = true
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

// prefixW is the selection marker plus the expand arrow.
const prefixW = 3

func (u *ui) layout(width int, groups []Group) layout {
	fixed := prefixW + 1 + countW + 1 + cpuW + 1 + memW + 2
	nameW := 12
	for _, g := range groups {
		nameW = max(nameW, utf8.RuneCountInString(g.Name))
		if u.expanded[g.Key] {
			for _, p := range g.Procs {
				nameW = max(nameW, 2+utf8.RuneCountInString(processName(p.Comm)))
			}
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
	header := fmt.Sprintf("%*s%s %*s %*s %*s  %s", prefixW, "",
		pad("APP", lay.nameW), countW, "PROCS", cpuW, "CPU%", memW, "MEM", "PIDS")
	out = append(out, escReverse+pad(header, width)+escReset)

	// Body rows: each group, followed by its processes when expanded.
	type row struct {
		g *Group
		p *Process
	}
	var rows []row
	cursorRow := 0
	for i := range vis {
		g := &vis[i]
		if i == u.cursor {
			cursorRow = len(rows)
		}
		rows = append(rows, row{g: g})
		if u.expanded[g.Key] {
			for j := range g.Procs {
				rows = append(rows, row{g: g, p: &g.Procs[j]})
			}
		}
	}

	u.bodyH = height - 3
	u.scrollTo(cursorRow)
	for i := u.offset; i < len(rows) && i < u.offset+u.bodyH; i++ {
		if r := rows[i]; r.p != nil {
			out = append(out, procLine(r.p, lay))
		} else {
			out = append(out, u.groupLine(r.g, lay, i == cursorRow))
		}
	}
	if len(vis) == 0 && u.err == nil {
		if u.filter != "" {
			out = append(out, fmt.Sprintf("%*sno app matches %q", prefixW, "", u.filter))
		} else {
			out = append(out, fmt.Sprintf("%*sno processes", prefixW, ""))
		}
	}
	for len(out) < height-1 {
		out = append(out, "")
	}
	// Keep the last line off the final column so the cursor never wraps
	// past the bottom of the screen.
	out = append(out, u.statusLine(width-1))
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
	left := fmt.Sprintf("mtop  %d %s in %d %s  sort %s  every %s",
		u.nprocs, plural(u.nprocs, "process", "processes"),
		shown, plural(shown, "group", "groups"), u.sort, u.interval)
	if u.filter != "" && !u.filtering {
		left += fmt.Sprintf("  filter %q", u.filter)
	}
	right := u.now().Format("15:04:05")
	gap := width - utf8.RuneCountInString(left) - len(right)
	if gap < 1 {
		return escBold + truncate(left, width) + escReset
	}
	return escBold + left + escReset + strings.Repeat(" ", gap) + right
}

func (u *ui) groupLine(g *Group, lay layout, selected bool) string {
	marker := " "
	if selected {
		marker = ">"
	}
	arrow := " "
	switch {
	case u.expanded[g.Key]:
		arrow = "▾"
	case len(g.Procs) > 1:
		arrow = "▸"
	}
	line := fmt.Sprintf("%s%s %s %*d %*.1f %*s  %s",
		marker, arrow, pad(g.Name, lay.nameW),
		countW, len(g.Procs),
		cpuW, g.CPU,
		memW, formatBytes(g.RSS),
		truncate(joinPIDs(g.PIDs(), " "), lay.pidsW))
	if selected {
		return escReverse + pad(line, lay.width) + escReset
	}
	return truncate(line, lay.width)
}

// procLine is one process under an expanded group.
func procLine(p *Process, lay layout) string {
	line := fmt.Sprintf("%*s%s %*s %*.1f %*s  %d",
		prefixW, "", pad("  "+processName(p.Comm), lay.nameW),
		countW, "",
		cpuW, p.CPU,
		memW, formatBytes(p.RSS),
		p.PID)
	return escDim + truncate(line, lay.width) + escReset
}

const keyHelp = "x term  X kill  enter expand  s sort  / filter  q quit"

func (u *ui) statusLine(width int) string {
	switch {
	case u.filtering:
		// A reverse-video space stands in for the cursor.
		return truncate("/"+u.filter, width-1) + escReverse + " " + escReset
	case u.err != nil:
		return truncate("error: "+u.err.Error(), width)
	case u.status != "" && u.now().Sub(u.statusAt) < statusTTL:
		return truncate(u.status, width)
	}
	return truncate(keyHelp, width)
}
