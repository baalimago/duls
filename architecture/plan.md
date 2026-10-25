# Plan

How a walk's fact set and a count's proof become the rows of the view: the
byte-first ordering, the promotion of large subdirectories, the folding of
small top-level entries, the peek column, and the bounds.

Read this when: changing what the view counts, how rows are chosen or
ordered, or how shares and the peek column are computed.

## Key files

| File | Purpose |
|------|---------|
| `internal/plan/plan.go` | `Planner`, `Plan`, `View`, `Entry`, `Item`, `Totals`, `TypeShare`, `Measured`, `Kind`, `Entries`, `Children`, `Palette`, `tree`, `row`, `group`, `totals`. |
| `internal/scan/walk.go` | `Snapshot`, `Dir`, `Child`, `Stop`: the facts the planner reads. |
| `internal/measure/measure.go` | `Result`, which the caller maps to `Measured`. |
| `internal/app/run.go` | The one caller: it maps `Snapshot` and `Result` to `Plan`. |

## Flow

```text
Plan(snapshot, measured)
  totals            files, bytes, lines, declared completeness, footer counts
  tree              one node per directory the walk reported
  groups            the root's directories and its largest files
  sort              bytes descending, then name
  fold              keep the first 7 when there are more than 8, fold the rest
  promote           a directory with 2 or more files becomes a row while a slot
                    remains, largest first, after its parent
  entry()           a row per group: name, path, files, bytes, exactness,
                    share, the six largest children, the count of the rest,
                    the colour
  |
  View{Totals, Entries}
```

## Ordering

Rows are ordered by bytes, never by lines. A directory's size is the byte
total its subtree holds; a file's size is its own `st_size`. Ties break by
name, so the order is total and the view is deterministic.

`Share` is the rounded percentage of the view's bytes:
`(200*part + whole) / (2*whole)`. It renders `<1%` when it rounds to zero.

## Promotion and folding

A view has 8 row slots. The root's largest entries fill them first, largest
first. When the root has more than 8 entries, the first 7 become rows and the
rest fold into one `Folded` row, which names them in their sorted order, sums
their files and bytes, and renders `+N smaller`.

A directory with 2 or more files and no row of its own is promoted into a
free slot, largest first, placed after its parent's subtree. A promoted row
is indented two spaces per path element. Promotion repeats until no slot
remains or no such directory is left.

The bounds are constants: at most 8 rows, at most 6 children per row, and at
most 6 file types. No flag changes them.

## The peek column

A directory row lists up to 6 children that have no row of their own: a
directory with its file count, a file with its line count. The rest become
`More` and render as `+N more`.

A child's line count comes from `Measured.Lines`, which the count fills only
for the files the peek column can show. A child the run did not measure
carries `-1` and renders `?`.

A subtree that the walk did not finish is not exact: its row lists the children
the walk has proved so far, and a directory child whose file count is a lower
bound carries `Exact` false. The row's bytes are a lower bound too. A folded row
is a lower bound when one of the entries it stands for is.

## Totals

`Totals.Files`, `Bytes` and `Lines` come from the snapshot and the count.
`BytesExact` is false when the walk stopped, a refused directory left a
subtree open, another filesystem was left unentered, or the walk could not read
a file's size: the byte total then omits a file the run counted. `LinesExact` is
false when any file is unknown or unreadable, when either phase stopped, or when
a directory was refused or a filesystem was left unentered.

`Totals.Stopped` and `Totals.StoppedPhase` name the phase that stopped: the
walk when the snapshot stopped, the count when only the count did. A refusal
stops nothing, and neither does a mounted filesystem, so neither is a stop:
they are counted in `UnreadableDirs` and in `OtherFilesystems`.

`Totals.Filesystem` is the device that holds the root, stated under the same
condition as the lower bounds: the device allocates what the run could not
reach, so a reader that sees the device's used bytes beside a lower-bound total
can see how much is missing. An exact run states no device. A run whose platform
cannot report the device states none either.

`Totals.Skipped`, `Unknown`, `Unreadable`, `UnreadableDirs`,
`OtherFilesystems` and `Reused` are the footer's counts. The planner prints
nothing; the renderer decides the text.

`Totals.Types` holds at most 6 file types, largest by file count first, ties by
extension. `Share` is the rounded percentage of `Totals.Files`, the same
rounding the rows use. `TypesMore` counts the types that did not fit. An empty
census gives no types, so a view with no files has no type line.

## Gotchas

- A file row reports `Files == 0`; its bytes are its own, so it is always exact.
  A directory row reports its subtree's files, bytes and share, and is a lower
  bound while its subtree is open.
- A share never exceeds 100 %: the walk folds the files it counted in an
  unfinished subtree into its ancestors, so no row outweighs the totals.
- `Entry.Lines` is `-1` unless the count proved a value for that exact path.
- `Item.Exact` is false only for a directory child of an unfinished subtree: a
  file item states its own bytes, so it is always exact.
- Promotion competes for the same 8 slots as the top level: a promoted row can
  push a smaller top-level entry into the folded row.
- `Color` derives from the top-level ancestor's path, not from the row order,
  so a row keeps its colour while the order changes between frames.
- `Entry.Depth` counts path elements; the root's rows are depth 0.
- `Entry.Path` is the row's slash-separated path below the root, so a promoted
  row is `meetings/autumn`. A Folded row has none: it stands for several
  top-level entries, which it keeps in `Folded`.
- The planner renders no text. `+N smaller`, `+N more` and the marks are the
  renderer's, and the agent document composes its own folded name.

## Related

- [scan.md](./scan.md): the facts the planner reads.
- [measure.md](./measure.md): the line counts and the accounting.
- [view.md](./view.md): how rows become lines.
