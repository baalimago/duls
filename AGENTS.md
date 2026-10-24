# AGENTS.md — duls

Kind of like `ls`, kind of like `du`. Not like one, not like the other.

## Architecture docs

`architecture/` holds one doc per concern: the overview, the CLI, the scan,
measurement, the planner, the rendered view, the frame, the cache, the
terminal presentation, errors, and testing. Start at
[architecture/README.md](architecture/README.md). It indexes every doc with a
one-line summary and suggests a reading order per task. Open the doc for your
concern before scanning the code; it lists the files and symbols to read.

Keep them true. The docs are only useful if an agent can trust them without
re-checking the code:

- A change that alters behavior, a flow, a file's role, a public name, a
  flag, an error message, an exit code, an invariant, or a rule described in
  an architecture doc updates that doc in the same commit.
- A new subsystem gets a new `architecture/<concern>.md` in the same shape
  (intro with "Read this when", Key files, Flow, Behavior, Gotchas, Related)
  and a line in `architecture/README.md`. A removed or renamed file or symbol
  is removed or renamed in every doc that cites it.
- Code comments cite a doc by path (`architecture/plan.md`), never by
  section number or line number.
- Cite files and symbols, never line numbers. Describe only what the code
  does; plans go in worklogs or issues.
- If a doc disagrees with the code, the code wins: fix the doc in your
  change, or say so if you cannot.

## Architecture

duls prints the size view of a directory: the view slivingdoc prints when an
initial pull materializes a notebook into an empty directory. It walks one
directory without reading content, counts the lines of the files the view
can show, builds an outline of the largest entries, and answers a colour
terminal with the visual view and a pipe with a JSON document. It has no
server, no network, and no configuration file. The only state beyond the
directory it reads is a cache of measured line counts.

The contract is split by concern under [`architecture/`](architecture/README.md).
Two forms share one set of facts. **Visual** is what a person at a colour
terminal reads: a coloured strip, a blank line, the dimmed header, and a boxed
row per entry. **Agent** is what a script reads: one JSON document of the
plan's facts, exact integers, null for a quantity the run did not prove, and
no escape byte. A pipe, a file, `/dev/null`, or a non-empty `NO_COLOR` gets the
document, and `-json` always does.

```text
                       duls [flags] [path]
                              |
                              v
+--------------------------------------------------------------+
|  main.go -> internal/cli                                     |
|  router: parse | help | version | one error line             |
+----------------------+---------------------------------------+
                       |
                       v
+--------------------------------------------------------------+
|  cmd/duls: Command, Parse, Run, Help                         |
+----------------------+---------------------------------------+
                       |
                       v
+--------------------------------------------------------------+
|  internal/app: resolve | walk | count | plan | answer | store|
+------+----------------+-----------+----------+----------------+
       |                |           |          |
       v                v           v          v
+--------------+ +------------+ +----------+ +----------------+
| internal/scan| | internal/  | | internal/| | internal/view  |
| walk, hidden | | measure    | | plan     | | strip, columns,|
| rule, kids,  | | policy,    | | order,   | | marks, footer  |
| witness, dev | | proof,     | | promote, | |                |
|              | | accounting | | fold     | |                |
+--------------+ +-----+------+ +----------+ +----------------+
                       |
                       v
                 +------------+  +--------------+  +------------+  +--------------+
                 | internal/  |  | internal/    |  | internal/  |  | internal/    |
                 | cache      |  | frame        |  | tui        |  | agent        |
                 | shards,    |  | trigger,     |  | form gate, |  | document,    |
                 | witness,   |  | repaint,     |  | palette,   |  | keys, null,  |
                 | windows    |  | erase        |  | width      |  | accounting   |
                 +------------+  +--------------+  +------------+  +--------------+
```

Runtime layout:

```text
argv                 the process command line
stdout               the answer (visual view or JSON document), help, or the
                     version line
stderr               exactly one "error: <message>" line on failure
working directory    the default root; a relative path resolves against it
directory            regular files only; entries never a symlink; hidden
                     skipped unless --all; a root named through a symlink
                     is resolved
```

### Package Map

