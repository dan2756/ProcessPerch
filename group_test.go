package main

import (
	"reflect"
	"testing"
)

func TestAppBundle(t *testing.T) {
	tests := []struct {
		comm, bundle, name string
	}{
		{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Google Chrome.app", "Google Chrome"},
		{"/Applications/Google Chrome.app/Contents/Frameworks/Google Chrome Framework.framework/Versions/141.0.7390.108/Helpers/Google Chrome Helper (GPU).app/Contents/MacOS/Google Chrome Helper (GPU)",
			"/Applications/Google Chrome.app", "Google Chrome"},
		{"/Applications/Visual Studio Code.app/Contents/Frameworks/Code Helper (Plugin).app/Contents/MacOS/Code Helper (Plugin)",
			"/Applications/Visual Studio Code.app", "Visual Studio Code"},
		{"/Applications/Xcode.app/Contents/Developer/Applications/Simulator.app/Contents/MacOS/Simulator",
			"/Applications/Xcode.app", "Xcode"},
		{"/System/Library/CoreServices/Dock.app/Contents/XPCServices/com.apple.dock.extra.xpc/Contents/MacOS/com.apple.dock.extra",
			"/System/Library/CoreServices/Dock.app", "Dock"},
		{"/private/var/folders/k2/x7v1/T/AppTranslocation/3F2A/d/Rectangle.app/Contents/MacOS/Rectangle",
			"/private/var/folders/k2/x7v1/T/AppTranslocation/3F2A/d/Rectangle.app", "Rectangle"},
		{"/Users/dj/Applications/Odd.APP/Contents/MacOS/odd", "/Users/dj/Applications/Odd.APP", "Odd"},
		{"Relative.app/Contents/MacOS/Relative", "Relative.app", "Relative"},
	}
	for _, tt := range tests {
		bundle, name, ok := appBundle(tt.comm)
		if !ok || bundle != tt.bundle || name != tt.name {
			t.Errorf("appBundle(%q) = %q, %q, %v; want %q, %q", tt.comm, bundle, name, ok, tt.bundle, tt.name)
		}
	}

	for _, comm := range []string{
		"kernel_task",
		"/sbin/launchd",
		"/System/Library/Frameworks/WebKit.framework/Versions/A/XPCServices/com.apple.WebKit.WebContent.xpc/Contents/MacOS/com.apple.WebKit.WebContent",
		"/Users/dj/src/my.application/bin/server",
		"/Users/dj/bin/tool.app", // an executable that happens to end in .app
		"/opt/.app/bin/x",        // ".app" alone is not a bundle name
		"kworker/0:1-events",
	} {
		if bundle, name, ok := appBundle(comm); ok {
			t.Errorf("appBundle(%q) = %q, %q; want no bundle", comm, bundle, name)
		}
	}
}

func TestProcessName(t *testing.T) {
	tests := map[string]string{
		"/sbin/launchd":        "launchd",
		"./bin/server":         "server",
		"-zsh":                 "zsh",
		"-":                    "-",
		"kernel_task":          "kernel_task",
		"kworker/0:1H-kblockd": "kworker/0:1H-kblockd",
		"tmux: server":         "tmux: server",
		"<defunct>":            "<defunct>",
	}
	for comm, want := range tests {
		if got := processName(comm); got != want {
			t.Errorf("processName(%q) = %q, want %q", comm, got, want)
		}
	}
}

func findGroup(groups []Group, name string) *Group {
	for i := range groups {
		if groups[i].Name == name {
			return &groups[i]
		}
	}
	return nil
}

func TestGroupDarwinFixture(t *testing.T) {
	groups := groupProcesses(loadFixture(t, "ps_darwin.txt"))

	tests := []struct {
		name  string
		count int
		cpu   float64
	}{
		{"Google Chrome", 14, 52.5},
		{"Visual Studio Code", 7, 14.1},
		{"Slack", 4, 3.4},
		{"Docker", 4, 16.8},
		{"Xcode", 3, 4.6},
		{"Dock", 2, 0.3},
		{"Rectangle", 1, 0},
		{"1Password", 2, 0.5},
		{"com.apple.WebKit.WebContent", 2, 1.6},
		{"cfprefsd", 2, 0},
		{"postgres", 2, 0},
		{"gopls", 1, 0.9},
		{"zsh", 1, 0},
		{"server", 1, 0},
		{"kernel_task", 1, 6.1},
	}
	for _, tt := range tests {
		g := findGroup(groups, tt.name)
		if g == nil {
			t.Errorf("no group %q", tt.name)
			continue
		}
		if len(g.Procs) != tt.count || g.CPU != tt.cpu {
			t.Errorf("%s: %d procs, %.1f%% cpu; want %d, %.1f%%", tt.name, len(g.Procs), g.CPU, tt.count, tt.cpu)
		}
	}

	chrome := findGroup(groups, "Google Chrome")
	if chrome.Bundle != "/Applications/Google Chrome.app" {
		t.Errorf("chrome bundle = %q", chrome.Bundle)
	}
	var rss uint64
	for _, p := range chrome.Procs {
		rss += p.RSS
	}
	if chrome.RSS != rss {
		t.Errorf("chrome RSS = %d, want %d", chrome.RSS, rss)
	}
	// Busiest process first.
	if pids := chrome.PIDs(); pids[0] != 706 || pids[1] != 701 {
		t.Errorf("chrome pids = %v", pids)
	}

	for _, name := range []string{"Google Chrome Helper", "Code Helper", "Simulator", "Docker Desktop"} {
		if findGroup(groups, name) != nil {
			t.Errorf("helper %q got its own group", name)
		}
	}
}

func TestGroupLinuxFixture(t *testing.T) {
	groups := groupProcesses(loadFixture(t, "ps_linux.txt"))
	for name, count := range map[string]int{
		"systemd":         2,
		"bash":            2,
		"Isolated Web Co": 2,
		"Web Content":     2,
		"firefox":         1,
		"tmux: server":    1,
	} {
		g := findGroup(groups, name)
		if g == nil || len(g.Procs) != count {
			t.Errorf("group %q = %+v, want %d procs", name, g, count)
		}
	}
}

func TestSortByCPU(t *testing.T) {
	groups := []Group{
		{Key: "b", Name: "beta", CPU: 1},
		{Key: "a", Name: "Alpha", CPU: 1},
		{Key: "c", Name: "gamma", CPU: 5},
		{Key: "d", Name: "delta", CPU: 1, RSS: 10},
	}
	sortByCPU(groups)
	var got []string
	for _, g := range groups {
		got = append(got, g.Name)
	}
	want := []string{"gamma", "delta", "Alpha", "beta"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}
