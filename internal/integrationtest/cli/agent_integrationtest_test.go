package cli_test

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/baalimago/duls/internal/integrationtest/itest"
)

// document is what a scenario reads of the agent answer. A key the document
// does not state stays at its zero value, which is how these tests prove that
// a quantity which does not belong to a row's kind is absent, not zero.
type document struct {
	Root   string `json:"root"`
	Totals struct {
		Files      int   `json:"files"`
		Bytes      int64 `json:"bytes"`
		Lines      int64 `json:"lines"`
		BytesExact bool  `json:"bytesExact"`
		LinesExact bool  `json:"linesExact"`
	} `json:"totals"`
	Types []struct {
		Ext   string `json:"ext"`
		Files int    `json:"files"`
		Share int    `json:"share"`
	} `json:"types"`
	Entries []entry `json:"entries"`
	// Filesystem is the device that holds the root, stated only when the run's
	// totals are lower bounds. A nil pointer is a document without the key.
	Filesystem *struct {
		Total int64 `json:"total"`
		Used  int64 `json:"used"`
		Avail int64 `json:"avail"`
	} `json:"filesystem"`
	Accounting struct {
		Stopped    string `json:"stopped"`
		Limit      string `json:"limit"`
		Measured   int    `json:"measured"`
		NotScanned int    `json:"notScanned"`
		Skipped    int    `json:"skipped"`
		Unknown    int    `json:"unknown"`
		Unreadable int    `json:"unreadable"`
		// UnreadableDirs counts the directories the walk could not list.
		UnreadableDirs int `json:"unreadableDirs"`
		Reused         int `json:"reused"`
	} `json:"accounting"`
}

type entry struct {
	Name     string   `json:"name"`
	Path     string   `json:"path"`
	Kind     string   `json:"kind"`
	Depth    int      `json:"depth"`
	Files    int      `json:"files"`
	Bytes    int64    `json:"bytes"`
	Share    int      `json:"share"`
	Lines    *int64   `json:"lines"`
	Children []child  `json:"children"`
	More     int      `json:"more"`
	Names    []string `json:"names"`
}

type child struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Files *int   `json:"files"`
	Lines *int64 `json:"lines"`
}

// decodeAgent reads the agent answer of a finished run.
func decodeAgent(t *testing.T, res itest.Result) document {
	t.Helper()
	if res.Code != 0 || res.Stderr != "" {
		t.Fatalf("duls = %d, %q; want 0 and no diagnostic", res.Code, res.Stderr)
	}
	var doc document
	if err := json.Unmarshal([]byte(res.Stdout), &doc); err != nil {
		t.Fatalf("the answer is not the agent document: %v\n%s", err, res.Stdout)
	}
	return doc
}

// entryNamed returns the row of the named entry.
func entryNamed(t *testing.T, doc document, name string) entry {
	t.Helper()
	for _, row := range doc.Entries {
		if row.Name == name {
			return row
		}
	}
	t.Fatalf("the document holds no row %q: %+v", name, doc.Entries)
	return entry{}
}

// TestScenarioPipeGetsTheAgentDocument proves a pipe gets the JSON document,
// never a layout: the algorithm's totals, type census, byte-ordered rows and
// accounting, with no escape byte anywhere.
func TestScenarioPipeGetsTheAgentDocument(t *testing.T) {
	t.Parallel()
	root := fixture(t)
	res := itest.Run(t, root, "-r", "0")
	if strings.Contains(res.Stdout, "\x1b") {
		t.Fatalf("the agent answer carries an escape byte: %q", res.Stdout)
	}
	doc := decodeAgent(t, res)
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Root != resolved {
		t.Fatalf("root = %q, want the resolved %q", doc.Root, resolved)
	}
	if doc.Totals.Files != 6 || doc.Totals.Bytes != 420 || doc.Totals.Lines != 84 {
		t.Fatalf("totals = %+v, want 6 files, 420 B and 84 lines", doc.Totals)
	}
	if !doc.Totals.BytesExact || !doc.Totals.LinesExact {
		t.Fatalf("totals = %+v, want both quantities exact", doc.Totals)
	}
	if len(doc.Types) != 1 || doc.Types[0].Ext != ".md" || doc.Types[0].Files != 6 || doc.Types[0].Share != 100 {
		t.Fatalf("types = %+v, want .md at 100%% of 6 files", doc.Types)
	}
	var names []string
	for _, row := range doc.Entries {
		names = append(names, row.Name)
	}
	if want := []string{"people", "meetings", "README.md"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("rows = %v, want the byte order %v", names, want)
	}
	people := entryNamed(t, doc, "people")
	if people.Kind != "dir" || people.Files != 2 || people.Bytes != 170 || people.Share != 40 {
		t.Fatalf("people = %+v, want a directory row of 2 files and 170 B", people)
	}
	if len(people.Children) != 2 || people.Children[0].Name != "ana.md" || people.Children[0].Lines == nil || *people.Children[0].Lines != 17 {
		t.Fatalf("people's children = %+v, want ana.md at 17 lines", people.Children)
	}
	readme := entryNamed(t, doc, "README.md")
	if readme.Kind != "file" || readme.Bytes != 100 || readme.Lines == nil || *readme.Lines != 20 {
		t.Fatalf("README.md = %+v, want a file row of 100 B and 20 lines", readme)
	}
	if readme.Files != 0 || readme.Share != 0 || len(readme.Children) != 0 {
		t.Fatalf("README.md = %+v, want no directory quantities on a file row", readme)
	}
	if doc.Accounting.Stopped != "none" || doc.Accounting.Measured != 6 || doc.Accounting.NotScanned != 0 {
		t.Fatalf("accounting = %+v, want a complete run", doc.Accounting)
	}
	if doc.Filesystem != nil {
		t.Fatalf("filesystem = %+v, want no device on a run whose totals are exact", doc.Filesystem)
	}
}

