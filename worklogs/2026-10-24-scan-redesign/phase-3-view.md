# Phase 3 — view

**Parent:** root · **Class:** contract · [README](README.md)

## Goal

Draw a planned view as terminal lines: a byte-first strip and columns, SI units,
the unknown and lower-bound marks, and a footer that states only facts about
this run.

## Interface

Written to `internal/view/view.go`.

```go
package view

import (
	"time"

	"github.com/baalimago/duls/internal/plan"
	"github.com/baalimago/duls/internal/tui"
)

// Options are the renderer's presentation choices.
type Options struct {
	Style tui.Style
	Width int
	Bytes bool
	Limit time.Duration
	// Provisional is set by the frame for a view the run has not finished: the
	// line total is a lower bound and no footer is printed, because the facts
	// the footer states are not final. The answer clears it. (Lines.10, I3, I12)
	Provisional bool
}

// Renderer draws one view.
type Renderer interface {
	// Lines renders v as lines without a trailing newline.
	//
	// - Plain output carries no escape sequence. Styled output stripped of
	//   escapes equals the plain output plus the strip, the blank line and the
	//   row boxes. (Lines.1, I5, I6)
	// - The width is clamped to [40, 150] and no line exceeds it in runes.
	//   (Lines.2)
	// - Line 1 is the totals line: files, size and lines, with `~` before a
	//   lower bound and `?` where a value is unknown; no unknown renders as
	//   zero. (Lines.3, I3)
	// - Line 2 is the header: entry, files, size, share, and the peek column.
	//   (Lines.4)
	// - A directory row shows files, size and share; a file row shows its line
	//   count in the detail column; a folded row shows `+N smaller`. (Lines.5)
	// - The peek column joins at most 6 items with " · ", appends `+N more`,
	//   and is cut with `…` when the width is short. (Lines.6)
	// - A footer follows the rows when the view is partial, stored facts were
	//   reused, or any skipped, unknown or unreadable count is nonzero. It names
	//   the stopped phase and limit when partial, the counts, and the cache token
	//   when stored facts were reused; it never prints elapsed time or a fact's
	//   age. (Lines.7, I1, I4, I23)
	// - Sizes use SI units with one decimal and the 999_500 knee; with Bytes
	//   set they are exact integers of bytes. (Lines.8)
	// - Equal views and options give equal lines; a frame uses these same lines
	//   and adds no ordering or display rule. (Lines.9, I15)
	// - Size, order, and share use bytes; the output depends only on facts and
	//   declared completeness. (I1, I2)
	// - With Provisional set the line total is marked a lower bound and no
	//   footer is printed, so a frame states no fact the run has not proved.
	//   (Lines.10, I3, I12)
	Lines(v plan.View, opts Options) []string
}
```

Consumes:

- `plan:View (phase 2)`
- `plan:Entry (phase 2)`
- `plan:Item (phase 2)`
- `plan:Totals (phase 2)`
- `tui:Style (import)`

## Acceptance criteria

| #  | Criterion                                                                   | Proves      | Proof                          |
| -- | --------------------------------------------------------------------------- | ----------- | ------------------------------ |
| 1  | Plain output at 150 columns carries no `\x1b` byte                           | Lines.1, I5 | `TestPlainHasNoEscapes` |
| 2  | Styled output with escapes removed equals plain plus strip, blank line, boxes | Lines.1, I6 | `TestStyledEqualsPlain` |
| 3  | Every line fits at widths 40, 60 and 150                                     | Lines.2 | `TestWidthBounds`              |
| 4  | An unmeasured line total prints `?` and a lower bound prints `~`             | Lines.3, I3 | `TestUnknownAndBoundMarks`, `TestTotalsMarkOnlyTheFactsTheRunCannotProve` |
| 5  | A directory row shows files, size and share; a file row its lines            | Lines.5, I2 | `TestRowColumns` |
| 6  | Six kids, `+N more` and a `…` cut at a narrow width                          | Lines.6, I13 | `TestPeekFitting` |
| 7  | A stopped run prints one footer naming the phase and the limit               | Lines.7, I4 | `TestFooterOnCut`, `TestFooterNamesTheStoppedPhase` |
| 8  | A skipped or unreadable file is counted in the footer, not marked in the row | Lines.7, I23 | `TestFooterCounts` |
| 9  | A run that reused stored facts prints the cache token; a cold run prints no footer | Lines.7, I4 | `TestFooterCacheToken` |
| 10 | SI rounding matches the 999_500 knee; `--bytes` prints integers              | Lines.8 | `TestSIUnits` |
| 11 | The same view renders the same lines twice                                    | Lines.9, I1 | `TestRenderIsDeterministic` |
| 12 | The strip spans the clamped width, and a provisional view marks its line total and prints no footer | Lines.1, Lines.10, I3, I12 | `TestStripSpansTheWidth`, `TestProvisionalMarksOnlyTheLineTotal` |
## Test cases

