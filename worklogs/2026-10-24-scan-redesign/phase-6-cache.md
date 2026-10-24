# Phase 6 — cache

**Parent:** root · **Class:** contract · [README](README.md)

## Goal

Store directory aggregates and per-file line counts in a bounded, crash-safe,
path-keyed store that satisfies both consumer seams, and never turns a cache
problem into a run failure.

## Interface

Written to `internal/cache/cache.go`.

```go
package cache

import (
	"time"

	"github.com/baalimago/duls/internal/measure"
)

// Config is one store's parameters.
type Config struct {
	Dir  string
	Root string
	Cap  int64
	Refresh time.Duration
	Now  func() time.Time
}

// Store is the on-disk measured-fact store.
type Store interface {
	measure.Measured

	// Flush writes what the mailbox still holds.
	//
	// - It returns once the mailbox is drained or the budget has passed.
	//   (Flush.1)
	// - The caller drops its error: a cache failure is a miss. (Flush.2, I14)
	// - Each shard is replaced atomically and a write merges with the facts
	//   already in the same shard; a writer killed mid-write leaves the
	//   previous shard or none. (Flush.3, D11)
	Flush(budget time.Duration) error

	// Close releases the store's writer.
	//
	// - It is safe after a failed Flush and safe to call twice. (Close.1)
	Close()
}

// Open opens the store.
//
// - An empty Dir returns a disabled Store and a nil error: every call misses
//   and writes nothing. A Root inside Dir also disables the store. A Dir inside
//   Root keeps the store open, and the caller skips that directory in its walk,
//   so the run never reports the files it writes. (Open.1, I10)
// - Failure to create or use the store directory returns an error in the form
//   `cache: open <Dir>: <err>`; the caller treats it as cache-off. (Open.2, I14)
// - No call on a Store reads or writes a path under Root unless Dir lies inside
//   Root, and the caller then leaves Dir out of its walk. (Open.3, I10)
// - A damaged shard is removed and all its keys read as misses. (Open.4, I14)
// - The cap is enforced by whole-shard eviction until the configured reserve
//   is reached. (Open.5, D11)
// - Keys use the resolved absolute path. A fact remains reusable only within
//   Refresh, retains its LF-rule line count, and persists only the proven fact
//   supplied by the counter. (Open.6, I1, I7, I9, I25)
func Open(c Config) (Store, error)
```

Consumes:

- `scan:Witness (phase 1)`
- `measure:Measured (phase 4)`
- `measure:Fact (phase 4)`

The store implements the count's one seam, `measure.Measured`:
`FileLines(path string, w scan.Witness) (int64, bool)` and
`StoreFact(measure.Fact)`.

## Acceptance criteria

| #  | Criterion                                                                          | Proves        | Proof                          |
| -- | ---------------------------------------------------------------------------------- | ------------- | ------------------------------ |
| 1  | One value of `Store` satisfies the count's seam                                    | Open.1    | `TestStoreSatisfiesTheMeasuredSeam` |
| 2  | A per-file fact is returned only when all witness fields match and it is fresh     | FileLines.1, FileLines.2 | `TestWitnessMismatchAndTrustWindow` |
| 3  | A shard with a bad checksum is dropped and reads as a miss                         | Open.4    | `TestTornShardIsAMiss`         |
| 4  | A store inside the root stays open and is skipped by the walk; a root inside the store disables the store | Open.1, Open.3, I10 | `TestRootOverlap` |
| 5  | A store above its cap evicts whole shards and keeps serving                        | Open.5 | `TestCapEvictsShards`         |
| 6  | A full mailbox drops a fact without blocking or failing                            | StoreFact.1 | `TestFullMailboxDrops`        |
| 7  | A crash leaves the previous shard intact or none at all                            | Flush.3 | `TestShardReplaceIsAtomic`     |
| 8  | `Flush` respects its budget and its error is safe to drop                          | Flush.1, Flush.2 | `TestFlushBudget`              |
| 9  | Two stores over one directory merge rather than lose facts                         | Flush.3 | `TestTwoStoresMerge`          |
| 10 | The key uses the resolved path; a fact respects Refresh and preserves lines        | Open.6, I1, I7, I9, I25 | `TestStoreUsesResolvedPathsAndTrustWindow` |

## Test cases

