# Scan redesign — bytes-first parallel walk, budget, cache, rasterising view

## Goal

Replace the single-threaded, line-first scan with a two-phase scanner: a
parallel breadth-first metadata walk that yields exact file counts and byte
totals in milliseconds, then a line-count pass over the files the view can
show, bounded by a root-context deadline. The rendered view orders and shares
by bytes, states plainly when it is partial, and on a styled terminal
rasterises itself into the final outline while the walk runs. A global,
path-keyed record store lets repeated runs get further and lets a root reuse
any subtree it has already seen. What must stay true: plain output carries no
escape sequence, styled output adds presentation and never a fact, the view is
a function of the facts and its declared completeness, and every failure is
returned as an error rather than logged.

## Glossary

| Term                  | Meaning                                                                                                                                           |
| --------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| fact                  | A measured value about one entry: file count, byte total, line count.                                                                             |
| fact set              | Every fact a run holds, from the walk, from measurement, and from the store.                                                                       |
| witness               | Timestamps that prove a fact: a file's `dev`/`ino`/`size`/`mtime`/`ctime` prove its line count.                                                  |
| trust window          | The `--refresh` duration. A stored line count from a younger measured fact may be reused as-is.                                                  |
| declared partial view | A view that says which phase stopped and what the limit was, and exits 0.                                                                         |
| frame                 | One drawn snapshot of the live outline before the final view.                                                                                     |
| bucket                | One top-level entry of the scanned root; the unit the walk publishes progress in.                                                                 |
| measured fact         | A stored per-file line count, keyed by resolved path and file witness.                                                                            |
| view order            | The line-independent order phase 2 consumes: directory sequence, then descending size, then path.                                                 |
| skipped               | A file a declared policy left unmeasured: outside the line denominator, counted in the footer.                                                    |
| unknown               | A file the run tried to measure and could not prove: inside the denominator, forcing a `~` line total.                                            |
| store                 | `$XDG_CACHE_HOME/duls/v1/`, the sharded measured-fact files.                                                                                      |

## Flows

A styled run on a terminal. The frame receives progress from the walk and the
count; it does not start another scan.

```text
duls [flags] [path] ──▶ internal/cli.Run ──▶ app.Run
                                             ├──▶ frame.New(options)
                                             ├──▶ cache.Open(config)
                                             ├──▶ scan.Walker.Walk(ctx, root, opts, sink)
                                             │       └──▶ scan.Sink.Publish(progress)
                                             ├──▶ plan.Planner.Plan(snapshot, empty measurement)
                                             ├──▶ measure.Counter.Count(ctx, plan, opts)
                                             │       ├──▶ measure.Measured.FileLines
                                             │       ├──▶ measure.Measured.StoreFact
                                             │       └──▶ scan.Sink.Publish(progress)
                                             ├──▶ plan.Planner.Plan(snapshot, measured)
                                             ├──▶ view.Renderer.Lines(view, options)
                                             ├──▶ frame.Rasterizer.Erase()
                                             ├──▶ cache.Store.Flush(budget)
                                             └──▶ cache.Store.Close()
```

The run prints the final view before cache facts are flushed.

A run into a pipe. Same path, no frame, plain lines, no escapes.

```text
duls [flags] [path] ──▶ internal/cli.Run ──▶ app.Run
                                             ├──▶ scan.Walker.Walk
                                             ├──▶ plan.Planner.Plan
                                             ├──▶ measure.Counter.Count
                                             ├──▶ plan.Planner.Plan
                                             └──▶ view.Renderer.Lines(plain, width)
```

When the run completes before its deadline, it prints a complete plain view,
with no frame or escape sequence.

A successful composition writes the complete view before it flushes cache facts.

A run whose budget fires. `DeadlineExceeded` renders; `Canceled` errors.

```text
context deadline ──▶ app.Run ──▶ scan.Walker.Walk
                                  └──▶ plan.Planner.Plan(snapshot, held facts)
context deadline ──▶ app.Run ──▶ measure.Counter.Count
                                  └──▶ plan.Planner.Plan(snapshot, held facts)
app.Run ──▶ view.Renderer.Lines(partial view, phase and limit)
context cancel ──▶ app.Run
```

The deadline returns a declared partial view and exit 0. Cancellation returns
one error line and exit 1.

A clean run, `duls -r 0`. No record is read and no record is written.

