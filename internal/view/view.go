// Package view draws a planned view as terminal lines: a byte-first strip and
// columns, SI units, the unknown and lower-bound marks, and a footer that
// states only facts about this run.
package view

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/baalimago/duls/internal/plan"
	"github.com/baalimago/duls/internal/scan"
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
	//   - The answer is the visual form: a colour strip and the blank line after
	//     it when the view has a row whose bytes the strip can show, then the
	//     dimmed totals, type line and header, then one row per entry, each
	//     behind a box in its entry's colour. (Lines.1, I5, I6)
	//   - The width is clamped to [40, 150] and no line exceeds it in runes.
	//     (Lines.2)
	//   - Line 1 is the totals line: files, size and lines, with `~` before a
	//     lower bound and `?` where a value is unknown; no unknown renders as
	//     zero. A file or line count of 1000 or more prints one decimal and an
	//     SI prefix. (Lines.3, I3)
	//   - Line 2 is the type line when the walk counted a file: `types` then
	//     each type as its share and its extension, largest first, with
	//     `+N more` for the types that did not fit, cut to the width. (Lines.11)
	//   - The header follows the type line: entry, files, size, share, and the
	//     peek column. (Lines.4)
	//   - A directory row shows files, size and share; a file row shows its line
	//     count in the detail column; a folded row shows `+N smaller`. (Lines.5)
	//   - The peek column joins at most 6 items with " · ", appends `+N more`,
	//     and is cut with `…` when the width is short. (Lines.6)
	//   - A footer follows the rows when the view is partial, records were reused,
	//     or any skipped, unknown, unreadable, refused-directory or
	//     other-filesystem count is nonzero. It names the stopped phase and limit
	//     when partial, the files the count never dispatched, the counts, and the
	//     cache token when records were reused; it states the root's device usage
	//     when the plan carries it, because that is what df reports and the rows
	//     are a part of it. It never prints elapsed time or record age. It
	//     continues on the next line rather than hiding a part, and every line
	//     fits the width. (Lines.7, I1, I4, I23)
	//   - Sizes use SI units with one decimal and the 999_500 knee; with Bytes
	//     set they are exact integers of bytes. (Lines.8)
	//   - Equal views and options give equal lines; a frame uses these same lines
	//     and adds no ordering or display rule. (Lines.9, I15)
	//   - Size, order, and share use bytes; the output depends only on facts and
	//     declared completeness. (I1, I2)
	//   - With Provisional set the line total is marked a lower bound and no
	//     footer is printed, so a frame states no fact the run has not proved.
	//     (Lines.10, I3, I12)
	Lines(v plan.View, opts Options) []string
}

// New returns the renderer.
func New() Renderer {
	return renderer{}
}

const (
	MinWidth  = 40
	MaxWidth  = 150
	filesCol  = 6
	sizeCol   = 6
	shareCol  = 6
	peekFloor = 8
	// rowIndent is the left margin of a row: two spaces and the box.
	rowIndent = 4
)

type renderer struct{}

func (renderer) Lines(v plan.View, opts Options) []string {
	width := clampWidth(opts.Width)
	rows := displayRows(v, opts.Bytes)
	cols := layoutOf(rows, width, rowIndent)
	var lines []string
	if bar := strip(v, opts.Style, width); bar != "" {
		lines = append(lines, bar, "")
	}
	lines = append(lines, "  "+opts.Style.Dim(cut(totalsLine(v.Totals, opts.Bytes, opts.Provisional), width-2)))
	if types := typesLine(v.Totals, width-2); types != "" {
		lines = append(lines, "  "+opts.Style.Dim(types))
	}
	header := pad("entry", cols.name) + " " + right("files", cols.files) + " " + right("size", cols.size) + " " + right("share", cols.share) + " " + cut("largest inside, without a row of its own", cols.peek)
	lines = append(lines, "  "+opts.Style.Dim(strings.TrimRight(header, " ")))
	for _, row := range rows {
		lines = append(lines, rowLine(row, cols, opts.Style))
	}
	if !opts.Provisional {
		for _, footer := range footerLines(v.Totals, opts.Limit, width, opts.Bytes) {
			lines = append(lines, "  "+opts.Style.Dim(footer))
		}
	}
	return lines
}

type display struct {
	name  string
	kind  plan.Kind
	files string
	size  string
	share string
	peek  string
	color int
}

