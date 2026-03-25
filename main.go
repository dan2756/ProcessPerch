// Command mtop is a terminal task manager that rolls helper processes up
// under the app that owns them.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "0.2.1"

const usageText = `mtop - processes grouped by the app that owns them

Usage:
  mtop [flags]
  mtop --json [--top N] [--sort KEY]
  mtop --watch --hook SPEC [--hook SPEC ...]

Flags:
  -n, --interval DUR     refresh every DUR, e.g. 2s, 500ms or plain seconds (default 2s)
      --json             print one snapshot of the groups as JSON and exit
      --top N            only show the first N groups
      --sort KEY         initial order: cpu, mem, name or count (default cpu)
      --hook SPEC        run a command when an app crosses a threshold (repeatable)
      --hook-cooldown DUR
                         fire a hook at most once per DUR for each app (default 5m)
      --watch            no UI: check hooks every interval and log firings to stderr
  -h, --help             show this help
      --version          print the version and exit

Hooks:
  SPEC is METRIC>VALUE:COMMAND (or >=), where METRIC is cpu (percent of one
  core), mem (bytes; K, M, G, T suffixes) or count (processes):

    --hook 'cpu>80:say "$MTOP_APP is hot"'
    --hook 'mem>2G:osascript -e "display notification \"$MTOP_APP\""'
    --hook 'count>50:echo "$MTOP_APP runs $MTOP_COUNT processes" >> ~/mtop.log'

  A hook fires once when an app goes over the threshold and re-arms when it
  drops back under. COMMAND runs with sh -c and gets MTOP_APP, MTOP_CPU,
  MTOP_MEM_BYTES, MTOP_COUNT, MTOP_PIDS and MTOP_HOOK in its environment.

Keys:
  j/k, arrows   move (g/G: top/bottom, PgUp/PgDn: page)
  enter, e      expand/collapse the selected app
  x             SIGTERM every process of the selected app
  X             SIGKILL every process of the selected app
  s             cycle sort: cpu, mem, name, count
  /             filter by name (enter keeps it, esc clears it)
  q, ctrl-c     quit

PID 0, PID 1 and mtop itself are never signalled.
`

type options struct {
	interval time.Duration
	json     bool
	top      int
	sort     sortKey
	hooks    []hook
	cooldown time.Duration
	watch    bool
	version  bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	opts, err := parseFlags(args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(stdout, usageText)
		return 0
	}
	if err != nil {
		fmt.Fprintf(stderr, "mtop: %v\nrun 'mtop --help' for usage\n", err)
		return 2
	}
	if opts.version {
		fmt.Fprintf(stdout, "mtop %s\n", version)
		return 0
	}

	switch {
	case opts.json:
		err = printJSON(stdout, opts)
	case opts.watch:
		err = runWatch(opts, stdout, stderr)
	default:
		err = runTUI(opts)
	}
	if err != nil {
		fmt.Fprintf(stderr, "mtop: %v\n", err)
		return 1
	}
	return 0
}

func parseFlags(args []string) (options, error) {
	opts := options{interval: 2 * time.Second, cooldown: 5 * time.Minute}

	fs := flag.NewFlagSet("mtop", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	interval := durationFlag{&opts.interval}
	fs.Var(interval, "n", "")
	fs.Var(interval, "interval", "")
	fs.BoolVar(&opts.json, "json", false, "")
	fs.IntVar(&opts.top, "top", 0, "")
	sortName := fs.String("sort", "cpu", "")
	var hookSpecs stringList
	fs.Var(&hookSpecs, "hook", "")
	fs.Var(durationFlag{&opts.cooldown}, "hook-cooldown", "")
	fs.BoolVar(&opts.watch, "watch", false, "")
	fs.BoolVar(&opts.version, "version", false, "")

	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	var err error
	if opts.sort, err = parseSortKey(*sortName); err != nil {
		return opts, err
	}
	if fs.NArg() > 0 {
		return opts, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if opts.interval < 100*time.Millisecond {
		return opts, fmt.Errorf("interval must be at least 100ms, got %s", opts.interval)
	}
	if opts.top < 0 {
		return opts, fmt.Errorf("--top must not be negative")
	}
	if opts.cooldown < 0 {
		return opts, fmt.Errorf("--hook-cooldown must not be negative")
	}
	for _, spec := range hookSpecs {
		h, err := parseHook(spec)
		if err != nil {
			return opts, err
		}
		opts.hooks = append(opts.hooks, h)
	}
	if opts.json && opts.watch {
		return opts, fmt.Errorf("--json and --watch cannot be combined")
	}
	if opts.watch && len(opts.hooks) == 0 {
		return opts, fmt.Errorf("--watch needs at least one --hook")
	}
	return opts, nil
}

// stringList collects a repeatable string flag.
type stringList []string

func (l *stringList) String() string { return strings.Join(*l, ", ") }

func (l *stringList) Set(s string) error {
	*l = append(*l, s)
	return nil
}

// durationFlag accepts Go durations ("1.5s", "500ms") as well as plain
// numbers of seconds ("2", "0.5"), like top(1) does.
type durationFlag struct{ d *time.Duration }

func (f durationFlag) String() string {
	if f.d == nil {
		return ""
	}
	return f.d.String()
}

func (f durationFlag) Set(s string) error {
	if secs, err := strconv.ParseFloat(s, 64); err == nil {
		*f.d = time.Duration(secs * float64(time.Second))
		return nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q", s)
	}
	*f.d = d
	return nil
}

// jsonGroup is the --json representation of a Group.
type jsonGroup struct {
	Name     string  `json:"name"`
	Bundle   string  `json:"bundle,omitempty"`
	Count    int     `json:"count"`
	CPU      float64 `json:"cpu"`
	MemBytes uint64  `json:"mem_bytes"`
	PIDs     []int   `json:"pids"`
}

func printJSON(w io.Writer, opts options) error {
	procs, err := listProcesses()
	if err != nil {
		return err
	}
	groups := groupProcesses(procs)
	sortGroups(groups, opts.sort)
	if opts.top > 0 && len(groups) > opts.top {
		groups = groups[:opts.top]
	}

	out := make([]jsonGroup, len(groups))
	for i, g := range groups {
		out[i] = jsonGroup{
			Name:     g.Name,
			Bundle:   g.Bundle,
			Count:    len(g.Procs),
			CPU:      g.CPU,
			MemBytes: g.RSS,
			PIDs:     g.PIDs(),
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
