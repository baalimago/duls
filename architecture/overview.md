# Overview

duls prints the size view of a directory: the view slivingdoc prints when an
initial pull materializes a notebook into an empty directory. It walks one
directory without reading content, counts the lines of the files the view can
show, and answers a colour terminal with the visual view and a pipe with a
JSON document. It has no server, no network, and no configuration file. The
only state outside the scanned directory is a cache of measured line counts.

Read this when: changing the pipeline, adding a package, or checking an
invariant before a change.

## Key files

| File | Purpose |
|------|---------|
| `main.go` | The process entry point: `cli.Run` over `os.Args` and the process streams. |
| `internal/cli/cli.go` | `Run`, `withProcessEnv`, the `command` interface, and the `router` that turns one command line into one exit code. |
| `cmd/duls/duls.go` | The command: flags, help, the positional path, and the call into `app.Run`. |
| `internal/app/run.go` | `Request`, `Run`, `resolveRoot`, `openStore`, `writeAgent`: resolve the directory, walk, count, encode or render, erase the frame, write, flush the store. |
| `internal/app/options.go` | `Options` (streams, environment, working directory, the form, style and width seams), `Out`, `ErrOut`, `EnvLookup`. |
| `internal/app/version.go` | `Version` and `VersionLine`. |
| `internal/scan/walk.go` | `Walker`, `Snapshot`, `Dir`, `Sink`: phase 1, the metadata walk. |
| `internal/measure/measure.go` | `Counter`, `Result`, `Fact`: phase 2, the line count. |
| `internal/plan/plan.go` | `Planner`, `Plan`, `View`, `Entry`, `Totals`: the rows. |
| `internal/view/view.go` | `Renderer`, `Lines`: the lines of the visual form. |
| `internal/agent/agent.go` | `Encode`, `Document`: the JSON document of agent mode. |
| `internal/frame/frame.go` | `Rasterizer`: the live outline on a terminal. |
| `internal/cache/cache.go` | `Open`, `Store`: measured facts between runs. |
| `internal/tui/style.go` | `Form`, `DetectForm`, `Style`, `DepthOf`, `IsTerminal`, `Width`, `Dim`, `Segment`. |

## Flow

```text
argv
  |
  v
main.go  os.Exit(cli.Run(ctx, os.Args, Options{Stdout, Stderr}))
  |
  v
internal/cli.Run
  withProcessEnv: a nil Env becomes os.Environ()
  router.run:
    command.Parse(args)      flags, --, one optional path
    command.Run(ctx)         version, or app.Run
  |
  v
internal/app.Run
  resolveRoot                an absolute directory, resolved through a link
  os.Stat                    it must be a directory
  scan.Preset(mode)          workers and read buffer
  formOf                     -json, else the stream and NO_COLOR
  frame.New                  enabled only on the visual form
  cache.Open                 disabled when the store cannot or must not exist
  scan.Walk                  phase 1: enumerate and stat, no content
  measure.Count              phase 2: count the admitted files
  plan.Plan(snapshot, held)  the answer's rows
  agent ?
    agent.Encode             the rows as one JSON document
    io.WriteString(stdout)   the document
  visual ?
    view.Lines               the answer's lines
    frame.Erase              before the first byte of the answer
    io.WriteString(stdout)   the lines
  store.Flush, store.Close   after the answer, silently
```

The deadline covers the walk and the count. Rendering and the write run after
it. A store failure never changes the answer.

## Package map

