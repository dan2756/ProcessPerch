package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseKeys(t *testing.T) {
	tests := []struct {
		in   string
		want []key
	}{
		{"j", []key{{kind: keyRune, r: 'j'}}},
		{"\x1b[A\x1b[B", []key{{kind: keyUp}, {kind: keyDown}}},
		{"\x1bOA", []key{{kind: keyUp}}}, // application cursor mode
		{"\x1b[5~\x1b[6~", []key{{kind: keyPageUp}, {kind: keyPageDown}}},
		{"\x1b[H\x1b[4~", []key{{kind: keyHome}, {kind: keyEnd}}},
		{"\x1b", []key{{kind: keyEsc}}},
		{"\r", []key{{kind: keyEnter}}},
		{"\x7f", []key{{kind: keyBackspace}}},
		{"\x03", []key{{kind: keyCtrlC}}},
		{"\x1b[1;5C", nil}, // ctrl-right: recognised and ignored
		{"é", []key{{kind: keyRune, r: 'é'}}},
		{"\x1b[", nil}, // truncated
		{"q\x01x", []key{{kind: keyRune, r: 'q'}, {kind: keyRune, r: 'x'}}},
	}
	for _, tt := range tests {
		if got := parseKeys([]byte(tt.in)); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("parseKeys(%q) = %+v, want %+v", tt.in, got, tt.want)
		}
	}
}

func TestReadKeys(t *testing.T) {
	keys := make(chan key, 8)
	readKeys(strings.NewReader("jk"), keys)
	var got []rune
	for k := range keys {
		got = append(got, k.r)
	}
	if string(got) != "jk" {
		t.Errorf("readKeys = %q", string(got))
	}
}
