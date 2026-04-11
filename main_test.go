package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestParseFlagsDefaults(t *testing.T) {
	opts, err := parseFlags(nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.interval != 2*time.Second || opts.cooldown != 5*time.Minute || opts.sort != sortCPU || opts.top != 0 {
		t.Errorf("defaults = %+v", opts)
	}
}

func TestParseFlags(t *testing.T) {
	opts, err := parseFlags([]string{
		"-n", "0.5", "--top", "3", "--sort", "mem",
		"--hook", "cpu>80:say hot", "--hook", "count>50:true",
		"--hook-cooldown", "90s", "--watch",
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.interval != 500*time.Millisecond || opts.top != 3 || opts.sort != sortMem ||
		opts.cooldown != 90*time.Second || !opts.watch || len(opts.hooks) != 2 {
		t.Errorf("opts = %+v", opts)
	}
	if opts.hooks[1].metric != metricCount {
		t.Errorf("second hook = %+v", opts.hooks[1])
	}

	if opts, err = parseFlags([]string{"--interval=1500ms"}); err != nil || opts.interval != 1500*time.Millisecond {
		t.Errorf("--interval=1500ms: %v, %v", opts.interval, err)
	}

	// Plain seconds are rounded to the nearest nanosecond, not cut off.
	if opts, err = parseFlags([]string{"-n", "4.1", "--hook-cooldown", "2.01"}); err != nil ||
		opts.interval != 4100*time.Millisecond || opts.cooldown != 2010*time.Millisecond {
		t.Errorf("-n 4.1 --hook-cooldown 2.01: %v, %v, %v", opts.interval, opts.cooldown, err)
	}
}

func TestParseFlagsErrors(t *testing.T) {
	for _, args := range [][]string{
		{"-n", "0"},
		{"-n", "50ms"},
		{"-n", "soon"},
		{"--top", "-1"},
		{"--sort", "pid"},
		{"--hook", "cpu80:say"},
		{"--hook-cooldown", "-1s"},
		{"--hook-cooldown", "1e10"},
		{"-n", "1e10"},
		{"-n", "inf"},
		{"-n", "NaN"},
		{"--watch"},
		{"--json", "--watch", "--hook", "cpu>1:true"},
		{"--bogus"},
		{"extra"},
	} {
		if _, err := parseFlags(args); err == nil {
			t.Errorf("parseFlags(%q) succeeded, want error", args)
		}
	}

	// Seconds that overflow a Duration are too large, not negative.
	for _, args := range [][]string{{"--hook-cooldown", "1e10"}, {"-n", "1e10"}, {"-n", "inf"}} {
		if _, err := parseFlags(args); err == nil || !strings.Contains(err.Error(), "out of range") {
			t.Errorf("parseFlags(%q) = %v, want out of range", args, err)
		}
	}
}

func TestRunHelpAndVersion(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"--help"}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "MTOP_PIDS") {
		t.Errorf("--help: code %d, output %q", code, out.String())
	}
	out.Reset()
	if code := run([]string{"--version"}, &out, &errOut); code != 0 || out.String() != "mtop "+version+"\n" {
		t.Errorf("--version: code %d, output %q", code, out.String())
	}
	errOut.Reset()
	if code := run([]string{"--sort", "pid"}, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "unknown sort key") {
		t.Errorf("bad flag: code %d, stderr %q", code, errOut.String())
	}
}

func TestRunJSON(t *testing.T) {
	// Runs the real ps, so only check the shape of the output.
	var out, errOut bytes.Buffer
	if code := run([]string{"--json", "--top", "2"}, &out, &errOut); code != 0 {
		t.Fatalf("code %d, stderr %q", code, errOut.String())
	}
	var groups []jsonGroup
	if err := json.Unmarshal(out.Bytes(), &groups); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, out.String())
	}
	if len(groups) == 0 || len(groups) > 2 {
		t.Fatalf("got %d groups, want 1 or 2", len(groups))
	}
	for _, g := range groups {
		if g.Name == "" || g.Count != len(g.PIDs) {
			t.Errorf("bad group %+v", g)
		}
	}
}
