package main

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseHook(t *testing.T) {
	tests := []struct {
		spec string
		want hook
	}{
		{"cpu>80:say chrome is hot",
			hook{cond: "cpu>80", metric: metricCPU, threshold: 80, command: "say chrome is hot"}},
		{"cpu>=150%:echo hi",
			hook{cond: "cpu>=150%", metric: metricCPU, orEqual: true, threshold: 150, command: "echo hi"}},
		{"mem>2G:notify $MTOP_APP",
			hook{cond: "mem>2G", metric: metricMem, threshold: 2 << 30, command: "notify $MTOP_APP"}},
		{" MEM > 1.5GB : osascript -e 'display notification \"x\"'",
			hook{cond: "MEM > 1.5GB", metric: metricMem, threshold: 1.5 * (1 << 30), command: "osascript -e 'display notification \"x\"'"}},
		{"cpu >= 80:echo spaced",
			hook{cond: "cpu >= 80", metric: metricCPU, orEqual: true, threshold: 80, command: "echo spaced"}},
		{"count>50:echo $MTOP_COUNT",
			hook{cond: "count>50", metric: metricCount, threshold: 50, command: "echo $MTOP_COUNT"}},
		// Only the first colon separates the command.
		{"cpu>10:echo a:b:c",
			hook{cond: "cpu>10", metric: metricCPU, threshold: 10, command: "echo a:b:c"}},
	}
	for _, tt := range tests {
		got, err := parseHook(tt.spec)
		if err != nil {
			t.Errorf("parseHook(%q): %v", tt.spec, err)
			continue
		}
		if got != tt.want {
			t.Errorf("parseHook(%q) = %+v, want %+v", tt.spec, got, tt.want)
		}
	}
}

func TestParseHookErrors(t *testing.T) {
	for _, spec := range []string{
		"",
		"cpu>80",
		"cpu>80:",
		"cpu>80:   ",
		"cpu80:say hi",
		"cpu<80:say hi",
		"disk>80:say hi",
		"cpu>lots:say hi",
		"cpu>-1:say hi",
		"cpu>NaN:say hi",
		"cpu>nan:say hi",
		"cpu>Inf:say hi",
		"cpu>inf:say hi",
		"cpu>+Inf:say hi",
		"mem>2Q:say hi",
		"count>1.5:say hi",
	} {
		if h, err := parseHook(spec); err == nil {
			t.Errorf("parseHook(%q) = %+v, want error", spec, h)
		}
	}
}

