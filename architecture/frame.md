# Frame

How a visual run draws the outline while the scan continues: the trigger, the
interval, the line-granular repaint, the geometry, and the one erase before
the answer.

Read this when: changing the animation, the throttle, or anything that writes
to a terminal before the final view.

## Key files

| File | Purpose |
|------|---------|
| `internal/frame/frame.go` | `New`, `Options`, `Rasterizer`, `Publish`, `Erase`, `due`, `frameDuty`, `paint`, `eraseSeq`, `drawAll`. |
| `internal/app/run.go` | Builds the frame, passes it as the walk's sink and the count's sink, and erases it before the write. `countSink` adds the walk's files and directories to phase-2 progress, because the count's counters start at zero. |
| `internal/scan/walk.go` | `Sink`, `Progress`, and `Progress.Tree`: the fact set an observation carries. |
| `internal/view/view.go` | The renderer the frame's `Lines` callback uses. |
| `internal/plan/plan.go` | The planner the frame's `Lines` callback uses. |

## Flow

```text
frame.New(Options{Out, Enabled, Trigger, Interval, Lines})
  start = Now()
  |
  Publish(progress)
    disabled, failed, erased   -> return
    counters went backwards    -> return
    not due                    -> return
    start = Now()
    lines = Lines(progress)     same planner and renderer as the answer
    empty                      -> record the counters, draw nothing
    equal to the frame on screen -> record the counters, draw nothing
    write paint(last, lines)
    lastCost = Now()-start      the interval waits five times this
  |
  Erase()
    clear exactly the last frame's lines, then accept no more frames
```

## Behavior

A frame is written only for the visual form, that is, only on a colour
terminal. A `-json` run, a pipe, a file, `/dev/null`, and a non-empty
`NO_COLOR` all run without a frame and without a single escape byte.

The first frame appears as soon as the first observation arrives: the
composition sets the trigger to zero, so a visual run draws the totals line, the
type line and the rows the walk holds at that moment. Later frames need at
least `Interval` since the last frame; the composition sets it to 16 ms, so a
small tree updates at the refresh rate of its terminal.

A frame pays for itself. The interval also stretches to five times the cost of
the last frame, so the frames that follow take at most a fifth of the run's
time: a tree with many directories, whose snapshot and render cost more, gets
fewer frames instead of a slower walk. The cost is the time `Lines` takes plus
the block it writes, and it is the previous frame's cost that sets the next
interval.

An observation carries `Progress.Tree`, a function that returns the fact set the
walk holds at that instant. The frame calls it only when it draws, and plans
that fact set with the same planner and renderer as the answer, so the rows,
their order, the strip, the type line and the peek column move while the walk
runs. A tree taken mid-walk is declared an unfinished walk, so its totals and
its inexact rows carry `~` until the answer replaces them. A `PhaseCount`
observation carries no tree, so the frame then draws the walk's stored snapshot
with the count's progress.

A repaint moves the cursor once to the top of the block, rewrites only the
lines that changed, and leaves the cursor below the block. A line that did not
change is passed over with a cursor move. A block whose height changed — the
strip arrives, the rows appear — is erased and drawn whole.

`Erase` clears exactly the lines of the last frame and nothing else, so the
answer starts where the frame's block started. After it, `Publish` writes
nothing and a second `Erase` writes nothing. `app.Run` erases after the count
returns and before the first byte of the view, and defers one more erase so a
run that fails after it painted leaves no frame behind the error line.

The frame's lines are the answer's lines: the same planner, the same renderer,
the same bounds, the same marks. The frame adds no ordering rule, no column,
and no vocabulary. Unknown cells are marked exactly as the final view would
mark them, and the peek column shows only what the run can prove.

The frame renders a provisional view: the line total is a lower bound and no
footer is printed, because a run that is still counting proves nothing final.
The answer clears that flag and carries its own marks and footer.

An observation whose file, byte, line, or directory counter is lower than the
last drawn one is ignored, so the frame never moves backwards. Counters are
cumulative per phase, so the composition joins the count's counters to the
walk's totals before they reach the frame.

A write failure disables the rasterizer for the rest of the run. It never
returns an error, never panics, and never retries.

## Gotchas

- A tree is built under the walk's aggregate lock, so a frame that draws pauses
  the walk's fold for the length of one snapshot; the plan and the render run
  after the lock is released. Only the observation the frame draws pays for it;
  the walk carries a function, not a copy. The interval stretch is what keeps
  that pause a fifth of the run at worst.
- The first paint has no cursor movement: it draws the whole block. The erase
  is what leaves cursor-up sequences in the stream.
- A frame is skipped when its lines equal the frame on screen, so a slow walk
  with unchanged facts does not repaint.
- A cut keeps the facts it counted, so the answer is never smaller than the last
  frame: the totals the frame counted up to are the totals the answer states.
- `Options.Now` is the test clock. The composition leaves it nil.

## Related

- [view.md](./view.md): the lines the frame draws.
- [scan.md](./scan.md): the walk's progress.
- [measure.md](./measure.md): the count's progress.