```text
duls/
|-- main.go
|-- cmd/duls/                the command: flags, help, Parse, Run
|-- internal/
|   |-- agent/               the JSON document a program reads
|   |-- app/                 process body: Options, Run, Request, the composition
|   |-- cache/               the sharded measured-fact store
|   |-- cli/                 the router: parsing, help, the one error line
|   |-- frame/               the live outline on a terminal
|   |-- measure/             phase 2: the line count, the policy, the proof
|   |-- plan/                the rows: ordering, promotion, folding, shares
|   |-- scan/                phase 1: the parallel metadata walk
|   |-- tui/                 the form gate, palette, width
|   |-- view/                the lines: strip, columns, marks, footer
|   `-- integrationtest/
|       |-- itest/           the black-box toolkit: build once, run, PTY
|       `-- cli/             the behavioral scenarios
`-- architecture/
```

Interfaces belong to the package that consumes them. `internal/cli` owns its
three-call `command` view. `internal/app` owns the `Options` seams and joins
the phases. `internal/measure` consumes `scan.Dir` and offers facts to
`cache`. `internal/cache` implements `measure.Measured`. No package exists
only to forward a function.

## Invariants

A change that touches one of these updates this doc and the matching doc in
the same commit.

- **The view is a pure function of the facts and its declared completeness.**
  A completed cold run is exact. A run that reused stored facts may report
  counts at most the trust window old, and its footer says so. A cut run is a
  declared partial view.
- **Size is bytes.** Rows are ordered and shared by `st_size`; files and lines
  are counts. A row's share is never a different quantity from its size.
- **No unknown cell and no false zero.** A value the run cannot prove renders
  `?` for unknown or `~` for a lower bound. A policy-skipped file leaves the
  line denominator, so the line total stays exact. A refused or vanished
  directory leaves its subtree open, and every total above it is a lower bound.
- **The footer is a fact about this run.** It names the phase that stopped,
  the limit it obeyed, and whether stored facts were reused. It never prints
  elapsed time or a fact's age. When the run's totals are lower bounds it also
  states the root's device usage, the figure `df` prints, so the space the run
  could not reach is declared rather than missing.
- **Two forms, one fact set.** A colour terminal gets the visual view. A
  pipe, a named file, `/dev/null`, a non-empty `NO_COLOR`, and `-json` get the
  agent document, which carries no escape sequence, and no such stream
  receives a frame.
- **The agent document states the facts.** Every number is the planner's own
  value, and `-bytes` never changes it. A quantity that does not belong to a
  row's kind, or that the run did not prove, has a null value or no key at
  all, never a zero.
- **The visual form adds presentation only.** Its colour, its strip, its
  blank line, its boxes, its SI units and its marks present the same facts, so
  the document's exact values are the reference behind every printed number.
- **Line fidelity.** One line per LF, plus the final run when the content does
  not end in LF; empty content has zero lines; a CR is part of its line.
- **The walk's reach.** The walk never follows a symlink below the root and
  never opens a special file. Only regular files count. Hidden entries are
  skipped unless `--all` is set. A root named through a symlink is resolved. A
  directory on another device than the root is not entered: another filesystem
  is mounted there, so it stays open, is counted in the footer, and makes every
  total above it a lower bound.
- **The cache is derived state.** Deleting the store changes only speed and
  the footer's cache token. Structure is always exact. Content is at most
  `--refresh` old, and only a proven count is stored.
- **The tool never finds itself.** A run never shows its store: a store inside
  the scanned root stays open and the walk leaves its directory out, and a root
  inside the store's directory disables the store.
- **A spent budget is not a failure.** `context.DeadlineExceeded` stops the
  scan and renders what is held, exit 0. `context.Canceled` is an error, exit
  1. A failure never prints a partial view. A permission refusal below the
  root is not a failure: the run declares the open subtree and exits 0.
- **The frame never outlives the answer.** A frame is written only to a
  terminal, at the first observation, at most once per 16 ms and never more
  often than one frame's drawing cost in five allows, and is erased before the
  first byte of the final view. Its geometry follows the rows the run holds at
  that observation, and the erase clears exactly the lines it drew.
- **The bounds are constants.** At most 8 rows and at most 6 children per row.
  No flag changes them.
- **Errors are returned, never logged, and never panic.** The one exception is
  the store: a cache read or write failure is a miss, never an error line.
- **Every goroutine is joined on a cooperative stop.** The walk and the count
  return only after their goroutines have exited, on success, on error, on
  deadline, and on cancel.

## Related

- [scan.md](./scan.md): phase 1.
- [measure.md](./measure.md): phase 2.
- [plan.md](./plan.md): the rows.
- [view.md](./view.md): the lines.
- [agent.md](./agent.md): the document.
- [frame.md](./frame.md): the live outline.
- [cache.md](./cache.md): the store.
- [cli.md](./cli.md): the command surface.
- [errors.md](./errors.md): the one error line.
- [testing.md](./testing.md): the gates.
