# Phase 7 — run

**Parent:** root · **Class:** composition · [README](README.md)

## Goal

Wire the walk, the planner, the renderer, the counter, the frame and the store
into one command, and make the flags, defaults, streams and exit codes the
contract.

## Interface

The command surface. It is written to `cmd/duls/command.go` (parsing) and
`internal/app/run.go` (the run body).

```text
duls [flags] [path]

  -a, --all          include hidden entries
      --all-types    measure every regular file; disable the deny-list
      --bytes        exact byte counts instead of SI units
  -m, --mode n       0|1|2|3: workers and buffer; default 1
  -r, --refresh d    record trust window; default 15m; 0 = no cache
  -t, --timeout d    run deadline; default 2s; 0 = none
  -h, --help         print this text on stdout and exit 0
  -v, --version      print "duls <version>" on stdout and exit 0

exit 0  a view, including a declared partial view; help; version
exit 1  a failure: one "error: <message>" line on stderr and no view
```

Clauses:

- **CLI.1** Flags and at most one positional may be interleaved in any order, and
  `--` ends flag parsing.
- **CLI.2** A relative path resolves against the working directory, an empty path
  means `.`, and a root named through a symlink is resolved before the walk (I8).
- **CLI.3** An unknown flag, a second positional, an unparsable duration, a
  negative `-r`, or a mode above 3 is a failure with exit 1 and one error line (I14).
- **CLI.4** A missing path, or a path that is not a directory, is a failure with
  exit 1; no view is printed (I8, I14).
- **CLI.5** Plain output is written when stdout is not a terminal or `NO_COLOR`
  is non-empty; a plain run is never animated (I5).
- **CLI.6** A styled run on a terminal animates from the first progress
  observation; the frame is erased before the first byte of the view (I12).
- **CLI.7** `XDG_CACHE_HOME` selects the store root, with `$HOME/.cache` as the
  fallback; an unset or unusable value means the store is off and the run still
  succeeds. A store inside the scanned root stays open and the walk leaves its
  directory out; a root inside the store disables it. A cache failure is not a
  run failure (I9, I10, I14).
- **CLI.8** A spent `-t` deadline prints a declared partial view and exits 0; a
  cancelled context prints one error line and exits 1. A partial view states
  what is unknown and where the scan stopped; unreadable and skipped files are
  accounted for (I1, I3, I4, I11, I18, I20, I23, I24).
- **CLI.9** A successful run writes the complete view and stores its facts.
  A store failure does not change the view or the exit status; an output failure
  is returned as one error line, never logged (I14).
- **CLI.10** The frame and final view use the same planner and renderer, and
  phase-2 order is fixed before counting; modes produce identical output when
  no deadline applies (I15, I17, I22).
- **CLI.11** `--help` prints the usage text and `--version` prints exactly
  `duls <version>` on stdout, both with exit 0.

## Consumes

- `scan:Walker (phase 1)`
- `scan:Sink (phase 1)`
- `scan:WalkOptions (phase 1)`
- `scan:Mode (phase 1)`
- `scan:Preset (phase 1)`
- `scan:Snapshot (phase 1)`
- `scan:Progress (phase 1)`
- `scan:Stop (phase 1)`
- `plan:Planner (phase 2)`
- `plan:View (phase 2)`
- `plan:Measured (phase 2)`
- `plan:Measured.Reused int (phase 2)`
- `plan:Measured.Stopped scan.Stop (phase 2)`
- `view:Renderer (phase 3)`
- `view:Options (phase 3)`
- `measure:Counter (phase 4)`
- `measure:Plan (phase 4)`
- `measure:Options (phase 4)`
- `measure:Result (phase 4)`
- `frame:Rasterizer (phase 5)`
- `frame:Options (phase 5)`
- `frame:New (phase 5)`
- `cache:Open (phase 6)`
- `cache:Store (phase 6)`
- `cache:Config (phase 6)`
- `cache:Store.Flush (phase 6)`
- `cache:Store.Close (phase 6)`
- `internal/tui:Style (import)`
- `internal/tui:Detect (import)`
- `internal/tui:IsTerminal (import)`
- `internal/tui:Width (import)`
- `internal/cli:Run(context.Context, []string, app.Options) int (import)`
- `cmd/duls:Command(app.Options) *command (import)`
- `internal/app:Run(context.Context, app.Options, app.Request) error (import)`
- `internal/app:Options (import)`
- `internal/app:Options.Out() io.Writer (import)`
- `internal/app:Options.ErrOut() io.Writer (import)`
- `internal/app:Request (import)`
- `internal/app:EnvLookup([]string) func(string) string (import)`
- `internal/app:VersionLine() string (import)`

