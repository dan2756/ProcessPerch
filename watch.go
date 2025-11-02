package main

import (
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// watcher is the headless mode: no UI, just evaluate hooks every interval
// and log each firing.
type watcher struct {
	hooks    *hookSet
	snapshot func() ([]Process, error)
	start    func(firing) error
	log      *log.Logger
}

// step takes one snapshot and starts whatever hooks it triggers.
func (w *watcher) step(now time.Time) {
	procs, err := w.snapshot()
	if err != nil {
		w.log.Print(err)
		return
	}
	for _, f := range w.hooks.check(groupProcesses(procs), now) {
		w.log.Print(f)
		if err := w.start(f); err != nil {
			w.log.Printf("hook %s for %s failed to start: %v", f.hook.cond, f.group.Name, err)
		}
	}
}

// runWatch runs the watcher until SIGINT, SIGTERM or SIGHUP. Firings are
// logged to stderr; hook commands inherit stdout and stderr.
func runWatch(opts options, stdout, stderr io.Writer) error {
	done := make(chan hookResult, 16)
	w := &watcher{
		hooks:    newHookSet(opts.hooks, opts.cooldown),
		snapshot: listProcesses,
		start: func(f firing) error {
			return startHook(f, stdout, stderr, done)
		},
		log: log.New(stderr, "mtop: ", log.LstdFlags),
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigs)

	w.log.Printf("watching %d %s every %s (cooldown %s)",
		len(opts.hooks), plural(len(opts.hooks), "hook", "hooks"), opts.interval, opts.cooldown)
	tick := time.NewTicker(opts.interval)
	defer tick.Stop()
	w.step(time.Now())
	for {
		select {
		case now := <-tick.C:
			w.step(now)
		case r := <-done:
			if r.err != nil {
				w.log.Printf("hook %s for %s failed: %v", r.firing.hook.cond, r.firing.group.Name, r.err)
			}
		case <-sigs:
			return nil
		}
	}
}
