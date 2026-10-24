# Phase 2 — plan

**Parent:** root · **Class:** contract · [README](README.md)

## Goal

Turn a walk's fact set, plus whatever the count phase proved, into the at most
eight rows the view shows, with byte shares, promotion, folding and a declared
completeness.

## Interface

Written to `internal/plan/plan.go`.

```go
package plan

import "github.com/baalimago/duls/internal/scan"

// Kind classifies a view row.
type Kind uint8

const (
	Directory Kind = iota
	File
	Folded
)

// Item is one child of a directory that has no row of its own.
// Lines is -1 when the run has not measured it.
type Item struct {
	Name  string
	Dir   bool
	Bytes int64
	Files int
	Lines int64
}

// Entry is one row of the view.
//
// - Kind decides which fields are meaningful. (Entry.1)
// - Exact is false when Bytes is a lower bound the run could not complete.
//   (Entry.2)
type Entry struct {
	Name   string
	Kind   Kind
	Depth  int
	Files  int
	Bytes  int64
	Exact  bool
	Lines  int64
	Share  int
	Kids   []Item
	More   int
	Folded []string
	Color  int
}

// Totals are the view's headline numbers.
type Totals struct {
	Files      int
	Bytes      int64
	BytesExact bool
	Lines      int64
	LinesExact bool
	Measured   int
	Skipped    int
	Unknown    int
	Unreadable int
	Stopped    scan.Stop
	// StoppedPhase names the phase that stopped: the walk when the snapshot
	// stopped, the count when only the count did. It is meaningful only when
	// Stopped is not StopNone.
	StoppedPhase scan.Phase
	Reused       int
}

// View is what the renderer draws.
type View struct {
	Totals  Totals
	Entries []Entry
}

// Measured is what the count phase proved.
type Measured struct {
	Lines      map[string]int64
	Total      int64
	Measured   int
	Skipped    int
	Unknown    int
	Unreadable int
	Reused     int
	Stopped    scan.Stop
}

// Planner chooses the rows of one view.
type Planner interface {
	// Plan returns the view of one fact set.
	//
	// - Entries has at most 8 rows; at most one is Kind Folded and it is last.
	//   (Plan.1)
	// - Top-level groups are ordered by Bytes descending, ties by Name. A
	//   promoted row follows its parent's subtree; promoted siblings are ordered
	//   by Bytes descending, then Name. Share is the rounded percentage of
	//   Totals.Bytes, and is 0 when Totals.Bytes is 0. (Plan.2, I2)
	// - A Kind Directory row reports subtree Files and Bytes; a Kind File row
	//   reports Files 0, its Bytes and its Lines; a Kind Folded row names the
	//   top-level entries that got no row and sums their Files and Bytes.
	//   (Plan.3)
	// - Kids holds at most 6 Items, sorted by Bytes descending then Name, and
	//   More counts the children that did not fit. (Plan.4, I13, I21)
	// - Promotion: a directory with 2 or more files and no row of its own
	//   becomes a row while a slot remains, inserted after its parent's
	//   subtree, largest first. (Plan.5, I13)
	// - Depth is the number of path elements in the row's path; the root's
	//   rows are depth 0. (Plan.6)
	// - Color is a stable index derived from the row's top-level ancestor path,
	//   independent of current row order. (Plan.7, D14)
	// - Totals.Lines is m.Total; Totals.LinesExact is false when
	//   m.Unknown+Totals.Unreadable > 0 or any phase stopped. Totals.Stopped
	//   and Totals.StoppedPhase name the phase that stopped, the walk first.
	//   Totals.BytesExact is false when the walk stopped. Rows whose subtree
	//   contains a Frontier path are lower bounds. (Plan.8, I1, I3, I4)
	// - Same snapshot and same Measured give an equal View; the frame has no
	//   separate planning or ordering rule. (Plan.9, I15, I17)
	// - The view depends only on the supplied facts and declared completeness.
	//   (I1)
	// - Lines from an unknown file are unknown, never zero; policy-skipped files
	//   are outside the denominator. (I3, I23)
	Plan(s scan.Snapshot, m Measured) View
}
```

Consumes:

- `scan:Snapshot (phase 1)`
- `scan:Dir (phase 1)`
- `scan:Child (phase 1)`
- `scan:Stop (phase 1)`
- `scan:Phase (phase 1)`

The package is new, so it collides with nothing: `internal/outline` keeps
serving the current binary until phase 7 stops calling it and phase 8 removes
it.

## Acceptance criteria