## Acceptance criteria

| #   | Criterion                                                                   | Proves        | Proof                                    |
| --- | --------------------------------------------------------------------------- | ------------- | ---------------------------------------- |
| 1   | A piped run prints a bytes-first view and no escape sequence                 | CLI.5, I2, I5 | `TestScenarioPipedViewIsBytesFirst` |
| 2   | The totals line names files, bytes and lines in SI units                     | CLI.5 | `TestScenarioOutlineOfATree` |
| 3   | A styled run at 40, 60 and 150 columns fits every line                       | CLI.6, I6 | `TestScenarioStyledViewAdaptsToTheTerminal` |
| 4   | A styled run from the first observation draws frames and erases them        | CLI.6, I12 | `TestScenarioFrameIsErasedBeforeTheView`, `TestRunFrameStartsAtOnce` |
| 5   | `-t 1ns` prints a declared partial view, exit 0, empty stderr                | CLI.8, I11 | `TestScenarioTimeoutIsDeclared` |
| 6   | A cancelled context prints one error line, exit 1, no view                   | CLI.8, I11, I20 | `TestCancelIsAnError` |
| 7   | A second run with the same store reports the cache token and does not regress | CLI.7, I9 | `TestScenarioSecondRunUsesTheStore` |
| 8   | `-r 0` writes nothing into the store                                        | CLI.7 | `TestScenarioRefreshZeroWritesNothing` |
| 9   | `-m 0` and `-m 3` print identical bytes with no deadline                     | CLI.10, I17, I22 | `TestScenarioModesAgree` |
| 10  | `--bytes` prints exact integers; `--all-types` adds the denied files' lines   | CLI.1 | `TestScenarioBytesAndAllTypes` |
| 11  | An unreadable file yields exit 0 with a footer count; a directory yields exit 1 | CLI.4, I24 | `TestScenarioUnreadableInput` |
| 12  | Help lists every flag and the version line is unchanged                      | CLI.11 | `TestScenarioHelpAndVersion` |
## Test cases

