package role_test

import (
	"testing"

	"github.com/baalimago/duls/internal/role"
)

func TestOf(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want role.Role
	}{
		{"main.go", role.Source},
		{"internal/scan/walk.go", role.Source},
		{"include/x.H", role.Source},
		{"install.sh", role.Source},
		{"security.go", role.Source},
		{"walk_test.go", role.Test},
		{"lib/parse_spec.rb", role.Test},
		{"src/net_unittest.cc", role.Test},
		{"src/app.test.tsx", role.Test},
		{"src/app.spec.js", role.Test},
		{"FooTest.java", role.Test},
		{"FooTests.cs", role.Test},
		{"UserSpec.scala", role.Test},
		{"test_parse.py", role.Test},
		{"conftest.py", role.Test},
		{"cli.bats", role.Test},
		{"tests/integration.rs", role.Test},
		{"Tests/AppTests/Main.swift", role.Test},
		{"pkg/testdata/golden.json", role.Test},
		{"ui/__snapshots__/a.snap", role.Test},
		{"docs/tests/a.md", role.Test},
		{"tests/docs/a.md", role.Test},
		{"test.go", role.Source},
		{"latest.go", role.Source},
		{"contest.md", role.Docs},
		{"api_test.yaml", role.Config},
		{"README.md", role.Docs},
		{"README", role.Docs},
		{"readme.rst", role.Docs},
		{"LICENSE", role.Docs},
		{"LICENSE-APACHE", role.Docs},
		{"COPYING.LESSER", role.Docs},
		{"CODE_OF_CONDUCT", role.Docs},
		{"docs/conf.py", role.Docs},
		{"Documentation/x.c", role.Docs},
		{"README.html", role.Docs},
		{"LICENSE.json", role.Docs},
		{"README_zh.md", role.Docs},
		{"license.go", role.Source},
		{"guide.pdf", role.Other},
		{"Makefile", role.Config},
		{"Dockerfile", role.Config},
		{"Dockerfile.dev", role.Config},
		{"CMakeLists.txt", role.Config},
		{"requirements-dev.txt", role.Config},
		{"go.mod", role.Config},
		{"deploy.YAML", role.Config},
		{".gitignore", role.Config},
		{".env.local", role.Config},
		{"go.sum", role.Generated},
		{"web/package-lock.json", role.Generated},
		{"Cargo.lock", role.Generated},
		{"flake.lock", role.Generated},
		{"api.pb.go", role.Generated},
		{"api_pb2.py", role.Generated},
		{"model.g.dart", role.Generated},
		{"types_gen.go", role.Generated},
		{"zz_generated.deepcopy.go", role.Generated},
		{"app.js.map", role.Generated},
		{"docs/app.js.map", role.Generated},
		{"tests/fixtures/custom.lock", role.Generated},
		{"tests/api.pb.go", role.Generated},
		{"vendor/golang.org/x/term/term.go", role.Vendored},
		{"web/node_modules/a/index.js", role.Vendored},
		{"third_party/zlib/zlib.h", role.Vendored},
		{"vendor/x/x_test.go", role.Vendored},
		{"tests/vendor/a.go", role.Vendored},
		{"static/app.min.js", role.Vendored},
		{"static/app-min.css", role.Vendored},
		{"photo.jpg", role.Other},
		{"data.csv", role.Other},
		{"notes", role.Other},
		{"trailing.", role.Other},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			if got := role.Of(tt.path); got != tt.want {
				t.Fatalf("Of(%q) = %s, want %s", tt.path, got, tt.want)
			}
		})
	}
}

func TestExt(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]string{
		"a.GO": ".go", "x.tar.gz": ".gz", "Makefile": "", ".hidden": "", "tail.go.": "", ".env.local": ".local",
	} {
		if got := role.Ext(name); got != want {
			t.Fatalf("Ext(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestString(t *testing.T) {
	t.Parallel()
	want := []string{"source", "test", "docs", "config", "generated", "vendored", "other"}
	for r := range role.Count {
		if got := role.Role(r).String(); got != want[r] {
			t.Fatalf("Role(%d) = %q, want %q", r, got, want[r])
		}
	}
	if got := role.Role(role.Count).String(); got != "role(7)" {
		t.Fatalf("an out-of-range role = %q, want it named by number", got)
	}
}

func BenchmarkOf(b *testing.B) {
	paths := []string{
		"internal/scan/walk.go", "internal/scan/walk_test.go", "README.md",
		"web/node_modules/react/index.js", "a/b/c/d/e/f/g/h.txt", "Makefile",
	}
	for i := range b.N {
		_ = role.Of(paths[i%len(paths)])
	}
}