| #  | Criterion                                                                       | Proves        | Proof                          |
| -- | ------------------------------------------------------------------------------- | ------------- | ------------------------------ |
| 1  | A root with 20 top-level entries yields 7 rows and one folded row                | Plan.1, I13 | `TestFoldKeepsEightRows`    |
| 2  | Groups follow byte order, promotions follow parents, and each share uses byte totals | Plan.2, I2 | `TestOrderAndShare`         |
| 3  | A file row carries its lines and no files; a directory row its subtree totals    | Entry.1, Plan.3 | `TestRowKinds` |
| 4  | A directory with 10 children lists 6 and reports `More == 4`                     | Plan.4, I13 | `TestKidsAndMore`            |
| 5  | A deep directory with 2 files is promoted into a free slot, in parent order      | Plan.5    | `TestPromotionFillsSlots`      |
| 6  | A cut walk marks every affected row and the totals inexact, never with a zero    | Entry.2, Plan.8, I3 | `TestCutWalkIsMarked` |
| 7  | Unmeasured lines render as unknown, and one unknown makes the total inexact      | Plan.8, I3 | `TestUnknownLines`            |
| 8  | The same inputs produce an equal `View`                                           | Plan.9, I17 | `TestPlanIsDeterministic`    |
| 9  | A row keeps its colour when another row overtakes it                              | Plan.7 | `TestColorFollowsTheEntry`   |
| 10 | A count that reused stored facts reports them; a cold count reports zero           | Plan.8, I4 | `TestReusedFactsAreCounted` |
| 11 | A stopped count marks the line total and its phase, never the walk's files or bytes | Plan.8, I3, I4 | `TestCutCountIsMarked` |
## Test cases

| Test                       | Level  | Proves      | Given                                                | Then                                        | Must not                            |
| -------------------------- | ------ | ----------- | ---------------------------------------------------- | ------------------------------------------- | ----------------------------------- |
| `TestFoldKeepsEightRows`    | unit   | AC1, Plan.1      | 20 top-level directories of 1 file each               | 8 entries, last is `Folded`, `Folded == 13` | a ninth row                         |
| `TestOrderAndShare`        | unit   | AC2, Plan.2, I2   | three entries of 50, 30 and 20 bytes                  | group order, promotion position, and each byte share | a share from file counts |
| `TestRowKinds`             | unit   | AC3, Entry.1, Plan.3 | a root file of 10 lines and a directory of 2 files | file row `Lines 10, Files 0`; dir row subtree totals | a file row with a sticky file count |
| `TestKidsAndMore`          | unit   | AC4, Plan.4, I13, I21 | a directory with 10 children                          | 6 items largest first, `More == 4`          | a seventh item                      |
| `TestPromotionFillsSlots`  | unit   | AC5, Plan.5, Plan.6 | 2 top-level entries, one with `deep/small/` of 2 files | promoted row follows its parent and has the correct depth | a promoted one-file directory |
| `TestCutWalkIsMarked`      | unit   | AC6, Entry.2, Plan.8, I1, I3 | a snapshot with `Stopped: StopDeadline` and a frontier | affected row and totals inexact | a zero byte or zero line total |
| `TestUnknownLines`         | unit   | AC7, Plan.8, I3  | `Measured{Unknown: 1, Total: 5}`                      | `Lines 5`, `LinesExact` false               | `LinesExact` true                   |
| `TestPlanIsDeterministic`  | unit   | AC8, Plan.9, I15, I17 | the same snapshot planned 100 times                   | all views `reflect.DeepEqual`               | map iteration order in the output   |
| `TestColorFollowsTheEntry` | unit   | AC9, Plan.7      | two frames where the second row grows larger          | path-derived color stays unchanged          | a colour tied to the row index      |
| `TestEmptyRoot`            | unit   | AC3, Plan.3      | an empty root                                         | a `View` with zero totals and no rows       | a nil map or a panicking share      |
| `TestReusedFactsAreCounted` | unit  | AC10, Plan.8, I4 | `Measured{Reused: 3}` and a cold `Measured`            | `Totals.Reused` is 3, then 0                | a reused count invented for a cold run |
| `TestCutCountIsMarked`     | unit   | AC11, Plan.8, I3, I4 | `Measured{Stopped: StopDeadline}` with an exact walk | lines inexact, `StoppedPhase` the count, bytes exact | a cut count calling the walk inexact |
## Error management

None. `Plan` is total over its inputs: a zero `Snapshot` yields an empty `View`,
and a negative or unmeasured `Lines` yields `-1` rather than an error (I3).

## Golden diff

None.

## Removes

None.

## Local facts

**Invariants.** Rows are chosen from `scan.Snapshot.Dirs` alone. The byte share
uses nearest-integer rounding; a share that rounds to zero renders `<1%` in the
renderer. Color is a stable function of the top-level ancestor path, not its
current row index (D14).

**Parameters.** None. All planner bounds are defined in the README Parameters table.

## Human required

None.

## Implementation notes

| Run   | Commit   | Command                    | Result                   |
| ----- | -------- | -------------------------- | ------------------------ |
| start | none     | `make qa` && `dupl -t 80`  | pass, coverage 92.3 %    |
| end   | none+wip | `make qa` && `dupl -t 80`  | pass, coverage 92.6 %    |
| end   | none+wip | `make qa` && `dupl -t 80`  | pass, coverage 92.6 %    |
| replan | none    | A01 ruling (b): subtree trust removed | `Totals.Trusted` replaced by `Totals.Reused`; `Measured.Reused` added |
| replan | none    | A04: the count's stop is declared | `Measured.Stopped` and `Totals.StoppedPhase` added; Plan.8 amended; AC11 added |

Andon: none.
Surprises:
- The walk keeps only six children per directory, so row-promotion candidates come from `Snapshot.Dirs`; a top-level file beyond the walk's six largest has no row and only stays in the totals.

## Review findings

### Review 1, 2026-10-24

R1-01 is filed on phase 7. Nothing to fix here.

Verified good: `plan.Totals.Reused` is the only field the view reads for the
cache token, and `TestReusedFactsAreCounted` now exists as the table named it.