func TestParseBytes(t *testing.T) {
	tests := map[string]uint64{
		"0":       0,
		"1048576": 1 << 20,
		"512B":    512,
		"800k":    800 << 10,
		"512M":    512 << 20,
		"2G":      2 << 30,
		"2GB":     2 << 30,
		"2GiB":    2 << 30,
		"1.5g":    3 << 29,
		"1T":      1 << 40,
	}
	for in, want := range tests {
		got, err := parseBytes(in)
		if err != nil || got != want {
			t.Errorf("parseBytes(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "G", "-1G", "1X", "NaN", "inf", "99999999E"} {
		if got, err := parseBytes(in); err == nil {
			t.Errorf("parseBytes(%q) = %d, want error", in, got)
		}
	}
}

func mustHook(t *testing.T, spec string) hook {
	t.Helper()
	h, err := parseHook(spec)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func chrome(cpu float64) []Group {
	return []Group{
		{Key: "/Applications/Google Chrome.app", Name: "Google Chrome", CPU: cpu, RSS: 3 << 30,
			Procs: []Process{{PID: 612}, {PID: 640}}},
		{Key: "WindowServer", Name: "WindowServer", CPU: 5, Procs: []Process{{PID: 160}}},
	}
}

func firedFor(fs []firing) []string {
	var names []string
	for _, f := range fs {
		names = append(names, f.hook.cond+" "+f.group.Name)
	}
	return names
}

func TestHookFiresOncePerCrossing(t *testing.T) {
	hs := newHookSet([]hook{mustHook(t, "cpu>80:true")}, 0)
	t0 := time.Date(2025, 11, 2, 22, 0, 0, 0, time.UTC)
	steps := []struct {
		cpu  float64
		fire bool
	}{
		{50, false},
		{90, true},  // crossed
		{95, false}, // still over: no repeat
		{80, false}, // not strictly over: re-armed
		{81, true},  // crossed again
		{120, false},
	}
	for i, st := range steps {
		got := hs.check(chrome(st.cpu), t0.Add(time.Duration(i)*2*time.Second))
		if fired := len(got) == 1; fired != st.fire {
			t.Errorf("step %d (cpu %.0f): fired %v, want %v", i, st.cpu, firedFor(got), st.fire)
		}
	}
}

func TestHookCooldown(t *testing.T) {
	hs := newHookSet([]hook{mustHook(t, "cpu>80:true")}, 5*time.Minute)
	t0 := time.Date(2025, 11, 2, 22, 0, 0, 0, time.UTC)
	at := func(d time.Duration, cpu float64) int { return len(hs.check(chrome(cpu), t0.Add(d))) }

	if at(0, 90) != 1 {
		t.Fatal("first crossing should fire")
	}
	at(10*time.Second, 10) // drops: re-armed, but cooling down
	if at(20*time.Second, 90) != 0 {
		t.Error("crossing during cooldown fired")
	}
	if at(4*time.Minute, 90) != 0 {
		t.Error("held crossing fired before the cooldown ended")
	}
	if at(5*time.Minute, 90) != 1 {
		t.Error("held crossing should fire once the cooldown is over")
	}
	if at(11*time.Minute, 90) != 0 {
		t.Error("still over the threshold: should not fire again")
	}
}

func TestHookCooldownIsPerGroup(t *testing.T) {
	hs := newHookSet([]hook{mustHook(t, "cpu>1:true")}, time.Hour)
	t0 := time.Date(2025, 11, 2, 22, 0, 0, 0, time.UTC)
	got := firedFor(hs.check(chrome(90), t0))
	want := []string{"cpu>1 Google Chrome", "cpu>1 WindowServer"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("fired %v, want %v", got, want)
	}
}

func TestHookRearmsWhenGroupExits(t *testing.T) {
	hs := newHookSet([]hook{mustHook(t, "count>1:true")}, 0)
	t0 := time.Date(2025, 11, 2, 22, 0, 0, 0, time.UTC)
	if len(hs.check(chrome(0), t0)) != 1 {
		t.Fatal("should fire")
	}
	hs.check(chrome(0)[1:], t0.Add(time.Second)) // Chrome quit
	if len(hs.state) != 0 {
		t.Errorf("state not pruned: %v", hs.state)
	}
	if len(hs.check(chrome(0), t0.Add(2*time.Second))) != 1 {
		t.Error("should fire again after the app came back")
	}
}

func TestMultipleHooks(t *testing.T) {
	hs := newHookSet([]hook{
		mustHook(t, "cpu>80:true"),
		mustHook(t, "mem>2G:true"),
		mustHook(t, "count>50:true"),
	}, 0)
	got := firedFor(hs.check(chrome(90), time.Now()))
	want := []string{"cpu>80 Google Chrome", "mem>2G Google Chrome"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("fired %v, want %v", got, want)
	}
}

func TestFiringEnv(t *testing.T) {
	h := mustHook(t, "mem>2G:true")
	f := firing{hook: &h, group: Group{
		Name: "Google Chrome", CPU: 52.5, RSS: 3113851904,
		Procs: []Process{{PID: 706}, {PID: 701}, {PID: 612}},
	}}
	want := []string{
		"MTOP_APP=Google Chrome",
		"MTOP_CPU=52.5",
		"MTOP_MEM_BYTES=3113851904",
		"MTOP_COUNT=3",
		"MTOP_PIDS=706 701 612",
		"MTOP_HOOK=mem>2G",
	}
	if got := f.env(); !reflect.DeepEqual(got, want) {
		t.Errorf("env = %q", got)
	}
	if got := f.String(); got != "hook mem>2G fired for Google Chrome (cpu 52.5%, mem 2.9G, 3 processes)" {
		t.Errorf("String = %q", got)
	}
}

func TestStartHookRunsShell(t *testing.T) {
	out := filepath.Join(t.TempDir(), "env.txt")
	h := mustHook(t, `cpu>80:printf '%s|%s|%s|%s|%s\n' "$MTOP_APP" "$MTOP_CPU" "$MTOP_MEM_BYTES" "$MTOP_COUNT" "$MTOP_PIDS" > `+out)
	f := firing{hook: &h, group: chrome(93.3)[0]}

	done := make(chan hookResult, 1)
	if err := startHook(f, nil, nil, done); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("hook failed: %v", r.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("hook did not finish")
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if want := "Google Chrome|93.3|3221225472|2|612 640\n"; string(got) != want {
		t.Errorf("hook saw %q, want %q", got, want)
	}
}

func TestStartHookReportsFailure(t *testing.T) {
	h := mustHook(t, "cpu>1:exit 3")
	done := make(chan hookResult, 1)
	if err := startHook(firing{hook: &h, group: chrome(5)[0]}, nil, nil, done); err != nil {
		t.Fatal(err)
	}
	if r := <-done; r.err == nil || !strings.Contains(r.err.Error(), "exit status 3") {
		t.Errorf("err = %v", r.err)
	}
}

func TestWatcherStep(t *testing.T) {
	procs := loadFixture(t, "ps_darwin.txt")
	var logs bytes.Buffer
	var started []string
	w := &watcher{
		hooks:    newHookSet([]hook{mustHook(t, "cpu>50:true"), mustHook(t, "count>10:true")}, time.Minute),
		snapshot: func() ([]Process, error) { return procs, nil },
		start: func(f firing) error {
			started = append(started, f.hook.cond+" "+f.group.Name)
			return nil
		},
		log: log.New(&logs, "", 0),
	}
	t0 := time.Date(2025, 11, 2, 22, 32, 0, 0, time.UTC)
	w.step(t0)
	w.step(t0.Add(2 * time.Second))

	want := []string{"cpu>50 Google Chrome", "count>10 Google Chrome"}
	if !reflect.DeepEqual(started, want) {
		t.Errorf("started %v, want %v", started, want)
	}
	wantLog := "hook cpu>50 fired for Google Chrome (cpu 52.5%, mem 2.9G, 14 processes)\n" +
		"hook count>10 fired for Google Chrome (cpu 52.5%, mem 2.9G, 14 processes)\n"
	if logs.String() != wantLog {
		t.Errorf("log = %q", logs.String())
	}
}