```text
duls/
|-- main.go                  entry point: cli.Run over os.Args
|-- cmd/duls/                the command: Command, Parse, Run, helpText
|-- internal/
|   |-- app/                 process body: Options, Run, Request, countSink,
|   |                        openStore, resolveRoot, writeAgent, Version
|   |-- agent/               Encode, Document, Totals, Accounting, entries
|   |-- cache/               Open, Store, FileLines, StoreFact, Flush, Close
|   |-- cli/                 the router: Run, withProcessEnv, the command view
|   |-- frame/               New, Rasterizer, Publish, Erase
|   |-- measure/             Counter, Count, Plan, Options, Result, Fact
|   |-- plan/                Planner, Plan, View, Entry, Totals, Measured
|   |-- scan/                Walker, Walk, Snapshot, Dir, Sink, Preset
|   |-- tui/                 Form, DetectForm, Style, DepthOf, IsTerminal, Width
|   |-- view/                Renderer, Lines, Options
|   `-- integrationtest/
|       |-- itest/           the black-box toolkit: build once, run, PTY
|       `-- cli/             the behavioral scenarios
|-- architecture/            one doc per concern; index in README.md
|-- img/                     the demo recording: tape, setup, fixture notebook, gif
`-- Makefile                 test / lint / fmt / build / qa / cover / clean
```

Interfaces belong to the package that consumes them. `internal/cli` owns its
`command` view (three calls). `internal/app` owns the `Options` stream,
environment, form, style, and width seams. `internal/measure` consumes
`scan.Dir` and offers facts to `measure.Measured`, which `internal/cache`
implements. No package exists only to forward a function.

### Event Flow

```text
Public surface:  duls [flags] [path]
                 -a|--all  -bytes  -json  -h|--help  -v|--version

cli.Run
  withProcessEnv      nil Env becomes os.Environ()
  router.run
    command.Parse       flag runs interleaved with positionals; "--" ends flags
                        at most one positional, the path
    command.Run         --version -> "duls <version>\n"
                        else app.Run

app.Run
  ctx.Err?             return the wrapped cancellation
  resolveRoot          empty -> ".", relative -> cwd join, absolute -> clean,
                        EvalSymlinks so a linked root is walked as its target
  os.Stat              a missing path or a non-directory is an error
  scan.Preset          mode -> workers, read buffer, nice
  frame.New            enabled only for the visual form
  cache.Open           disabled when the store cannot or must not exist
  scan.Walk            phase 1: readdir and lstat only; hidden skipped unless
                        --all; regular files only; another device left open;
                        kids, totals, frontier
  scan.FilesystemOf    the root's device: total, used and available bytes,
                        absent when the platform cannot report it
  measure.Count        phase 2: fixed order, policy, witness, proof
  plan.Plan            rows: byte order, promote, fold, peek, totals
  agent form           agent.Encode -> one JSON document
  visual form          view.Lines width clamp [40,150], then frame.Erase
                        before the first byte of the answer
  io.WriteString       stdout; a write failure is an error
  store.Flush, Close   after the answer, and a failure is a miss
