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
	"time"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "0.1.0"

const usageText = `mtop - processes grouped by the app that owns them

Usage:
  mtop [flags]

Flags:
  -n, --interval DUR   refresh every DUR, e.g. 2s, 500ms or plain seconds (default 2s)
      --json           print one snapshot of the groups as JSON and exit
      --top N          only show the N busiest groups
  -h, --help           show this help
      --version        print the version and exit

Keys:
  j/k, arrows   move
  q, ctrl-c     quit
`

type options struct {
	interval time.Duration
	json     bool
	top      int
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

	if opts.json {
		err = printJSON(stdout, opts)
	} else {
		err = runTUI(opts)
	}
	if err != nil {
		fmt.Fprintf(stderr, "mtop: %v\n", err)
		return 1
	}
	return 0
}

func parseFlags(args []string) (options, error) {
	opts := options{interval: 2 * time.Second}

	fs := flag.NewFlagSet("mtop", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	interval := durationFlag{&opts.interval}
	fs.Var(interval, "n", "")
	fs.Var(interval, "interval", "")
	fs.BoolVar(&opts.json, "json", false, "")
	fs.IntVar(&opts.top, "top", 0, "")
	fs.BoolVar(&opts.version, "version", false, "")

	if err := fs.Parse(args); err != nil {
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
	return opts, nil
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
	sortByCPU(groups)
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
