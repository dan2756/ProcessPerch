package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// formatBytes renders a byte count in short binary units: 512K, 14M, 1.2G.
// The result is at most four characters wide for anything under 1000 PiB.
func formatBytes(n uint64) string {
	if n < 1024 {
		return strconv.FormatUint(n, 10) + "B"
	}
	const units = "KMGTPE"
	v := float64(n)
	for i := 0; i < len(units); i++ {
		v /= 1024
		if v < 999.5 || i == len(units)-1 {
			if v < 9.95 {
				return fmt.Sprintf("%.1f%c", v, units[i])
			}
			return fmt.Sprintf("%.0f%c", v, units[i])
		}
	}
	panic("unreachable")
}

// parseBytes parses a size such as 2G, 1.5GB, 512MiB, 800k or a plain byte
// count. Units are binary (1K = 1024 bytes).
func parseBytes(s string) (uint64, error) {
	t := strings.ToUpper(strings.TrimSpace(s))
	t = strings.TrimSuffix(t, "IB")
	t = strings.TrimSuffix(t, "B")
	mult := 1.0
	if n := len(t); n > 0 {
		if i := strings.IndexByte("KMGTPE", t[n-1]); i >= 0 {
			mult = math.Pow(1024, float64(i+1))
			t = t[:n-1]
		}
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
	if err != nil || v < 0 || math.IsNaN(v) || math.IsInf(v, 0) || v*mult >= math.MaxInt64 {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	return uint64(v * mult), nil
}

func roundTenth(v float64) float64 {
	return math.Round(v*10) / 10
}

// joinPIDs formats pids separated by sep.
func joinPIDs(pids []int, sep string) string {
	var b strings.Builder
	for i, pid := range pids {
		if i > 0 {
			b.WriteString(sep)
		}
		b.WriteString(strconv.Itoa(pid))
	}
	return b.String()
}

// wide holds the East Asian wide and fullwidth ranges, which take two
// terminal columns: CJK, kana, Hangul, fullwidth forms and the common emoji
// blocks.
var wide = &unicode.RangeTable{
	R16: []unicode.Range16{
		{0x1100, 0x115f, 1},
		{0x2329, 0x232a, 1},
		{0x2e80, 0x303e, 1},
		{0x3041, 0x33ff, 1},
		{0x3400, 0x4dbf, 1},
		{0x4e00, 0x9fff, 1},
		{0xa000, 0xa4cf, 1},
		{0xa960, 0xa97f, 1},
		{0xac00, 0xd7a3, 1},
		{0xf900, 0xfaff, 1},
		{0xfe10, 0xfe19, 1},
		{0xfe30, 0xfe6f, 1},
		{0xff00, 0xff60, 1},
		{0xffe0, 0xffe6, 1},
	},
	R32: []unicode.Range32{
		{0x1f300, 0x1f64f, 1},
		{0x1f900, 0x1f9ff, 1},
		{0x20000, 0x2fffd, 1},
		{0x30000, 0x3fffd, 1},
	},
}

// runeWidth is the number of terminal columns r takes up. Combining marks
// (as in the decomposed file names macOS often uses) and other zero-width
// characters take none.
func runeWidth(r rune) int {
	switch {
	case unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf):
		return 0
	case unicode.Is(wide, r):
		return 2
	}
	return 1
}

// displayWidth is the number of terminal columns s takes up.
func displayWidth(s string) int {
	n := 0
	for _, r := range s {
		n += runeWidth(r)
	}
	return n
}

// truncate shortens s to at most n terminal columns, marking the cut with
// an ellipsis.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if displayWidth(s) <= n {
		return s
	}
	w := 0
	for i, r := range s {
		if w += runeWidth(r); w > n-1 {
			return s[:i] + "…"
		}
	}
	return s
}

// pad left-aligns s in a field of n terminal columns, truncating if needed.
func pad(s string, n int) string {
	s = truncate(s, n)
	if w := displayWidth(s); w < n {
		s += strings.Repeat(" ", n-w)
	}
	return s
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
