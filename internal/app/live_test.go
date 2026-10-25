package app

import (
	"strings"
	"testing"

	"github.com/baalimago/duls/internal/plan"
	"github.com/baalimago/duls/internal/scan"
	"github.com/baalimago/duls/internal/tui"
	"github.com/baalimago/duls/internal/view"
)

// TestLiveRendersTheFactsHeldNow proves the frame's lines come from the tree an
// observation carries, so a visual run draws its rows and type line while the
// walk still runs (architecture/frame.md).
func TestLiveRendersTheFactsHeldNow(t *testing.T) {
	t.Parallel()
	drawing := &live{
		planner:  plan.New(),
		renderer: view.New(),
		opts:     view.Options{Width: 100, Style: tui.New(tui.TrueColor), Provisional: true},
	}
	lines := drawing.lines(scan.Progress{
		Phase: scan.PhaseWalk,
		Files: 2,
		Bytes: 100,
		Tree: func() scan.Snapshot {
			return scan.Snapshot{
				Files: 2, Bytes: 100, Stopped: scan.StopDeadline,
				Frontier: []string{"pics"},
				Types:    map[string]int{".png": 2},
				Dirs: []scan.Dir{
					{Path: "", Files: 2, Bytes: 100, Kids: []scan.Child{{Name: "pics", Dir: true, Files: 2, Bytes: 100}}},
					{Path: "pics", Files: 2, Bytes: 100},
				},
			}
		},
	})
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"~2 files", "~100 B", "types", ".png", "pics"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("live lines =\n%s\nlack %q", joined, want)
		}
	}
	if strings.Contains(joined, "partial:") {
		t.Fatalf("live lines =\n%s\nstate a footer the run has not proved", joined)
	}
}