```text
duls -r 0 [path] ──▶ internal/cli.Run ──▶ app.Run ──▶ cache.Open
                                                      ├──▶ scan.Walker.Walk
                                                      └──▶ measure.Counter.Count
                                                              ├──▶ measure.Measured.FileLines
                                                              └──▶ measure.Measured.StoreFact
```

All fact lookups miss and all offers are dropped. The cold view omits the
cache token.

## Status board

**Line:** done

| # | Node    | Parent | Class       | Interface                                  | Status      |
| - | ------- | ------ | ----------- | ------------------------------------------ | ----------- |
| 1 | walk    | root   | contract    | `scan.Walker`, `scan.Sink`                 | Complete    |
| 2 | plan    | root   | contract    | `plan.Planner`                          | Complete    |
| 3 | view    | root   | contract    | `view.Renderer`                            | Complete    |
| 4 | measure | root   | contract    | `measure.Counter`, `measure.Measured`      | Complete    |
| 5 | frame   | root   | contract    | `frame.Rasterizer`, a `scan.Sink`          | Complete    |
| 6 | cache   | root   | contract    | `cache.Open` returning `cache.Store`       | Complete    |
| 7 | run     | root   | composition | `duls [flags] [path]`                      | Complete    |
| 8 | prune   | root   | prune       | its Removes table                          | Complete    |

## Scope

This effort does not build:

- A daemon, server, network client, configuration file, or any state outside
  the scanned directory and the store.
- Multi-root invocation, a JSON or other machine-readable view, or a second
  output layout.
- Hard-link or reflink accounting: a file counts once per name it has.
- Any change to how a downstream consumer reads the line-based view.
- A persisted walk cursor or frontier. The store is the content resume state
  (D13).
- A per-file index of names or inodes, a mirror of the directory tree, or
  content fingerprints.
- A content sniff or any classifier beyond the extension deny-list (D4).
- Exact line counts for a file above the 64 MiB cap while a deadline is set
  (D22).
- Windows-specific store behaviour beyond falling back to a miss.

## Root facts

**I1 — Purity in facts and declared completeness.** The view is a pure function
of the fact set and the completeness the footer states. A completed cold run is
exact. A completed run that reused stored facts may report line counts that are
at most the trust window old, and its footer says so. A truncated run is a
declared partial view. (Binds phases 1, 2, 3, 6, 7.)

**I2 — Size is bytes.** Rows are ordered and shared by `st_size`; files and
lines are counts; `%` is byte share. A row's `%` is never a different quantity
from its size column. (Binds phases 2, 3.)

**I3 — No unknown cell and no false zero.** A value the run cannot prove
renders `?` for unknown or `~` for a lower bound, never `0`, and the peek
column shows no child name the run cannot prove. A file a declared policy left
unmeasured is not unknown: it leaves the line denominator and the footer counts
it, so the line total stays exact. (Binds phases 2, 3, 4, 5.)

**I4 — The footer is a fact about this run.** It names the phase that stopped,
the limit it obeyed, and whether stored facts were reused. It never prints
elapsed time or a fact's age. (Binds phases 2, 3, 7.)

**I5 — A pipe is not styled.** No non-terminal stream carries an escape
sequence. A pipe, a named file, `/dev/null`, a non-empty `NO_COLOR`, and
`-json` all get the agent document, and no such stream receives a frame.
(Binds phases 3, 5, 7.)

**I6 — The visual form adds presentation only.** Its colour, its strip, its
blank line, its boxes and its SI units state no fact the plan does not hold, so
the agent document of the same view carries the same facts. (Binds phases 3, 5.)

**I7 — Line fidelity.** One line per LF, plus the final run when the content
does not end in LF; empty content has zero lines; a CR is part of its line.
(Binds phases 4, 6.)

**I8 — The walk's reach.** The walk never follows a symlink below the root and
never opens a special file; only regular files count; hidden entries are
skipped unless `--all`; a root named through a symlink is resolved. (Binds
phases 1, 6, 7.)

**I9 — The cache is derived state.** Deleting the store changes only speed and
the footer's cache token. Structure is always exact: every run enumerates every
directory it reports. Content is at most `--refresh` old, and only a proven
count is stored. (Binds phases 1, 4, 6.)

**I10 — The tool never finds itself.** A run never shows its store: a store
inside the scanned root stays open and the walk leaves its directory out, and a
root inside the store's directory disables the store. (Binds phases 6, 7.)

