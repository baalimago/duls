package agent_test

import (
	"strings"
	"testing"
	"time"

	"github.com/baalimago/duls/internal/agent"
	"github.com/baalimago/duls/internal/plan"
	"github.com/baalimago/duls/internal/scan"
)

func TestEncodeWritesTheAlgorithmFacts(t *testing.T) {
	t.Parallel()
	got, err := agent.Encode(agent.Request{Root: "/root", View: plan.View{
		Totals: plan.Totals{
			Files: 6, Bytes: 420, Lines: 84, BytesExact: true, LinesExact: true,
			Measured: 6,
			Types:    []plan.TypeShare{{Ext: ".md", Files: 6, Share: 100}},
		},
		Entries: []plan.Entry{
			{
				Name: "people", Path: "people", Kind: plan.Directory, Files: 2,
				Bytes: 170, Share: 40, Exact: true,
				Kids: []plan.Item{{Name: "ana.md", Bytes: 85, Lines: 17}, {Name: "ben.md", Bytes: 85, Lines: 17}},
			},
			{Name: "README.md", Path: "README.md", Kind: plan.File, Bytes: 100, Lines: 20, Exact: true},
		},
	}})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want := `{"root":"/root",` +
		`"totals":{"files":6,"bytes":420,"lines":84,"bytesExact":true,"linesExact":true,"typesMore":0},` +
		`"types":[{"ext":".md","files":6,"share":100}],` +
		`"entries":[{"name":"people","path":"people","kind":"dir","depth":0,"files":2,"bytes":170,"share":40,"exact":true,` +
		`"children":[{"name":"ana.md","kind":"file","files":null,"lines":17},{"name":"ben.md","kind":"file","files":null,"lines":17}],"more":0},` +
		`{"name":"README.md","path":"README.md","kind":"file","depth":0,"bytes":100,"exact":true,"lines":20}],` +
		`"accounting":{"stopped":"none","measured":6,"notScanned":0,"skipped":0,"unknown":0,"unreadable":0,"unreadableDirs":0,"otherFilesystems":0,"reused":0}}`
	if string(got) != want {
		t.Fatalf("document =\n%s\nwant\n%s", got, want)
	}
}

func TestEncodeStatesWhatTheRunCouldNotProve(t *testing.T) {
	t.Parallel()
	got, err := agent.Encode(agent.Request{Root: "/r", Limit: 1500 * time.Millisecond, View: plan.View{
		Totals: plan.Totals{
			Files: 9, Bytes: 400, BytesExact: false, Lines: 12, LinesExact: false,
			Measured: 3, Skipped: 1, Unknown: 2, Unreadable: 1, UnreadableDirs: 2, OtherFilesystems: 3, Reused: 2,
			Stopped: scan.StopDeadline, StoppedPhase: scan.PhaseCount,
			Filesystem: &scan.Filesystem{Total: 1000, Used: 400, Avail: 500},
		},
		Entries: []plan.Entry{
			{
				Name: "deep", Path: "deep", Kind: plan.Directory, Files: 4, Bytes: 300,
				Share: 75, Exact: false,
				Kids: []plan.Item{{Name: "sub", Dir: true, Files: 3}, {Name: "a.md", Lines: -1}},
				More: 2,
			},
			{Name: "open.md", Path: "open.md", Kind: plan.File, Bytes: 100, Exact: true, Lines: -1},
			{Kind: plan.Folded, Files: 2, Bytes: 50, Share: 12, Folded: []string{"a", "b"}},
		},
	}})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	for _, want := range []string{
		`"bytesExact":false`,
		`"linesExact":false`,
		`"stopped":"count","limit":"1.5s"`,
		`"measured":3,"notScanned":3,"skipped":1,"unknown":2,"unreadable":1,"unreadableDirs":2,"otherFilesystems":3,"reused":2`,
		`{"name":"sub","kind":"dir","files":3,"lines":null,"exact":false}`,
		`{"name":"a.md","kind":"file","files":null,"lines":null}`,
		`"kind":"file","depth":0,"bytes":100,"exact":true,"lines":null`,
		`{"name":"+2 smaller","kind":"folded","files":2,"bytes":50,"share":12,"exact":false,"names":["a","b"]}`,
		`"filesystem":{"total":1000,"used":400,"avail":500}`,
	} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("document =\n%s\nwant it to hold\n%s", got, want)
		}
	}
}

func TestEncodeEmptyViewHasNoTypes(t *testing.T) {
	t.Parallel()
	got, err := agent.Encode(agent.Request{Root: "/r", View: plan.View{}})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	for _, want := range []string{`"types":[]`, `"entries":[]`, `"stopped":"none"`} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("document = %s, want it to hold %s", got, want)
		}
	}
}

func TestEncodeRejectsAnEntryWithoutAKind(t *testing.T) {
	t.Parallel()
	_, err := agent.Encode(agent.Request{View: plan.View{Entries: []plan.Entry{{Name: "x", Kind: 9}}}})
	if err == nil || !strings.Contains(err.Error(), `entry "x" has no kind`) {
		t.Fatalf("err = %v, want the unknown kind", err)
	}
}
