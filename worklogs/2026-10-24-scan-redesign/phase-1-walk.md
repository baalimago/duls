# Phase 1 — walk

**Parent:** root · **Class:** contract · [README](README.md)

## Goal

Enumerate a directory tree in parallel without reading file content, and return
a fact set whose values do not depend on the schedule, together with a declared
completeness.

## Interface

Written to `internal/scan/walk.go`.

```go
package scan

import (
	"context"
	"time"
)

// Mode names one of the four presets -m selects (README D8).
type Mode uint8

// Modes after what the running computer fan will act like
const (
	ModeOwl  Mode = iota 		// 1 worker, 64 KiB readdir buffer, nice 10
	ModeDefault             // 4 workers, 256 KiB, nice 5
	ModeDrone               // GOMAXPROCS workers, 1 MiB, nice 0
	ModeHelicopter          // 4*GOMAXPROCS workers, 4 MiB, no caps
)

// Preset returns the options of a mode.
//
// - Reports false for a mode above ModeHelicopter and returns zero WalkOptions. (Preset.1)
// - Fills Workers, Buffer and Nice; leaves All at zero for the caller to set
//   from flags. (Preset.2)
// - Workers and Buffer are always at least 1. (Preset.3)
func Preset(m Mode) (WalkOptions, bool)

// WalkOptions are the parameters of one walk. The zero value walks one directory
// with one worker.
// Skip holds absolute paths of entries to leave out: a skipped directory is
// never listed and its subtree never counts, and a skipped file counts for
// nothing. The composition skips the store's directory when it lies inside the
// root, so a run never reports the files it writes (I10).
type WalkOptions struct {
	All     bool
	Skip    []string
	Workers int
	Buffer  int
	Nice    int
}

// Stop names why a walk ended before it finished.
type Stop uint8

const (
	StopNone     Stop = iota // the walk finished
	StopDeadline             // the run's deadline cut the walk
)

// Witness proves a fact about a file or a directory.
type Witness struct {
	Dev, Ino    uint64
	Size        int64
	Mod, Change time.Time
}

// Child is one named child of a directory, kept only for the six largest.
// Lines is -1 when the run has not measured it.
type Child struct {
	Name  string
	Dir   bool
	Bytes int64
	Files int
	Lines int64
}

// Dir is one directory the walk read. Path is slash-separated, relative to the
// root, and "" is the root itself. Files and Bytes are subtree totals; a total
// is a lower bound when its subtree intersects Snapshot.Frontier.
type Dir struct {
	Path    string
	Files   int
	Bytes   int64
	Kids    []Child
	Witness Witness
}

// Snapshot is one walk's fact set. Frontier contains slash-separated relative
// directory paths with unfinished enumeration; "" means the root is unfinished.
type Snapshot struct {
	Root       string
	Dirs       []Dir
	Frontier   []string
	Files      int
	Bytes      int64
	Unreadable int
	Stopped    Stop
}

// Sink receives monotone progress from a walk or a count (README D29).
type Sink interface {
	// Publish records one observation.
	//
	// - Files, Bytes, Lines and Dirs are cumulative counters; each is greater
	//   than or equal to its value in the previous observation of the same
	//   phase. (Publish.1)
	// - A walk publishes after each 4096 files per worker with PhaseWalk, and
	//   cumulative Files, Bytes and Dirs counters; Bytes is the byte total of
	//   the files already counted, and Lines is zero during the walk. Dirs
	//   counts completed directories. (Publish.2, D29)
	// - Publish never blocks: a sink that cannot keep up drops the update. (Publish.3)
	Publish(p Progress)
}

// Progress is one cumulative observation of work done. Files counts completed
// files, Bytes counts their bytes, Lines counts measured lines, and Dirs counts
// completed directories.
type Progress struct {
	Phase Phase
	Files int64
	Bytes int64
	Lines int64
	Dirs  int64
}

// Phase names the stage that produced a Progress.
type Phase uint8

const (
	PhaseWalk Phase = iota
	PhaseCount
)

// Walker enumerates a tree without reading file content.
type Walker interface {
	// Walk enumerates root and returns its fact set.
	//
	// - Dirs is in DirSeq order: a depth-first flatten, root first, siblings
	//   by subtree bytes descending then name; the order is total. (Walk.1)
	// - The result is a fact set with declared completeness; every reported
	//   directory is enumerated, and Frontier identifies each unfinished
	//   subtree after a deadline. (Walk.2, I1, I9, I18)
	// - Only readdir and lstat are used; no regular file is opened. (Walk.3, I16)
	// - The fact set is independent of Workers and of interleaving; a tie
	//   inside a directory breaks by name. (Walk.4, I17)
	// - Kids holds at most the six largest children by bytes, ties by name,
	//   and no per-file value survives the walk. (Walk.5, I21)
	// - context.DeadlineExceeded returns the facts held, Stopped set, and a
	//   nil error. context.Canceled returns a wrapped error and no snapshot.
	//   (Walk.6, I11)
	// - A directory that cannot be read, a child that cannot be stat'ed, or a
	//   root that is not a directory returns a wrapped error and no snapshot.
	//   Permission refusal for a regular file keeps its file and byte facts
	//   and increments Unreadable; no other error is classified as permission.
	//   (Walk.7, I24)
	// - Every goroutine it started has exited before it returns. (Walk.8, I19)
	// - A root named through a symlink is resolved before the walk starts, and
	//   hidden entries are skipped unless All is true; only regular files
	//   count, and no symlink below the root is followed. (Walk.9, I8)
	// - On failure, the first observed failure cancels remaining work and no
	//   snapshot is returned. (Walk.10, I20)
	Walk(ctx context.Context, root string, opts WalkOptions, sink Sink) (Snapshot, error)
}
```

