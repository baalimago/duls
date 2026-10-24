# Testing

The two test layers, the strict gate, and what each layer is allowed to prove.

Read this when: adding a feature, writing a scenario, or changing the gate.

## Key files

| File | Purpose |
|------|---------|
| `Makefile` | `lint`, `test`, `qa`: the one Go test command. |
| `internal/scan/walk_test.go` | Phase 1: schedule independence, DirSeq, no open, deadline, cancel, permission, joins, kids, presets, progress. |
| `internal/measure/measure_test.go` | Phase 2: dispatch order, policy, proof, accounting, deadline, progress, the store seam. |
| `internal/plan/plan_test.go` | The rows: folding, ordering, shares, promotion, marks, determinism, colours. |
| `internal/view/view_test.go` | The lines: the strip, the columns, width, marks, the footer. |
| `internal/agent/agent_test.go` | The document: the keys, the null values, the accounting, the folded row, the unknown kind. |
| `internal/frame/frame_test.go` | The live outline: trigger, interval, repaint, geometry, erase. |
| `internal/cache/cache_test.go` | The store: witness, trust window, torn shards, cap, overlap, mailbox, flush budget. |
| `internal/app/run_test.go` | The composition: a real directory, refusals, cancellation, the write failure, the visual and agent forms. |
| `internal/app/options_test.go` | The environment lookup, stream fallbacks, seams. |
| `internal/cli/cli_test.go` | The router: help, version, bad flags, missing directory, streams, cancellation. |
| `cmd/duls/duls_test.go` | Parsing: flags after the path, `--`, one path, the flag values. |
| `internal/tui/style_test.go` | The palette, the depth, and the form gate. |
| `internal/tui/style_pty_linux_test.go` | The terminal gate and width on a pseudo-terminal. |
| `internal/integrationtest/itest` | The black-box toolkit: build once, run the real binary, the PTY runner. |
| `internal/integrationtest/cli` | The scenarios: the visual view, the agent document, flags, paths, the deadline, the store, the frame. |

## Layers

**Unit tests** cover pure functions and defensive branches: the walk's
schedule independence, the count's order and proof, the planner's rows, the
renderer's lines, the encoder's document, the frame's timing, the store's
windows, the router's branches. They use the injected seams: `Options.Form`,
`Options.Style` and `Options.Width` in `app`, `frame.Options.Now`, and
`measure.Options.Open`.

**Black-box scenarios** cover the behavioral contract of the binary. They
build the released command once in `itest.Main`, run it as a real process in a
temporary directory, and compare events in with events out. They are the spec
for the command surface, the streams, the exit codes, the visual layout, the
document, the store, and the frame. Where a scenario and prose disagree, the
scenario is the spec.

**The pseudo-terminal scenarios** prove the character-device branch end to
end: the form gate, the true-color palette, the width gate, `NO_COLOR`, and
the frame's erase. A host without `/dev/ptmx` skips them with a message naming
that capability; no other skip is legitimate.

## The gate

`make test` runs:

```sh
go test -race -count=3 -timeout=30s -coverpkg=./... -coverprofile=.build/cover.out ./...
```

and fails below 70 % statement coverage. 90 % is preferred; the tree is at
about 90 % today.

`make lint` runs `gofumpt -l`, `go vet`, `staticcheck`, and `go fix -diff`,
and fails on any output. `make qa` is both.

Do not weaken `-race`, `-count`, or `-timeout`. Do not add a build tag, an
environment variable, or a flag that hides a subset of the suite.

## Gotchas

- The scenario binary is built by `go build`, not by the test binary, so its
  output has no coverage instrument. Coverage comes from the unit tests; the
  scenarios protect behavior.
- The frame scenario needs a run with more than one directory and enough files
  to publish progress, so it writes a tree of 20000 files. It is the slowest
  scenario and it runs without `t.Parallel`.
- The store scenario needs a settled witness, so it waits out the 2 s settle
  window before its first run. Both store scenarios sleep in parallel.
- A scenario that compares two or more views disables the store with `-r 0`.
  A slow run can make a fresh witness older than the 2 s settle window, so
  the first run stores facts and the next run adds `cache reused`.
- A PTY test must disable `OPOST` on the slave and drain the master
  concurrently with `Wait`, or the capture either gains `\r` bytes or
  deadlocks on a full buffer.
- `t.Parallel` is used throughout. A test that changes the process
  environment, the working directory, or a package variable must not run in
  parallel; duls injects every seam, so no test needs to.
- The integration package's working directory is its own package directory.
  `itest.repoRoot` walks up from its source file, not from the process working
  directory.

## Related

- [view.md](./view.md): the layout the scenarios pin.
- [tui.md](./tui.md): the seams and the terminal gate.
- [frame.md](./frame.md): the animation the PTY scenario proves.