// TestScenarioJSONFlagMatchesThePipe proves -json states the same document on a
// terminal as the pipe does, so an agent never depends on how it is called.
func TestScenarioJSONFlagMatchesThePipe(t *testing.T) {
	t.Parallel()
	root := fixture(t)
	piped := itest.Run(t, root, "-r", "0")
	terminal := itest.RunOnTTY(t, root, 100, "-r", "0", "-json")
	if strings.Contains(terminal.Stdout, "\x1b") {
		t.Fatalf("duls -json on a terminal carries an escape byte: %q", terminal.Stdout)
	}
	if got, want := decodeAgent(t, terminal), decodeAgent(t, piped); !reflect.DeepEqual(got, want) {
		t.Fatalf("duls -json = %+v\nwant the pipe's %+v", got, want)
	}
}

// TestScenarioNoColorGetsTheAgentDocument proves a non-empty NO_COLOR selects
// the agent form even on a terminal, and no frame.
func TestScenarioNoColorGetsTheAgentDocument(t *testing.T) {
	t.Parallel()
	res := itest.RunOnTTYEnv(t, fixture(t), 100, []string{"NO_COLOR=1"}, "-r", "0")
	if strings.Contains(res.Stdout, "\x1b") {
		t.Fatalf("NO_COLOR got an escape byte and a frame: %q", res.Stdout)
	}
	if doc := decodeAgent(t, res); doc.Totals.Files != 6 {
		t.Fatalf("totals = %+v, want the six fixture files", doc.Totals)
	}
}

// TestScenarioAgentDocumentStatesWhatItCouldNotProve proves the document marks
// a line count the run did not prove as null, and counts the file it left out.
func TestScenarioAgentDocumentStatesWhatItCouldNotProve(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write(t, root, "ok.md", 3)
	writeRaw(t, root, "pic.png", "a\nb\nc\nd\n")
	doc := decodeAgent(t, itest.Run(t, root, "-r", "0"))
	denied := entryNamed(t, doc, "pic.png")
	if denied.Lines != nil {
		t.Fatalf("pic.png lines = %v, want null for a count the run did not prove", *denied.Lines)
	}
	if doc.Accounting.Skipped != 1 || doc.Accounting.Measured != 1 {
		t.Fatalf("accounting = %+v, want the denied file skipped and the other measured", doc.Accounting)
	}
	if !doc.Totals.LinesExact || doc.Totals.Lines != 3 {
		t.Fatalf("totals = %+v, want the proved line total", doc.Totals)
	}
}

// TestScenarioAgentDocumentDeclaresTheDeadline proves a cut run names the phase
// that stopped, the limit it obeyed, and the files the count never reached.
func TestScenarioAgentDocumentDeclaresTheDeadline(t *testing.T) {
	t.Parallel()
	doc := decodeAgent(t, itest.Run(t, fixture(t), "-r", "0", "-t", "1ns"))
	if doc.Accounting.Stopped != "walk" || doc.Accounting.Limit != "1ns" {
		t.Fatalf("accounting = %+v, want the walk's 1ns deadline", doc.Accounting)
	}
	if doc.Totals.BytesExact {
		t.Fatalf("totals = %+v, want the bytes a lower bound after a cut walk", doc.Totals)
	}
}

// TestScenarioAgentDocumentHoldsTheAlgorithm proves the document states what
// the planner decided: a promoted subdirectory as a deeper row, and the
// entries that got no row of their own as one folded row.
func TestScenarioAgentDocumentHoldsTheAlgorithm(t *testing.T) {
	t.Parallel()
	t.Run("a promoted subdirectory", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		write(t, root, "big/sub/a.md", 4)
		write(t, root, "big/sub/b.md", 4)
		for _, name := range []string{"c", "d", "e", "f", "g"} {
			write(t, root, name+"/one.md", 1)
		}
		doc := decodeAgent(t, itest.Run(t, root, "-r", "0"))
		var promoted *entry
		for i, row := range doc.Entries {
			if row.Path == "big/sub" {
				promoted = &doc.Entries[i]
			}
		}
		if promoted == nil {
			t.Fatalf("rows = %+v, want the promoted big/sub", doc.Entries)
		}
		if promoted.Kind != "dir" || promoted.Depth != 1 || promoted.Files != 2 {
			t.Fatalf("promoted = %+v, want a depth-1 directory row of 2 files", *promoted)
		}
	})
	t.Run("the folded remainder", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		for i := range 10 {
			write(t, root, fmt.Sprintf("d%02d/note.md", i), i+1)
		}
		doc := decodeAgent(t, itest.Run(t, root, "-r", "0"))
		if len(doc.Entries) != 8 {
			t.Fatalf("rows = %d, want the eight-row bound", len(doc.Entries))
		}
		last := doc.Entries[len(doc.Entries)-1]
		if last.Kind != "folded" || last.Name != "+3 smaller" {
			t.Fatalf("last row = %+v, want the three smallest folded", last)
		}
		if want := []string{"d02", "d01", "d00"}; !reflect.DeepEqual(last.Names, want) {
			t.Fatalf("folded = %v, want the byte order %v", last.Names, want)
		}
	})
}