func displayRows(v plan.View, bytes bool) []display {
	rows := make([]display, 0, len(v.Entries))
	for _, entry := range v.Entries {
		row := display{color: entry.Color}
		switch entry.Kind {
		case plan.Folded:
			row.name = fmt.Sprintf("+%s smaller", siCount(int64(len(entry.Folded))))
			row.files = mark(siCount(int64(entry.Files)), entry.Exact)
			row.size = mark(byteSize(entry.Bytes, bytes), entry.Exact)
			row.share = mark(percent(entry.Share, entry.Bytes), entry.Exact)
			row.kind = plan.Folded
		case plan.File:
			row.name = strings.Repeat("  ", entry.Depth) + entry.Name
			row.files = "-"
			row.size = byteSize(entry.Bytes, bytes)
			row.share = "-"
			row.peek = linesText(entry.Lines)
			row.kind = plan.File
		default:
			row.name = strings.Repeat("  ", entry.Depth) + entry.Name
			row.files = mark(siCount(int64(entry.Files)), entry.Exact)
			row.size = mark(byteSize(entry.Bytes, bytes), entry.Exact)
			row.share = mark(percent(entry.Share, entry.Bytes), entry.Exact)
			row.peek = peek(entry)
			row.kind = plan.Directory
		}
		rows = append(rows, row)
	}
	return rows
}

type columns struct {
	name  int
	files int
	size  int
	share int
	peek  int
}

func layoutOf(rows []display, width, indent int) columns {
	cols := columns{name: utf8.RuneCountInString("entry"), files: filesCol, size: sizeCol, share: shareCol}
	for _, row := range rows {
		cols.name = max(cols.name, utf8.RuneCountInString(row.name))
		cols.files = max(cols.files, utf8.RuneCountInString(row.files))
		cols.size = max(cols.size, utf8.RuneCountInString(row.size))
		cols.share = max(cols.share, utf8.RuneCountInString(row.share))
	}
	fixed := cols.files + cols.size + cols.share + 4
	cols.name = min(cols.name+1, max(4, width-indent-fixed-peekFloor))
	cols.peek = max(1, width-indent-fixed-cols.name)
	return cols
}

func rowLine(row display, cols columns, style tui.Style) string {
	line := pad(cut(row.name, cols.name), cols.name) + " " +
		right(row.files, cols.files) + " " +
		right(row.size, cols.size) + " " +
		right(row.share, cols.share) + " " +
		cut(row.peek, cols.peek)
	line = strings.TrimRight(line, " ")
	mark := "■"
	if row.kind == plan.Folded {
		mark = style.Dim(mark)
	} else {
		mark = style.Segment(row.color, mark)
	}
	return "  " + mark + " " + style.Segment(row.color, line)
}

func totalsLine(t plan.Totals, bytes, provisional bool) string {
	complete := (t.Stopped == scan.StopNone || t.StoppedPhase != scan.PhaseWalk) && t.UnreadableDirs == 0 && t.OtherFilesystems == 0
	files := mark(siCount(int64(t.Files))+" "+unit(t.Files, "file", "files"), complete)
	size := mark(byteSize(t.Bytes, bytes), t.BytesExact)
	var lines string
	if t.Lines < 0 {
		lines = "?"
	} else {
		lines = mark(siCount(t.Lines)+" "+unit(int(t.Lines), "line", "lines"), t.LinesExact && !provisional)
	}
	return files + " · " + size + " · " + lines
}

func typesLine(t plan.Totals, width int) string {
	if len(t.Types) == 0 {
		return ""
	}
	items := make([]string, 0, len(t.Types)+1)
	for _, share := range t.Types {
		items = append(items, fmt.Sprintf("%d%% %s", share.Share, typeName(share.Ext)))
	}
	if t.TypesMore > 0 {
		items = append(items, fmt.Sprintf("+%s more", siCount(int64(t.TypesMore))))
	}
	return cut("types  "+strings.Join(items, " · "), width)
}

func typeName(ext string) string {
	if ext == "" {
		return "(none)"
	}
	return ext
}

func footerLines(t plan.Totals, limit time.Duration, width int, bytes bool) []string {
	parts := footerParts(t, limit, bytes)
	room := width - 2
	var lines []string
	current := ""
	for _, part := range parts {
		switch {
		case current == "":
			current = part
		case utf8.RuneCountInString(current)+3+utf8.RuneCountInString(part) <= room:
			current += " · " + part
		default:
			lines = append(lines, cut(current, room))
			current = part
		}
	}
	if current != "" {
		lines = append(lines, cut(current, room))
	}
	return lines
}

