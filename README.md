# mtop

A Mac task manager that groups helper processes by the app that owns them.

Activity Monitor and `top` show Google Chrome as a dozen "Google Chrome Helper
(Renderer)" rows and VS Code as a pile of "Code Helper" processes. `mtop` rolls
them up so you see one line per app: how many processes it runs, how much CPU
and memory they use together, and which PIDs they are.

```
mtop  412 processes in 96 groups  every 2s                         19:38:13
  APP                     PROCS   CPU%   MEM  PIDS
> Google Chrome              14   52.5  2.9G  706 701 640 612 702 709 641 …
  Docker                      4   16.8  2.4G  2130 2101 2112 2110
  Visual Studio Code          7   14.1  1.1G  1215 1230 1210 1204 1240 …
  WindowServer                1    9.8  280M  160
```

## Install

```
go install github.com/dan2756/mtop@latest
```

## Usage

```
mtop                 # interactive view, refreshes every 2s
mtop -n 1            # refresh every second
mtop --json --top 5  # print the five busiest apps as JSON and exit
mtop --sort mem      # start sorted by memory
```

| Flag | Meaning |
| --- | --- |
| `-n`, `--interval DUR` | refresh interval: `2s`, `500ms`, or plain seconds (`0.5`); default `2s` |
| `--json` | print one snapshot of the groups as JSON and exit |
| `--top N` | only show the first N groups |
| `--sort KEY` | initial order: `cpu` (default), `mem`, `name` or `count` |
| `--version` | print the version |

## Keys

| Key | Action |
| --- | --- |
| `j` / `k`, arrows | move the cursor |
| `g` / `G`, PgUp / PgDn | jump to top / bottom, page |
| `enter`, `e` | expand or collapse the selected app to show its processes |
| `x` | send SIGTERM to every process of the selected app |
| `X` | send SIGKILL to every process of the selected app |
| `s` | cycle the sort order: cpu, mem, name, count |
| `/` | filter apps by name; `enter` keeps the filter, `esc` clears it |
| `q`, `ctrl-c` | quit |

Killing is deliberately one key with no confirmation dialog. The status line
tells you what happened, e.g. `sent SIGTERM to 14 processes of Google Chrome`.
PID 0, PID 1 and mtop itself are never signalled, even when they are part of
the selected group.

## How grouping works

A process belongs to the outermost `.app` bundle in its executable path, so
`/Applications/Google Chrome.app/Contents/Frameworks/.../Google Chrome Helper (GPU).app/...`
is counted under **Google Chrome**. Processes outside an app bundle are grouped
by name (all `cfprefsd` processes together). On Linux there are no bundles, so
everything is grouped by process name.

Data comes from `ps -axww -o pid=,ppid=,pcpu=,rss=,comm=`, which works on both
macOS and Linux.

## License

MIT