| Test                        | Level  | Proves       | Given                                        | Then                                                | Must not                       |
| --------------------------- | ------ | ------------ | -------------------------------------------- | --------------------------------------------------- | ------------------------------ |
| `TestPlainHasNoEscapes`     | unit   | AC1, Lines.1, I5  | a full view, plain, width 150                 | no `\x1b` in any line                                | a stray SGR from a kid colour  |
| `TestStyledEqualsPlain`     | unit   | AC2, Lines.1, I6  | the same view, styled at 150                  | stripped styled output equals the plain additions    | a fact that only styled output has |
| `TestWidthBounds`           | unit   | AC3, Lines.2      | one view at widths 1, 40, 60, 150, 400        | every line fits within the clamp                     | a rune that overflows a column |
| `TestUnknownAndBoundMarks`  | unit   | AC4, Lines.3, I3  | `Lines: -1`; `Lines: 5, LinesExact: false`    | `?` and `~5`                                         | `0` for an unknown value       |
| `TestRowColumns`            | unit   | AC5, Lines.4, Lines.5, I2 | a directory row and a file row plus header | header and files/size/share/line columns align | a column shifted by one row kind |
| `TestPeekFitting`           | unit   | AC6, Lines.6, I13 | 6 kids plus `More: 3` at width 40 and 150     | all six at 150, a `…` cut at 40, `+3 more`           | a wrapped or truncated name without `…` |
| `TestFooterOnCut`           | unit   | AC7, Lines.7, I4 | `Stopped: StopDeadline`, `Limit: 2s`          | one footer naming `walk` and `2s`                    | elapsed time or a fact's age   |
| `TestFooterNamesTheStoppedPhase` | unit | AC7, Lines.7, I4 | a cut walk and a cut count, `Limit: 2s`   | each footer names its own phase and the limit        | a count cut called a walk cut  |
| `TestTotalsMarkOnlyTheFactsTheRunCannotProve` | unit | AC4, Lines.3, I3 | a cut walk and a cut count, exact files and bytes | files marked only when the walk stopped, lines always | a `~` on a fact the run proved |
| `TestFooterCounts`          | unit   | AC8, Lines.7, I23 | skipped, unknown and unreadable counts on a complete cold view | all three counts in the footer | a marker in a row's cell |
| `TestFooterCacheToken`      | unit   | AC9, Lines.7, I4 | `Totals{Reused: 2}` and `Totals{Reused: 0}` | the first footer names the cache; the second has none | elapsed time or a fact's age |
| `TestSIUnits`               | unit   | AC10, Lines.8 | sizes 0, 999, 1_000, 999_499, 999_500, 1e12   | `0 B, 999 B, 1.0 kB, 999.5 kB, 1.0 MB, 1.0 TB`; `--bytes` as integers | a unit switch below the knee |
| `TestRenderIsDeterministic` | unit   | AC11, Lines.9, I1, I15 | the same view rendered 100 times              | identical `[]string`                                  | map order leaking into a column |
| `TestStripSpansTheWidth`    | unit   | AC12, Lines.1, I6 | a full view at widths 40, 60, 100, 150        | the strip is exactly as wide as the clamped view       | a strip that stops at a fixed column |
| `TestProvisionalMarksOnlyTheLineTotal` | unit | AC12, Lines.10, I3, I12 | the same view with and without `Provisional` | the provisional line total is a lower bound and no footer; the answer is exact and carries its footer | a provisional view that states a final fact |
## Error management

None. `Lines` is total: a zero `View` renders the empty-state totals line, and a
width below 40 is clamped rather than refused (Lines.2).

## Golden diff

None.

## Removes

None.

## Local facts

**Invariants.** Column widths derive from the header and widest cell. The plain
form uses the same rendering path with a zero `tui.Style`, never a second
renderer. The footer keeps every applicable count and limit.

**Parameters.**

None. Renderer parameters are defined in the README Parameters table.

## Human required

None.

## Implementation notes

| Run   | Commit   | Command                    | Result                   |
| ----- | -------- | -------------------------- | ------------------------ |
| start | none     | `make qa` && `dupl -t 80`  | pass, coverage 92.6 %    |
| end   | none+wip | `make qa` && `dupl -t 80`  | pass, coverage 92.1 %    |
| end   | none+wip | `make qa` && `dupl -t 80`  | pass, coverage 92.1 %    |
| replan | none    | A01 ruling (b): the footer names reused stored facts | `Totals.Reused` replaces `Totals.Trusted`; AC9 added |
| replan | none    | A04: the footer names the count's stop | `TestFooterNamesTheStoppedPhase` and `TestTotalsMarkOnlyTheFactsTheRunCannotProve` added; AC4 and AC7 widened |
| replan | none    | R1-05: the strip spans the width; the frame renders provisionally | `stripMax` removed; `Options.Provisional` added with Lines.10 and AC12 |

Andon: none.
Surprises:
- The row's box is presentation, so the styled layout reserves two extra columns; a styled line still never exceeds the width.

## Review findings

### Review 1, 2026-10-24

Nothing to fix in this phase.

Verified good: a count-only stop marks the line total and never the file or
byte totals, because `totalsLine` gates the file mark on the walk alone while
`plan.totals` clears `LinesExact` for either stop.

- [x] **R1-05 (minor, closed):** the strip stopped at 60 columns. It now spans
      the clamped width, and `TestStripSpansTheWidth` proves 40, 60, 100 and
      150.