**I11 — A spent budget is not a failure.** `context.DeadlineExceeded` stops the
scan and renders what is held, exit 0. `context.Canceled` is an error, exit 1.
A failure never prints a partial view. (Binds phases 1, 4, 7.)

**I12 — The frame never outlives the answer.** A frame is written only to a
terminal, from the first progress observation, never more than once per 100 ms,
and is erased before the first byte of the final view. It marks every value it
has not proved, prints no footer, and states no fact the run has not proved.
Its geometry follows the rows the run can prove, and the erase clears exactly
the lines the last frame drew, so the answer lands where that block started.
(Binds phases 5, 7.)

**I13 — Bounds are constants.** At most 8 rows and at most 6 children per row.
No flag changes them. (Binds phases 2, 5.)

**I14 — Errors are returned, never logged.** The one named exception is the
store: a cache read or write failure is a miss, never an error line and never
an exit code. (Binds phases 6, 7.)

**I15 — One ordering rule.** The frame is the same planner and the same
renderer fed a partial fact set; it adds no ordering rule, no column, and no
vocabulary of its own. (Binds phases 2, 3, 5.)

**I16 — Phase 1 opens no file.** The walk enumerates and stats; it never opens
a regular file for reading. Content is touched only by phase 2. (Binds phases
1, 4.)

**I17 — Determinism, stated honestly.** Phase-1 facts are independent of the
worker count and of the interleaving; a tie inside a bucket breaks by name. A
run with no deadline prints the same bytes for `-m 0`, `-m 1` and `-m 3`. A run
with a deadline cuts the dispatch, never the order: the measured set is a
prefix plus at most one straggler per worker, and the footer names the cut.
(Binds phases 1, 2, 4.)

**I18 — The walk is breadth-first by depth.** Unfinished work at any instant is
one depth band plus the directories its workers hold, and a cut leaves that
band as the frontier. (Binds phases 1, 7.)

**I19 — Every goroutine is joined on a cooperative stop.** `Walk` and `Count`
return only after every goroutine they started has exited, on success, on
error, on deadline, and on cancel. A syscall blocked in the kernel is outside
the tool's control: the gate proves the join with cooperative readers, and the
docs state the residual limit. (Binds phases 1, 4.)

**I20 — The first failure wins.** A failure cancels the remaining work and the
run returns that error with no view. (Binds phases 1, 4, 7.)

**I21 — Memory follows the frontier, not the file count.** The walk's queue
holds directory paths, each worker holds one listing and one read buffer, and a
file's facts fold into its directory when read. The only per-file values that
survive the walk are the six largest children of each directory, which the view
needs for its peek column. (Binds phases 1, 2.)

**I22 — The order is fixed before measurement starts.** The phase-2 order is a
function of phase-1 facts and each file's own `st_size` alone, so no measured
count can change what is measured next. (Binds phases 4, 7.)

**I23 — The accounting closes.** Each file the walk saw is measured, skipped by
a declared policy, or unknown, exactly once. The footer counts the last two, and
measured plus skipped plus unknown equals the file count the rows report.
(Binds phases 4, 7.)

**I24 — Permission is classified, not conflated.** An unreadable regular file
keeps its byte and file facts, has unknown lines, and is counted by the footer;
the run exits 0. An unreadable directory is a failure: the run cannot state a
file or byte total, so it prints no view and exits 1. No other errno is treated
as permission. (Binds phases 1, 4, 7.)

> Superseded: a refused directory below the root is no longer a failure. It is a
declared open subtree, the totals above it are lower bounds, and the run exits 0;
a refused root still fails. See [scan.md](../../architecture/scan.md) and
[errors.md](../../architecture/errors.md).

**I25 — A count is stored only after proof.** A line count is stored only when
the witness the count took before the read still matches on the descriptor and on
the path after the read, the read reached EOF, and the count does not exceed the
file's byte size. A count that fails any test is discarded, not stored, and not
an error. (Binds phases 4, 6.)

**Parameters.**