| Test                          | Level  | Proves              | Given                                  | Then                                              | Must not                       |
| ----------------------------- | ------ | ------------------- | -------------------------------------- | ------------------------------------------------- | ------------------------------ |
| `TestStoreSatisfiesTheMeasuredSeam` | unit | AC1, Open.1    | one open store                         | it satisfies `measure.Measured`                   | a missing method               |
| `TestWitnessMismatchAndTrustWindow` | unit | AC2, FileLines.1, FileLines.2 | a fact with changed witness and ages inside/outside Refresh | only exact, fresh fact hits | a partial or stale hit |
| `TestTornShardIsAMiss`        | unit   | AC3, Open.4         | a shard file with a flipped byte         | a miss, and the shard is removed                  | a panic or an error            |
| `TestRootOverlap`            | unit   | AC4, Open.1, Open.3, I10 | store inside root; root inside store | the first keeps the store and serves a fact; the second is disabled and writes nothing | a fact under the root |
| `TestCapEvictsShards`         | unit   | AC5, Open.5         | a store filled past its cap              | the store stays under the cap and still reads hits | a fact lost from a kept shard  |
| `TestFullMailboxDrops`        | unit   | AC6, StoreFact.1    | a store whose writer is blocked          | every offer returns immediately                   | a block, an error or a panic   |
| `TestShardReplaceIsAtomic`    | unit   | AC7, Flush.3        | a rename interrupted by a fake failure   | the previous shard still loads                    | a half-written shard trusted   |
| `TestFlushBudget`             | unit   | AC8, Flush.1, Flush.2 | queued facts and a short budget        | returns within budget; error can be dropped       | a hang past the budget         |
| `TestTwoStoresMerge`          | unit   | AC9, Flush.3        | two stores over one directory, disjoint keys | both facts readable after the second flush      | a lost fact                    |
| `TestSettleWindow`            | unit   | StoreFact.2         | a witness inside and outside the window  | young facts are not persisted; old facts are      | a young fact                   |
| `TestDisabledStoreIsSilent`   | unit   | Open.1              | `Dir: ""`                                | every call misses, nothing is created             | a directory created anyway     |
| `TestStoreRetainsLineCounts`  | unit   | FileLines.3, I7     | an LF-rule count, then a store round trip | the same count is returned                       | a changed count                |
| `TestStoreUsesResolvedPathsAndTrustWindow` | unit | AC10, Open.6, I1, I7, I9, I25 | symlink root, normalized path, proven facts within and beyond Refresh | only normalized fresh fact hits; line count is unchanged | stale, unproven or aliased path hit |
| `TestCloseAfterFailedFlush`  | unit   | Close.1             | a failed Flush, then two Close calls      | no panic or second release                        | an error or double close       |
| `TestOpenFailureDisablesCache` | unit | Open.2, I14       | a store directory that cannot be created | error returned; caller can continue without cache | cache error as run failure     |

## Error management

| Failure                        | Expected outcome                                              | Test                     |
| ------------------------------ | ------------------------------------------------------------- | ------------------------ |
| The store directory cannot be created or used | `cache: open <Dir>: <err>`, the caller runs without a store | `TestOpenFailureDisablesCache` |
| A shard is torn or corrupt     | a miss; the shard is removed; no error                          | `TestTornShardIsAMiss`   |
| The mailbox is full            | the fact is dropped; no error                                   | `TestFullMailboxDrops`   |
| The flush budget expires       | the remaining facts are dropped; the error is dropped by the caller | `TestFlushBudget` |

## Golden diff

None.

## Removes

None.

## Local facts

**Invariants.** A shard is replaced by writing a temporary file in the same
directory and renaming it over the shard, so a reader sees the old file or the
new one. The index is sorted by key hash and a lookup is one open plus a binary
search. Keys are the resolved absolute path; a measured fact adds the five witness
fields to the value, not the key. The store keeps no
names, no inodes and no tree mirror. Eviction is shard-granular: a shard is
dropped whole, oldest first, until the store reaches its configured reserve.

**Parameters.**

| Parameter       | Default            | Decided by |
| --------------- | ------------------ | ---------- |
| Eviction target | `90 %` of the cap  | phase 6    |

## Human required

None.

## Implementation notes

| Run   | Commit   | Command                    | Result                   |
| ----- | -------- | -------------------------- | ------------------------ |
| start | none     | `make qa` && `dupl -t 80`  | pass, coverage 90.7 %    |
| end   | none+wip | `make qa` && `dupl -t 80`  | pass, coverage 90.1 %    |
| end   | none+wip | `make lint` && `make test` && `dupl -t 80` | pass, coverage 90.1 % |
| replan | none    | A01 ruling (b): the directory aggregate is gone | `Store` keeps `measure.Measured`; `Config.All`, `DirRecord` and `StoreDir` removed |

Andon: none.
Surprises:
- `TestShardReplaceIsAtomic` needs the rename seam, so it is an internal test that type-asserts the store and replaces `rename`.

## Review findings

### Review 1, 2026-10-24

Nothing to fix in this phase.

Verified good: a cache failure is always a miss — `TestTornShardIsAMiss`,
`TestDisabledStoreIsSilent` and `TestOpenFailureDisablesCache` each prove that
no error reaches the run — and `TestRootOverlap` proves I10 in both
directions. A live run on a 6-file tree shows the store holding two shards, the
warm second run printing `cache reused`, and no store file under the root.
