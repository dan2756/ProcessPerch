package main

import (
	"path"
	"sort"
	"strings"
)

// Group is a set of processes that belong to the same app.
type Group struct {
	Key    string  // outermost bundle path for apps, process name otherwise
	Name   string  // display name, e.g. "Google Chrome"
	Bundle string  // outermost .app bundle path, empty for plain processes
	CPU    float64 // summed %CPU
	RSS    uint64  // summed resident memory in bytes
	Procs  []Process
}

// PIDs returns the group's process IDs in display order.
func (g *Group) PIDs() []int {
	pids := make([]int, len(g.Procs))
	for i, p := range g.Procs {
		pids[i] = p.PID
	}
	return pids
}

// groupProcesses rolls processes up by owning app. Groups come back in the
// order their first process appeared; callers sort them.
func groupProcesses(procs []Process) []Group {
	var groups []Group
	index := make(map[string]int)
	for _, p := range procs {
		key, name, bundle := classify(p.Comm)
		i, ok := index[key]
		if !ok {
			i = len(groups)
			index[key] = i
			groups = append(groups, Group{Key: key, Name: name, Bundle: bundle})
		}
		g := &groups[i]
		g.CPU += p.CPU
		g.RSS += p.RSS
		g.Procs = append(g.Procs, p)
	}
	for i := range groups {
		// Round away float noise from summing so equal totals sort as equal.
		groups[i].CPU = roundTenth(groups[i].CPU)
		sortProcs(groups[i].Procs)
	}
	return groups
}

// classify returns the grouping key, display name and bundle path for a
// ps comm value.
func classify(comm string) (key, name, bundle string) {
	if b, n, ok := appBundle(comm); ok {
		return b, n, b
	}
	n := processName(comm)
	return n, n, ""
}

// appBundle finds the outermost .app bundle in an executable path.
//
// Helpers live in bundles nested inside the app, e.g.
//
//	/Applications/Google Chrome.app/Contents/Frameworks/Google Chrome
//	Framework.framework/Versions/X/Helpers/Google Chrome Helper (GPU).app/
//	Contents/MacOS/Google Chrome Helper (GPU)
//
// so the first path component ending in ".app" is the one the user thinks
// of as the app. The last component is the executable itself and is never
// treated as a bundle.
func appBundle(comm string) (bundlePath, name string, ok bool) {
	if !strings.Contains(comm, "/") {
		return "", "", false
	}
	parts := strings.Split(comm, "/")
	for i, part := range parts[:len(parts)-1] {
		if len(part) > len(".app") && strings.EqualFold(part[len(part)-4:], ".app") {
			return strings.Join(parts[:i+1], "/"), part[:len(part)-4], true
		}
	}
	return "", "", false
}

// processName is the display name for a process that is not part of an app
// bundle. On macOS comm is usually a full path; on Linux it is already a
// short task name that may itself contain slashes (kworker/0:1), so only
// things that look like paths are shortened.
func processName(comm string) string {
	name := comm
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, "./") || strings.HasPrefix(name, "../") {
		name = path.Base(name)
	}
	// Login shells show up as "-zsh".
	if len(name) > 1 && name[0] == '-' {
		name = name[1:]
	}
	return name
}

// sortProcs orders a group's processes busiest first.
func sortProcs(procs []Process) {
	sort.SliceStable(procs, func(i, j int) bool {
		a, b := procs[i], procs[j]
		if a.CPU != b.CPU {
			return a.CPU > b.CPU
		}
		if a.RSS != b.RSS {
			return a.RSS > b.RSS
		}
		return a.PID < b.PID
	})
}

// sortByCPU orders groups by total CPU, then memory, then name.
func sortByCPU(groups []Group) {
	sort.SliceStable(groups, func(i, j int) bool {
		a, b := &groups[i], &groups[j]
		if a.CPU != b.CPU {
			return a.CPU > b.CPU
		}
		if a.RSS != b.RSS {
			return a.RSS > b.RSS
		}
		return lessName(a, b)
	})
}

func lessName(a, b *Group) bool {
	la, lb := strings.ToLower(a.Name), strings.ToLower(b.Name)
	if la != lb {
		return la < lb
	}
	return a.Key < b.Key
}
