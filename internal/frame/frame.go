// Package frame draws the live outline into a terminal while a scan runs and
// erases it before the answer is written.
package frame

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/baalimago/duls/internal/scan"
)

// Options configure one rasterizer.
type Options struct {
	Out      io.Writer
	Enabled  bool
	Trigger  time.Duration
	Interval time.Duration
	Now      func() time.Time
	Lines    func(scan.Progress) []string
}

// Rasterizer draws the live outline into a terminal.
type Rasterizer interface {
	// Publish offers one progress observation to the frame.
	//
	//   - It is a no-op when Enabled is false, for every call; a disabled frame
	//     emits no escape sequence. (Publish.1, I5)
	//   - The first frame appears no earlier than Trigger after New, and never
	//     before the first observation. (Publish.2, I12)
	//   - At most one frame is written per Interval, never before the first
	//     observation, and never sooner than five times the cost of the last
	//     frame, so the frames that follow take at most a fifth of the run's
	//     time. (Publish.3, I12, D29)
	//   - A frame is written only when its lines differ from the frame on screen;
	//     a line that is identical is passed over with a cursor move, and a block
	//     whose height changed is erased and drawn whole. (Publish.4, I12)
	//   - The frame is exactly the lines Lines returns for that observation; the
	//     rasterizer adds and drops none, so it adds presentation only. Unknown
	//     and lower-bound marks and the row/child bounds are unchanged. (Publish.5, I3, I6, I13, I15)
	//   - An observation whose Files, Bytes, Lines or Dirs counter is lower than
	//     the last one drawn is ignored, so the frame never moves backwards. (Publish.6, D29)
	//   - A write failure disables the rasterizer for the rest of the run; it
	//     never returns an error, never panics and never retries. (Publish.7)
	//   - The frame's lines come from the same planner and renderer as the answer;
	//     it defines no ordering or output facts. (Publish.8, I15)
	scan.Sink

	// Erase removes the frame.
	//
	// - It clears exactly the lines of the last frame, and lines nothing else.
	//   (Erase.1)
	// - After Erase, Publish writes nothing and a second Erase writes nothing.
	//   (Erase.2)
	Erase()
}

// New returns a rasterizer for one run.
func New(opts Options) Rasterizer {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &raster{opts: opts, start: opts.Now()}
}

// frameDuty makes the interval at least this many times the cost of the last
// frame, so a frame spends at most a fifth of the run on drawing.
const frameDuty = 5

type raster struct {
	opts Options

	mu       sync.Mutex
	start    time.Time
	lastAt   time.Time
	lastCost time.Duration
	last     []string
	lastObs  scan.Progress
	haveObs  bool
	drawn    bool
	erased   bool
	failed   bool
}

func (r *raster) Publish(p scan.Progress) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.opts.Enabled || r.failed || r.erased || r.opts.Lines == nil {
		return
	}
	if r.haveObs && !grows(r.lastObs, p) {
		return
	}
	if !r.due() {
		return
	}
	start := r.opts.Now()
	defer func() { r.lastCost = r.opts.Now().Sub(start) }()
	lines := r.opts.Lines(p)
	if len(lines) == 0 {
		r.accept(p, nil)
		return
	}
	if r.drawn && equal(r.last, lines) {
		r.accept(p, lines)
		return
	}
	if err := r.write(paint(r.last, lines)); err != nil {
		r.failed = true
		return
	}
	r.accept(p, lines)
}

func (r *raster) Erase() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.erased = true
	if !r.opts.Enabled || r.failed || len(r.last) == 0 {
		return
	}
	if err := r.write(eraseSeq(len(r.last))); err != nil {
		r.failed = true
	}
	r.last = nil
}

func (r *raster) accept(p scan.Progress, lines []string) {
	r.lastObs = p
	r.lastAt = r.opts.Now()
	r.haveObs = true
	r.last = lines
	r.drawn = len(lines) > 0
}

func (r *raster) due() bool {
	now := r.opts.Now()
	if !r.drawn {
		return now.Sub(r.start) >= r.opts.Trigger
	}
	return now.Sub(r.lastAt) >= max(r.opts.Interval, frameDuty*r.lastCost)
}

func (r *raster) write(s string) error {
	if s == "" {
		return nil
	}
	if r.opts.Out == nil {
		return fmt.Errorf("frame: no stream")
	}
	_, err := io.WriteString(r.opts.Out, s)
	return err
}

func grows(prev, next scan.Progress) bool {
	return next.Files >= prev.Files && next.Bytes >= prev.Bytes &&
		next.Lines >= prev.Lines && next.Dirs >= prev.Dirs
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// paint returns the bytes that replace prev with next. The cursor starts and
// ends on the column 0 of the line below the block.
func paint(prev, next []string) string {
	if len(prev) != len(next) {
		return eraseSeq(len(prev)) + drawAll(next)
	}
	if len(next) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\x1b[%dA\r", len(prev))
	for i, line := range next {
		if line != prev[i] {
			b.WriteString("\x1b[2K")
			b.WriteString(line)
		}
		b.WriteString(down)
	}
	return b.String()
}

// drawAll writes the whole block below the cursor.
func drawAll(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// eraseSeq clears height lines above the cursor and leaves the cursor at the top
// of the cleared block.
func eraseSeq(height int) string {
	if height <= 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(up)
	for i := range height {
		b.WriteString(clear)
		if i < height-1 {
			b.WriteString(up)
		}
	}
	return b.String()
}

const (
	clear = "\x1b[2K"
	up    = "\x1b[1A\r"
	down  = "\x1b[1B\r"
)
