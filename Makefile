GO ?= go
GOFLAGS ?= -v
GOPATH_BIN := $(shell $(GO) env GOPATH)/bin

GOIMPORTS_VERSION ?= v0.49.0
GOLANGCI_LINT_VERSION ?= v1.64.8
GOTESTSUM_VERSION ?= v1.13.0
GOCOVER_COBERTURA_VERSION ?= v1.5.0
GO_WINRES_VERSION ?= v0.3.3

APP_VERSION ?= 0.0.0-dev
WINDOWS_EXE := Jorbique-VPN.exe
WINDOWS_ZIP := Jorbique-VPN-windows.zip
MACOS_PKG := Jorbique-VPN-macOS.pkg
BINARY_DIR := release/binaries

# The unit suite builds like the shipped macOS binary and the Linux runner: without cgo.
# gocover-cobertura resolves paths through go/build, so it runs under the same setting.
GO_BUILD_ENV = CGO_ENABLED=0

# internal/macos runs here too, so its tests feed coverage.out. Only cli cannot compile off
# darwin (DaemonLoop has no stub); applekit has no files for linux, so go list drops it there.
GO_TEST_FILTER = grep -Ev '(^jorbiquevpn/cmd/jorbiquevpn$$|^jorbiquevpn/internal/tray$$|^jorbiquevpn/cmd/jorbiquevpn-macos$$|^jorbiquevpn/internal/macos/cli$$)'
GO_LINT_DIR_FILTER = grep -Ev '(cmd/jorbiquevpn$$|internal/tray$$|cmd/jorbiquevpn-macos$$|/internal/macos/)'
GO_LINT_WINDOWS_PKG_FILTER = grep -Ev '(^jorbiquevpn/cmd/jorbiquevpn-macos$$|/internal/macos)'
GO_LINT_WINDOWS_DIR_FILTER = grep -Ev '(cmd/jorbiquevpn-macos$$|/internal/macos)'

# The one macOS package test-unit cannot build off darwin; only a Mac runs its tests.
MACOS_TEST_PKGS := ./internal/macos/cli/...
MACOS_LINT_DIRS := internal/macos cmd/jorbiquevpn-macos

.PHONY: all ci setup setup-test-tools setup-lint-tools lint lint-windows vet format deadcode test \
	test-unit test-macos test-integration coverage-html build-windows package-macos build-smoke-windows \
	build-smoke-macos frontend-build frontend-stub tidy clean

all: ci

ci: lint lint-windows test build-smoke-windows build-smoke-macos

# frontend/embed.go requires a non-empty dist/; CI frontend:build may be absent for Go-only
# jobs. It writes a placeholder rather than copying the sources: the UI is built by Vite now,
# so there is no app.js or styles.css to copy, and the Go jobs never serve this file — they
# only need the embed pattern to match something.
frontend-stub:
	@mkdir -p frontend/dist
	@test -f frontend/dist/index.html || printf '<!doctype html><title>ci-stub</title>\n' > frontend/dist/index.html

setup-test-tools: frontend-stub
	@test -x "$(GOPATH_BIN)/gotestsum" || $(GO) install gotest.tools/gotestsum@$(GOTESTSUM_VERSION)
	@test -x "$(GOPATH_BIN)/gocover-cobertura" || $(GO) install github.com/boumenot/gocover-cobertura@$(GOCOVER_COBERTURA_VERSION)

