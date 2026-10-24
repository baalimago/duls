# Phase 8 — prune

**Parent:** root · **Class:** prune · [README](README.md)

## Goal

Remove the single-pass, line-first scanner and the renderer it fed, now that
phase 7 no longer calls either.

## Interface

The Removes table below. The phase adds no member and changes no behaviour: no
golden changes and no test that describes current output changes.

Consumes: None.

## Acceptance criteria

| #  | Criterion                                                                | Proves     | Proof                        |
| -- | ------------------------------------------------------------------------ | ---------- | ---------------------------- |
| 1  | Every removed member has no hit outside its allowed hits                 | Removes        | `TestRemovedNamesAbsent`     |
| 2  | The phase changes no golden output                                        | Removes        | `make test`                  |
| 3  | The binary's output is unchanged by this phase                            | Removes        | `make test`                  |
| 4  | Coverage stays above the configured floor after the old tests are removed | Removes        | `make test`                  |
## Test cases

| Test                                    | Level              | Proves | Given                     | Then                                  | Must not            |
| --------------------------------------- | ------------------ | ------ | ------------------------- | ------------------------------------- | ------------------- |
| `TestRemovedNamesAbsent`                 | repeatable command | AC1 | every member in the Removes table | command below reports no hits outside allowed hits | a live reference |
| `make test`                              | repeatable command | AC2, AC3, AC4 | pruned tree and current goldens | suite passes, outputs stay fixed, coverage meets the floor | a skipped test or golden edit |
| `go build ./...`                         | repeatable command | AC3 | the pruned source tree | build succeeds | a dead interface |

`TestRemovedNamesAbsent` runs this command from the repository root:

```sh
test ! -e internal/app/outline.go && test ! -e internal/app/outline_test.go && test ! -d internal/outline && ! rg -n 'scan\.Files|scan\.Options|scan\.countFile|scan\.countLines|scan\.ctxReader|scan\.lineCounter|outline\.(Build|FileLines|node|insert|settle|size|hiddenDir|entry|promote|share)|app\.writeOutline|app\.(outlineRows|outlineRow|outlineWidthFor|stripWidths|fit|cut|pad|percent|paint|base|quantity|unit)|internal/app/outline\.go|internal/outline' --glob '!worklogs/**' .
```
## Error management

None. This phase removes code; every failure it can produce is a compile or
test failure that the gates already catch.

## Golden diff

None.

## Removes

| Member | Consumers | Re-pointed to | Allowed hits |
| ------ | --------- | ------------- | ------------ |
| `scan.Files` | `app.Run` | `scan.Walker.Walk` | 0 |
| `scan.Options` | `scan.Files`, `app.Request` | `scan.WalkOptions` | 0 |
| `scan.countFile` | `scan.Files` | `measure.Counter.Count` | 0 |
| `scan.countLines` | `scan.countFile` | `measure` (LF rule) | 0 |
| `scan.ctxReader` | `scan.countFile` | `measure` chunk reader | 0 |
| `scan.lineCounter` | `scan.countLines` | `bytes.Count` in `measure` | 0 |
| `outline.Build` | `app.Run` | `plan.Planner.Plan` | 0 |
| `outline.FileLines` | `scan.Files`, `outline.Build` | `scan.Dir`, `scan.Child` | 0 |
| `outline.node`, `outline.insert`, `outline.settle`, `outline.size`, `outline.hiddenDir`, `outline.entry`, `outline.promote`, `outline.share` | `outline.Build` | `plan` | 0 |
| `app.writeOutline` | `app.Run` | `view.Renderer.Lines` | 0 |
| `app.outlineRows`, `app.outlineRow`, `app.outlineWidthFor`, `app.stripWidths`, `app.fit`, `app.cut`, `app.pad`, `app.percent`, `app.paint`, `app.base`, `app.quantity`, `app.unit` | `app.writeOutline` | `view` | 0 |
| `internal/app/outline.go`, `internal/app/outline_test.go` | — | `internal/view` tests | 0 |
| `internal/outline` package | `internal/app`, `internal/scan` | `internal/plan` | 0 |

## Local facts

None.

## Human required

None.

## Implementation notes

| Run   | Commit   | Command                    | Result                                                     |
| ----- | -------- | -------------------------- | ---------------------------------------------------------- |
| start | none     | `make qa` && `dupl -t 80`  | not recorded when the tree arrived pruned                   |
| end   | none+wip | `make qa` && `dupl -t 80`  | pass, coverage 89.4 %; `TestRemovedNamesAbsent` exits 0      |
| end   | none+wip | `make qa` && `dupl -t 80`  | pass, coverage 89.4 %; `TestRemovedNamesAbsent` exits 0      |

Andon: none.
Surprises:
- The removal command's bare `\b(promote|share)\s*\(` also hit `plan`'s own helpers, so the pattern names the `outline.` package instead.
- The records for this phase were written after the tree arrived pruned; the gates cited are the ones run at the close.

## Review findings

### Review 1, 2026-10-24

Nothing to fix in this phase.

Verified good: `internal/outline`, `internal/scan/scan.go`, its test file and
`internal/app/outline.go` are gone, the removal command exits 0, and no golden
changed — the scenario fixtures are the same files the plan phase's tests use,
not the pruned renderer's goldens.