```

## Startup Wiring (`main.go`)

`main.go` is one call: `os.Exit(cli.Run(ctx, os.Args, app.Options{Stdout:
os.Stdout, Stderr: os.Stderr}))`. The router owns the command line:

1. **Fill the environment.** A nil `Options.Env` becomes `os.Environ()`, so
   the process and the form gate read the same variables. Tests pass their
   own.
2. **Parse.** `cmd/duls` parses flags and positionals in one pass that
   tolerates flags after the path and honours `--`. `-h` and `--help` become
   `flag.ErrHelp`; the router prints the help text on stdout and exits 0.
3. **Run.** `--version` prints `duls <version>` and exits 0. Otherwise
   `app.Run` resolves, inspects, scans, builds, and writes the answer.
4. **Report a failure.** Exactly one line on stderr, `error: <message>`, and
   exit 1. No stack trace, no log record, no second line.

`app.Version` is the one package-level value. The linker injects it through
`-X github.com/baalimago/duls/internal/app.Version`; a development build
keeps `0.1.0-dev`. Every other dependency is injected.

## Integration Tests

Any new feature or behavior change starts by wrapping it in black-box
integration scenarios in `internal/integrationtest/`. The scenarios ARE the
feature's behavioral contract. Write them before or alongside the
implementation, never after.

The suite is a toolkit plus one concern package. `internal/integrationtest/itest`
is a real (non-test) package and the only shared surface: it builds the
binary once, runs it as a real process, sanitizes the environment, strips
ANSI escapes, and provides the pseudo-terminal runner on Linux. The scenario
package `cli` holds `_test.go` files only, one file per use case named
`<topic>_integrationtest_test.go`, plus its own `main_test.go` calling
`itest.Main`. A helper that more than one package needs goes into `itest`,
never into a scenario file that another package would have to import.

- **Black-box means events in → events out.** A scenario runs the real
  binary; it does not call an internal function.
- **Scenarios carry the spec so prose does not have to.** Where prose and a
  passing scenario disagree, the scenario is the spec.
- **Unit tests cover what scenarios cannot reach cheaply** — pure functions,
  defensive branches, the layout at an exact width and style. Do not
  duplicate full event chains at both levels.
- **When a feature breaks existing integration tests, update the tests to
  match the feature.** Scenarios encode current intended behavior, not frozen
  history. Before you rewrite an assertion, understand what it protected. If
  the breakage is a side effect rather than the feature's purpose, it is a
  regression finding, not a test to silence.

## Conventions

**Error wrapping.** `fmt.Errorf("package: what failed: %w", err)`. The package
name leads, and the operation follows. Wrap the cause with `%w`, not `%v`, so
callers can use `errors.Is`.

**Invariants that a change must not break.** These come from the accepted
architecture, and a change that touches one updates the matching doc under
`architecture/` in the same commit:

- The answer is a pure function of the facts and the completeness it declares:
  the same facts produce the same output on every platform. A run that reused
  stored facts says so, and a cut run is a declared partial view.
- A file's lines follow the LF rule: one per LF, plus the final run when the
  content does not end in LF; empty content has zero lines; a CR is part of
  its line.
- Phase 1 opens no file. A walk failure returns an error and no partial view.
  A permission-refused directory below the root is not a walk failure: it is a
  declared open subtree, and a vanished entry is skipped.
- Size is bytes. Rows are ordered and shared by `st_size`, and a row's share is
  never a different quantity from its size column.
- No unknown cell and no false zero: a value the run cannot prove renders `?`
  or `~` in the visual form, is `null` or absent in the document, and is never
  a zero. A policy-skipped file leaves the line denominator and is counted in
  the output. A refused directory leaves its subtree open, so every total above
  it is a lower bound and the footer counts it.
- The footer and `accounting` are facts about this run: the phase that
  stopped, the limit it obeyed, the counts, and the cache token. Never elapsed
  time, never an age. A run whose totals are lower bounds also states the root's
  device, the figure `df` prints, because the device allocates what the walk
  could not reach; an exact run states none.
- The agent document carries no escape sequence and every number in it is the
  plan's exact value; `-bytes` changes the visual form only. A non-terminal
  stream, a non-empty `NO_COLOR`, and `-json` all select it, and such a stream
  never gets a frame.
- The visual form adds presentation, never a fact: colour, the strip, its
  blank line, the boxes, and SI units over the same facts.
- The walk never follows a symlink below the root and never opens a special
  file. A root named through a symlink is resolved. Hidden entries are
  skipped unless `--all` is set. A directory on another device than the
  root's is not entered, and the footer counts it.
- The bounds are constants: at most 8 entries, at most 6 largest children per
  entry. No flag changes them.
- The visual layout is the one slivingdoc prints for an initial pull, pinned by
  a scenario at 150 columns; the document carries the same facts.
- The cache is derived state: deleting it changes only speed and the footer's
  cache token. A cache failure is a miss, never an error line or an exit code.
  A run never shows its store: a store inside the scanned root is left out of
  the walk, and a root inside the store disables the store.
- A spent deadline renders what is held and exits 0; a cancelled context is an
  error with no view. The frame is erased before the first byte of the answer.
- Every goroutine the walk or the count starts has exited before it returns,
  on success, on error, on deadline, and on cancel.
- A function that returns an error never panics, and a failure is returned,
  never logged.
- The MCP, storage, network, and configuration surfaces do not exist here.
  duls reads one directory and writes one view; a feature that needs more
  belongs in another tool.

**Exit codes.** The categories are stable API: 0 for a printed view, a declared
partial view, help, and version; 1 for every failure. Error text can change.
The exit code and the one-line stderr contract must not.

## Duplication policy

Duplication is not always a defect. `dupl -t 80` is a signal, not a verdict.
Use these principles to decide whether a reported clone needs fixing.

### Acceptable duplication (do not refactor)

- **Interface + fake mirroring.** A fake that mirrors an interface's method set
  is the proof that the contract is satisfiable.
- **Thin wrappers over a shared helper.** Several small functions that differ
  only in a key kind or namespace and all delegate to one implementation are
  the abstraction, not a clone.
- **Cross-package contract assertions.** The unit tests and the black-box
  scenarios assert the same view from opposite sides; merging them makes the
  black-box suite depend on a test helper of the package it observes from
  outside.
- **Test-setup boilerplate.** The per-test harness preamble (temp roots,
  fixtures, cleanup) is structural, not logical, duplication.
- **Table-driven test loops.** `for _, tt := range tests { t.Run(tt.name, …`
  is the idiom, not a clone.

### Actionable duplication (fix these)

- **Two or more functions or tests whose bodies differ only in parameterised
  values** (paths, error sentinels, input payloads, expected strings). Merge
  them into a table-driven test or extract a parameterised helper.
- **Production code where the same sequence of operations appears verbatim**
  with different call-site constants. Extract a function.
- **Identical setup + teardown across >3 tests in the same file.** Extract a
  test helper (`newTestXxx`) local to that file.

## Code style

- Encode any failure in expectation as an error. If `findData(path)` returns
  `(data, error)` and does not find data, that is an error. Encode why in the
  error. Same goes for any absence.
- Return types are self-describing. A bare `bool` or a naked `int` beside a
  value forces the meaning into a comment or into the call site's variable
  name — return a named type instead: an enum for a classification, a count
  for a quantity. If a return value needs a comment to explain it, it needs a
  type.
- Never "log error and return", always return the error. This leaves a much
  more testable solution.
- If in an async routine, create an error channel passed to the parent, who
  then is responsible to manage the error.
- Do not leave bloated redundant comments. Private functions rarely need any
  comments at all. Public functions should only describe non-intuitive
  functionality.
- Make all public functions intuitive via typed return values (including
  typed errors).
- No reusable component should ever log. Return data should be
  self-descriptive via errors and types, described above.
- If some piece of code is written twice, it should be abstracted.
- Never panic in a function which returns an error.
- Return an error on every failure except in utmost circumstances.
- Avoid package-level state, always inject dependencies.

## QA validation

Run `make qa`. It runs `lint` and `test`. Before signing off on ANY change,
every gate below must pass. Tool versions are pinned to the implementation
baseline; do not substitute `@latest`.

There is **one** Go test command. No target, build tag, environment variable,
or flag runs a subset of the suite.

| Tool        | Command                                                        |
| ----------- | -------------------------------------------------------------- |
| Format      | `go run mvdan.cc/gofumpt@v0.11.0 -w -l .`                      |
| Staticcheck | `go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...`       |
| Lint        | `go vet ./...`                                                 |
| Fix         | `go fix -diff ./...` (must print nothing)                      |
| Test        | `make test` (race, 3 counts, 30 s, coverage with a 70 % floor) |
| Dupl        | `go run github.com/mibk/dupl@v1.0.0 -t 80 .`                   |

The dupl check is a signal, not a verdict — see the Duplication policy above.

`make test` **MUST** pass unedited. The strictness is intentional: it produces
a highly testable, efficient system that follows strict inversion of control.
Do not modify the timeout, count, or race. Do not add test skips,
false-positive tests, or any other cheat. Instead, start testing early and
ensure that the test passes for each new modification.

A `testing.Short()` guard or a second "full gate" command is the same cheat
wearing a different hat: it advertises a fast path that hides coverage. The
only legitimate skip is a genuine platform capability the host cannot
provide, and it must name that capability. The pseudo-terminal scenarios skip
with `a pseudo-terminal is required: <cause>` when `/dev/ptmx` is absent; no
other skip is allowed.

**Testing coverage** must stay above 70 %. 90 % is preferred; the tree is at
90 % today. `make cover` opens the profile from the last run.

[`architecture/testing.md`](architecture/testing.md) records the test layers,
the concurrency and timeout rationale, and the coverage measurement details.

**General programming guidelines:**

- Leave minimal comments, prefer pointing at documentation/architecture via
  fully descriptive links:
  `// Call does X (architecture/plan.md).`
