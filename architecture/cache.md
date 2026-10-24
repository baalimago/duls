# Cache

How duls keeps measured line counts between runs: the sharded store, the key
and its witness, the trust window, the settle window, the cap, and the rule
that a cache failure is a miss.

Read this when: changing what is stored, how a key is formed, how a store is
opened or evicted, or why a second run does or does not reuse facts.

## Key files

| File | Purpose |
|------|---------|
| `internal/cache/cache.go` | `Open`, `Config`, `Store`, `FileLines`, `StoreFact`, `Flush`, `Close`, `writeShard`, `enforce`, `resolve`, `Inside`, `within`, `settled`, `factKey`, `encodeFact`, `decodeFact`. |
| `internal/cache/atomic_internal_test.go` | The shard replacement: the temp file and the rename. |
| `internal/measure/measure.go` | `Measured` and `Fact`: the seam the store implements. |
| `internal/app/options.go` | `Options.cacheDir`: `$XDG_CACHE_HOME`, or `$HOME/.cache`, then `duls/v1`. |
| `internal/app/run.go` | `openStore`, which turns a store failure into a disabled store, and `storeSkip`, which leaves the store's directory out of the walk. |

## Flow

```text
Open(Config{Dir, Root, Cap, Refresh})
  Dir == "" or Refresh <= 0        -> a disabled store
  Root inside Dir                  -> a disabled store
  MkdirAll(Dir)                    -> error, which the caller turns into off
  |
  FileLines(path, witness)         -> a count, or a miss
  StoreFact(Fact{Path, Lines, W})  -> a memory offer, dropped when the fact is young
  Flush(budget)                    -> shard writes, cancelled by the budget
  Close()                          -> release the writer
```

## Placement and key

`$XDG_CACHE_HOME/duls/v1/` holds 256 shard files named `shard-NNN.bin`. An
unset `XDG_CACHE_HOME` falls back to `$HOME/.cache`; with neither set the run
has no store, and no error either.

A fact is keyed by the resolved absolute path plus the `--all` value. The
value is a line count plus the file's witness: `dev`, `ino`, `size`, `mtime`
and `ctime`, and the time the fact was written.

Two files that share a name never share a fact, because the key is a resolved
path and the witness is the file's own.

The store holds measured line counts only. Structure is never stored: every
run enumerates every directory it reports, so a warm run and a cold run agree
on files, bytes and rows.

## The two windows

The trust window is `--refresh`, default 15 minutes. A stored fact is reused
only while `now - written < Refresh`. `-r 0` disables the store entirely: no
fact is read and no fact is written.

The settle window is 2 seconds. A fact whose witness is younger than that is
not stored, because the file may still be written. A refused store costs a
re-read and nothing else.

## Durability and failure

A shard is replaced by writing a temporary file and renaming it, so a writer
killed mid-write leaves the previous shard or none. Each shard carries a
magic string, a version, and a CRC32C over its contents; a shard that fails
either check is removed and all its keys read as misses.

A write merges with the facts already in the same shard. The store is capped
at 32 MB and evicts whole shards, least recently used first, until it is
under 90 % of the cap.

Writes go through a bounded mailbox of 256 facts. A full mailbox drops the
persistence of a fact, never a fact: the run keeps its counts.

No cache call returns an error to the run. `Open` may fail, and the caller
then opens a disabled store. `Flush` may fail, and the caller drops the
error. A cache problem never changes the view, the exit code, or stderr.

## Overlap

A store inside the scanned root stays open, and the composition passes its
directory to `scan.WalkOptions.Skip`, so the walk neither lists it nor counts
the files it writes. The store's directory therefore never has a row.

A root inside the store's directory, or equal to it, disables the store: a run
never reads or writes a store under the root it scans. `Open` compares the two
paths once, in that direction, and returns a disabled store when they overlap.

## Gotchas

- The witness a fact carries is the one the count took at dispatch, not the
  one the walk recorded.
- A stored count is only offered after the count proved it: the witness
  matched on the descriptor and on the path after close, the read reached EOF,
  and the count does not exceed the file's size.
- Deleting the store changes speed and the footer's `cache reused` token. It
  never changes a file count, a byte total, or a row.
- The store is path-keyed, globally. Two different roots that hold the same
  file path at different times share its fact, which is why the witness is
  part of the key.
- A store inside the scanned root is common under `$HOME`: the walk skips its
  directory, so the view never shows the shards, and the second run still
  reuses the facts the first one wrote.

## Related

- [measure.md](./measure.md): the count that produces a fact.
- [cli.md](./cli.md): `-r` and `XDG_CACHE_HOME`.
- [scan.md](./scan.md): `WalkOptions.Skip`, which leaves the store's directory out.
- [errors.md](./errors.md): why no cache failure reaches stderr.
