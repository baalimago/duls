package duls

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/duls/internal/app"
)

func TestSetupResolvesOneOptionalPath(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		args []string
		path string
		all  bool
		err  string
	}{
		{"no argument keeps the working directory", nil, "", false, ""},
		{"one argument is the path", []string{"notes"}, "notes", false, ""},
		{"a flag may follow the path", []string{"notes", "-a"}, "notes", true, ""},
		{"the terminator makes a flag a path", []string{"--", "-a"}, "-a", false, ""},
		{"two arguments are refused", []string{"one", "two"}, "", false, "at most one path"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := Command(app.Options{})
			err := c.Parse(tt.args)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("Parse error = %v, want %q", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if c.path != tt.path || c.all != tt.all {
				t.Fatalf("command = path %q all %t, want path %q all %t", c.path, c.all, tt.path, tt.all)
			}
		})
	}
}

func TestHelpNamesTheCommandAndFlags(t *testing.T) {
	t.Parallel()
	help := Command(app.Options{}).Help()
	for _, want := range []string{"duls - the size view", "Usage:", "duls [flags] [path]", "--all", "--version", "-json"} {
		if !strings.Contains(help, want) {
			t.Fatalf("help is missing %q:\n%s", want, help)
		}
	}
}

func TestRunPrintsTheVersion(t *testing.T) {
	t.Parallel()
	var out strings.Builder
	c := Command(app.Options{Stdout: &out})
	if err := c.Parse([]string{"--version"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := out.String(); got != app.VersionLine()+"\n" {
		t.Fatalf("version output = %q, want %q", got, app.VersionLine()+"\n")
	}
}

func TestRunPrintsTheAgentDocumentOnAPipe(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	c := Command(app.Options{Stdout: &out, Cwd: root})
	if err := c.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.HasPrefix(out.String(), "{\"root\":") {
		t.Fatalf("answer = %q, want the agent document", out.String())
	}
}

func TestParseReadsEveryFlag(t *testing.T) {
	t.Parallel()
	c := Command(app.Options{})
	if err := c.Parse([]string{"--all", "--all-types", "--bytes", "-json", "-m", "3", "-r", "0", "-t", "5s", "notes"}); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := app.Request{Path: "notes", All: true, Types: true, Bytes: true, JSON: true, Mode: 3, Refresh: 0, Timeout: 5 * time.Second}
	if got := c.Request(); got != want {
		t.Fatalf("Request = %+v, want %+v", got, want)
	}
}

func TestParseDefaults(t *testing.T) {
	t.Parallel()
	c := Command(app.Options{})
	if err := c.Parse(nil); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := app.Request{Mode: 1, Refresh: 15 * time.Minute, Timeout: 2 * time.Second}
	if got := c.Request(); got != want {
		t.Fatalf("Request = %+v, want %+v", got, want)
	}
}

func TestParseRejectsBadValues(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"an unknown flag", []string{"--nope"}, "flag: "},
		{"an unparsable duration", []string{"-t", "soon"}, "flag: "},
		{"a negative refresh", []string{"-r", "-1s"}, "refresh must not be negative"},
		{"a negative timeout", []string{"-t", "-1s"}, "timeout must not be negative"},
		{"a mode above three", []string{"-m", "4"}, "mode must be 0, 1, 2 or 3"},
		{"a negative mode", []string{"-m", "-1"}, "mode must be 0, 1, 2 or 3"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := Command(app.Options{}).Parse(tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Parse error = %v, want %q", err, tt.want)
			}
		})
	}
}