| Parameter                                                 | Default                                        | Decided by |
| --------------------------------------------------------- | ---------------------------------------------- | ---------- |
| `-t`, `--timeout` (root context deadline; `0` = none)     | `2s`                                           | D6         |
| `-m`, `--mode` (workers, read buffer, nice)               | `1`                                            | D8         |
| `-r`, `--refresh` (content trust window; `0` = no read, no write) | `15m`                                          | D10        |
| `-a`, `--all` (include hidden entries)                    | off                                            | existing   |
| `--bytes` (exact integers instead of SI)                  | off                                            | D2         |
| `--all-types` (measure every regular file)                | off                                            | D4         |
| Extension deny-list (final-extension categories)           | images, archives, media, objects, executables  | D4         |
| Frame trigger                                             | `0`: the first observation                     | D14        |
| Frame interval                                            | `100ms`                                        | D14        |
| Progress publication                                      | every `4096` files per worker                  | D14        |
| Width clamp                                               | `[40, 150]` columns                            | existing   |
| SI rounding knee                                          | `999_500` of the next unit                     | D2         |
| Rows, children per row                                    | `8`, `6`                                       | existing   |
| Store cap, shards                                         | `32 MB`, `256`                                 | D11        |
| Record size, measured-fact size                           | `~122 B`, `~48 B`                              | D11, D13   |
| Store flush budget after the write                        | `200ms`                                        | D17        |
| Strip width                                               | the clamped view width                         | human check 5 |
| Large-file cap under a deadline                           | `64 MiB`                                       | D22        |
| Phase-2 chunk                                             | `clamp(mode buffer, 16 KiB, 64 MiB / workers)` | D23        |
| Line denominator                                          | policy-skipped files excluded                  | D25        |
| Store mailbox                                             | `256` facts                                    | D28        |
| Fact settle window                                        | `2s`                                           | D27        |

**Decision record.** The reasoning and the rejected alternatives are in the
design handover note `2026-10-24T11-20-00Z_scan-redesign-handover` and the three
cache reviews it cites.

- **D1** — Two phases: a parallel breadth-first metadata walk with no content
  reads, then line counts. The budget cuts either.
- **D2** — Bytes are the primary metric (`st_size`), SI units, one decimal, the
  `999_500` knee; `--bytes` prints exact integers.
- **D3** — Files stay a count column; lines are a detail column.
- **D4** — Phase 2 measures every regular file the deny-list admits,
  extensionless included; `--all-types` disables the deny-list. The 64 MiB cap
  and a refused open are separate, declared exclusions.
- **D5** — Phase 2 order is `(DirSeq, size desc, path)`, where `DirSeq` is the
  directory's index in the phase-1 depth-first flatten ordered by subtree bytes
  then path. Top-level files, the only rows that print a count, are measured
  first. No global sort and no per-bucket round-robin.
- **D6** — `-t` is the root context deadline of the whole run; default `2s`;
  `0` = none.
- **D7** — A spent budget renders a declared partial view and exits 0; a caller
  cancel is an error and exits 1.
- **D8** — `-m 0..3` are constants, not heuristics: `0` = 1 worker, nice 10;
  `1` = 4 workers, nice 5; `2` = `GOMAXPROCS`, nice 0; `3` = 4×`GOMAXPROCS`,
  no caps.
- **D9** — The cache is on by default; measured facts are keyed by the resolved
  absolute path, globally.
- **D10** — `-r` is the trust window for stored line counts, default `15m`;
  `-r 0` means no read and no write; a negative value is a flag error.
- **D11** — Store: 256 shards, CRC32C, temp-file plus rename, merge-on-write,
  32 MB cap, shard-granular LRU, silent failures.
- **D12** — Structure is always exact: every run enumerates every directory it
  reports. Only content is cached.
- **D13** — Measured line counts are stored per file, keyed by resolved path
  plus `size`/`mtime`/`ino`. There is no walk cursor: the store is the content
  resume state.
- **D14** — The animation is the rasterising live outline: from the first
  observation, 10 fps, styled terminal only, unknown cells marked, colour by
  entry identity, line-granular cursor movement, one erase before the final
  view.
- **D15** — The view prints the limit and the stopping phase, never the elapsed
  time or a record's age.
- **D16** — The peek column shows only child names the run can prove; a
  truncated subtree shows no peek.
- **D17** — Store writes run on a background writer with its own 200 ms budget
  after the view is printed; an unfinished write is dropped, never an error.
- **D18** — A stored fact is keyed by a resolved path plus a file witness, so
  two files that share a name never share a fact.
- **D19** — The walk is breadth-first by depth with a fan-out pool of `-m`
  workers; each worker folds into its own per-bucket counters and publishes to
  the shared counters every 4096 files, so no lock sits on the hot path.