func footerParts(t plan.Totals, limit time.Duration, bytes bool) []string {
	var parts []string
	if t.Stopped != scan.StopNone {
		phase := "walk"
		if t.StoppedPhase == scan.PhaseCount {
			phase = "count"
		}
		partial := "partial: " + phase + " deadline"
		if limit > 0 {
			partial += " " + limit.String()
		}
		parts = append(parts, partial)
	}
	if notScanned := t.Files - t.Measured - t.Skipped - t.Unknown; notScanned > 0 {
		parts = append(parts, "not scanned "+siCount(int64(notScanned)))
	}
	if t.Skipped > 0 {
		parts = append(parts, "skipped "+siCount(int64(t.Skipped)))
	}
	if t.Unknown > 0 {
		parts = append(parts, "unknown "+siCount(int64(t.Unknown)))
	}
	if t.Unreadable > 0 {
		parts = append(parts, "unreadable "+siCount(int64(t.Unreadable)))
	}
	if t.UnreadableDirs > 0 {
		parts = append(parts, "unreadable dirs "+siCount(int64(t.UnreadableDirs)))
	}
	if t.OtherFilesystems > 0 {
		parts = append(parts, "other filesystems "+siCount(int64(t.OtherFilesystems)))
	}
	if t.Filesystem != nil {
		parts = append(parts, "filesystem "+byteSize(t.Filesystem.Used, bytes)+" used of "+byteSize(t.Filesystem.Total, bytes))
	}
	if t.Reused > 0 {
		parts = append(parts, "cache reused")
	}
	return parts
}

func peek(entry plan.Entry) string {
	items := make([]string, 0, len(entry.Kids)+1)
	for _, kid := range entry.Kids {
		switch {
		case kid.Dir:
			items = append(items, kid.Name+" "+siCount(int64(kid.Files)))
		case kid.Lines < 0:
			items = append(items, kid.Name+" ?")
		default:
			items = append(items, kid.Name+" "+siCount(kid.Lines))
		}
	}
	if entry.More > 0 {
		items = append(items, "+"+siCount(int64(entry.More))+" more")
	}
	return strings.Join(items, " · ")
}

func linesText(lines int64) string {
	if lines < 0 {
		return "?"
	}
	return siCount(lines) + " " + unit(int(lines), "line", "lines")
}

// siCount prints a count below 1000 exactly and above it with one decimal and
// an SI prefix, using the knee the byte formatter uses.
func siCount(n int64) string {
	if n < 1000 {
		return strconv.FormatInt(n, 10)
	}
	value, index := float64(n), 0
	for value >= 999.5 && index < len(prefixes)-1 {
		value /= 1000
		index++
	}
	if index == 0 {
		return strconv.FormatInt(n, 10)
	}
	return fmt.Sprintf("%.1f%s", value, prefixes[index])
}

type segment struct {
	bytes int64
	color int
	dim   bool
}

func strip(v plan.View, style tui.Style, width int) string {
	var segments []segment
	var total int64
	for _, entry := range v.Entries {
		if entry.Depth != 0 && entry.Kind != plan.Folded {
			continue
		}
		segments = append(segments, segment{bytes: entry.Bytes, color: entry.Color, dim: entry.Kind == plan.Folded})
		total += entry.Bytes
	}
	cols := width
	if len(segments) == 0 || total <= 0 || cols < 1 {
		return ""
	}
	widths := stripWidths(segments, total, cols)
	var b strings.Builder
	for i, seg := range segments {
		block := strings.Repeat("█", widths[i])
		if seg.dim {
			b.WriteString(style.Dim(block))
		} else {
			b.WriteString(style.Segment(seg.color, block))
		}
	}
	return b.String()
}

func stripWidths(segments []segment, total int64, cols int) []int {
	widths := make([]int, len(segments))
	if len(segments) == 0 || total <= 0 {
		return widths
	}
	used := 0
	for i, seg := range segments {
		widths[i] = max(1, int(seg.bytes*int64(cols)/total))
		used += widths[i]
	}
	for i := 0; used < cols; i = (i + 1) % len(widths) {
		widths[i]++
		used++
	}
	for used > cols {
		widest := 0
		for i, w := range widths {
			if w > widths[widest] {
				widest = i
			}
		}
		if widths[widest] <= 1 {
			break
		}
		widths[widest]--
		used--
	}
	return widths
}

var units = []string{"B", "kB", "MB", "GB", "TB", "PB", "EB"}

var prefixes = []string{"", "k", "M", "G", "T", "P", "E"}

func byteSize(n int64, exact bool) string {
	if exact {
		return fmt.Sprintf("%d B", n)
	}
	if n < 0 {
		n = 0
	}
	value, index := float64(n), 0
	for index < len(units)-1 && value >= 999.5 {
		value /= 1000
		index++
	}
	if index == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f %s", value, units[index])
}

func percent(share int, bytes int64) string {
	if share == 0 && bytes > 0 {
		return "<1%"
	}
	return fmt.Sprintf("%d%%", share)
}

func mark(text string, exact bool) string {
	if exact {
		return text
	}
	return "~" + text
}

func clampWidth(width int) int {
	if width < 1 {
		return MaxWidth
	}
	return min(max(width, MinWidth), MaxWidth)
}

func pad(s string, width int) string {
	return s + strings.Repeat(" ", max(0, width-utf8.RuneCountInString(s)))
}

func right(s string, width int) string {
	return strings.Repeat(" ", max(0, width-utf8.RuneCountInString(s))) + s
}

func cut(s string, width int) string {
	if width < 1 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	return string(runes[:width-1]) + "…"
}

func unit(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
