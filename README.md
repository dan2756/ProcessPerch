# mtop

A Mac task manager that groups helper processes by the app that owns them.

Activity Monitor and `top` show Google Chrome as a dozen "Google Chrome Helper
(Renderer)" rows and VS Code as a pile of "Code Helper" processes. `mtop` rolls
them up so you get one line per app: how many processes it runs, how much CPU
and memory they use together, and their PIDs. One key kills the whole app,
`--json` gives you the same view for scripts, and threshold hooks run a
command when an app gets out of hand.

```
mtop  412 processes in 31 groups  sort cpu  every 2s                  19:38:13
   APP                         PROCS   CPU%   MEM  PIDS
>▸ Google Chrome                  14   52.5  2.9G  706 701 640 612 702 709 64…
 ▸ Docker                          4   16.8  2.4G  2130 2101 2112 2110
 ▸ Visual Studio Code              7   14.1  1.1G  1215 1230 1210 1204 1240 1…
   WindowServer                    1    9.8  280M  160
   kernel_task                     1    6.1  4.5M  0
 ▸ Xcode                           3    4.6  1.7G  1900 1905 1906
 ▸ Slack                           4    3.4  695M  1520 1502 1510 1511
x term  X kill  enter expand  s sort  / filter  q quit
```

Press `enter` on a group to see what is inside it:

```
>▾ Google Chrome                        14   52.5  2.9G  706 701 640 612 702 …
     Google Chrome Helper (Renderer)         21.7  717M  706
     Google Chrome Helper (Renderer)         12.3  501M  701
     Google Chrome Helper (GPU)               8.9  292M  640
     Google Chrome                            4.2  403M  612
```

## Install

```
go install github.com/dan2756/mtop@latest
```

Requires Go 1.24 or newer to build. mtop is a single binary; at runtime it
only needs `ps` and `sh`.

## Usage

```
mtop                    # interactive view, refreshes every 2s
mtop -n 1               # refresh every second
mtop --sort mem         # start sorted by memory
mtop --json --top 5     # the five busiest apps as JSON, then exit

# say something when any app goes over 80% CPU
mtop --hook 'cpu>80:say "$MTOP_APP is hot"'

# no UI: watch for runaway apps and log what fired
mtop --watch -n 5 --hook 'mem>4G:osascript -e "display notification \"$MTOP_APP is over 4G\""'
```

