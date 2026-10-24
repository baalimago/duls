# Errors

How duls reports a failure: the wrapping rule, the one line on stderr, the
exit code, and the failures that are not errors at all.

Read this when: adding an error, changing an error message, or changing an
exit code.

## Key files

| File | Purpose |
|------|---------|
| `internal/cli/cli.go` | `router.fail`: one `error: <message>` line on stderr. |
| `cmd/duls/duls.go` | `Parse` and `validate`: the flag and path errors. |
| `internal/app/run.go` | `resolveRoot` and `openStore`: the path, inspect, and not-a-directory failures, and the store fallback. |
| `internal/scan/walk.go` | The walk failures, each wrapped with the path. |
| `internal/measure/measure.go` | The read and close failures of the count. |
| `internal/cache/cache.go` | The one subsystem whose failures never reach stderr. |

## Behavior

**Wrap with `%w`.** The convention is
`fmt.Errorf("package: what failed: %w", err)`, with the package name first
and the operation after. A caller can use `errors.Is` and `errors.As` on the
cause; the router does exactly that for `flag.ErrHelp`.

**One line, always.** `router.fail` replaces every newline in the message with
a space and writes `error: <message>\n` to stderr. stdout stays empty on a
failure. A script can read the whole refusal with one read.

**Exit codes.** 0 for a printed view, a declared partial view, help, and
version. 1 for every error.

**What a message may name.** A local CLI tool may name the path it could not
inspect or read; that is the operator's next step. It never prints a
credential, because it reads none. It never prints a stack trace and never
panics in a function that returns an error.

## What is not an error

A spent deadline is not a failure. `context.DeadlineExceeded` stops the walk
or the count and the run prints a declared partial view with exit 0.

A cancelled context is a failure: `context.Canceled` returns one error line
and exit 1, with no view. Cancellation outranks a deadline.

A refused file is not a failure. A regular file whose open is refused for
permission keeps its file and byte facts, shows `?` for its lines, and is
counted in the footer; the run exits 0.

A refused directory below the root is not a failure either. The walk leaves
it open, the totals it feeds become lower bounds, the footer counts it, and the
run exits 0. A refused **root** is a failure: the run cannot state a file or
byte total for the directory it was asked about, prints no view, and exits 1.

An entry that vanished between the listing and the `lstat` is not a failure:
no fact remains to prove, so the walk skips it.

A cache failure is not a failure. A store that cannot be opened is replaced
by a disabled store; a store write that fails is dropped. Neither reaches
stderr, the view, or the exit code.

## Gotchas

- A flag failure carries the `run 'duls -h' for its flags` hint; a path-count
  failure does not.
- `errors.Is(err, flag.ErrHelp)` is the help path, not a failure path.
- Wrapping twice with the same package name reads as `duls: duls: ...`. Wrap
  at the layer that owns the operation.
- Only a permission error takes the refused-file branch. A read failure, a
  close failure, or a failed witness is a different outcome.
- The walk classifies one permission case per kind: a refused file keeps its
  facts and is counted, a refused directory is declared an open subtree. A
  `lstat` that reports the entry is gone is a skip, not a policy.

## Related

- [cli.md](./cli.md): the command surface and exit codes.
- [overview.md](./overview.md): the invariants.
- [measure.md](./measure.md): the policy outcomes that are not errors.
