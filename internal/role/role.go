// Package role reads a file's role from its path below the root, never from
// its content (architecture/role.md).
package role

import (
	"strconv"
	"strings"
)

// Role is the part a file plays in a tree.
type Role uint8

const (
	Source Role = iota
	Test
	Docs
	Config
	Generated
	Vendored
	Other
)

// Count is the number of roles; a Role indexes an array of this length.
const Count = int(Other) + 1

// Tally is what the count did with one role's files (architecture/role.md).
type Tally struct {
	Files   int
	Lines   int64
	Skipped int
}

var names = [Count]string{"source", "test", "docs", "config", "generated", "vendored", "other"}

func (r Role) String() string {
	if int(r) >= Count {
		return "role(" + strconv.Itoa(int(r)) + ")"
	}
	return names[r]
}

// Of returns the role of a root-relative, slash-separated path (architecture/role.md).
func Of(path string) Role {
	dir, name := "", path
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		dir, name = path[:i], path[i+1:]
	}
	place := ofDirs(dir)
	stem, ext := split(name)
	byExt, known := extensions[ext]
	switch {
	case place == Vendored || minified(name):
		return Vendored
	case lockfiles[name] || (known && byExt == Generated) || generatedName(name, stem):
		return Generated
	case place == Test || (known && byExt == Source && testName(name, stem, ext)):
		return Test
	case place == Docs || leadingWord(name, licenseWords, exact):
		return Docs
	}
	if r, ok := filenames[name]; ok {
		return r
	}
	switch {
	case strings.HasPrefix(name, "Dockerfile.") || (strings.HasPrefix(name, "requirements") && ext == ".txt"):
		return Config
	case known:
		return byExt
	case leadingWord(name, docsWords, strings.EqualFold):
		return Docs
	case strings.HasPrefix(name, "."):
		return Config
	}
	return Other
}

// Ext returns the final extension of a base name, lowercased with its dot, and
// "" when it has none (architecture/scan.md).
func Ext(name string) string {
	_, ext := split(name)
	return ext
}

func ofDirs(dir string) Role {
	place := Other
	for dir != "" {
		var part string
		part, dir, _ = strings.Cut(dir, "/")
		switch part {
		case "vendor", "node_modules", "third_party", "third-party", "bower_components":
			return Vendored
		case "test", "tests", "Test", "Tests", "__tests__", "spec", "specs", "testdata", "__mocks__", "__snapshots__":
			place = Test
		case "doc", "docs", "Doc", "Docs", "documentation", "Documentation":
			if place != Test {
				place = Docs
			}
		}
	}
	return place
}

func split(name string) (stem, ext string) {
	i := strings.LastIndexByte(name, '.')
	if i <= 0 || i == len(name)-1 {
		return name, ""
	}
	return name[:i], strings.ToLower(name[i:])
}

func minified(name string) bool {
	return hasAnySuffix(name, ".min.js", "-min.js", ".min.css", "-min.css")
}

func generatedName(name, stem string) bool {
	return strings.HasPrefix(name, "zz_generated") ||
		hasAnySuffix(stem, ".pb", "_pb", "_pb2", "_pb2_grpc", "_gen", ".gen", "_generated", ".generated", ".g", ".freezed")
}

func testName(name, stem, ext string) bool {
	if ext == ".py" && (strings.HasPrefix(name, "test_") || name == "conftest.py") {
		return true
	}
	return hasAnySuffix(stem, "_test", "_tests", "_spec", "_unittest", ".test", ".spec", "Test", "Tests", "Spec")
}

func exact(a, b string) bool { return a == b }

// leadingWord reports a name that is one of words, alone or followed by '.',
// '-' or '_', compared with equal.
func leadingWord(name string, words []string, equal func(a, b string) bool) bool {
	for _, word := range words {
		if len(name) < len(word) || !equal(name[:len(word)], word) {
			continue
		}
		if len(name) == len(word) || strings.IndexByte(".-_", name[len(word)]) >= 0 {
			return true
		}
	}
	return false
}

func hasAnySuffix(s string, suffixes ...string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(s, suffix) {
			return true
		}
	}
	return false
}

var licenseWords = []string{"README", "LICENSE", "LICENCE", "COPYING"}

var docsWords = []string{
	"readme", "license", "licence", "copying", "copyright", "changelog", "changes",
	"history", "news", "contributing", "contributors", "authors", "notice",
	"code_of_conduct", "security", "install", "maintainers",
}

var lockfiles = map[string]bool{
	"go.sum": true, "go.work.sum": true, "package-lock.json": true, "npm-shrinkwrap.json": true,
	"yarn.lock": true, "pnpm-lock.yaml": true, "bun.lock": true, "bun.lockb": true,
	"deno.lock": true, "Cargo.lock": true, "Gemfile.lock": true, "Podfile.lock": true,
	"poetry.lock": true, "Pipfile.lock": true, "uv.lock": true, "pdm.lock": true,
	"composer.lock": true, "mix.lock": true, "pubspec.lock": true, "flake.lock": true,
	"Package.resolved": true, "packages.lock.json": true, "gradle.lockfile": true,
}

