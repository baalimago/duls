package scan

import (
	"strings"
	"testing"
)

// TestFnmatchAgreesWithGlibc pins fnmatch to glibc. The expected values were
// recorded from glibc 2.43 fnmatch(pattern, name, 0) under C.UTF-8.
func TestFnmatchAgreesWithGlibc(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		pattern, name string
		want          bool
	}{
		{`*`, `/a/b`, true},
		{`?`, `/`, true},
		{`a*b`, `a/b`, true},
		{`a*b`, `a/c`, false},
		{`x?y`, `x/y`, true},
		{`a[/]b`, `a/b`, true},
		{`x[/]`, `x/`, true},
		{`*.log`, `.x.log`, true},
		{`**`, ``, true},
		{`[!.]*`, `src`, true},
		{`[!.]*`, `.git`, false},
		{`[^.]*`, `src`, true},
		{`[]]`, `]`, true},
		{`[!]]`, `a`, true},
		{`[!]]`, `]`, false},
		{`[a-]`, `-`, true},
		{`[-a]`, `-`, true},
		{`[z-a]`, `m`, false},
		{`[a-c]`, `b`, true},
		{`[a-c]`, `d`, false},
		{`[?-[]`, `[`, true},
		{`\*`, `*`, true},
		{`\*`, `x`, false},
		{`[\]]`, `]`, true},
		{`[\]]`, `\`, false},
		{`[a\]]`, `]`, true},
		{`a\/b`, `a/b`, true},
		{`\`, `\`, false},
		{`[`, `[`, true},
		{`a[`, `a[`, true},
		{`a[b`, `a[b`, true},
		{`[]`, `[]`, true},
		{`[-`, `[-`, true},
		{`[ab`, `[ab`, true},
		{`[ab`, `a`, false},
		{`[a-`, `[a-`, false},
		{`[--`, `[--`, false},
		{`[[:A]`, `A`, true},
		{`[[:digit:]]`, `5`, true},
		{`[[:digit:]]`, `x`, false},
		{`[[:alpha:]x]`, `x`, true},
		{`[[:upper:]]`, `A`, true},
		{`[[:upper:]]`, `a`, false},
		{`[[:space:]]`, ` `, true},
		{`[[:xdigit:]]`, `F`, true},
		{`[[:punct:]]`, `$`, true},
		{`[[:alnum:]]`, `_`, false},
		{`[a-[:digit:]]`, `d]`, true},
		{`[:?-[:upper:]*`, `:x`, false},
		{`[?-[:upper:]*`, `@x`, true},
		{`[[:digit:][:nope:]]`, `1`, true},
		{`[[:digit:][:nope:]]`, `x`, false},
		{`[[:nope:][:digit:]]`, `1`, false},
		{`[a[:digit:]x]`, `a`, true},
		{`[a[:digit:]\]`, `a\`, false},
		{"*caf\xe9*", "caf\xe8s", false},
		{"*caf\xe9*", "caf\xe9s", true},
		{"[\x80-\xff]", "\xe9", true},
		{"?", "\xe9", true},
		{"[[:alpha:]]", "\xe9", false},
	} {
		if got := fnmatch(tt.pattern, tt.name) == matched; got != tt.want {
			t.Errorf("fnmatch(%q, %q) = %t, want %t", tt.pattern, tt.name, got, tt.want)
		}
	}
}

// TestClassesMatchByRune proves every POSIX class name is known and tests a
// rune of valid UTF-8, not a byte.
func TestClassesMatchByRune(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		class string
		in    rune
		out   rune
	}{
		{"alnum", 'é', '_'},
		{"alpha", 'é', '1'},
		{"blank", '\t', 'x'},
		{"cntrl", '\x07', 'x'},
		{"digit", '7', 'a'},
		{"graph", 'é', ' '},
		{"lower", 'é', 'É'},
		{"print", ' ', '\x07'},
		{"punct", '~', 'a'},
		{"space", '\n', 'x'},
		{"upper", 'É', 'é'},
		{"xdigit", 'f', 'g'},
	} {
		pattern := "[[:" + tt.class + ":]]"
		if fnmatch(pattern, string(tt.in)) != matched || fnmatch(pattern, string(tt.out)) == matched {
			t.Errorf("%s: want %q in and %q out", pattern, tt.in, tt.out)
		}
	}
	if fnmatch("?", "é") != matched || fnmatch("??", "é") == matched {
		t.Error("'?' must match one rune of valid UTF-8, not one byte")
	}
}

// TestCompiledTestsAgreeWithEverySuffix proves each fast path decides what
// fnmatch over the path and every suffix after a '/' decides.
func TestCompiledTestsAgreeWithEverySuffix(t *testing.T) {
	t.Parallel()
	paths := []string{
		"/r/a", "/r/src/gen", "/r/x/src/gen", "/r/src/gen/f.log", "/r/.git",
		"/r/a.log", "/r/b/a.log", "/home/me/proj/build", "/r/node_modules/x", "/r/caf\xe9.log",
	}
	patterns := []string{
		"a", "gen", "src/gen", "/r/a", "*.log", "*", "*/gen", "/r/*", "/home/*/build",
		"?", "[!.]*", ".[!g]*", "s?c/gen", "[sx]*", "x*gen", "a.log", "node_modules", "caf?.log",
	}
	for _, pattern := range patterns {
		test := compile(pattern)
		for _, path := range paths {
			want := fnmatch(pattern, path)
			for i := 0; i < len(path)-1 && want == mismatch; i++ {
				if path[i] == '/' {
					want = fnmatch(pattern, path[i+1:])
				}
			}
			if got := test.match(path, suffixStarts(path, nil)); got != want {
				t.Errorf("kind %d: %q on %q = %d, want %d", test.kind, pattern, path, got, want)
			}
		}
	}
}

// TestRefuseNamesTheReason proves every pattern no path can match, and every
// syntax duls does not implement, is refused with its reason, and that valid
// patterns pass.
func TestRefuseNamesTheReason(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		pattern string
		want    string
	}{
		{"", "is empty"},
		{"src/", "drop the trailing '/'"},
		{"/", "drop the trailing '/'"},
		{"*/", "drop the trailing '/'"},
		{`src\/`, "drop the trailing '/'"},
		{"x[/]", "drop the trailing '/'"},
		{`x[\/]`, "drop the trailing '/'"},
		{"a//b", "use a single '/'"},
		{"a[/]/b", "use a single '/'"},
		{"./src", "drop its '.' and '..' segments"},
		{"a/../b", "drop its '.' and '..' segments"},
		{"a/.", "drop its '.' and '..' segments"},
		{`\.\./x`, "drop its '.' and '..' segments"},
		{"a/[.]/b", "drop its '.' and '..' segments"},
		{`a\`, `escape the trailing '\' as '\\'`},
		{"[a-", "its bracket runs off the end"},
		{"x[--", "its bracket runs off the end"},
		{`[a\`, "its bracket runs off the end"},
		{"[[:nope:]]", "unknown character class [:nope:]"},
		{"[[:digit:][:nope:]]", "unknown character class [:nope:]"},
		{"[[.a.]]", "collating symbol"},
		{"[[=a=]]", "collating symbol"},
		{"[a-[.z.]]", "collating symbol"},
		{"[a-[=b=]]", "collating symbol"},
		{"src", ""},
		{"/abs/path", ""},
		{"*.log", ""},
		{".git", ""},
		{"..x", ""},
		{"a[/]b", ""},
		{"[", ""},
		{"[-", ""},
		{"[a-[:digit:]]", ""},
		{"[!.]*", ""},
		{"caf\xe9", ""},
	} {
		err := refuse(tt.pattern)
		if tt.want == "" {
			if err != nil {
				t.Errorf("refuse(%q) = %v, want it accepted", tt.pattern, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("refuse(%q) = %v, want %q", tt.pattern, err, tt.want)
		}
	}
}