- **D20** — Breadth-first is chosen for the view, not for the disk: every
  top-level bucket grows in step, so the live outline fills level by level
  instead of one row leaping ahead of the rest.
- **D21** — Phase 2 owns its pool and dispatches one file per free worker over
  an unbuffered channel; no read-ahead queue, one reusable chunk per worker, and
  the order comes from phase-1 facts so it cannot chase its own results.
- **D22** — A file larger than 64 MiB is not measured when the run has a
  deadline: its cell is `?` and it is counted as skipped. `-t 0` lifts the cap,
  so the ground-truth run measures everything.
- **D23** — Phase 2 reads in chunks of `clamp(mode buffer, 16 KiB, 64 MiB /
  workers)` and reuses one buffer per worker, so in-flight bytes are a function
  of `-m` and never of file size. No file is read whole.
- **D24** — An unreadable file is a declared unknown (exit 0); an unreadable
  directory is a failure (exit 1, no view). Only permission errors take the
  first branch: a read error, a close error, or a failed witness does not.
  Superseded for directories: a refused directory below the root is declared an
  open subtree (exit 0) and only a refused root fails.
- **D25** — Lines are counted with `bytes.Count` per chunk, carrying the last
  byte and the byte total across chunks; a policy-skipped file leaves the
  denominator, so the totals stay exact while nothing is unknown.
- **D26** — Cancellation outranks the deadline: if both are observable after the
  workers join, the run returns `context.Canceled` and exits 1.
- **D27** — A fact is proven by `dev`/`ino`/`size`/`mtime`/`ctime` equality
  across the dispatch `lstat`, the pre-read `fstat` and the post-read `fstat`,
  plus a `lstat` of the path after close; a mismatch is discarded silently, and
  a witness younger than 2 s is not stored.
- **D28** — The coordinator folds each outcome and calls the store; no worker
  touches it. Store writes pass through a bounded mailbox of 256 facts, and a
  full mailbox drops persistence, never a fact.
- **D29** — The frame publishes four monotone counters — files, bytes, lines
  and directories done — through the same sink as the walk, at most once per
  directory and once per 100 ms. Nothing else.
- **D30** — Phase 2 sets its own worker count and chunk from `-m` and never
  reuses the walk's goroutines: the walk's pool is joined before the count
  starts.

**Measurements** (host: 8 CPUs, NVMe; full table in the handover note).

| Fixture                                     | Result                     | Decides                                |
| ------------------------------------------- | -------------------------- | -------------------------------------- |
| `~/go/pkg/mod`, 224,285 files, 5.8 GB       | 38.1 s today; 92.6 M lines | the redesign's premise                 |
| stat-only walk of the same tree, one thread | ~3.2 s warm                | metadata is cheap; content is the cost |
| one 200 MB file                             | 0.17 s (~1.2 GB/s)         | per-file overhead dominates, not bytes |

## Validation policy

### Gates

Run all of them at the start of every phase, and twice at its end.

```sh
go run mvdan.cc/gofumpt@v0.11.0 -l .            # must print nothing
go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...
go vet ./...
go fix -diff ./...                              # must print nothing
make test
go run github.com/mibk/dupl@v1.0.0 -t 80 .
make qa
```

### Human checks

- **Phase 5 — frame:** run `duls ~` and `duls -m 0 .` on a real terminal at
  40, 100 and 150 columns. Confirm no flicker, scroll, leftover frame, early
  frame, or misplaced final view. A pseudo-terminal proves the bytes; only a
  person judges the appearance.

## Findings

