package main

import (
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type metric int

const (
	metricCPU metric = iota
	metricMem
	metricCount
)

// hook runs a shell command when a group goes over a threshold, e.g.
// "cpu>80:say chrome is hot".
type hook struct {
	cond      string // the condition as written, e.g. "cpu>80"
	metric    metric
	orEqual   bool // >= instead of >
	threshold float64
	command   string
}

// parseHook parses METRIC>VALUE:COMMAND (or >=). METRIC is cpu (percent),
// mem (bytes, with K/M/G/T suffixes) or count (processes).
func parseHook(spec string) (hook, error) {
	cond, command, ok := strings.Cut(spec, ":")
	cond = strings.TrimSpace(cond)
	command = strings.TrimSpace(command)
	if !ok || command == "" {
		return hook{}, fmt.Errorf("hook %q: want METRIC>VALUE:COMMAND, e.g. 'cpu>80:say hot'", spec)
	}
	name, value, ok := strings.Cut(cond, ">")
	if !ok {
		return hook{}, fmt.Errorf("hook %q: condition %q needs a '>'", spec, cond)
	}
	h := hook{cond: cond, command: command}
	name = strings.ToLower(strings.TrimSpace(name))
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "=") {
		h.orEqual = true
		value = strings.TrimSpace(value[1:])
	}

	var err error
	switch name {
	case "cpu":
		h.metric = metricCPU
		h.threshold, err = strconv.ParseFloat(strings.TrimSuffix(value, "%"), 64)
	case "mem", "rss":
		h.metric = metricMem
		var n uint64
		n, err = parseBytes(value)
		h.threshold = float64(n)
	case "count", "procs":
		h.metric = metricCount
		var n int
		n, err = strconv.Atoi(value)
		h.threshold = float64(n)
	default:
		return hook{}, fmt.Errorf("hook %q: unknown metric %q (want cpu, mem or count)", spec, name)
	}
	// NaN slips past "< 0" (NaN < 0 is false) and would never fire.
	if err != nil || h.threshold < 0 || math.IsNaN(h.threshold) || math.IsInf(h.threshold, 0) {
		return hook{}, fmt.Errorf("hook %q: bad %s threshold %q", spec, name, value)
	}
	return h, nil
}

func (h *hook) value(g *Group) float64 {
	switch h.metric {
	case metricMem:
		return float64(g.RSS)
	case metricCount:
		return float64(len(g.Procs))
	}
	return g.CPU
}

func (h *hook) over(g *Group) bool {
	v := h.value(g)
	if h.orEqual {
		return v >= h.threshold
	}
	return v > h.threshold
}

// firing is one hook going off for one group.
type firing struct {
	hook  *hook
	group Group
}

// env is the extra environment the hook command runs with.
func (f firing) env() []string {
	g := &f.group
	return []string{
		"MTOP_APP=" + g.Name,
		"MTOP_CPU=" + strconv.FormatFloat(g.CPU, 'f', 1, 64),
		"MTOP_MEM_BYTES=" + strconv.FormatUint(g.RSS, 10),
		"MTOP_COUNT=" + strconv.Itoa(len(g.Procs)),
		"MTOP_PIDS=" + joinPIDs(g.PIDs(), " "),
		"MTOP_HOOK=" + f.hook.cond,
	}
}

func (f firing) String() string {
	g := &f.group
	return fmt.Sprintf("hook %s fired for %s (cpu %.1f%%, mem %s, %d %s)",
		f.hook.cond, g.Name, g.CPU, formatBytes(g.RSS), len(g.Procs), plural(len(g.Procs), "process", "processes"))
}

// hookState tracks one hook for one group.
type hookState struct {
	armed bool      // false after firing until the group drops back under
	fired time.Time // last time the hook fired for this group
}

type stateKey struct {
	hook  int
	group string
}

// hookSet decides when hooks fire. A hook fires when a group crosses its
// threshold, then stays quiet until the group drops back under it (or
// exits). On top of that, a hook fires at most once per cooldown for a given
// group; a crossing during the cooldown is held until the cooldown is over
// and fires then if the group is still over the threshold.
type hookSet struct {
	hooks    []hook
	cooldown time.Duration
	state    map[stateKey]*hookState
}

func newHookSet(hooks []hook, cooldown time.Duration) *hookSet {
	return &hookSet{hooks: hooks, cooldown: cooldown, state: make(map[stateKey]*hookState)}
}

// check evaluates every hook against a fresh snapshot and returns what
// should fire now.
func (hs *hookSet) check(groups []Group, now time.Time) []firing {
	if len(hs.hooks) == 0 {
		return nil
	}
	seen := make(map[stateKey]bool)
	var out []firing
	for i := range hs.hooks {
		h := &hs.hooks[i]
		for _, g := range groups {
			if !h.over(&g) {
				continue
			}
			k := stateKey{i, g.Key}
			seen[k] = true
			st := hs.state[k]
			if st == nil {
				st = &hookState{armed: true}
				hs.state[k] = st
			}
			if !st.armed || (!st.fired.IsZero() && now.Sub(st.fired) < hs.cooldown) {
				continue
			}
			st.armed = false
			st.fired = now
			out = append(out, firing{hook: h, group: g})
		}
	}
	for k, st := range hs.state {
		if seen[k] {
			continue
		}
		// Under the threshold or gone: re-armed. Forget it entirely once
		// the cooldown no longer matters, so the map does not grow with
		// every short-lived process name.
		st.armed = true
		if now.Sub(st.fired) >= hs.cooldown {
			delete(hs.state, k)
		}
	}
	return out
}

type hookResult struct {
	firing firing
	err    error
}

// startHook runs the firing's command with sh -c in the background and
// reports on done when it exits. The command gets the MTOP_* variables on
// top of mtop's own environment.
func startHook(f firing, stdout, stderr io.Writer, done chan<- hookResult) error {
	cmd := exec.Command("sh", "-c", f.hook.command)
	cmd.Env = append(os.Environ(), f.env()...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	// Ctrl-C goes to the foreground process group. A hook that is still
	// running should not take that signal just because mtop did.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		done <- hookResult{firing: f, err: cmd.Wait()}
	}()
	return nil
}
