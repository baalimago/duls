# Phase 5 — frame

**Parent:** root · **Class:** contract · [README](README.md)

## Goal

Draw the live outline into a terminal as the scan reports progress, with
line-granular cursor movement, and erase it before the real answer is written.

## Interface

Written to `internal/frame/frame.go`.

```go
package frame

import (
	"io"
	"time"

	"github.com/baalimago/duls/internal/scan"
)

// Options configure one rasterizer.
type Options struct {
	Out      io.Writer
	Enabled  bool
	Trigger  time.Duration
	Interval time.Duration
	Now      func() time.Time
	Lines    func(scan.Progress) []string
}

// Rasterizer draws the live outline into a terminal.
type Rasterizer interface {
	// Publish offers one progress observation to the frame.
	//
	// - It is a no-op when Enabled is false, for every call; a disabled frame
	//   emits no escape sequence. (Publish.1, I5)
	// - The first frame appears no earlier than Trigger after New, and never
	//   before the first observation. (Publish.2, I12)
	// - At most one frame is written per directory and per Interval. (Publish.3, I12, D29)
	// - A frame is written only when its lines differ from the frame on screen;
	//   a line that is identical is passed over with a cursor move, and a block
	//   whose height changed is erased and drawn whole. (Publish.4, I12)
	// - The frame is exactly the lines Lines returns for that observation; the
	//   rasterizer adds and drops none, so it adds presentation only. Unknown
	//   and lower-bound marks and the row/child bounds are unchanged. (Publish.5, I3, I6, I13, I15)
	// - An observation whose Files, Bytes, Lines or Dirs counter is lower than
	//   the last one drawn is ignored, so the frame never moves backwards. (Publish.6, D29)
	// - A write failure disables the rasterizer for the rest of the run; it
	//   never returns an error, never panics and never retries. (Publish.7)
	// - The frame's lines come from the same planner and renderer as the answer;
	//   it defines no ordering or output facts. (Publish.8, I15)
	scan.Sink

	// Erase removes the frame.
	//
	// - It clears exactly the lines of the last frame, and lines nothing else.
	//   (Erase.1)
	// - After Erase, Publish writes nothing and a second Erase writes nothing.
	//   (Erase.2)
	Erase()
}

// New returns a rasterizer for one run.
func New(opts Options) Rasterizer
```

Consumes:

- `scan:Progress (phase 1)`
- `scan:Phase (phase 1)`
- `scan:Sink (phase 1)`

## Acceptance criteria

| #  | Criterion                                                                 | Proves        | Proof                        |
| -- | ------------------------------------------------------------------------- | ------------- | ---------------------------- |
| 1  | A disabled rasterizer writes nothing for any observation                   | Publish.1, I5 | `TestDisabledWritesNothing` |
| 2  | No frame before the trigger, whatever the observation count                | Publish.2, I12 | `TestTriggerDelaysFirstFrame` |
| 3  | Two observations inside one interval produce one frame                     | Publish.3, I12 | `TestIntervalThrottles` |
| 4  | An unchanged line costs a cursor move, not a repaint                       | Publish.4 | `TestUnchangedLinesAreSkipped` |
| 5  | A frame whose height changes draws the whole new block, and the erase clears exactly what was drawn | Publish.4, Erase.1, I12 | `TestFrameKeepsGeometry` |
| 6  | A decreasing observation is ignored                                        | Publish.6 | `TestMonotoneObservations` |
| 7  | A failing writer disables the frame without panicking                      | Publish.7 | `TestWriteFailureDisables` |
| 8  | Erase clears exactly the frame and silences later writes                   | Erase.1, Erase.2, I12 | `TestErase` |
| 9  | Frame lines keep unknown marks and the renderer's row and child bounds      | Publish.5, I3, I13, I15 | `TestFramePreservesViewContract` |
## Test cases