Consumes: None.

## Acceptance criteria

| #   | Criterion                                                                             | Proves                                                                   | Proof                            |
| --- | ------------------------------------------------------------------------------------- | ------------------------------------------------------------------------ | -------------------------------- |
| 1   | Two walks of one tree with different `Workers` return equal snapshots                 | Walk.4, I17                                                                      | `TestWalkIsScheduleIndependent`  |
| 2   | `Dirs` is ordered root-first, then by subtree bytes descending, then by name          | Walk.1                                                                   | `TestDirsAreInDirSeqOrder`       |
| 3   | A 1 MiB regular file is walked without a read syscall on it                           | Walk.3, I16                                                                      | `TestWalkNeverOpensAFile`        |
| 4   | A deadline identifies every unfinished subtree and returns no error                   | Walk.2, Walk.6, I11, I18                                                         | `TestDeadlineReturnsSnapshot`    |
| 5   | A cancel mid-walk returns a wrapped `context.Canceled` and no snapshot                | Walk.6, I11                                                              | `TestCancelReturnsError`         |
| 6   | A directory failure returns no snapshot; a permission-refused file remains a fact     | Walk.7, I24                                                              | `TestPermissionPolicy`           |
| 7   | `Walk` leaves no goroutine running, on every exit path                                | Walk.8, I19                                                              | `TestWalkJoinsWorkers`           |
| 8   | Each directory keeps at most six children, the largest by bytes                       | Walk.5, I21, I13                                                                 | `TestKidsAreBounded`             |
| 9   | Modes 0–3 have positive worker and buffer counts; other values are rejected           | Preset.1, Preset.2, Preset.3                                             | `TestPresetBounds`               |
| 10  | Progress carries monotone files, lines and directory counts                           | Publish.1, Publish.2, Publish.3                                          | `TestProgressContract`           |
| 11  | Child symlinks are not followed and hidden entries follow `All`                       | Walk.9, I8                                                               | `TestWalkEntryPolicy`            |
| 12  | The first walk failure cancels other work and returns no snapshot                     | Walk.10, I20                                                              | `TestFirstFailureCancelsWorkers` |
| 13  | A skipped path is absent from the snapshot and from every total                        | Walk.9, I10                                                              | `TestSkipLeavesEntriesOut`           |

## Test cases

