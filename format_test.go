package main

import "testing"

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		n    uint64
		want string
	}{
		{0, "0B"},
		{1023, "1023B"},
		{1024, "1.0K"},
		{14880 * 1024, "15M"},
		{512 * 1024, "512K"},
		{1000 * 1024, "1.0M"},
		{1536 * 1024 * 1024, "1.5G"},
		{2 << 30, "2.0G"},
		{100 << 30, "100G"},
		{1 << 40, "1.0T"},
	}
	for _, tt := range tests {
		if got := formatBytes(tt.n); got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestTruncateAndPad(t *testing.T) {
	if got := truncate("Google Chrome", 6); got != "Googl…" {
		t.Errorf("truncate = %q", got)
	}
	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate = %q", got)
	}
	if got := truncate("anything", 0); got != "" {
		t.Errorf("truncate to 0 = %q", got)
	}
	if got := pad("Café", 6); got != "Café  " {
		t.Errorf("pad = %q", got)
	}
}

func TestWideAndCombiningWidth(t *testing.T) {
	const nfd = "Übersicht" // Übersicht with a combining diaeresis
	tests := []struct {
		s    string
		want int
	}{
		{"Xcode", 5},
		{nfd, 9},
		{"漢字漢字漢", 10},
		{"ｶﾀｶﾅ", 4}, // halfwidth katakana
		{"ＡＢ", 4},
		{"한글", 4},
	}
	for _, tt := range tests {
		if got := displayWidth(tt.s); got != tt.want {
			t.Errorf("displayWidth(%q) = %d, want %d", tt.s, got, tt.want)
		}
	}

	if got := pad(nfd, 12); got != nfd+"   " {
		t.Errorf("pad NFD = %q", got)
	}
	if got := pad("漢字", 6); got != "漢字  " {
		t.Errorf("pad wide = %q", got)
	}
	// A wide character that does not fit before the ellipsis is dropped
	// whole, and pad fills the column it leaves.
	if got := truncate("漢字漢字漢", 6); got != "漢字…" {
		t.Errorf("truncate wide = %q", got)
	}
	if got := pad("漢字漢字漢", 6); got != "漢字… " {
		t.Errorf("pad wide = %q", got)
	}
	// Combining marks stay with the letter they belong to.
	if got := truncate(nfd, 3); got != "Üb…" {
		t.Errorf("truncate NFD = %q", got)
	}
}

func TestJoinPIDs(t *testing.T) {
	if got := joinPIDs([]int{612, 640, 641}, " "); got != "612 640 641" {
		t.Errorf("joinPIDs = %q", got)
	}
	if got := joinPIDs(nil, " "); got != "" {
		t.Errorf("joinPIDs(nil) = %q", got)
	}
}
