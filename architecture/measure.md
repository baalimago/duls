# Measure

How duls counts lines: the fixed dispatch order, the policy that decides what
is measured, the witness that proves a count, the accounting that closes, and
the facts offered to the store.

Read this when: changing what is measured, how a count is proven, what the
footer counts, or how a deadline cuts the count.

## Key files

| File | Purpose |
|------|---------|
| `internal/measure/measure.go` | `Counter`, `Count`, `Plan`, `Options`, `Result`, `Fact`, `Measured`, `denied`, `tick`. |
| `internal/scan/walk.go` | `Dir`, `Child` and `Snapshot`: the dispatch input. |
| `internal/cache/cache.go` | The `Store` that implements `Measured`. |

## Flow

```text
Count(ctx, Plan{Root, Dirs}, Options)
  for each dir in Dirs                DirSeq order from the walk
    readdir                            hidden policy, deny-list, size, witness
    sort admitted files                size descending, then name
    dispatch one file per free worker  no read-ahead queue
      open, chunked read, witness      dev/ino/size/mtime/ctime before and after
      bytes.Count per chunk            LF rule, carry the last byte across chunks
      prove                            witness matches, EOF reached, count <= size
    fold the outcomes                  measured, skipped, unknown, unreadable
    publish                            at most once per 100 ms, forced per directory
  |
  Result{Lines, Measured, Skipped, Unknown, Unreadable, Reused, Named, Stopped}
```

## The order

The order is a function of phase-1 facts and each file's own size, so no
measured count can change what is measured next. Directories come in the
walk's DirSeq order; inside one directory the admitted files go largest
first, ties by name. Top-level files are measured early, because they are the
only rows that print a count.

One file is dispatched per free worker over an unbuffered channel. There is
no read-ahead queue, one reusable chunk per worker, and no file is read whole.
The chunk is `clamp(mode buffer, 16 KiB, 64 MiB / workers)`.

## Policy

An admitted file is a regular file the deny-list does not name, hidden entries
following `--all`. The deny-list names final-extension categories: images,
archives, media, object files and executables. `--all-types` disables it.

A denied file is `Skipped` and is never opened. A file at or above 64 MiB is
also `Skipped` when the run has a deadline; `-t 0` lifts the cap.

A refused open for permission is `Unknown` and `Unreadable`, and it is not an
error. Any other read or close failure returns a wrapped error and no result.

A skipped file leaves the line denominator, so the line total stays exact. An
unknown file stays in the denominator and forces a `~` lower bound.

## Proof

A count is stored only when the witness taken before the read still matches on
the descriptor and on the path after close, the read reached EOF, and the
count does not exceed the file's byte size. A count that fails any test is
discarded, not stored, and not an error.

The settle window is the store's rule: a witness younger than 2 s is not
stored, because the file may still be written.

## Accounting

Each file the walk saw is measured, skipped by a declared policy, or unknown,
exactly once. The footer counts the last two, and measured plus skipped plus
unknown equals the file count the rows report.

`Named` holds the line count of every measured file that is one of its own
directory's six largest children. That is exactly the set the peek column can
print, so a proven count is never shown as `?`.

`Reused` counts the facts a store returned, and the footer prints
`cache reused` when it is nonzero.

## The deadline

`context.DeadlineExceeded` returns the facts held with `Stopped` set and a nil
error. `context.Canceled` returns a wrapped error and no result, and it
outranks the deadline. The first non-context failure cancels the remaining
workers.

Every goroutine the count started has exited before it returns.

## Gotchas

- The count takes its own witness at dispatch. The walk records a witness, but
  a file can change between the phases, so the walk's value proves nothing.
- `Unknown` includes `Unreadable`; the footer prints both counts.
- A directory that cannot be read is a failure, not an unknown: the count
  cannot state a file total without it.
- The store is only reached from the coordinator. No worker touches it, and a
  full mailbox drops a fact rather than blocking a worker.

## Related

- [scan.md](./scan.md): phase 1, which enumerates and stats.
- [cache.md](./cache.md): where a proven fact goes.
- [view.md](./view.md): how the footer states the accounting.
