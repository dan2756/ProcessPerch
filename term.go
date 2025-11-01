package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"golang.org/x/term"
)

const (
	escAltScreenOn  = "\x1b[?1049h"
	escAltScreenOff = "\x1b[?1049l"
	escHideCursor   = "\x1b[?25l"
	escShowCursor   = "\x1b[?25h"
	escHome         = "\x1b[H"
	escClearScreen  = "\x1b[2J"
	escClearLine    = "\x1b[K"
	escReset        = "\x1b[0m"
	escBold         = "\x1b[1m"
	escDim          = "\x1b[2m"
	escReverse      = "\x1b[7m"
)

// terminal owns the tty while the UI is running: raw mode on stdin and the
// alternate screen on stdout.
type terminal struct {
	in, out *os.File
	state   *term.State
	once    sync.Once
}

func openTerminal() (*terminal, error) {
	in, out := os.Stdin, os.Stdout
	if !term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(out.Fd())) {
		return nil, errors.New("not a terminal (use --json for a one-shot snapshot)")
	}
	state, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return nil, fmt.Errorf("entering raw mode: %w", err)
	}
	t := &terminal{in: in, out: out, state: state}
	io.WriteString(out, escAltScreenOn+escHideCursor+escHome+escClearScreen)
	return t, nil
}

// restore puts the terminal back the way we found it. It is safe to call
// more than once, so every exit path can call it.
func (t *terminal) restore() {
	t.once.Do(func() {
		io.WriteString(t.out, escReset+escShowCursor+escAltScreenOff)
		term.Restore(int(t.in.Fd()), t.state)
	})
}

// size reports the terminal size, falling back to 80x24.
func (t *terminal) size() (width, height int) {
	w, h, err := term.GetSize(int(t.out.Fd()))
	if err != nil || w <= 0 || h <= 0 {
		return 80, 24
	}
	return w, h
}

func (t *terminal) write(s string) {
	io.WriteString(t.out, s)
}
