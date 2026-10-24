# Terminal presentation

How a stream decides between the visual view and the agent document, what the
palette is, how the width is read, and which seams make the rendering testable
without a terminal.

Read this when: adding a colour, a mark, or a style; changing the form gate; or
writing a test for the visual form.

## Key files

| File | Purpose |
|------|---------|
| `internal/tui/style.go` | `Form` (`Visual`, `Agent`), `DetectForm`, `Depth`, `Style`, `New`, `DepthOf`, `IsTerminal`, `Width`, `Dim`, `Bold`, `Segment`, `segmentHues`. |
| `internal/app/options.go` | `Options.Form`, `Options.Style` and `Options.Width`: the test seams, used when non-nil. |
| `internal/app/run.go` | The only consumer: it asks for the form and the depth of the stream the answer goes to. |

## Flow

```text
app.Run
  form = opts.formOf(out, req.JSON)
    -json set                       -> Agent
    Options.Form != nil             -> Options.Form(out)
    else tui.DetectForm(out, EnvLookup(Env))
      NO_COLOR non-empty            -> Agent
      out is not a terminal         -> Agent
      otherwise                     -> Visual
  style = opts.styleOf()
    Options.Style != nil ? Options.Style() : tui.DepthOf(EnvLookup(Env))
      COLORTERM=truecolor | 24bit   -> Style, TrueColor
      otherwise                     -> Style, Basic
  width = opts.widthOf(out)
    Options.Width != nil ? Options.Width(out) : tui.Width(out)
      no descriptor or no size      -> 0, which the renderer reads as 150
```

## Behavior

`DetectForm` chooses the answer's encoding. A colour terminal gets the visual
view, because that is what a person reads. A pipe, a file, `/dev/null`, and a
non-empty `NO_COLOR` get the agent document, because that is what a program
reads. `NO_COLOR` is honoured before the terminal gate, so piping the output of
a terminal process still gives the document when the variable is set.

**`-json` wins.** `Options.formOf` returns `Agent` for a `-json` run before it
asks the seam, the environment or the stream, so a program that asked for the
document always gets it. The seam is consulted next, and the stream last.

`IsTerminal` asks the terminal itself (`golang.org/x/term`) about the stream's
file descriptor. A pipe, a file, a buffer and `/dev/null` are not terminals; a
pseudo-terminal is. `Width` asks the same descriptor for its column count and
returns 0 when it has none.

`DepthOf` reads `COLORTERM` and chooses the depth of a visual stream. It is the
only input to `New`, which returns a `Style` of that depth; the style has no
other mode, and a visual run is always styled.

The palette is slivingdoc's: the segment hues in 24-bit when `COLORTERM`
announces it, and their 16-colour codes otherwise. Dim is `\x1b[2m`, bold is
`\x1b[1m`, and every styled token ends in `\x1b[0m`. An empty token stays
empty, so no method emits a bare reset.

The test seams are decisive: when `Options.Form`, `Options.Style` or
`Options.Width` is set, the stream is not consulted. `internal/app` tests use
them to prove the visual layout at an exact width; the black-box scenarios use
a real pseudo-terminal to prove the same gate end to end. The frame is enabled
only for the visual form, so the agent document never draws one.

## Gotchas

- `NO_COLOR` on a terminal changes the form, not the colour: the answer
  becomes the agent document, which has no layout at all.
- `DetectForm` is called once per run. The form does not change while the
  answer is written.
- A pseudo-terminal's default post-processing rewrites LF to CRLF. A test
  that compares bytes must disable `OPOST` on the slave before the child
  starts.
- The linked terminal library is the only third-party runtime dependency.
  The rest of the tool is the standard library.

## Related

- [view.md](./view.md): what a visual stream carries.
- [agent.md](./agent.md): what an agent stream carries.
- [testing.md](./testing.md): the pseudo-terminal scenarios.
