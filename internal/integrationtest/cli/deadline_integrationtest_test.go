package cli_test

import (
	"strings"
	"testing"

	"github.com/baalimago/duls/internal/integrationtest/itest"
)

// TestScenarioTimeoutIsDeclared proves a spent budget renders what is held,
// declares the phase and the limit, and exits 0 (I11, I4).
func TestScenarioTimeoutIsDeclared(t *testing.T) {
	t.Parallel()
	res := itest.RunVisual(t, fixture(t), 150, "-t", "1ns")
	if res.Code != 0 || res.Stderr != "" {
		t.Fatalf("duls -t 1ns = %d, %q; want exit 0 and no diagnostic", res.Code, res.Stderr)
	}
	lines := strings.Split(strings.TrimSuffix(res.Stdout, "\n"), "\n")
	footer := lines[len(lines)-1]
	for _, want := range []string{"partial: walk deadline 1ns", "filesystem ", " used of "} {
		if !strings.Contains(footer, want) {
			t.Fatalf("footer = %q, want %q", footer, want)
		}
	}
	if !strings.Contains(lines[0], "~") {
		t.Fatalf("totals = %q, want a lower bound the run can prove", lines[0])
	}
	if strings.Contains(lines[0], "· 0 B ·") {
		t.Fatalf("totals = %q, want no false zero", lines[0])
	}
}