| Test                             | Level | Proves                                                                         | Given                                                                      | Then                                                                                                                     | Must not                                                          |
| -------------------------------- | ----- | ------------------------------------------------------------------------------ | -------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------- |
| `TestWalkIsScheduleIndependent`  | unit  | AC1, Walk.4, I17                                                                       | a fixture with 200 files, 20 directories                                   | snapshots for 1, 4 and 16 workers are `reflect.DeepEqual`                                                                | any difference in `Files` or `Bytes`                              |
| `TestDirsAreInDirSeqOrder`       | unit  | AC2, Walk.1                                                                    | `a/` 10 B, `b/` 30 B, `c/` 30 B                                            | `["", "b", "c", "a"]`                                                                                                    | any dependence on readdir order                                   |
| `TestWalkNeverOpensAFile`        | unit  | AC3, Walk.3, I16                                                                       | a fixture whose open calls are counted                                     | zero opens of a regular file                                                                                             | a read buffer allocated for content                               |
| `TestDeadlineReturnsSnapshot`    | unit  | AC4, Walk.2, Walk.6, I1, I11, I18                                                      | one worker ends in the middle of a depth band                              | frontier identifies unfinished subtrees; `Stopped == StopDeadline`, nil error                                            | an unreported partial subtree                                     |
| `TestCancelReturnsError`         | unit  | AC5, Walk.6, I11                                                               | a context cancelled before the walk                                        | error wrapping `context.Canceled`, zero `Snapshot`                                                                       | a snapshot returned with the error                                |
| `TestPermissionPolicy`           | unit  | AC6, Walk.7, I24                                                               | a fake filesystem: unreadable dir, permission-refused file                 | error naming the dir; the file counted in `Unreadable`                                                                   | an error for the file                                             |
| `TestRootErrors`                 | unit  | Walk.7                                                                         | missing root and a non-directory root                                      | wrapped stat/not-directory error, no snapshot                                                                            | a partial snapshot                                                |
| `TestChildStatFailure`           | unit  | Walk.7                                                                         | a child entry whose `lstat` fails                                          | wrapped stat error, no snapshot                                                                                          | a skipped child                                                   |
| `TestWalkJoinsWorkers`           | unit  | AC7, Walk.8, I19                                                               | success, deadline, cancel, and error paths                                 | `runtime.NumGoroutine` returns to its baseline                                                                           | a worker left blocked                                             |
| `TestKidsAreBounded`             | unit  | AC8, Walk.5, I21, I13                                                                  | a directory with 10 children                                               | 6 kids, largest first, ties by name                                                                                      | a seventh child retained                                          |
| `TestPresetBounds`               | unit  | AC9, Preset.1, Preset.2, Preset.3                                              | modes 0–3 and 4                                                            | positive workers/buffer; All zero; mode 4 reports false                                                                 | a zero worker/buffer or a nonzero policy                            |
| `TestProgressContract`           | unit  | AC10, Publish.1, Publish.2, Publish.3                                          | records walk progress and supplies a non-blocking sink                     | Files, Bytes and Dirs are cumulative at each stride, Lines stays zero, and the sink drops an update rather than blocking | a decreasing counter, a nonzero walk Lines, or a blocking publish |
| `TestWalkEntryPolicy`            | unit  | AC11, Walk.9, I8                                                               | hidden file, child symlink and special file                                | hidden policy applies; link and special file are not counted                                                             | following/opening either                                          |
| `TestSkipLeavesEntriesOut`           | unit  | AC13, Walk.9, I10                                                              | a root with a skipped directory and a skipped file                         | neither appears in `Dirs`, `Kids` or any total                                                                           | a skipped path in the snapshot                                    |
| `TestFirstFailureCancelsWorkers` | unit  | AC12, Walk.10, I20                                                              | two workers, one returns an error                                          | first error returned and other worker joined                                                                             | partial snapshot                                                  |

## Error management

| Failure                               | Expected outcome                                             | Test                          |
| ------------------------------------- | ------------------------------------------------------------ | ----------------------------- |
| Root missing or not a directory       | `scan: stat root: <err>` or not-directory error; no snapshot | `TestRootErrors`              |
| A directory cannot be read            | `scan: read <path>: <err>`, no snapshot                      | `TestPermissionPolicy`        |
| A child cannot be stat'ed             | `scan: stat <path>: <err>`, no snapshot                      | `TestChildStatFailure`        |
| A regular file has permission refused | not an error: `Unreadable++`, its file/byte facts stand      | `TestPermissionPolicy`        |
| The deadline fires                    | nil error, `Stopped: StopDeadline`, frontier returned        | `TestDeadlineReturnsSnapshot` |
| The caller cancels                    | `scan: walk: context canceled`, no snapshot                  | `TestCancelReturnsError`      |

## Golden diff

None.

## Removes

None.

## Local facts

**Invariants.** Failed enumeration does not create a complete aggregate, and no
result carries a value the walk did not observe. Worker completion and the
returned frontier preserve the declared completeness (I18, I19).

**Parameters.** None. Walk parameters are defined in the README Parameters table.

## Human required

None.

## Implementation notes

| Run   | Commit   | Command                | Result                       |
| ----- | -------- | ---------------------- | ---------------------------- |
| start | none     | `make qa` && `dupl -t 80` | pass, coverage 90.0 %     |
| end   | none+wip | `make qa` && `dupl -t 80` | pass, coverage 92.3 %     |
| end   | none+wip | `make qa` && `dupl -t 80` | pass, coverage 92.3 %     |
| replan | none    | A01 ruling (b): drop subtree trust | `Records`, `Record`, `Dir.Trusted` and the store seam removed |

Andon: none.
Surprises:
- Directory witnesses need `syscall.Stat_t`, so witness extraction is split into `witness_linux.go` and a portable fallback.

## Review findings

### Review 1, 2026-10-24

Nothing to fix in this phase.

Verified good: `TestWalkNeverOpensAFile` proves I16 through a fake file system,
`TestWalkIsScheduleIndependent` proves I17 for 0, 1 and 3 workers, and
`TestWalkJoinsWorkers` proves I19 on success, deadline and cancel. A live run
confirms I18: `-t 1ms` on 224 286 files returns a frontier and a marked view.
