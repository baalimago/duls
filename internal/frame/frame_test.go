package frame_test

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/duls/internal/frame"
	"github.com/baalimago/duls/internal/scan"
)

var epoch = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

type clock struct{ at time.Time }

func newClock() *clock { return &clock{at: epoch} }

func (c *clock) now() time.Time { return c.at }

func (c *clock) advance(d time.Duration) { c.at = c.at.Add(d) }

func static(lines ...string) func(scan.Progress) []string {
	return func(scan.Progress) []string { return lines }
}

func obs(files, bytes, lines, dirs int64) scan.Progress {
	return scan.Progress{Phase: scan.PhaseWalk, Files: files, Bytes: bytes, Lines: lines, Dirs: dirs}
}

func open(out *bytes.Buffer, c *clock, lines func(scan.Progress) []string, trigger, interval time.Duration) frame.Rasterizer {
	return frame.New(frame.Options{
		Out:      out,
		Enabled:  true,
		Trigger:  trigger,
		Interval: interval,
		Now:      c.now,
		Lines:    lines,
	})
}

func TestDisabledWritesNothing(t *testing.T) {
	var out bytes.Buffer
	c := newClock()
	r := frame.New(frame.Options{Out: &out, Trigger: 0, Interval: 0, Now: c.now, Lines: static("one", "two")})
	for i := range 100 {
		c.advance(10 * time.Millisecond)
		r.Publish(obs(int64(i), int64(i)*10, 0, int64(i)))
	}
	if out.Len() != 0 {
		t.Fatalf("Publish.1: a disabled frame wrote %q", out.String())
	}
	r.Erase()
	if out.Len() != 0 {
		t.Fatalf("Publish.1: a disabled Erase wrote %q", out.String())
	}
}

func TestTriggerDelaysFirstFrame(t *testing.T) {
	var out bytes.Buffer
	c := newClock()
	r := open(&out, c, static("one"), 400*time.Millisecond, 100*time.Millisecond)
	c.advance(399 * time.Millisecond)
	r.Publish(obs(1, 10, 0, 1))
	if out.Len() != 0 {
		t.Fatalf("Publish.2: a frame appeared before the trigger: %q", out.String())
	}
	c.advance(time.Millisecond)
	r.Publish(obs(2, 20, 0, 2))
	if out.Len() == 0 {
		t.Fatal("Publish.2: no frame at the trigger")
	}
}

func TestIntervalThrottles(t *testing.T) {
	var out bytes.Buffer
	c := newClock()
	lines := func(p scan.Progress) []string {
		return []string{fmt.Sprintf("files %d", p.Files), fmt.Sprintf("dirs %d", p.Dirs)}
	}
	r := open(&out, c, lines, 0, 100*time.Millisecond)
	r.Publish(obs(1, 10, 0, 1))
	first := out.Len()
	if first == 0 {
		t.Fatal("Publish.3: the first observation drew nothing")
	}
	for i := range int64(5) {
		c.advance(10 * time.Millisecond)
		r.Publish(obs(2+i, 20, 0, 1))
	}
	if out.Len() != first {
		t.Fatalf("Publish.3: a second frame landed in one directory: %q", out.String()[first:])
	}
	c.advance(10 * time.Millisecond)
	r.Publish(obs(9, 90, 0, 2))
	if out.Len() != first {
		t.Fatalf("Publish.3: a frame landed inside the interval: %q", out.String()[first:])
	}
	c.advance(90 * time.Millisecond)
	r.Publish(obs(10, 100, 0, 3))
	if out.Len() == first {
		t.Fatal("Publish.3: no frame after a new directory and the interval")
	}
}

func TestUnchangedLinesAreSkipped(t *testing.T) {
	var out bytes.Buffer
	c := newClock()
	step := 0
	lines := func(scan.Progress) []string {
		if step == 0 {
			return []string{"alpha-one", "beta-two", "gamma-three"}
		}
		return []string{"alpha-one", "beta-CHANGED", "gamma-three"}
	}
	r := open(&out, c, lines, 0, 100*time.Millisecond)
	r.Publish(obs(1, 10, 0, 1))
	first := out.Len()
	step = 1
	c.advance(100 * time.Millisecond)
	r.Publish(obs(2, 20, 0, 2))
	repaint := out.String()[first:]
	if !strings.Contains(repaint, "beta-CHANGED") {
		t.Fatalf("Publish.4: the changed line was not repainted: %q", repaint)
	}
	for _, unchanged := range []string{"alpha-one", "gamma-three"} {
		if strings.Contains(repaint, unchanged) {
			t.Fatalf("Publish.4: the unchanged line %q was repainted: %q", unchanged, repaint)
		}
	}
	if !strings.Contains(repaint, "\x1b[2K") {
		t.Fatalf("Publish.4: the changed line was not cleared first: %q", repaint)
	}
}

