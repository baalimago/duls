# Phase 4 — measure

**Parent:** root · **Class:** contract · [README](README.md)

## Goal

Count the lines of admitted files in a fixed order, with a bounded pool, under
the run's deadline, publishing a fact only when the file's witness survived the
read.

## Interface

Written to `internal/measure/measure.go`.

```go
package measure

import (
	"context"
	"os"
	"time"

	"github.com/baalimago/duls/internal/scan"
)

// Plan is the dispatch input: the walk's directories, in DirSeq order.
type Plan struct {
	Root string
	Dirs []scan.Dir
}

// Measured is the store seam the count consumes. Phase 6 implements it.
type Measured interface {
	// FileLines returns the line count stored for a file with this witness.
	//
	// - It reports false on any witness difference and never returns an error.
	//   (FileLines.1)
	// - It reports false for a fact outside the configured trust window; a
	//   damaged store reads as a miss. (FileLines.2)
	// - A returned count follows the LF rule. (FileLines.3, I7)
	FileLines(path string, w scan.Witness) (int64, bool)

	// StoreFact offers one proven fact to the store.
	//
	// - It never blocks the count and never fails: a full mailbox drops the
	//   fact, which costs a future re-read and nothing else. (StoreFact.1)
	// - It does not persist a fact until its witness is older than the settle
	//   window. (StoreFact.2, I25)
	StoreFact(f Fact)
}

// Fact is one proven line count.
type Fact struct {
	Path  string
	Lines int64
	W     scan.Witness
}

// Options are the count's parameters.
type Options struct {
	Workers int
	Chunk   int
	Large   int64
	All     bool
	Types   bool
	Now     func() time.Time
	Open    func(string) (*os.File, error)
	Store   Measured
	Sink    scan.Sink
}

// Result is what one count proved.
type Result struct {
	Lines      int64
	Measured   int
	Skipped    int
	Unknown    int
	Unreadable int
	Reused     int
	Named      map[string]int64
	Stopped    scan.Stop
}

// Counter counts lines under a deadline.
type Counter interface {
	// Count walks the plan's directories and counts the admitted files.
	//
	// - Directories are visited in plan.Dirs order; no directory is skipped as
	//   trusted. Inside one directory the admitted files are dispatched by size
	//   descending then name, one file per free worker, and no file is dispatched
	//   twice. At dispatch, Count takes its own path witness; it does not use a
	//   per-file witness from the walk. Worker interleaving does not change the
	//   completed measured set. (Count.1, I17, I22)
	// - An admitted file is a regular file whose final extension is not on the
	//   deny-list; an extensionless file is admitted. Options.Types admits
	//   every regular file. Hidden entries follow Options.All. A denied file is
	//   Skipped, never opened. (Count.2, I8)
	// - A file whose size is at least Options.Large, when Large is above zero,
	//   is Skipped without being opened. (Count.3)
	// - A file is read in Chunk-sized pieces over one buffer per worker, and
	//   lines follow the LF rule. Count is the content-reading phase; the walk
	//   does not open regular files. (Count.4, I7, I16)
	// - A fact exists only when the dispatch witness, pre-read and post-read
	//   descriptor witnesses, and the path witness after close agree, the read
	//   reached EOF, and the count does not exceed the file's size; anything else
	//   is Unknown and is not stored. (Count.5, I3, I25)
	// - A store hit with a matching witness is Measured without opening the
	//   file and counts in Reused; the coordinator is the only caller of
	//   StoreFact. (Count.6, I9)
	// - An open refused for permission is Unknown and Unreadable, not an
	//   error. A read or close failure returns a wrapped error and no Result;
	//   a failed witness is Unknown and is not stored. (Count.7, I24)
	// - context.DeadlineExceeded returns the whole facts held with Stopped set
	//   and a nil error. context.Canceled returns a wrapped error and no
	//   Result, and it outranks the deadline when both are observable.
	//   (Count.8, I11)
	// - Measured + Skipped + Unknown equals the number of files dispatched, and
	//   Unreadable is counted inside Unknown. (Count.9, I23)
	// - Every goroutine it started has exited before it returns. (Count.10, I19)
	// - Named holds the line count of every file it measured that is one of its
	//   directory's six largest children by bytes, ties by name, keyed by
	//   root-relative path; it holds no other key. (Count.11)
	// - Cancellation wins over a simultaneous deadline; the first non-context
	//   failure cancels remaining workers. (Count.12, I20, D26)
	// - Count publishes PhaseCount progress through opts.Sink with cumulative
	//   completed Files, measured Lines and completed Dirs counters, and Bytes
	//   held at the walk's byte total. (Count.13, D29)
	Count(ctx context.Context, plan Plan, opts Options) (Result, error)
}
```

