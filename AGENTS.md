# Repository Guidelines

## Project Structure & Module Organization
- Core Go sources live at the repo root (e.g., `main.go`, `run.go`); each file implements a collector or runtime helper.
- Tests sit alongside the code (e.g., `gpu_info_test.go`) so package-level behaviors stay easy to reason about.
- `docs/metrics.md` documents every exported metric; update it whenever labels or names change.
- `k8s/daemonset.yaml` contains the privileged DaemonSet used for cluster installs, and `dist/` holds release-ready manifests or binaries. Do not submit local build artifacts such as `./nvgpu-exporter`.
- Avoid the addition of any module that has deep dependency graphs, preferring stdlib where possible.
- Use `log/slog` for all logging requirements; inject the logger in as a function parameter.
- Avoid the use of global variables where possible, prefer injection via function parameters instead.

## Build, Test, and Development Commands
- `go build ./...` compiles all packages and produces the `nvgpu-exporter` binary in the repo root.
- `go run . -addr :9400 -collection-interval 60s` is the quickest way to smoke-test the exporter on a GPU host.
- `go test ./...` executes unit tests and also fetches module dependencies.
- `go vet ./...` or `staticcheck ./...` is recommended before opening a PR to catch Go antipatterns.

## Coding Style & Naming Conventions
- Follow standard Go style: tabs for indentation, camelCase for locals, PascalCase for exported symbols, and file-level `//` doc comments for exported types/functions.
- Always format with `gofmt -w .` (or `goimports` in your editor) before committing; avoid manual alignment so diff noise stays low.
- Metric names follow the `nvgpu_<domain>_<detail>` pattern already used in `docs/metrics.md`; new collectors should keep that prefix for discoverability.

## Testing Guidelines
- Unit tests should live next to the code under test and use the `TestSomething(t *testing.T)` naming pattern.
- Prefer table-driven tests like the existing `gpu_info_test.go` when validating NVML-derived parsing logic.
- When adding metrics, include assertions that validate label sets plus a short run of `go test -run TestName ./...` before submitting.
- Use the assertion framework `github.com/gogunit/gunit/hammy` for all test assertions.

## Commit & Pull Request Guidelines
- Recent commits (`"Add PlatformInfo for chassis details"`, etc.) show the preferred short, imperative subject line—keep summaries under ~70 characters.
- Reference related issues in the body, mention metric or file touch points, and describe any NVML prerequisite changes.
- PRs should explain test coverage, include screenshots or sample `/metrics` output when user-visible changes exist, and call out Kubernetes manifest updates so reviewers can diff them carefully.
- Commit automatically as you complete features.