func TestFrameKeepsGeometry(t *testing.T) {
	var out bytes.Buffer
	c := newClock()
	thirteen := make([]string, 13)
	for i := range thirteen {
		thirteen[i] = fmt.Sprintf("row-%d", i)
	}
	lines := static(thirteen...)
	r := open(&out, c, lines, 0, 100*time.Millisecond)
	r.Publish(obs(1, 10, 0, 1))
	if got := strings.Count(out.String(), "\n"); got != 13 {
		t.Fatalf("Publish.4: the frame drew %d lines, want 13", got)
	}
	if strings.Contains(out.String(), "\x1b[1A") || strings.Contains(out.String(), "\x1b[1B") {
		t.Fatalf("Publish.4: the first frame moved the cursor: %q", out.String())
	}

	var grown bytes.Buffer
	c = newClock()
	step := 0
	grow := func(scan.Progress) []string {
		if step == 0 {
			return thirteen[:12]
		}
		return thirteen
	}
	r = open(&grown, c, grow, 0, 100*time.Millisecond)
	r.Publish(obs(1, 10, 0, 1))
	first := grown.Len()
	step = 1
	c.advance(100 * time.Millisecond)
	r.Publish(obs(2, 20, 0, 2))
	if got := strings.Count(grown.String()[first:], "\n"); got != 13 {
		t.Fatalf("Publish.4: the grown frame drew %d lines, want 13", got)
	}
	if got := strings.Count(grown.String(), "row-12"); got != 1 {
		t.Fatalf("Publish.4: the grown frame drew row-12 %d times, want 1", got)
	}
}

func TestMonotoneObservations(t *testing.T) {
	base := obs(10, 1000, 100, 3)
	for _, tt := range []struct {
		name string
		next scan.Progress
	}{
		{"files", obs(9, 2000, 200, 4)},
		{"bytes", obs(11, 999, 200, 4)},
		{"lines", obs(11, 2000, 99, 4)},
		{"dirs", obs(11, 2000, 200, 2)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			c := newClock()
			r := open(&out, c, static("one", "two"), 0, 100*time.Millisecond)
			r.Publish(base)
			drawn := out.Len()
			if drawn == 0 {
				t.Fatal("Publish.6: the baseline observation drew nothing")
			}
			c.advance(100 * time.Millisecond)
			r.Publish(tt.next)
			if out.Len() != drawn {
				t.Fatalf("Publish.6: the decreasing %s observation drew %q", tt.name, out.String()[drawn:])
			}
		})
	}
}

type failing struct{ attempts int }

func (f *failing) Write([]byte) (int, error) {
	f.attempts++
	return 0, errors.New("boom")
}

func TestWriteFailureDisables(t *testing.T) {
	out := &failing{}
	c := newClock()
	r := frame.New(frame.Options{Out: out, Enabled: true, Now: c.now, Lines: static("one")})
	r.Publish(obs(1, 10, 0, 1))
	if out.attempts != 1 {
		t.Fatalf("Publish.7: wrote %d times, want 1", out.attempts)
	}
	c.advance(time.Second)
	r.Publish(obs(2, 20, 0, 2))
	r.Erase()
	if out.attempts != 1 {
		t.Fatalf("Publish.7: retried the failed write %d times", out.attempts-1)
	}
}

func TestErase(t *testing.T) {
	var out bytes.Buffer
	c := newClock()
	r := open(&out, c, static("alpha", "beta", "gamma"), 0, 100*time.Millisecond)
	r.Publish(obs(1, 10, 0, 1))
	frameAt := out.Len()
	if frameAt == 0 {
		t.Fatal("Erase.1: nothing to erase")
	}
	r.Erase()
	cleared := out.String()[frameAt:]
	if got := strings.Count(cleared, "\x1b[2K"); got != 3 {
		t.Fatalf("Erase.1: cleared %d lines, want 3: %q", got, cleared)
	}
	for _, text := range []string{"alpha", "beta", "gamma"} {
		if strings.Contains(cleared, text) {
			t.Fatalf("Erase.1: erased text %q: %q", text, cleared)
		}
	}
	erased := out.Len()
	c.advance(time.Second)
	r.Publish(obs(2, 20, 0, 2))
	r.Erase()
	if out.Len() != erased {
		t.Fatalf("Erase.2: wrote after Erase: %q", out.String()[erased:])
	}
}

func TestEraseWithoutAFrame(t *testing.T) {
	var out bytes.Buffer
	c := newClock()
	r := open(&out, c, static("alpha"), 0, 100*time.Millisecond)
	r.Erase()
	if out.Len() != 0 {
		t.Fatalf("Erase.1: Erase without a frame wrote %q", out.String())
	}
}

func TestFramePreservesViewContract(t *testing.T) {
	rows := []string{
		"8 files · ~40 B · ?",
		"  entry  files  size  share  largest inside",
		"  ■ src      4  20 B    50%  one 12 · two 3",
		"  ■ docs     2  10 B    25%  three ?",
		"  ■ main.go  -   8 B    20%  ?",
		"  ■ a.go     -  ~4 B    <1%  9 lines",
		"  ■ b.go     -  ~4 B    <1%  9 lines",
		"  ■ c.go     -  ~4 B    <1%  9 lines",
		"  partial: walk deadline 2s · skipped 1",
	}
	var out bytes.Buffer
	c := newClock()
	r := open(&out, c, static(rows...), 0, 100*time.Millisecond)
	r.Publish(obs(8, 40, -1, 3))
	want := strings.Join(rows, "\n") + "\n"
	if out.String() != want {
		t.Fatalf("Publish.5: the frame is not the supplied lines:\n got %q\nwant %q", out.String(), want)
	}
}
