# duls — build and test entry points.
#
# There is one Go test command. No target hides a subset of the suite behind
# a build tag, an environment variable, or a flag: `make test` runs every Go
# test that exists.

GO ?= go
GOFUMPT := go run mvdan.cc/gofumpt@v0.11.0
STATICCHECK := go run honnef.co/go/tools/cmd/staticcheck@v0.7.0

BUILD_DIR := .build
BIN := $(BUILD_DIR)/duls
COVER_PROFILE := $(BUILD_DIR)/cover.out
# The documented statement-coverage floor; 90 % is preferred.
COVER_FLOOR := 70

# VERSION is the release version reported by --version. Release pipelines
# override it with the tag-derived version; development builds keep the -dev
# suffix.
VERSION ?= 0.1.0-dev
VERSION_LDFLAG := -X github.com/baalimago/duls/internal/app.Version=$(VERSION)
LDFLAGS := -s -w $(VERSION_LDFLAG)

# Every Go source the binary is built from, so a changed source rebuilds it.
GO_SOURCES := go.mod go.sum $(shell find . -path ./.build -prune -o -name '*.go' -print)

.PHONY: all test cover lint fmt build qa clean

all: qa

build: $(BIN)

$(BIN): $(GO_SOURCES)
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $@ .

# test — every Go test in the repository, under the race detector, three
# times, with a per-package deadline and coverage across every package.
#
# Coverage uses -coverpkg=./... because the black-box scenario suite exercises
# packages other than its own; per-package coverage would understate it badly.
#
# Do not weaken -race, -count, or -timeout.
test: $(BIN)
	$(GO) test -race -count=3 -timeout=30s -coverpkg=./... -coverprofile=$(COVER_PROFILE) ./...
	@total=$$($(GO) tool cover -func=$(COVER_PROFILE) | tail -1 | awk '{print $$3}'); \
	 echo "== coverage: $$total (floor $(COVER_FLOOR)%) =="; \
	 awk -v got="$${total%\%}" -v floor=$(COVER_FLOOR) 'BEGIN { \
	   if (got + 0 < floor) { printf "coverage %s%% is below the %d%% floor\n", got, floor; exit 1 } }'

# cover — open the coverage profile from the last `make test` in a browser.
cover: $(COVER_PROFILE)
	$(GO) tool cover -html=$(COVER_PROFILE)

# lint — formatting and static analysis. These are not tests; they are the
# other half of the local gate.
lint:
	@echo "== gofumpt =="
	@files="$$($(GOFUMPT) -l .)"; test -z "$$files" || { echo "gofumpt: unformatted files:"; echo "$$files"; exit 1; }
	@echo "== go vet =="
	$(GO) vet ./...
	@echo "== staticcheck =="
	$(STATICCHECK) ./...
	@echo "== go fix =="
	@out="$$($(GO) fix -diff ./...)"; test -z "$$out" || { echo "go fix: fixes needed:"; echo "$$out"; exit 1; }

# fmt — apply gofumpt to the tree.
fmt:
	$(GOFUMPT) -w -l .

# qa — lint plus the test suite.
qa: lint test

clean:
	rm -rf $(BUILD_DIR)