var filenames = map[string]Role{
	"Makefile": Config, "makefile": Config, "GNUmakefile": Config, "Dockerfile": Config,
	"Containerfile": Config, "Jenkinsfile": Config, "Vagrantfile": Config, "Procfile": Config,
	"Gemfile": Config, "Rakefile": Config, "Podfile": Config, "Brewfile": Config,
	"Pipfile": Config, "Justfile": Config, "justfile": Config, "Tiltfile": Config,
	"Earthfile": Config, "CMakeLists.txt": Config, "go.mod": Config, "go.work": Config,
	"BUILD": Config, "WORKSPACE": Config, "Kconfig": Config, "CODEOWNERS": Config,
	"OWNERS": Config, "configure": Config,
}

var extensions = map[string]Role{
	// Source: programming, query, shader, markup and style languages.
	".go": Source, ".c": Source, ".h": Source, ".cc": Source, ".cpp": Source, ".cxx": Source,
	".hh": Source, ".hpp": Source, ".hxx": Source, ".inl": Source, ".ipp": Source,
	".rs": Source, ".py": Source, ".pyi": Source, ".pyx": Source, ".pxd": Source,
	".js": Source, ".jsx": Source, ".mjs": Source, ".cjs": Source, ".ts": Source,
	".tsx": Source, ".mts": Source, ".cts": Source, ".java": Source, ".kt": Source,
	".kts": Source, ".scala": Source, ".sc": Source, ".groovy": Source, ".cs": Source,
	".csx": Source, ".fs": Source, ".fsi": Source, ".fsx": Source, ".vb": Source,
	".swift": Source, ".m": Source, ".mm": Source, ".rb": Source, ".rake": Source,
	".php": Source, ".pl": Source, ".pm": Source, ".lua": Source, ".sh": Source,
	".bash": Source, ".zsh": Source, ".fish": Source, ".ksh": Source, ".ps1": Source,
	".psm1": Source, ".bat": Source, ".cmd": Source, ".r": Source, ".jl": Source,
	".dart": Source, ".ex": Source, ".exs": Source, ".erl": Source, ".hrl": Source,
	".clj": Source, ".cljs": Source, ".cljc": Source, ".hs": Source, ".lhs": Source,
	".ml": Source, ".mli": Source, ".elm": Source, ".nim": Source, ".zig": Source,
	".odin": Source, ".gleam": Source, ".v": Source, ".sv": Source, ".vhd": Source,
	".vhdl": Source, ".sql": Source, ".proto": Source, ".graphql": Source, ".gql": Source,
	".css": Source, ".scss": Source, ".sass": Source, ".less": Source, ".styl": Source,
	".html": Source, ".htm": Source, ".xhtml": Source, ".vue": Source, ".svelte": Source,
	".astro": Source, ".asm": Source, ".s": Source, ".f": Source, ".f90": Source,
	".f95": Source, ".for": Source, ".vim": Source, ".el": Source, ".lisp": Source,
	".scm": Source, ".rkt": Source, ".tf": Source, ".nix": Source, ".sol": Source,
	".cu": Source, ".cuh": Source, ".cr": Source, ".gd": Source,
	".glsl": Source, ".hlsl": Source, ".wgsl": Source, ".vert": Source, ".frag": Source,
	".wat": Source, ".ipynb": Source, ".pas": Source, ".ada": Source, ".adb": Source,
	".ads": Source, ".hx": Source, ".coffee": Source, ".purs": Source,
	".awk": Source, ".tcl": Source, ".jsonnet": Source, ".libsonnet": Source, ".cue": Source,
	".dhall": Source, ".rego": Source, ".mojo": Source,
	// Test: a format that only holds tests.
	".bats": Test,
	// Docs: prose and plain-text document formats.
	".md": Docs, ".markdown": Docs, ".mdown": Docs, ".mkd": Docs, ".mdx": Docs, ".rst": Docs,
	".adoc": Docs, ".asciidoc": Docs, ".txt": Docs, ".text": Docs, ".org": Docs,
	".rdoc": Docs, ".pod": Docs, ".textile": Docs, ".tex": Docs, ".rtf": Docs,
	// Config: settings, manifests and build descriptions.
	".yml": Config, ".yaml": Config, ".toml": Config, ".json": Config, ".jsonc": Config,
	".json5": Config, ".ini": Config, ".cfg": Config, ".conf": Config, ".config": Config,
	".env": Config, ".properties": Config, ".xml": Config, ".plist": Config, ".mk": Config,
	".mak": Config, ".cmake": Config, ".gradle": Config, ".bazel": Config, ".bzl": Config,
	".dockerfile": Config, ".tfvars": Config, ".hcl": Config, ".cabal": Config,
	".csproj": Config, ".fsproj": Config, ".vbproj": Config, ".vcxproj": Config,
	".sln": Config, ".props": Config, ".targets": Config, ".editorconfig": Config,
	// Generated: lockfiles and source maps.
	".lock": Generated, ".map": Generated,
}