setup-lint-tools: setup-test-tools
	@test -x "$(GOPATH_BIN)/goimports" || $(GO) install golang.org/x/tools/cmd/goimports@$(GOIMPORTS_VERSION)
	@test -x "$(GOPATH_BIN)/golangci-lint" || $(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

setup: setup-lint-tools
	$(GO) mod download

frontend-build:
	cd frontend && npm ci && npm run build

vet:
	@pkgs="$$($(GO) list ./... | $(GO_TEST_FILTER))"; \
	$(GO_BUILD_ENV) $(GO) vet $$pkgs

lint: setup-lint-tools vet
	@{ $(GO) list -f '{{.Dir}}' ./... | $(GO_LINT_DIR_FILTER); printf '%s\n' $(MACOS_LINT_DIRS); } | xargs gofmt -l > /tmp/jorbiquevpn-gofmt.txt; \
	test ! -s /tmp/jorbiquevpn-gofmt.txt || (cat /tmp/jorbiquevpn-gofmt.txt && exit 1)
	@$(GO) list -f '{{.Dir}}' ./... | $(GO_LINT_DIR_FILTER) | xargs $(GOPATH_BIN)/golangci-lint run --out-format code-climate:gl-code-quality-report.json,colored-line-number

# The pass above runs one GOOS, so it never compiles a _windows.go file, and it skips the two
# packages that only build on Windows. This is the same linters against GOOS=windows: without
# it the Windows half of the client is the half nothing looks at.
lint-windows: setup-lint-tools
	@pkgs="$$($(GO) list ./... | $(GO_LINT_WINDOWS_PKG_FILTER))"; \
	GOOS=windows $(GO) vet $$pkgs
	@$(GO) list -f '{{.Dir}}' ./... | $(GO_LINT_WINDOWS_DIR_FILTER) | \
	GOOS=windows xargs $(GOPATH_BIN)/golangci-lint run --out-format code-climate:gl-code-quality-report-windows.json,colored-line-number

format: setup-lint-tools
	@{ $(GO) list -f '{{.Dir}}' ./... | $(GO_LINT_DIR_FILTER); printf '%s\n' $(MACOS_LINT_DIRS); } | xargs gofmt -w
	@{ $(GO) list -f '{{.Dir}}' ./... | $(GO_LINT_DIR_FILTER); printf '%s\n' $(MACOS_LINT_DIRS); } | xargs $(GOPATH_BIN)/goimports -w

deadcode:
	$(GO) run golang.org/x/tools/cmd/deadcode@latest ./...

test-unit: setup-test-tools
	$(info Running unit tests...)
	@pkgs="$$($(GO) list ./... | $(GO_TEST_FILTER))"; \
	$(GO_BUILD_ENV) $(GOPATH_BIN)/gotestsum --format testname --junitfile junit-report.xml -- -coverprofile=coverage.out $$pkgs
	@$(GO) tool cover -func=coverage.out
	$(GO_BUILD_ENV) $(GOPATH_BIN)/gocover-cobertura < coverage.out > coverage.xml

# cli only compiles on darwin and there is no macOS runner, so no CI job runs this target.
# build-smoke-macos still vets it for darwin.
test-macos: setup-test-tools
	$(GO_BUILD_ENV) $(GOPATH_BIN)/gotestsum --format testname -- $(MACOS_TEST_PKGS)

test: test-unit $(if $(filter darwin,$(shell $(GO) env GOHOSTOS)),test-macos)

# End-to-end integration tests (build-tagged; excluded from default unit/CI coverage).
# setup-test-tools pulls in frontend-stub + gotestsum; junit report shows up in the GitLab Tests tab.
# Set SINGBOX_BIN=/path/to/sing-box to also validate generated configs with the real binary.
test-integration: setup-test-tools
	$(info Running integration tests...)
	$(GOPATH_BIN)/gotestsum --format testname --junitfile junit-integration.xml -- -tags integration -count=1 ./test/integration/...

coverage-html: test-unit
	$(GO) tool cover -html=coverage.out -o coverage.html

build-windows:
	APP_VERSION="$(APP_VERSION)" GO_WINRES_VERSION="$(GO_WINRES_VERSION)" bash ./scripts/build-windows.sh

package-macos:
	APP_VERSION="$(APP_VERSION)" bash ./build-macos.sh package

# Uses frontend/dist from frontend:build artifact in CI; stub fills gaps locally (no npm in golang image).
build-smoke-windows: frontend-stub
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
		$(GO) build -tags desktop \
		-ldflags "-w -s -H windowsgui" \
		-o /tmp/Jorbique-VPN-smoke.exe ./cmd/jorbiquevpn

build-smoke-macos: frontend-stub
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 $(GO) build -o /tmp/JorbiqueVPN-smoke ./cmd/jorbiquevpn-macos
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 $(GO) vet ./internal/macos/... ./cmd/jorbiquevpn-macos/...

tidy:
	$(GO) mod tidy
	$(GO) mod verify

clean:
	rm -f coverage.out coverage.xml coverage.html junit-report.xml junit-integration.xml gl-code-quality-report.json
	$(GO) clean