| Test                                        | Level                | Proves    | Given                              | Then                                              | Must not                     |
| ------------------------------------------- | -------------------- | --------- | ---------------------------------- | ------------------------------------------------- | ---------------------------- |
| `TestScenarioOutlineOfATree`                | integration scenario | AC2, CLI.5 | the six-file fixture             | bytes-first view, measured peek cells carry their counts, no `~`, no footer | an escape byte or a false `?` |
| `TestScenarioPipedViewIsBytesFirst`         | integration scenario | AC1, CLI.5, I2, I5 | fixture, stdout piped        | size column, rows and plain output                  | line count as headline       |
| `TestScenarioStyledViewAdaptsToTheTerminal` | integration scenario | AC3, CLI.6, I6 | PTY at 40, 60 and 150 columns | boxes and strip; stripped equals plain additions    | a line wider than terminal    |
| `TestScenarioFrameIsErasedBeforeTheView`    | integration scenario | AC4, CLI.6, I12 | PTY and a 20 000-file tree | a frame is drawn, erased, and the answer lands where it was | a leftover frame line        |
| `TestRunFrameStartsAtOnce`                  | unit | AC4, CLI.6, I12 | a styled seam over 20 directories | the first paint precedes the answer and states lower bounds | a frame that waits, or an answer carrying a mark |
| `TestScenarioTimeoutIsDeclared`             | integration scenario | AC5, CLI.8, I1, I3, I4, I11, I18 | `-t 1ns` | frontier is declared, phase/limit footer, exit 0 | a number the run cannot prove |
| `TestCancelIsAnError`                       | unit                 | AC6, CLI.8, I11, I20 | a context cancelled before the run | one error line, exit 1, empty stdout | a partial view |
| `TestScenarioSecondRunUsesTheStore`         | integration scenario | AC7, CLI.7, I9 | two runs with isolated cache | second prints cache token; numbers equal or tighter | store inside fixture |
| `TestScenarioRefreshZeroWritesNothing`      | integration scenario | AC8, CLI.7 | `-r 0`, isolated cache dir | cache remains empty | a record written |
| `TestScenarioModesAgree`                    | integration scenario | AC9, CLI.10, I17, I22 | `-m 0`, `-m 1`, `-m 3`, `-t 0` | identical stdout and exit code | a differing byte |
| `TestScenarioBytesAndAllTypes`              | integration scenario | AC10, CLI.1 | `--bytes`; `--all-types` | integers; denied file's lines appear | changed facts |
| `TestScenarioUnreadableInput`               | integration scenario | AC11, CLI.4, I23, I24 | permission-refused file; then directory | exit 0 with footer; exit 1 with one line | error for the file |
| `TestScenarioHelpAndVersion`                | integration scenario | AC12, CLI.1, CLI.11 | `-h`; `-v` | help lists every flag; version line is `duls <version>`; both exit 0 | a changed version line or nonzero exit |
| `TestScenarioEmptyDirectory`                 | integration scenario | CLI.5, I2 | empty root | empty bytes view matches the golden | extra line total |
| `TestScenarioHiddenEntries`                  | integration scenario | CLI.1, I8 | hidden entries with and without `--all` | rows follow hidden policy | hidden file without flag |
| `TestScenarioFlagsAfterThePath`             | integration scenario | CLI.1 | `path -a`; `-- -a` | flag applies; `--` makes it a path | a parse error |
| `TestScenarioPathResolution`                | integration scenario | CLI.2, I8 | relative path, empty path, and root symlink | each resolves to the intended directory | path outside the selected root |
| `TestScenarioBadFlags`                      | integration scenario | CLI.3, I14 | unknown flag, bad duration, negative refresh, invalid mode, two paths | one error line and exit 1 | view or success |
| `TestScenarioMissingDirectoryIsOneLine`     | integration scenario | CLI.4, I14 | a missing path and a regular-file path | each run writes one `error:` line and exits 1 | a view or a second line |
| `TestScenarioEnvironmentIsolation`          | integration scenario | CLI.7, I14 | cache environment unset or unusable | both runs succeed | cache error on stderr |
| `TestScenarioStoreUnderTheRootIsSkipped`     | integration scenario | CLI.7, I10, I14 | cache root inside the scanned root | run succeeds, reuses its facts and shows no shard | a cache error or a store file with a row |
| `TestScenarioStoreFallsBackToHome`           | integration scenario | CLI.7, I9, I14 | `XDG_CACHE_HOME` empty and `HOME` set | the store lands under `$HOME/.cache/duls/v1` and the second run reuses it | a store under the scanned root or a cache error |
| `TestScenarioNoColorForcesPlain`             | integration scenario | CLI.5, I5 | styled PTY with non-empty `NO_COLOR` | no escape or frame bytes | styled output |
| `TestScenarioWritesViewAndStoreFacts`        | integration scenario | CLI.9, I14 | successful run with writable cache | complete view and persisted cache facts | cache failure changes output or status |
| `TestRunReportsAWriteFailure`                | unit | CLI.9, I14 | app options with a failing stdout writer | one write error is returned | cache failure or success |
## Error management

| Failure                              | Expected outcome                                                     | Test                                  |
| ------------------------------------ | -------------------------------------------------------------------- | ------------------------------------- |
| Unknown flag or bad value            | `error: flag: <detail>`, exit 1, no view                               | `TestScenarioBadFlags`                |
| A second positional                  | `error: at most one path`, exit 1                                      | `TestScenarioBadFlags`                |
| Missing or non-directory path        | `error: <detail>`, exit 1                                              | `TestScenarioMissingDirectoryIsOneLine` |
| A scan failure                       | one wrapped `error:` line, exit 1, no view                             | `TestCancelIsAnError`         |
| The deadline fires                   | a declared partial view, exit 0                                        | `TestScenarioTimeoutIsDeclared`       |
| The write fails                      | `error: write: <detail>`, exit 1                                       | `TestRunReportsAWriteFailure`         |
| The store cannot be opened           | no error; the run proceeds without a store                             | `TestScenarioEnvironmentIsolation`    |

## Golden diff

| Golden                                                        | Change                                                                 |
| ------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `TestScenarioOutlineOfATree` expected view                     | the totals line leads with bytes; rows gain a size column and a byte share; each measured peek cell prints its count instead of `?` |
| `TestScenarioEmptyDirectory` expected view                     | the empty view reports `0 B` and no line total                          |
| `TestScenarioHiddenEntries` expected view                      | the same rows with the size column added                                |
| `TestScenarioStyledViewAdaptsToTheTerminal` expected frames    | the strip splits by bytes; no footer on a complete cold run              |
| `TestScenarioHelpAndVersion` expected help text                | `-m`, `-r`, `-t`, `--bytes`, `--all-types` are listed                    |

