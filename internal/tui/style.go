// Package tui is the presentation gate of the size view: the two forms, the
// palette, and the stream width. A terminal gets the visual form; a pipe, a
// file, /dev/null, and a non-empty NO_COLOR get the agent document.
package tui

import (
	"io"
	"os"

	"golang.org/x/term"
)

// Form is the answer's encoding.
type Form int

const (
	// Visual is the view a person reads at a colour terminal.
	Visual Form = iota
	// Agent is the JSON document a program reads.
	Agent
)

// Depth is how many colours a visual stream shows.
type Depth int

const (
	// Basic is the 16-colour ANSI palette every terminal understands.
	Basic Depth = iota
	// TrueColor is the 24-bit palette of slivingdoc.dev, announced by
	// COLORTERM=truecolor or COLORTERM=24bit.
	TrueColor
)

// Style renders the tokens of one visual stream at its colour depth.
type Style struct {
	depth Depth
}

// New returns a style of the given depth.
func New(depth Depth) Style { return Style{depth: depth} }

// DetectForm chooses the form of out: agent when NO_COLOR is non-empty or out
// is not a real terminal, visual otherwise (architecture/cli.md).
func DetectForm(out io.Writer, getenv func(string) string) Form {
	if getenv("NO_COLOR") != "" || !IsTerminal(out) {
		return Agent
	}
	return Visual
}

// DepthOf reads the colour depth the environment announces: TrueColor for
// COLORTERM=truecolor or COLORTERM=24bit, Basic otherwise.
func DepthOf(getenv func(string) string) Style {
	switch getenv("COLORTERM") {
	case "truecolor", "24bit":
		return New(TrueColor)
	}
	return New(Basic)
}

// IsTerminal reports whether out is a real terminal: it has a file
// descriptor that the terminal ioctl accepts. A pipe, a file, a buffer and
// /dev/null are not.
func IsTerminal(out io.Writer) bool {
	f, ok := out.(interface{ Fd() uintptr })
	if !ok || f == (*os.File)(nil) {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// Width is out's width in columns, or 0 when it has none: a stream
// without a file descriptor, or a device that reports no size.
func Width(out io.Writer) int {
	f, ok := out.(interface{ Fd() uintptr })
	if !ok {
		return 0
	}
	w, _, err := term.GetSize(int(f.Fd()))
	if err != nil {
		return 0
	}
	return w
}

// hue is one palette colour in both depths.
type hue struct {
	basic string
	true  string
}

const (
	codeDim   = "\x1b[2m"
	codeBold  = "\x1b[1m"
	codeReset = "\x1b[0m"
)

// segmentHues tell a strip's segments apart; the failure red is left out.
var segmentHues = []hue{
	{basic: "\x1b[34m", true: "\x1b[38;2;90;162;255m"},
	{basic: "\x1b[32m", true: "\x1b[38;2;25;184;118m"},
	{basic: "\x1b[33m", true: "\x1b[38;2;240;160;60m"},
	{basic: "\x1b[35m", true: "\x1b[38;2;190;120;255m"},
	{basic: "\x1b[95m", true: "\x1b[38;2;240;110;170m"},
	{basic: "\x1b[36m", true: "\x1b[38;2;120;200;220m"},
	{basic: "\x1b[93m", true: "\x1b[38;2;200;200;90m"},
	{basic: "\x1b[94m", true: "\x1b[38;2;150;170;255m"},
}

// Segment paints t in the i-th colour of a strip, wrapping after the last.
func (s Style) Segment(i int, t string) string {
	return s.hue(t, segmentHues[i%len(segmentHues)])
}

// Dim paints secondary detail.
func (s Style) Dim(t string) string { return s.code(t, codeDim) }

// Bold paints what the reader must check.
func (s Style) Bold(t string) string { return s.code(t, codeBold) }

func (s Style) hue(t string, h hue) string {
	if s.depth == TrueColor {
		return s.code(t, h.true)
	}
	return s.code(t, h.basic)
}

// code paints t with one escape code. An empty token stays empty so a row
// never emits a bare reset.
func (s Style) code(t, code string) string {
	if t == "" {
		return t
	}
	return code + t + codeReset
}
