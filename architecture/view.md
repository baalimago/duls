# View

How a plan becomes the lines duls prints on a terminal: the strip, the columns,
the marks, the footer, and the width bounds.

Read this when: changing any line of the visual answer, adding a column, or
touching the width adaptation.

## Key files

| File | Purpose |
|------|---------|
| `internal/view/view.go` | `Renderer`, `Lines`, `Options` (`Style`, `Width`, `Bytes`, `Limit`, `Provisional`), `MinWidth`, `MaxWidth`, `rowIndent`, `displayRows`, `layoutOf`, `rowLine`, `totalsLine`, `typesLine`, `footerLines`, `footerParts`, `peek`, `strip`, `stripWidths`, `byteSize`, `siCount`, `mark`, `cut`, `pad`. |
| `internal/plan/plan.go` | The `View`, `Entry`, `Item` and `Totals` values the renderer draws. |
| `internal/tui/style.go` | `Style`, `Dim`, `Bold`, `Segment`, and the depth the renderer paints at. |

## The answer

```text
  ██████████████████████████████████████████████████░

  6 files · 718 B · 10 lines
  types  100% .md
  entry       files   size  share largest inside, without a row of its own
  ■ README.md       -  700 B      - 1 line
  ■ meetings        3   12 B     2% a.md 3 · b.md 2 · c.md 1
  ■ people          2    6 B     1% ana.md 2 · ben.md 1
```

The first two lines are the strip: `█` segments that split the clamped width
over the top-level and folded rows by their byte totals, one palette colour per
segment, and the folded segment dim. A blank line follows it. The strip and its
blank line appear only when the view has a row whose bytes the strip can show,
so a view without such a row starts at the totals line.

Then come the dimmed totals line, the type line when the walk counted a file,
the dimmed header, and one row per entry behind a `■` box in the entry's
colour; the folded row's box is dim. Every line except the strip starts with
two spaces and ends without trailing spaces. The header is a fixed sentence, cut
to the width.

The type line names the share of each type as a percentage of the file count,
largest first, and counts the types that did not fit as `+N more`. A name with
no extension prints `(none)`. The line is cut with `…` like the peek column, so
a narrow terminal shows fewer types.

A row's name is its path, indented two spaces per depth. A folded row's name is
`+N smaller`. A file row prints `-` in the files and share columns and its line
count in the peek column.

A value the run cannot prove carries a mark: `~` before a lower bound and `?`
where the value is unknown. A file the run did not measure shows `?`; it never
shows a zero. A refused directory proves nothing about its subtree, so the file
count, the byte total and every row above it are lower bounds.

Sizes use SI units with one decimal and the `999_500` knee of the next unit.
With `--bytes` the renderer prints exact integers and no unit scaling.

File and line counts use SI units too: a count below 1000 prints as an integer
and above it with one decimal and a `k`, `M`, `G` prefix at the same knee. The
rule covers the totals line, the files column, the peek column and the footer
counts. `--bytes` does not touch a count.

## Columns and width

The width is clamped to `[MinWidth, MaxWidth]`, that is 40 to 150 columns. A
stream that reports no width gets 150.

The fixed columns are 6 runes each for files, size and share, plus four spaces
— two of the row margin `rowIndent` and the box with its space. The name column
takes the longest name plus one rune, bounded so the peek column keeps at least
8 runes. The peek column takes the rest.

`cut` shortens a string to its width and ends it with `…`. Measure before you
paint: every width is counted on unpainted text, so escape bytes never count
as columns.

## The footer

The footer is dimmed text after the rows. It appears when the view is partial,
when records were reused, or when any skipped, unknown, unreadable,
refused-directory or other-filesystem count is nonzero. It states:

- `partial: <phase> deadline <limit>` when a phase stopped, naming the walk or
  the count that stopped and the limit it obeyed.
- `not scanned N` when the count never dispatched a file, that is
  `Files − Measured − Skipped − Unknown`. It is zero after a completed count,
  and a cut run prints it whenever the count stopped on the walk's own
  deadline: those are files the walk proved and the count never opened. It is
  never a count of what a cut walk left unseen, because no phase can name the
  files an open frontier holds; the `partial:` part states that limit.
- `skipped N`, `unknown N`, `unreadable N` for the accounting.
- `unreadable dirs N` for the directories the walk could not list.
- `other filesystems N` for the directories the walk left because another
  filesystem is mounted there.
- `filesystem X used of Y` for the device that holds the root, when the plan
  carries it. It is the quantity `df` prints, so it explains the difference
  between the rows and what the device allocates: refused directories, mounts
  the walk left, files the root may not read, and the space no name holds any
  more. The rows are a part of it, never another quantity beside it.
- `cache reused` when stored facts were used.

It never prints elapsed time or a record's age. A footer that does not fit the
width continues on the next line, so no count is hidden at 40 columns.

With `Options.Provisional` set — the frame's case — the line total is marked a
lower bound and no footer is printed, because a run that is still counting
proves nothing final. The answer clears it.

## Gotchas

- The strip is the only line that fills the width exactly; every other line is
  at most the width.
- An empty view renders the totals line and the header, and no rows. No
  entry carries bytes, so no strip is drawn and the frame's first paint
  starts at the totals line.
- A wide character counts as one rune and may occupy two terminal columns.
  The renderer bounds runes, so a tree of wide names may still wrap.
- A row's colour comes from the entry, not from the row index, so a frame and
  the final view agree on every colour.
- The renderer always paints. Its only consumer is the visual form: a stream
  that is not a terminal gets the agent document instead, so no line of this
  file ever reaches a pipe.

## Related

- [plan.md](./plan.md): where entries, shares and marks come from.
- [agent.md](./agent.md): the same facts as the document a pipe gets.
- [tui.md](./tui.md): the form gate, the palette and the width source.
- [frame.md](./frame.md): the same renderer, drawn while the run continues.
