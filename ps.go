package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
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

// listProcesses takes one snapshot of the process table. The ps process
// itself is left out: it is ours and gone by the time anyone looks.
func listProcesses() ([]Process, error) {
	cmd := exec.Command("ps", psArgs...)
	cmd.Env = psEnv(os.Environ())
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("ps: %v: %s", err, bytes.TrimSpace(ee.Stderr))
		}
		return nil, fmt.Errorf("ps: %w", err)
	}
	procs, err := parsePS(bytes.NewReader(out))
	if err != nil {
		return nil, err
	}
	return withoutPID(procs, cmd.Process.Pid), nil
}

// psEnv forces C number formatting for ps. The BSD ps on macOS honours
// LC_NUMERIC, so under a German locale %cpu comes out as "12,5". Only the
// numeric category is overridden: with LC_ALL=C, ps would also stop
// printing non-ASCII app names ("Übersicht") as-is.
func psEnv(env []string) []string {
	var all string
	out := make([]string, 0, len(env)+2)
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "LC_ALL="); ok {
			all = v
			continue
		}
		if !strings.HasPrefix(kv, "LC_NUMERIC=") {
			out = append(out, kv)
		}
	}
	if all != "" {
		// LC_ALL beats every LC_* variable, so it has to go; hand its
		// value to LC_CTYPE, the category that decides how names print.
		kept := out[:0]
		for _, kv := range out {
			if !strings.HasPrefix(kv, "LC_CTYPE=") {
				kept = append(kept, kv)
			}
		}
		out = append(kept, "LC_CTYPE="+all)
	}
	return append(out, "LC_NUMERIC=C")
}

func withoutPID(procs []Process, pid int) []Process {
	out := procs[:0]
	for _, p := range procs {
		if p.PID != pid {
			out = append(out, p)
		}
	}
	return out
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
	// Accept a decimal comma too, in case ps ignored LC_NUMERIC or the
	// output was captured elsewhere.
	cpu, err := strconv.ParseFloat(strings.Replace(fields[2], ",", ".", 1), 64)
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
