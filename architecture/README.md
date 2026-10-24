# duls architecture

duls prints the size view of a directory: the view slivingdoc prints when
an initial pull materializes a notebook into an empty directory. It walks one
directory without reading content, counts the lines of the files the view can
show, and answers a terminal with the visual view and a pipe with a JSON
document.

Start here, then open the doc for your concern. Each doc names the files and
symbols to read before you change them.

| Doc | Answers |
|-----|---------|
| [overview.md](./overview.md) | What duls is, how a command line becomes a view, the package map, and the invariants no change may break. |
| [cli.md](./cli.md) | The command surface, flags, path resolution, streams, environment, and exit codes. |
| [scan.md](./scan.md) | Phase 1: the parallel metadata walk, the fact set, the frontier, and progress. |
| [measure.md](./measure.md) | Phase 2: the dispatch order, the policy, the proof, the accounting, and the deadline. |
| [plan.md](./plan.md) | How facts become rows: byte ordering, promotion, folding, shares, and the bounds. |
| [view.md](./view.md) | How rows become lines: the strip, the columns, width adaptation, marks, and the footer. |
| [agent.md](./agent.md) | The JSON document a program reads: its shape, its keys, and the values a run leaves out. |
| [frame.md](./frame.md) | The live outline: trigger, repaint, geometry, and the erase before the answer. |
| [cache.md](./cache.md) | The measured-fact store: shards, keys, the two windows, the cap, and silent failures. |
| [tui.md](./tui.md) | How a stream decides between the visual and agent forms, the palette, and the terminal width. |
| [errors.md](./errors.md) | How a failure is wrapped, printed, and turned into an exit code, and what is not a failure. |
| [testing.md](./testing.md) | The unit and black-box layers, the QA gate, and the coverage rule. |

## Reading order by task

- **Add or change a flag, help text, or exit code:** [cli.md](./cli.md), then
  [errors.md](./errors.md).
- **Change what the walk counts or when it stops:** [scan.md](./scan.md).
- **Change what is measured or how a count is proven:** [measure.md](./measure.md).
- **Change what the view counts or how entries are chosen:** [plan.md](./plan.md),
  then [view.md](./view.md).
- **Change a line a person sees:** [view.md](./view.md), then [tui.md](./tui.md).
- **Change what a program reads:** [agent.md](./agent.md), then
  [plan.md](./plan.md).
- **Change the animation:** [frame.md](./frame.md), then [view.md](./view.md).
- **Change what is stored between runs:** [cache.md](./cache.md).
- **Add a package or move a boundary:** [overview.md](./overview.md).
- **Write or fix a test:** [testing.md](./testing.md).
- **Start from zero:** this file, [overview.md](./overview.md), then
  [cli.md](./cli.md).