Consumes:

- `scan:Dir (phase 1)`
- `scan:Snapshot (phase 1)`
- `scan:Sink (phase 1)`
- `scan:Stop (phase 1)`
- `scan:Witness (phase 1)`

## Acceptance criteria

| #  | Criterion                                                                      | Proves        | Proof                          |
| -- | ------------------------------------------------------------------------------ | ------------- | ------------------------------ |
| 1  | Directories are visited in `Dirs` order and files by size then name             | Count.1, I17, I22 | `TestDispatchOrder` |
| 2  | A nested directory's files are dispatched after every earlier directory's       | Count.1  | `TestDirSeqBeatsGlobalSize`    |
| 3  | A denied extension is never opened, and still counts as a file elsewhere        | Count.2 | `TestDenyListNeverOpens`     |
| 4  | A file above the cap under a deadline is skipped without a read                 | Count.3 | `TestLargeFileSkipped`      |
| 5  | A file above the cap with `-t 0` is measured                                    | Count.3 | `TestLargeFileMeasuredWithoutDeadline` |
| 6  | A file that changes during the read yields no fact and no error                 | Count.5, I25 | `TestChangedDuringReadIsUnknown` |
| 7  | A symlink swapped in after the walk is not followed                             | Count.5, I25 | `TestSymlinkSwapRejected`   |
| 8  | A permission refusal counts as unknown, not as a failure                        | Count.7, I24 | `TestPermissionIsUnknown` |
| 9  | The deadline keeps whole facts and sets `Stopped`; cancel returns an error       | Count.8, I11 | `TestDeadlineAndCancel` |
| 10 | Measured plus skipped plus unknown equals the dispatched count                  | Count.9, I23 | `TestAccountingCloses` |
| 11 | `Named` holds the cells the peek column can show, and no other          | Count.11 | `TestNamedCells/nested`      |
| 12 | A store hit is measured without an open and counted in `Reused`                 | Count.6 | `TestCachedFactSkipsOpen`      |
## Test cases

| Test                                 | Level  | Proves       | Given                                  | Then                                          | Must not                          |
| ------------------------------------ | ------ | ------------ | -------------------------------------- | --------------------------------------------- | --------------------------------- |
| `TestDispatchOrder`                  | unit   | AC1, Count.1, I17, I22 | a fake `Open` that logs paths          | the log equals the comparator order | a file dispatched twice |
| `TestDirSeqBeatsGlobalSize`          | unit   | AC2, Count.1      | two directories in `Dirs` order, first has large files | all first-directory files are dispatched before the later directory | a global size sort |
| `TestDenyListNeverOpens`             | unit   | AC3, Count.2  | `a.PNG`, `b.json`, `c`, `d.gz.txt`     | `a.PNG` skipped, the rest opened               | any open of `a.PNG`               |
| `TestLargeFileSkipped`               | unit   | AC4, Count.3 | a 32 GiB sparse file, `-t 2s`          | `Skipped == 1`, no read                        | a read of the sparse file         |
| `TestLargeFileMeasuredWithoutDeadline` | unit | AC5, Count.3 | the same with `-t 0`                   | a line number, `Skipped == 0`                  | a cap without a deadline          |
| `TestChangedDuringReadIsUnknown`     | unit   | AC6, Count.5, I3, I25 | a fake whose two `fstat` calls differ   | `Unknown == 1`, zero `StoreFact` calls         | a stored fact                     |
| `TestSymlinkSwapRejected`            | unit   | AC7, Count.5, I25 | a real file replaced by a symlink after dispatch | `Unknown == 1`, target never read | a follow through the link |
| `TestPermissionIsUnknown`            | unit   | AC8, Count.7, I24 | `Open` returns `fs.ErrPermission`       | `Unknown == Unreadable == 1`, nil error        | an error                          |
| `TestReadErrorFails`                 | unit   | Count.7      | `Open` succeeds, the reader returns EIO | a wrapped error, no `Result`                   | an unknown fact                   |
| `TestDeadlineAndCancel`              | unit   | AC9, Count.8, I11 | a reader that blocks, then a deadline; then a cancel | deadline: `Stopped`, nil error; cancel: error | a fact from the blocked file      |
| `TestAccountingCloses`               | unit   | AC10, Count.9, I23 | measured, denied, capped and refused files | `Measured+Skipped+Unknown == dispatched` | a file counted twice |
| `TestNamedCells`                     | unit   | AC11, Count.11 | two cases: a root with 10 files of 1–10 bytes, one directory with 7 nested files | 6 entries in `Named`, the sixth largest and not the smallest | an entry outside the six largest   |
| `TestCountJoinsWorkers`              | unit   | Count.10, I19 | success, deadline, cancel paths         | goroutines back to baseline                    | a blocked worker                  |
| `TestLinesAtChunkEdges`              | unit   | Count.4, I7  | LF on every chunk boundary, CRLF, empty, no final LF | counts equal the LF rule           | an off-by-one at a boundary       |
| `TestCancelOutranksDeadlineAndFirstErrorWins` | unit | Count.12, I20 | context cancel plus deadline; then two worker errors | cancellation wins; first error cancels peers | a success result or leaked worker |
| `TestCachedFactSkipsOpen`             | unit   | AC12, Count.6, FileLines.1, FileLines.3, I7 | fresh matching cached line count | measured fact returned with zero opens, `Reused == 1` | a read despite a cache hit |
| `TestCacheReturnsMissOutsideTrustWindow` | unit | FileLines.2 | a fact outside the configured trust window | a miss and normal measurement proceeds | a stale hit |
| `TestFullMailboxDrops`                 | unit   | StoreFact.1 | a full store mailbox during count | the result is complete; persistence is dropped | blocking or an error |
| `TestStoreRetainsLineCounts`            | unit   | FileLines.3, I7 | a cached LF-rule line count | the same exact count is returned | altered line semantics |
| `TestSettleWindow`                    | unit   | StoreFact.2, I25 | a newly proven fact and its witness age | the store does not persist the fact before the settle window | a young stored fact |
| `TestModesHaveSameMeasuredSet`        | unit   | I17 | same fixture with 1, 4 and 16 workers and no deadline | all runs have the same measured, skipped and unknown files | a schedule-dependent set |
| `TestCountProgressContract`           | unit   | Count.13 | a count with a recording sink and several files | PhaseCount progress has cumulative Files, Lines and Dirs counters and the walk's Bytes total | a decreasing counter, a changed Bytes, or a walk-phase observation |
## Error management

