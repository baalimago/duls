# Agent

How a plan becomes the JSON document a program reads: the shape, the meaning of
each key, and the values a run that cannot prove a quantity leaves out.

Read this when: changing the document, adding a key, or changing which stream
or flag selects it.

## Key files

| File | Purpose |
|------|---------|
| `internal/agent/agent.go` | `Request`, `Document`, `Totals`, `Type`, `DirEntry`, `FileEntry`, `FoldedEntry`, `Child`, `Accounting`, `Encode`, `row`, `child`, `lines`, `foldedName`. |
| `internal/app/run.go` | `writeAgent` and the `form == tui.Agent` branch of `Run`. |
| `internal/app/options.go` | `Request.JSON`, `Options.Form`, `Options.formOf`. |
| `internal/tui/style.go` | `Form`, `Agent`, `DetectForm`. |
| `internal/plan/plan.go` | `View`, `Entry`, `Item`, `Totals`: the facts the encoder reads. |

## Flow

```text
app.Run
  form = opts.formOf(out, req.JSON)      -json, else DetectForm
  walk, count, plan.Plan                 the same facts the visual form draws
  form == tui.Agent ?
    writeAgent(out, store, root, req.Timeout, planned)
      agent.Encode(Request{Root, Limit, View})
      out.Write(document + "\n")
      store.Flush(flushBudget)
    return
  renderer.Lines, frame.Erase, write     the visual form
```

The encoder reads one `plan.View` and writes one JSON object. It adds no
ordering, no rounding and no unit: the plan already chose the rows, and the
document states them. No frame is drawn, so a program's stream never holds a
cursor move or an escape byte.

## The document

```json
{"root":"/notes/people",
 "totals":{"files":3,"bytes":152,"lines":11,"bytesExact":true,"linesExact":true,"typesMore":0},
 "types":[{"ext":".md","files":3,"share":100}],
 "entries":[{"name":"cleo.md","path":"cleo.md","kind":"file","depth":0,"bytes":67,"exact":true,"lines":4}],
 "accounting":{"stopped":"none","measured":3,"notScanned":0,"skipped":0,"unknown":0,"unreadable":0,"unreadableDirs":0,"otherFilesystems":0,"reused":0}}
```

`Encode` writes the object on one line, and `writeAgent` ends it with one LF, so
a reader gets one record per run. The sample above shows one of the three rows
of that view. The keys are:

- `root`: the resolved directory the run inspected.
- `totals`: the file count, the byte total, the line total, two exactness
  flags, and `typesMore`, the number of file types the census left out.
- `types`: at most six file types, largest by file count first. `ext` is the
  final extension with its dot, or `""` for a name without one.
- `entries`: the planned rows in the planner's order. `kind` is `dir`, `file`
  or `folded`.
- `accounting`: what the run proved and what it did not.

A directory row states its subtree's `files`, `bytes` and `share`, its
`children` (at most six) and `more`, the children that did not fit. A file row
states its own `bytes` and `lines`. A folded row states the summed `files`,
`bytes` and `share` of the top-level entries that got no row, their `names` in
byte order, and a `name` of the form `+N smaller`. Every row carries `exact`,
which is false when its `bytes` is a lower bound.

A child states `name` and `kind`. A directory child states `files` and a null
`lines`; a file child states `lines` and a null `files`.

**`filesystem` states the device.** The object is `total`, `used` and `avail`
bytes of the filesystem that holds the root, the quantity `df` prints. It is
present only when the run's totals are lower bounds, because then the device
holds what the run could not reach; an exact run states no device, and so does
a platform that cannot report one. The run's own `bytes` are a part of this
number, never a second quantity beside it.

## Behavior

**Numbers are exact integers.** A byte count, a line count, a file count and a
share are the planner's own value. `-bytes` changes the visual form only, so it
never changes this document.

**An absent value is not a zero.** `lines` is `null` when the count did not
prove a value for that file, and a directory child has no `lines` value at all,
because a line count does not belong to its kind. `exact` is false when `bytes`
is a lower bound. A reader that sees no value must report that it has none.

**`accounting` states the completeness.** `stopped` is `none` when both phases
finished, else the phase that stopped, `walk` or `count`. `limit` names the
deadline that phase obeyed and is absent when no phase stopped or the run had
no deadline. `measured`, `skipped`, `unknown`, `unreadable`, `unreadableDirs`,
`otherFilesystems` and `reused` are the same counts the visual footer prints.
`notScanned` is `files - measured - skipped - unknown`, which is the file count
the count phase never dispatched. A run whose `unreadableDirs` or
`otherFilesystems` is above zero states lower-bound `files`, `bytes` and
`lines` without naming a stopped phase.

**One document per run.** `types` and `entries` are always arrays, empty when
the run found nothing, never `null`. A command that wants a compact answer
pipes the document through `jq`.

**A failure is an error.** An entry of an unknown kind fails the encode with
`agent: encode: entry "<name>" has no kind: <n>`; a marshal failure fails with
`agent: encode: …`. `writeAgent` reports either on stderr as one `error:` line
and exits 1. It never falls back to the visual form, so a program never reads a
layout it did not ask for.

## Gotchas

- The folded row has no name in the plan: `plan.Entry.Name` is empty and
  `Folded` holds the names. The encoder composes `+N smaller` from the count.
- `share` is the planner's rounded percentage of `totals.bytes`, so the shares
  of the rows need not add up to 100.
- A run that reused stored line counts states `reused` and still reports
  `linesExact`: the count is proven, only older than the trust window.
- The document goes to stdout with the same write-failure rule as the visual
  form: a failed write is an error, exit 1.

## Related

- [plan.md](./plan.md): the rows, shares and marks the document states.
- [cli.md](./cli.md): `-json`, `NO_COLOR`, and the streams.
- [tui.md](./tui.md): how the form is detected.
- [view.md](./view.md): the visual form of the same facts.