| Flag | Meaning |
| --- | --- |
| `-n`, `--interval DUR` | refresh interval: `2s`, `500ms`, or plain seconds (`0.5`); default `2s`, minimum `100ms` |
| `--json` | print one snapshot of the groups as JSON and exit |
| `--top N` | only show the first N groups (after sorting and filtering) |
| `--sort KEY` | initial order: `cpu` (default), `mem`, `name` or `count` |
| `--hook SPEC` | run a command when an app crosses a threshold; repeatable (see [Hooks](#hooks)) |
| `--hook-cooldown DUR` | fire each hook at most once per DUR for a given app; default `5m` |
| `--watch` | headless: no UI, evaluate hooks every interval and log firings to stderr |
| `-h`, `--help` | show usage |
| `--version` | print the version |

Flags take one or two dashes, and `--top=5` works as well as `--top 5`.

## Keys

| Key | Action |
| --- | --- |
| `j` / `k`, arrows | move the cursor |
| `g` / `G`, PgUp / PgDn | jump to top / bottom, page |
| `enter`, `e` | expand or collapse the selected app to show its processes |
| `x` | send SIGTERM to every process of the selected app |
| `X` | send SIGKILL to every process of the selected app |
| `s` | cycle the sort order: cpu, mem, name, count |
| `/` | filter apps by name (case-insensitive); `enter` keeps the filter, `esc` clears it |
| `q`, `ctrl-c` | quit |

Killing is deliberately one key with no confirmation dialog. The status line
tells you what happened, e.g. `sent SIGTERM to 14 processes of Google Chrome`,
plus any processes that had already exited or could not be signalled
(`operation not permitted` for other users' processes unless you are root).

The cursor sticks to the app, not to the row: when a refresh reorders the
list, the selection moves with the app, so `x` hits what you selected.

**Safety:** PID 0 (`kernel_task`), PID 1 (`launchd`/`init`) and mtop itself
are never signalled, even when they are part of the selected group.
(`kill(0, sig)` would otherwise hit mtop's own process group.)

## JSON output

`mtop --json` prints one snapshot as an array of groups, in `--sort` order
(excerpt):

```json
[
  {
    "name": "Docker",
    "bundle": "/Applications/Docker.app",
    "count": 4,
    "cpu": 16.8,
    "mem_bytes": 2629828608,
    "pids": [
      2130,
      2101,
      2112,
      2110
    ]
  },
  {
    "name": "WindowServer",
    "count": 1,
    "cpu": 9.8,
    "mem_bytes": 293601280,
    "pids": [
      160
    ]
  }
]
```

`bundle` is only present for processes that live in an app bundle. `cpu` is
the sum of the processes' %CPU (100 = one core) and `mem_bytes` the sum of
their resident memory. To list every app using more than 1 GB:

```
mtop --json | jq -r '.[] | select(.mem_bytes > 1e9) | .name'
```

## Hooks

A hook is `METRIC>VALUE:COMMAND` (or `>=`):

| Metric | Value |
| --- | --- |
| `cpu` | total %CPU of the app, where 100 is one full core (`cpu>150` = 1.5 cores) |
| `mem` | total resident memory; `K`, `M`, `G`, `T` suffixes, binary units (`mem>2G`, `mem>1.5GB`) |
| `count` | number of processes (`count>50`) |

Everything after the first `:` is the command, so the command itself may
contain colons.

Hooks are checked against every app on every refresh. When an app goes over
the threshold the command runs once; it fires again only after the app has
dropped back under the threshold (or quit) and crossed again. On top of that,
`--hook-cooldown` (default 5m) limits each hook to one firing per app per
cooldown. A crossing that happens during the cooldown is not lost: if the app
is still over the threshold when the cooldown ends, the hook fires then.
Apps that are already over a threshold when mtop starts fire on the first
refresh.

The command runs in the background with `sh -c`, in its own process group so
quitting mtop (including with Ctrl-C) does not signal it, stdin from
`/dev/null`, and mtop's environment plus:

| Variable | Example |
| --- | --- |
| `MTOP_APP` | `Google Chrome` |
| `MTOP_CPU` | `52.5` |
| `MTOP_MEM_BYTES` | `3074752512` |
| `MTOP_COUNT` | `14` |
| `MTOP_PIDS` | `706 701 640 612 ...` (space separated) |
| `MTOP_HOOK` | `cpu>80` (the condition that fired) |

Hooks apply to every app; to target one, test `MTOP_APP` in the command:

```
mtop --watch --hook 'cpu>200:if [ "$MTOP_APP" = "Google Chrome" ]; then kill $MTOP_PIDS; fi'
```

Use `if`, not `[ ... ] && ...`: the `&&` form exits 1 for every other app,
which is then reported as a failed hook. The hook still fires, and is
logged, for every app over the threshold.

In the interactive view hook output is discarded (it would draw over the
screen) and the status line shows what fired or failed. In `--watch` mode
each firing is logged to stderr and the command's own output goes to mtop's
stdout and stderr:

```
$ mtop --watch --hook 'count>10:echo "$MTOP_APP: $MTOP_COUNT processes"'
mtop: 2026/04/11 19:02:02 watching 1 hook every 2s (cooldown 5m0s)
mtop: 2026/04/11 19:02:02 hook count>10 fired for Google Chrome (cpu 52.5%, mem 2.9G, 14 processes)
Google Chrome: 14 processes
```

A hook that exits non-zero is logged as `hook ... failed: exit status N`.
`--watch` runs until interrupted (SIGINT, SIGTERM or SIGHUP); run it from a
LaunchAgent or a spare `tmux` window to keep it around.

## How grouping works

A process belongs to the **outermost** `.app` bundle in its executable path.
Chrome's helpers live in bundles nested deep inside Chrome's own bundle:

```
/Applications/Google Chrome.app/Contents/Frameworks/Google Chrome Framework.framework/
  Versions/141.0.7390.108/Helpers/Google Chrome Helper (Renderer).app/Contents/MacOS/
  Google Chrome Helper (Renderer)
```

so they are all counted under **Google Chrome**. The same goes for Electron
apps (`Code Helper` under Visual Studio Code, `Slack Helper` under Slack),
apps shipped inside other apps (Xcode's `Simulator.app` counts as Xcode) and
XPC services inside an app bundle. Two copies of the same app in different
places are two groups.

Processes outside an app bundle are grouped by executable name: every
`cfprefsd` together, login shells (`-zsh`) with `zsh`.

Data comes from `ps -axww -o pid=,ppid=,pcpu=,rss=,comm=`, which works with
both the BSD `ps` on macOS and procps on Linux. Lines that cannot be parsed
are skipped instead of failing the whole snapshot, and mtop's own `ps` child
is left out.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | normal exit, including `q`, `ctrl-c`, and SIGINT/SIGTERM/SIGHUP |
| 1 | runtime error: `ps` failed, or the interactive view was started without a terminal |
| 2 | bad flags or arguments |

## Platform notes

- **macOS** is the main target. `%CPU` from `ps` is the kernel's decaying
  average of recent usage, so it moves a little more smoothly than `top`'s
  per-interval numbers.
- **Memory** is resident set size (RSS). Pages shared between processes are
  counted once per process, so an app's total can be higher than Activity
  Monitor's "Memory" column, which uses physical footprint.
- **Safari and other WebKit apps** run web content in
  `com.apple.WebKit.WebContent` processes that live in a system framework, not
  inside Safari.app, so those form their own group.
- **Locales:** `ps` is run with `LC_NUMERIC=C`, so decimal-comma locales
  (`de_DE`, `fr_FR`, ...) do not break %CPU parsing. Non-ASCII app names still
  display as-is.
- **Linux** has no app bundles, so everything is grouped by process name. The
  kernel truncates names to 15 characters (Firefox content processes show up
  as `Isolated Web Co`), and procps reports `%CPU` averaged over each
  process's whole lifetime, so it reacts slowly. Keep that in mind for `cpu`
  hooks.
- mtop needs no special permissions, but it can only signal processes you own
  unless run as root.

## License

MIT
