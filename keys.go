package main

import (
	"io"
	"unicode/utf8"
)

type keyKind int

const (
	keyRune keyKind = iota
	keyUp
	keyDown
	keyPageUp
	keyPageDown
	keyHome
	keyEnd
	keyEnter
	keyEsc
	keyBackspace
	keyCtrlC
)

type key struct {
	kind keyKind
	r    rune // set for keyRune
}

// readKeys decodes key presses from r until it fails, then closes keys.
func readKeys(r io.Reader, keys chan<- key) {
	defer close(keys)
	buf := make([]byte, 256)
	for {
		n, err := r.Read(buf)
		for _, k := range parseKeys(buf[:n]) {
			keys <- k
		}
		if err != nil {
			return
		}
	}
}

// parseKeys decodes one read's worth of raw terminal input. Terminals send
// an escape sequence in a single write, so a lone ESC byte at the end of a
// read is the Escape key itself.
func parseKeys(b []byte) []key {
	var keys []key
	for len(b) > 0 {
		c := b[0]
		switch {
		case c == 0x1b && len(b) > 1 && (b[1] == '[' || b[1] == 'O'):
			// CSI or SS3: parameter bytes, then a final byte in 0x40-0x7e.
			i := 2
			for i < len(b) && (b[i] < 0x40 || b[i] > 0x7e) {
				i++
			}
			if i == len(b) {
				return keys // truncated sequence; drop it
			}
			if k, ok := csiKeys[string(b[2:i+1])]; ok {
				keys = append(keys, key{kind: k})
			}
			b = b[i+1:]
		case c == 0x1b:
			keys = append(keys, key{kind: keyEsc})
			b = b[1:]
		case c == '\r' || c == '\n':
			keys = append(keys, key{kind: keyEnter})
			b = b[1:]
		case c == 0x7f || c == 0x08:
			keys = append(keys, key{kind: keyBackspace})
			b = b[1:]
		case c == 0x03:
			keys = append(keys, key{kind: keyCtrlC})
			b = b[1:]
		case c < 0x20:
			b = b[1:] // other control keys are ignored
		default:
			r, size := utf8.DecodeRune(b)
			keys = append(keys, key{kind: keyRune, r: r})
			b = b[size:]
		}
	}
	return keys
}

var csiKeys = map[string]keyKind{
	"A":  keyUp,
	"B":  keyDown,
	"5~": keyPageUp,
	"6~": keyPageDown,
	"H":  keyHome,
	"1~": keyHome,
	"7~": keyHome,
	"F":  keyEnd,
	"4~": keyEnd,
	"8~": keyEnd,
}