| Test                          | Level | Proves             | Given                                   | Then                                                | Must not                     |
| ----------------------------- | ----- | ------------------ | --------------------------------------- | --------------------------------------------------- | ---------------------------- |
| `TestDisabledWritesNothing`   | unit  | AC1, Publish.1, I5 | `Enabled: false`, 100 observations       | zero bytes written                                  | a frame or a cursor move     |
| `TestTriggerDelaysFirstFrame` | unit  | AC2, Publish.2, I12 | a fake clock and the configured trigger | no write before trigger, a frame at trigger or later | an early frame |
| `TestIntervalThrottles`       | unit  | AC3, Publish.3, I12 | several observations in one directory inside the interval | at most one frame per directory and interval | two frames in one interval or directory |
| `TestUnchangedLinesAreSkipped` | unit | AC4, Publish.4 | two frames differing in one line         | unchanged lines only move cursor                    | a full repaint on frame two  |
| `TestFrameKeepsGeometry`      | unit  | AC5, Publish.4, Publish.5, I12 | `Lines` returns 12 lines, then 13 | the grown block is drawn whole | a stale line or a missing row |
| `TestMonotoneObservations`    | unit  | AC6, Publish.6 | files, bytes, lines or dirs decreases after a drawn observation | the later observation draws nothing | a frame that loses its totals |
| `TestWriteFailureDisables`    | unit  | AC7, Publish.7 | a writer that returns an error            | no panic, no further writes, `Erase` is silent        | an error returned or a retry |
| `TestErase`                   | unit  | AC8, Erase.1, Erase.2, I12 | a three-line frame, then `Erase` twice | one clear sequence, then zero bytes | a clear after the final view |
| `TestEraseWithoutAFrame`      | unit  | Erase.1            | `Erase` before any frame                  | zero bytes                                           | a stray cursor-up            |
| `TestFramePreservesViewContract` | unit | AC9, Publish.5, Publish.8, I3, I13, I15 | supplied renderer lines with `?`, `~`, and bounded rows | frame preserves the lines unchanged | invented facts or extra rows |
## Error management

| Failure            | Expected outcome                                        | Test                       |
| ------------------ | ------------------------------------------------------- | -------------------------- |
| The write fails    | the rasterizer disables itself; the run continues        | `TestWriteFailureDisables` |

## Golden diff

None.

## Removes

None.

## Local facts

**Invariants.** A frame is updated only within the lines it owns. No escape is
emitted when `Enabled` is false; the caller derives this from the stream and
`NO_COLOR` (I5).

**Parameters.** None. Frame timing is defined in the README Parameters table.

## Human required

A pseudo-terminal proves the frame bytes, not its appearance.

- **Artifact:** a recorded judgement in the journal for the README human check.
- **The agent stops** after phases 1–8 are `Complete` and their hermetic gates
  pass. It sets `Line: waiting on human: root` before the root review.
- **Before:** the agent runs the PTY scenarios and records the frame byte
  sequence. It continues with later phases without a human handoff at phase 5.
  **After:** the maintainer runs `duls ~` and `duls -m 0 .` on a real terminal
  at 40, 100 and 150 columns, confirms no flicker, scroll, leftover frame,
  early frame or misplaced final view, and records the result. The maintainer
  sets the line to `running`, then root review resumes.

## Implementation notes

| Run   | Commit   | Command                    | Result                   |
| ----- | -------- | -------------------------- | ------------------------ |
| start | none     | `make qa` && `dupl -t 80`  | pass, coverage 91.1 %    |
| end   | none+wip | `make qa` && `dupl -t 80`  | pass, coverage 90.8 %    |
| end   | none+wip | `make lint` && `make test` && `dupl -t 80` | pass, coverage 90.7 % |
| replan | none    | R1-04: the composition sets the trigger to 0 | the frame draws from the first observation; `TestRunFrameStartsAtOnce` added in phase 7 |

Andon: none.

## Review findings

### Review 1, 2026-10-24

- [x] **R1-02 (minor, maintainer):** the Human check is now recorded. The
      maintainer kept the view and asked for two changes: the frame starts at
      once and fills the detail in, and the strip reaches the full width. Both
      are R1-04 and R1-05; the trigger became the composition's 0 and this
      phase's clauses are unchanged.

Verified good: the frame draws nothing before the trigger, its geometry is the
final view's, and it is erased before the first answer byte. `dupl` reports the
PTY-opening helper twice (here and in `itest`); the policy accepts test-setup
boilerplate of that kind.
