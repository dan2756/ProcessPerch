package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

// Process is one row of ps output.
type Process struct {
	PID  int
	PPID int
	CPU  float64 // percent of one core, as reported by ps
	RSS  uint64  // resident set size in bytes
	Comm string  // executable path on macOS, task name on Linux
}

// psArgs works with both the BSD ps on macOS and procps on Linux. The empty
// headers suppress the header line, and comm comes last because it is the
// only column that can contain spaces.
var psArgs = []string{"-axww", "-o", "pid=,ppid=,pcpu=,rss=,comm="}

// listProcesses takes one snapshot of the process table.
func listProcesses() ([]Process, error) {
	out, err := exec.Command("ps", psArgs...).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("ps: %v: %s", err, bytes.TrimSpace(ee.Stderr))
		}
		return nil, fmt.Errorf("ps: %w", err)
	}
	return parsePS(bytes.NewReader(out))
}

// parsePS parses the output of ps -o pid=,ppid=,pcpu=,rss=,comm=.
//
// Blank lines and a header line are ignored. A line that does not parse is
// skipped rather than failing the whole snapshot, since a process can exit
// or be renamed while ps is printing; parsePS only returns an error if
// nothing at all could be parsed.
func parsePS(r io.Reader) ([]Process, error) {
	var (
		procs    []Process
		firstErr error
		lineNo   int
	)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		lineNo++
		line := strings.TrimRight(sc.Text(), " \t\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		p, err := parsePSLine(line)
		if err != nil {
			if isPSHeader(line) {
				continue
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("ps output line %d: %w", lineNo, err)
			}
			continue
		}
		procs = append(procs, p)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading ps output: %w", err)
	}
	if len(procs) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return procs, nil
}

func parsePSLine(line string) (Process, error) {
	var fields [4]string
	rest := line
	for i := range fields {
		rest = strings.TrimLeft(rest, " \t")
		end := strings.IndexAny(rest, " \t")
		if end <= 0 {
			return Process{}, errors.New("too few columns")
		}
		fields[i], rest = rest[:end], rest[end:]
	}
	// Everything after the fourth column is the command, spaces and all.
	comm := strings.TrimLeft(rest, " \t")
	if comm == "" {
		return Process{}, errors.New("missing command")
	}

	pid, err := strconv.Atoi(fields[0])
	if err != nil || pid < 0 {
		return Process{}, fmt.Errorf("bad pid %q", fields[0])
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil || ppid < 0 {
		return Process{}, fmt.Errorf("bad ppid %q", fields[1])
	}
	cpu, err := strconv.ParseFloat(fields[2], 64)
	if err != nil || cpu < 0 {
		return Process{}, fmt.Errorf("bad %%cpu %q", fields[2])
	}
	rss, err := strconv.ParseUint(fields[3], 10, 64)
	if err != nil {
		return Process{}, fmt.Errorf("bad rss %q", fields[3])
	}
	return Process{
		PID:  pid,
		PPID: ppid,
		CPU:  cpu,
		RSS:  rss * 1024, // ps reports KiB on both platforms
		Comm: comm,
	}, nil
}

func isPSHeader(line string) bool {
	f := strings.Fields(line)
	return len(f) > 0 && f[0] == "PID"
}
