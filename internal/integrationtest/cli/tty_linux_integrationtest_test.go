//go:build linux

package cli_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/baalimago/duls/internal/integrationtest/itest"
)

// TestScenarioFrameIsErasedBeforeTheView proves a visual run that outlives the
// trigger draws frames, erases them, and lands its complete answer where the
// frame was: the tail after the last erase opens with the strip and carries the
// footer the provisional frame never prints (I6, I12).
func TestScenarioFrameIsErasedBeforeTheView(t *testing.T) {
	root := wide(t, 40, 500)
	writeRaw(t, root, "d0/pic.png", "a\n")
	res := itest.RunOnTTY(t, root, 150, "-m", "0", "-t", "0", "-r", "0", root)
	if res.Code != 0 || res.Stderr != "" {
		t.Fatalf("duls = %d, %q; want 0", res.Code, res.Stderr)
	}
	if count := strings.Count(res.Stdout, "\x1b[1A"); count < 2 {
		t.Fatalf("the run drew no frame to erase: %d cursor-ups", count)
	}
	cut := strings.LastIndex(res.Stdout, "\x1b[2K")
	if cut < 0 {
		t.Fatalf("the run erased no frame: %q", res.Stdout)
	}
	if frame := itest.StripANSI(res.Stdout[:cut]); strings.Contains(frame, "skipped 1") {
		t.Fatalf("a frame stated the answer's footer: %q", frame)
	}
	tail := itest.StripANSI(res.Stdout[cut+len("\x1b[2K"):])
	body := strings.Split(strings.TrimSuffix(tail, "\n"), "\n")
	if len(body) < 3 || !strings.HasPrefix(body[0], "█") {
		t.Fatalf("the answer after the erase does not open with the strip: %q", body)
	}
	answer := strings.Join(body[2:], "\n")
	if !strings.Contains(answer, "skipped 1") {
		t.Fatalf("the answer after the erase = %q, want the run's footer", answer)
	}
	if strings.Contains(answer, "~") || strings.Contains(answer, "partial") {
		t.Fatalf("the answer after the erase carries a provisional mark: %q", answer)
	}
}

// TestScenarioFrameDrawsTheViewItIsBuilding proves the frame draws the rows the
// walk holds while it runs: a row marked a lower bound appears before the
// answer, which no frame can show once the walk has returned and its facts are
// final (I12, I15, architecture/frame.md).
func TestScenarioFrameDrawsTheViewItIsBuilding(t *testing.T) {
	root := nest(t, 30, 4, 200)
	res := itest.RunOnTTY(t, root, 150, "-m", "0", "-t", "0", "-r", "0", root)
	if res.Code != 0 || res.Stderr != "" {
		t.Fatalf("duls = %d, %q; want 0", res.Code, res.Stderr)
	}
	cut := strings.LastIndex(res.Stdout, "\x1b[2K")
	if cut < 0 {
		t.Fatalf("the run drew no frame: %q", res.Stdout)
	}
	frames := itest.StripANSI(res.Stdout[:cut])
	for line := range strings.SplitSeq(frames, "\n") {
		if strings.Contains(line, "■") && strings.Contains(line, "d0") && strings.Contains(line, "~") {
			return
		}
	}
	t.Fatalf("no frame drew a row of the facts the walk held:\n%s", frames)
}

// answer is the view a visual run wrote last: everything after the frame's
// final erase, or the whole stream when no frame was drawn.
func answer(styled string) string {
	if cut := strings.LastIndex(styled, "\x1b[2K"); cut >= 0 {
		return styled[cut+len("\x1b[2K"):]
	}
	return styled
}

// TestScenarioVisualViewAdaptsToTheTerminal proves the form gate and the width
// gate on a real pseudo-terminal: a terminal gets the strip and the palette,
// the strip spans the clamped view width, and no line is wider than it when the
// terminal is narrower than the view can render.
func TestScenarioVisualViewAdaptsToTheTerminal(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		cols  int
		width int
	}{
		{"a 60-column terminal", 60, 60},
		{"a terminal narrower than the view", 20, 40},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := itest.RunOnTTY(t, fixture(t), tt.cols)
			if res.Code != 0 || res.Stderr != "" {
				t.Fatalf("duls = %d, %q; want 0", res.Code, res.Stderr)
			}
			if !strings.Contains(res.Stdout, "\x1b[38;2;") || !strings.Contains(res.Stdout, "█") {
				t.Fatalf("a terminal got no styled strip: %q", res.Stdout)
			}
			lines := strings.Split(strings.TrimSuffix(itest.StripANSI(answer(res.Stdout)), "\n"), "\n")
			if got := utf8.RuneCountInString(lines[0]); got != tt.width {
				t.Fatalf("strip is %d columns, want the %d of the clamped width", got, tt.width)
			}
			for _, line := range lines {
				if got := utf8.RuneCountInString(line); got > tt.width {
					t.Fatalf("line is %d columns, want at most %d: %q", got, tt.width, line)
				}
			}
		})
	}
}