| ID | Severity | Node | Owner | Status | Summary |
| -- | -------- | ---- | ----- | ------ | ------- |
| V1-01 | major | 1, 4 | plan | Closed +wip | check 9 · `scan.Snapshot` and `measure.Count` · resolve how every phase-1 file witness reaches the count without violating I21 |
| V1-02 | major | 1, 5 | fix | Closed +wip | check 4 · `scan.Progress` · publish the three D29 counters and test their monotonic behavior |
| V1-03 | major | 1–7 | fix | Closed +wip | check 4 · phase test tables · cite every acceptance criterion and clause and add missing test rows |
| V1-04 | major | 7 | fix | Closed +wip | check 3 · `Consumes` · use the provider packages and member names that declare the consumed signatures |
| V1-05 | major | root | fix | Closed +wip | check 8 · Flows · make each call arrow target a declared or consumed member |
| V1-06 | major | 8 | fix | Closed +wip | check 4 · phase 8 · trace each removal and acceptance criterion to a concrete command row |
| V1-07 | major | 1–7 | fix | Closed +wip | check 5 · Local facts parameter tables · define each shared literal once at the README root |
| V1-08 | major | 5 | fix | Closed +wip | check 10 · Human required · align the phase handoff with the README's exact terminal check |
| A01 | design andon | 7 | maintainer | Closed none | `scan.Dir` and `scan.Record` carry no children and no reachable line total, so a warm record hit erases the view's rows and line total · repro `XDG_CACHE_HOME=<tmp> duls <fixture>` twice · second run prints `0 lines` and no rows · none |
| A02 | design andon | 7 | maintainer | Closed none | `measure.Result.Named` carries root-level files only (Count.11), so `plan.Entry.Kids` render every measured nested file as `?` · `make qa` · `TestScenarioOutlineOfATree` golden · none |
| A04 | design andon | 2, 3, 7 | maintainer | Closed +wip | `plan.Measured` and `plan.Totals` carry no way to say that the count stopped, so a count-only deadline prints an exact line total with no footer · repro `duls -t 1200ms ~/go/pkg/mod` · before the fix: `224286 files · 5.4 GB · 5576149 lines` and no `partial:` line · none |
| R1-01 | minor | 7 | fix | Closed +wip | phase 7 named `TestScenarioCancelIsAnError` as an integration scenario, but no process can observe a cancel: the command installs no signal handler and its surface has no cancel input, so the row could not exist at that level · `rg TestScenarioCancelIsAnError` · the planned test is absent · none |
| R1-02 | minor | 5 | maintainer | Closed +wip | the phase-5 Human check had no journal record, and its four byte-level claims pass while the appearance it asks a person to judge cannot be measured from the stream · repro `make qa` · `TestScenarioFrameIsErasedBeforeTheView` passes; `duls ~` at 40, 100 and 150 columns is unrun by a person · none |
| R1-03 | minor | 7 | fix | Closed +wip | the repository README still showed the line-first view (`6 files · 84 lines`, directories before files) and listed four of the eight flags, so the docs disagreed with every run · `duls -h` against `README.md` · four flags were undocumented and the example view named a column no run prints · none |
| R1-04 | minor | 5, 7 | fix | Closed +wip | the human check found the animation too late: the composition waited 400 ms and the frame rendered nothing before the walk's snapshot existed, so a 900 ms walk showed a static terminal · repro `duls ~` · the first byte of a frame arrived after the walk, not at 3 ms · none |
| R1-05 | minor | 3 | fix | Closed +wip | the strip stopped at 60 columns, so at a 150-column terminal the bar filled 40 % of the line and no document said why · repro `duls ~` at 150 columns · the human check counted about half the width · none |

### Andon rulings

**A01 — a warm record hit destroyed the view.** Ruled **(b): drop subtree
trust and keep per-file facts.** A directory record cannot carry what a row
needs: the six largest children of every directory at every promotable depth,
and a line total no walk caller can prove. Structure is therefore always
enumerated; the store keeps only measured line counts keyed by a file witness;
`plan.Measured.Reused` carries the footer token. Rejected: (a) storing a view
in each record, which is unbounded because promotion can nest and no phase can
write a line total; (c) trusting nested subtrees, which still cannot print a
line total or a peek and makes a second run differ from the first (AC7).

**A02 — a measured file printed `?`.** Ruled **(a): `Named` follows the
peek.** Count.11 widens from the eight largest root-level files to every measured
file that is one of its directory's six largest children; `plan.entry` already looks such a count up by
nested path. Rejected: (b) and (c), which keep a false unknown in a view whose
count the run proved, which I3 forbids.

**A04 — a count-only stop printed an exact line total.** Ruled **(a): the
stopped phase is a fact of the view.** `plan.Measured` gained
`Stopped scan.Stop` and `plan.Totals` gained `StoppedPhase scan.Phase`, so
`plan.totals` marks the line total a lower bound when either phase stopped and
names which one, and `view.footerParts` prints `partial: count deadline <limit>`.
Rejected: (b) reporting a count stop as a walk stop, which would claim the file
and byte totals are lower bounds when they are exact; (c) printing no footer for
a count stop, which I4 forbids because the footer must name the phase that
stopped. Phase 2's Interface block and clause Plan.8 carry both fields.