| Failure                          | Expected outcome                                                      | Test                  |
| -------------------------------- | --------------------------------------------------------------------- | --------------------- |
| A read, close or metadata call fails | `measure: read <path>: <err>`, no result                              | `TestReadErrorFails`  |
| A witness changes mid-read       | not an error: `Unknown++`, nothing stored                                | `TestChangedDuringReadIsUnknown` |
| Open refused for permission      | not an error: `Unknown++`, `Unreadable++`                                | `TestPermissionIsUnknown` |
| The deadline fires               | nil error, `Stopped: StopDeadline`, whole facts kept                     | `TestDeadlineAndCancel` |
| The caller cancels               | `measure: count: context canceled`, no result                            | `TestDeadlineAndCancel` |
| A denied or oversized file       | not a failure: `Skipped++`                                               | `TestDenyListNeverOpens` |

## Golden diff

None.

## Removes

None.

## Local facts

**Invariants.** One file is in flight per worker, so in-flight bytes depend on
worker and chunk settings rather than file size (D23). The count retains only
the bounded line cells in `Named`: the eight largest root-level files and each
directory's six largest children, which is exactly what a peek can show (I21,
I3). Other facts fold into the aggregate. The coordinator alone calls the store
(D28). A directory is listed again when its turn comes.

**Parameters.**

The extension deny-list is defined in the README Parameters table (D4).

## Human required

None.

## Implementation notes

| Run   | Commit   | Command                    | Result                   |
| ----- | -------- | -------------------------- | ------------------------ |
| start | none     | `make qa` && `dupl -t 80`  | pass, coverage 92.1 %    |
| end   | none+wip | `make qa` && `dupl -t 80`  | pass, coverage 91.1 %    |
| end   | none+wip | `make qa` && `dupl -t 80`  | pass, coverage 91.1 %    |
| replan | none    | A01 ruling (b) and A02 ruling (a) | trusted-directory skip removed; `Named` follows the peek; `Reused` added |
| replan | none    | dupl signal: the two `Named` tests cloned | `TestNamedCells` is table-driven over a root and a nested directory; `TestNamedFollowsThePeek` removed and Count.11 names `TestNamedCells/nested` |

Andon: none.
Surprises:
- A deadline test needs a file that is slow to read; a multi-gigabyte sparse file gives the cut without disk traffic.
- `dupl` reports the fixture helper shared by the scan and measure test packages; the policy accepts test-setup duplication.

## Review findings

### Review 1, 2026-10-24

Nothing to fix in this phase.

Verified good: `Named` is filled from the peek cells only — a root-level file
beyond the six largest and a nested file the directory row cannot show are both
absent, and `TestNamedCells` proves both layouts.