## Removes

None.

## Local facts

**Invariants.** The composition calls `Plan` twice: once with an empty
`Measured` for the frame, once with the count's overlay for the answer. The
frame is constructed before the walk, erased after the last `Count` returns and
before the write, and never constructed for a plain run. The deadline applies to
the walk and the count only; the renderer, the write and `Store.Flush` with its
configured budget run after it. `Plan` is called with the same snapshot, so the
frame and answer agree on every cell the count did not touch (I15). The composition maps
`scan.Snapshot` to `measure.Plan`, builds worker and policy options from the
parsed flags and `Preset`, and maps `measure.Result.Named` and its aggregate
counters to `plan.Measured`, and maps `measure.Result.Reused` to `plan.Measured.Reused` so the
footer keeps its cache token. The `cache.Open` result supplies
`measure.Options.Store`. The frame's `Lines` callback uses the same planner
and renderer as the final view. Docs updated in this phase:
`architecture/overview.md`, `cli.md`, `outline.md`, `view.md`, `errors.md`,
`testing.md`, plus a new `architecture/cache.md`, and the invariant list in
`AGENTS.md`.

**Parameters.** None. Command defaults are defined in the README Parameters table.

## Human required

None.

## Implementation notes

| Run   | Commit | Command                                                                                                         | Result                                                       |
| ----- | ------ | --------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------ |
| start | none   | `make qa` (gofumpt, `go vet`, staticcheck, `go fix -diff`, `test -race -count=3 -coverpkg=./...`) + `dupl -t 80` | all packages ok, coverage 89.7 %; dupl 2 clone groups (test setup and the PTY harness) |
| replan | none  | A01 ruling (b) and A02 ruling (a) | the walk reads no store; the footer token comes from `plan.Measured.Reused`; measured peek cells print counts |
| end   | none+wip | `make qa` && `dupl -t 80` | pass, coverage 89.4 %; dupl 2 clone groups, both accepted |
| replan | none  | R1-04: the frame starts at the first observation | `Trigger: 0`; `live.lines` draws the walk's running totals before the snapshot exists; the live view sets `Provisional` |
| end   | none+wip | `make qa` && `dupl -t 80` | pass, coverage 89.4 %; dupl 2 clone groups, both accepted |

Andon: none.
Surprises:
- The tree arrived with the previous session's unrecorded draft of this phase; `make test` failed on three stale goldens, now corrected to the view the code prints (5 B per fixture line, strip four columns narrower than the clamped width).
- `plan.totals` sets `LinesExact` false when the walk stopped, so a cut walk cannot print an exact `0 lines` (I3).
- `internal/app/outline.go` is deleted ahead of phase 8 because staticcheck U1000 rejects its now-dead helpers.
- The phase-2 counters restart at zero after the walk, so the frame saw every count observation as a decrease and drew nothing; `internal/app/run.go` adds the walk's files and directories to phase-2 progress.
- A count-only deadline printed an exact line total with no footer; the fix needed `plan.Measured.Stopped` and `plan.Totals.StoppedPhase` (A04).
- The store keeps nothing for files whose witness is younger than the settle window, so a first run against a fresh fixture warms nothing.

## Review findings

### Review 1, 2026-10-24

- [x] **R1-01 (minor, closed):** the phase named `TestScenarioCancelIsAnError`
      as an integration scenario, but a process cannot observe a cancel. The
      row now names `TestCancelIsAnError` at the router level, and the judge
      documents why: the command installs no signal handler and its surface has
      no cancel input.

Verified good, all four flows of the README run as written: a piped run prints
the plain bytes-first view; a styled run draws and erases frames before the
answer; `-t 1200ms` on 224 286 files prints `224286 files · 5.4 GB · ~5576149
lines` and `partial: count deadline 1.2s`; `-t 1ms` marks files, bytes and lines
and prints `partial: walk deadline 1ms`; and a warm second run prints the same
view plus `cache reused`.

- [x] **R1-03 (minor, closed):** the repository README described the
      pre-redesign view and four of the eight flags. It now shows a real run,
      lists every flag, and states the marks, the store and the deadline.
- [x] **R1-04 (minor, closed):** the animation started after the walk. The
      trigger is 0 and the frame renders the walk's running totals, so a
      224 286-file walk paints at 3 ms and repaints every 100 ms.
