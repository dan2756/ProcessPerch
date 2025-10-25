package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
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

// truncate shortens s to at most n runes, marking the cut with an ellipsis.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n-1]) + "…"
}

// pad left-aligns s in a field of n runes, truncating if needed.
func pad(s string, n int) string {
	s = truncate(s, n)
	if c := utf8.RuneCountInString(s); c < n {
		s += strings.Repeat(" ", n-c)
	}
	return s
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