## Session journal

- 2026-10-24 · plan · README drafted
- 2026-10-24 · plan · phase-2 concurrency and order settled from two adversarial reviews
- 2026-10-24 · maintainer · README signed
- 2026-10-24 · plan · phases 1–8 written from the signed README
- 2026-10-24 · validate · V1 Not ready · inspection: checks 1–10 found V1-01 through V1-08 · notes: 0
- 2026-10-24 · plan · README amended: I25 (V1-01) — the count proves its own witness, phase 1 carries none per file
- 2026-10-24 · fix · V1-01 closed · inspection: phase 1 has no per-file witness; phase 4 takes its own witness at dispatch
- 2026-10-24 · fix · V1-02 closed · inspection: checks 1 and 5 progress clauses and tests re-read; monotone Files, Lines and Dirs are defined and covered
- 2026-10-24 · fix · V1-03 closed · inspection: all phase clauses, criteria, and bound invariants have test rows; re-read checks passed
- 2026-10-24 · fix · V1-04 closed · inspection: repository imports now name their declaring package paths and actual members
- 2026-10-24 · fix · V1-05 closed · inspection: all flow arrows target declared or consumed members; no unmatched reply arrows remain
- 2026-10-24 · fix · V1-06 closed · inspection: every removal and criterion maps to a command row; the old outline test file is also checked absent
- 2026-10-24 · fix · V1-07 closed · inspection: the shared deny-list parameter is defined at root and removed from phase 4 local parameters
- 2026-10-24 · fix · V1-08 closed · inspection: phase 5 stops at the root handoff and names the README's exact terminal check
- 2026-10-24 · validate · V2 Ready · check 1 python3 structural audit: pass; check 2 go build ./... in temporary interface assembly and inspection: pass; check 3 python3 dependency DAG plus rg signatures: pass; check 4 python3 clause, criterion, invariant and error audit: pass; check 5 python3 placement audit: pass; check 6 python3 class audit: pass; check 7 inspection: pass; check 8 python3 flow-target audit: pass; check 9 inspection: pass; check 10 python3 human-check audit: pass · notes: 0
- 2026-10-24 · plan · README amended: D29 — the frame publishes files, bytes, lines and directories
- 2026-10-24 · plan · phases 1, 4, 5 amended: `scan.Progress` carries Bytes, held at the walk total during PhaseCount
- 2026-10-24 · work · phase 1 Complete
- 2026-10-24 · work · phase 2 Complete
- 2026-10-24 · work · phase 3 Complete
- 2026-10-24 · work · phase 4 Complete
- 2026-10-24 · work · phase 5 Complete
- 2026-10-24 · work · phase 6 Complete
- 2026-10-24 · work · phase 7 · A01, A02
- 2026-10-24 · validate · V3 Ready · prior: V1-01 resolved, V1-02 resolved, V1-03 resolved, V1-04 resolved, V1-05 resolved, V1-06 resolved, V1-07 resolved, V1-08 resolved; check 1 `python3 structural audit`: pass; check 2 `python3 /tmp/duls-v3-assemble.py /home/imago/Projects/public/duls/worklogs/2026-10-24-scan-redesign /tmp/duls-v3-interfaces.0zay4S && (cd /tmp/duls-v3-interfaces.0zay4S && go test ./...)`: pass; check 3 `inspection + rg imported signatures`: pass; check 4 `python3 changed-text assertions + inspection`: pass; check 5 `inspection placement`: pass; check 6 `inspection classes`: pass; check 7 `inspection naming`: pass; check 8 `inspection flows`: pass; check 9 `inspection provenance`: pass; check 10 `inspection humans`: pass · notes: 1 (gofmt diff in illustrative interface blocks)
- 2026-10-24 · maintainer · A01 closed · re-plan · missed by check 9
- 2026-10-24 · maintainer · A02 closed · re-plan · missed by check 9
- 2026-10-24 · plan · README amended: glossary, flows, I1, I4, I9, I10, D9, D10, D12, D13, D18
- 2026-10-24 · plan · phase 1 re-planned
- 2026-10-24 · plan · phase 2 re-planned
- 2026-10-24 · plan · phase 3 re-planned
- 2026-10-24 · plan · phase 4 re-planned
- 2026-10-24 · plan · phase 6 re-planned
- 2026-10-24 · plan · phase 7 re-planned
- 2026-10-24 · validate · V4 Ready · prior: V1-01 resolved, V1-02 resolved, V1-03 resolved, V1-04 resolved, V1-05 resolved, V1-06 resolved, V1-07 resolved, V1-08 resolved; check 1 `python3 structural audit`: pass; check 2 `python3 /tmp/duls-v4/assemble.py`: pass; check 3 `inspection + rg declared members`: pass; check 4 `python3 clause, criterion and invariant audit`: pass; check 5 `inspection placement`: pass; check 6 `inspection classes`: pass; check 7 `inspection naming`: pass; check 8 `inspection flows`: pass; check 9 `inspection provenance`: pass; check 10 `inspection humans`: pass · notes: 1 (an unused `scan` import in the re-planned phase 6 block, fixed before the verdict)
- 2026-10-24 · maintainer · interfaces 1–4, 6–7 signed
- 2026-10-24 · work · phase 7 · A04
- 2026-10-24 · fix · A04 implemented, ruling owed · `make qa` (coverage 89.4 %), `dupl -t 80` (2 accepted clone groups): pass
- 2026-10-24 · work · phase 7 Complete
- 2026-10-24 · work · phase 8 Complete
- 2026-10-24 · fix · R1-01 closed · `rg TestScenarioCancelIsAnError`: only the planned rows, which now name `TestCancelIsAnError` · A03 reclassified: the clause stood and only the test level was wrong
- 2026-10-24 · plan · phases 2, 3, 4, 7 amended: A04 fields and rows, the duplicate `Named` tests merged, the cancel row's level
- 2026-10-24 · review · root round 1 not ready · `make qa` ×2: pass, coverage 89.4 % · `dupl -t 80`: 2 accepted clone groups · `go test -race -shuffle=on` ×2: pass · A04 open, R1-02 open (root human check unrecorded)
- 2026-10-24 · fix · R1-03 closed · `duls -h` and a real run against the rewritten `README.md`: the view, the flags, the marks, the store and the deadline all match
- 2026-10-24 · maintainer · human check 5 · kept the view; the frame must start at once and fill the detail in, and the strip must reach the full width
- 2026-10-24 · plan · README amended: I12, D14, frame trigger `0`, strip width
- 2026-10-24 · plan · phases 3, 5, 7 amended: `Options.Provisional`, the strip width, the instant frame
- 2026-10-24 · fix · R1-02, R1-04, R1-05 closed · `make qa` (coverage 89.9 %), `dupl -t 80`: pass · PTY capture: first frame at 3 ms, strip 100 columns at a 100-column terminal
- 2026-10-24 · plan · A04 ruled (a) · I10 rewritten, CLI.7 rewritten, phase 1 gains `WalkOptions.Skip` with AC13, phase 6 `Open.1`/`Open.3` and AC4 restated, the tables cite `TestRootOverlap`, `TestSkipLeavesEntriesOut` and `TestScenarioStoreUnderTheRootIsSkipped`
- 2026-10-24 · fix · A04 closed · the stopped phase is a fact of the view: `plan.Measured.Stopped`, `plan.Totals.StoppedPhase`, `partial: <phase> deadline <limit>`
- 2026-10-24 · fix · a styled view no longer prints the empty strip and its blank separator before its first row, so a frame starts at the totals line · `TestStyledViewWithoutRowsStartsAtTheTotalsLine`, `TestRunStyledEmptyViewStartsAtTheTotalsLine`, PTY capture at 100 columns
- 2026-10-24 · fix · `app.storeSkip` leaves the store's directory out of a `$HOME` walk; `TestScenarioStoreFallsBackToHome` pins the `$HOME/.cache` fallback; `TestScenarioFrameIsErasedBeforeTheView` runs both runs with `-r 0`, so the store cannot change the compared bytes
- 2026-10-24 · review · root round 2 · line done · `make qa` ×2 after the last change: pass, coverage 90.3 % and 90.2 % · `dupl -t 80`: 2 accepted clone groups · A04 closed, every phase Complete, the docs and the code agree on the store's placement, the styled skeleton and the frame's geometry
- 2026-10-24 · plan · README amended: I5, I6 · agent mode replaced the plain form after this line closed: a pipe, a file, `/dev/null`, a non-empty `NO_COLOR` and `-json` get a JSON document from `internal/agent`, so I5 speaks of the agent document and I6 of the visual form adding presentation. The rest of this worklog records the design as it was signed
