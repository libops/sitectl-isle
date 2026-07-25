# AGENTS.md

Guidance for AI agents (Claude Code, etc.) working in this repository.

## Project Overview

sitectl-isle is a [sitectl](https://github.com/libops/sitectl) plugin for Islandora (ISLE) utilities and migration tools. It adds ISLE-specific commands to sitectl via the plugin SDK, including component management, cache warming, site creation, migration from legacy configs, and validation.

Docs: [https://sitectl.libops.io](https://sitectl.libops.io)

## Build, Test, and Lint Commands

```bash
# Install dependencies and build
make build

# Run linter (includes gofmt and golangci-lint)
make lint

# Run all tests with race detection
make test

# Run a specific test
go test -v -race ./pkg/... -run TestSpecificTestName

# Run tests for a single package
go test -v -race ./cmd

# Use Go workspaces to develop against local sitectl repo
make work
```

## Architecture

### Plugin Structure

This binary is a sitectl plugin. It is invoked by sitectl when discovered in `$PATH` as `sitectl-isle`. The entry point is `main.go`, which calls `cmd.RegisterCommands(sdk)` to register direct commands and RPC runners with the plugin SDK.

Under the current RPC model, host-driven component/status/validate behavior is exposed through SDK runners and component command extensions. Do not reintroduce the legacy `RegisterContextValidator` path; `cmd/dependencies_test.go` asserts that the plugin has no registered context validators.

### Key Commands (`cmd/`)

- **Direct Cobra commands**: `cache.go`, `migrate.go`, and `sync.go`
- **RPC create runner**: `create.go`
- **RPC validate runner**: `validate.go`
- **RPC component extensions**: `extensions.go`, `status.go`, `component_review.go`, and `component_set.go`
- **Component definitions**: `component.go`

### Key Packages (`pkg/`)

**`pkg/components`**: ISLE component definitions and logic
**`pkg/create`**: Site creation scaffolding
**`pkg/externalcantaloupe`**: External Cantaloupe IIIF server integration
**`pkg/jobs`**: Background job definitions registered with the plugin SDK
**`pkg/traefikconfig`**: Traefik reverse proxy configuration helpers

### Plugin SDK Usage

Always use the sitectl SDK functions rather than re-implementing:

- **`docker.ExecCapture()`**: Capture stdout from a container exec — do not write your own wrapper
- **`job.ConfirmDatabaseReplacement()`**: Prompt before destructive DB imports — do not copy locally
- **`job.ResolveRecentArtifact()`**: Resolve or produce a dated artifact (today/yesterday reuse)
- **`job.StageArtifactBetweenContexts()`**: Download from source, upload to target
- **`job.DownloadContextFile()`** / **`job.EnsurePathAbsentOnContext()`** / **`job.EnsureDirOnContext()`**: File transfer helpers
- **`plugin.debugui`**: Use `debugui.RenderPanel`, `debugui.FormatRows` — do not copy locally
- **`helpers.FirstNonEmpty()`**: Returns first non-empty string from a variadic list
- **`helpers.GetContextFromArgs()`**: Extracts `--context` from `DisableFlagParsing` commands

## Go Coding Conventions

### Core Principles

- **Simplicity First:** Favor simple, readable code over clever solutions
- **Idiomatic Go:** Follow standard Go conventions and community practices
- **Standard Library:** Prefer the Go standard library over third-party dependencies

### Code Style

- Follow all conventions outlined in [Effective Go](https://go.dev/doc/effective_go)
- Use `gofmt` to format all code before committing
- Keep functions small and focused on a single responsibility
- Create utility functions for any behavior that repeats more than twice
- Name variables clearly; avoid abbreviations unless universally understood (e.g., `i` for index)

### Naming Conventions

- **Packages:** Short, concise, lowercase, single-word names
- **Interfaces:** Use `-er` suffix for single-method interfaces (e.g., `Reader`, `Writer`)
- **Getters:** Omit `Get` prefix (use `Name()`, not `GetName()`)
- **Acronyms:** Keep consistent case (e.g., `userID`, `HTTPServer`, not `userId`, `HttpServer`)

### Dependency Management

- Default to the standard library; only introduce external dependencies when necessary
- Prefer `net/http` for routing (Go 1.22+ has built-in advanced routing)
- Document why any external dependency is required

### Error Handling

- Always check and handle errors explicitly
- Use `RunE` (not `Run`) for Cobra commands so errors propagate correctly
- Return errors rather than calling `os.Exit` or `log.Fatal` outside of `main`/`Execute`
- Wrap errors with context: `fmt.Errorf("context: %w", err)`
- Don't ignore errors with `_` without a clear reason
- Output to `cmd.OutOrStdout()` / `cmd.ErrOrStderr()`, not `fmt.Println` / `os.Stdout`

```go
// Good
if err := doSomething(); err != nil {
    return fmt.Errorf("failed to do something: %w", err)
}
```

### Plugin/SDK Reuse

- Before writing a helper, check `pkg/docker`, `pkg/job`, `pkg/helpers`, and `pkg/plugin/debugui` in sitectl
- Do not copy debug panel styles or render functions locally — use `debugui`
- Do not copy `ExecCapture` — use `docker.ExecCapture`
- Do not copy `ConfirmDatabaseReplacement` — use `job.ConfirmDatabaseReplacement`
- Follow the Go idiom: a little copying is better than a little abstraction, but we already have the dependency

### Go Workspaces

- **Never use `replace` directives** in `go.mod` for local development
- Use `make work` (runs `scripts/use-go-work.sh`) to create a `go.work` file instead

### Concurrency

- Prefer channels for communication between goroutines
- Avoid shared mutable state; use `sync.Mutex` when necessary
- Always run tests with `-race`: `go test -race ./...`
- Use `context.Context` for cancellation and timeout control

### Logging

- Use `log/slog` for all structured logging
- Log levels: Debug (diagnostics), Info (general), Warn (potentially harmful), Error (failures)
- Never log sensitive data (passwords, tokens, secrets)

```go
slog.Info("user authenticated", "user_id", userID, "ip_address", ipAddr)
```

### Command UX and Documentation Contract

- Treat Cobra `Use`, `Short`, `Long`, examples, and flag usage as the canonical command reference. Explain application outcome, affected Docker/runtime resources, prerequisites, side effects, and risk as needed.
- Keep Islandora- or Drupal-specific workflows in their application plugins; use core sitectl for shared Compose lifecycle and service operations.
- Interactive create choices must explain their implications and use defaults in this order: explicit flag, stored context, detected value, product default. Only `--yolo` bypasses decision review.
- Generated references belong on the matching `sitectl-docs/plugins/isle` page. Never edit `sitectl-docs/snippets/commands` by hand.
- When changing command architecture, help, or docs generation, use the `maintain-sitectl-cli-docs` skill when installed.

### Testing

- Write unit tests for all new features and bug fixes
- Use table-driven tests for multiple scenarios
- Use `t.Helper()` in test helper functions
- Run with race detection: `go test -race ./...`

```go
func TestCalculate(t *testing.T) {
    tests := []struct {
        name    string
        input   int
        want    int
        wantErr bool
    }{
        {"positive number", 5, 25, false},
        {"zero", 0, 0, false},
        {"negative number", -5, 0, true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := Calculate(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if got != tt.want {
                t.Errorf("got %v, want %v", got, tt.want)
            }
        })
    }
}
```

### Documentation

- Every exported function, type, method, and package must have a comment
- Start comments with the name of the element being documented
- Comment the **why**, not the **what** for internal logic

```go
// UserService handles all user-related operations.
type UserService struct{}

// GetUser retrieves a user by their ID.
func (s *UserService) GetUser(id string) (*User, error) {}
```

### Linting

- Use `golangci-lint` for all linting checks
- Fix all linting issues before committing
- Run `make lint` before pushing

## Development Notes

- The plugin binary must be named `sitectl-isle` and placed in `$PATH` for sitectl to discover it
- Uses the plugin SDK's `RegisterCommands` pattern — direct commands and RPC runners are registered in `cmd/root.go`
- Do not add `isleContextValidator` back as a registered context validator; host validation dispatches through the validate runner
- The `migrate` command uses `DisableFlagParsing: false` — output goes through `cmd.OutOrStdout()`
- The `--yolo` flag bypasses `ConfirmDatabaseReplacement` prompts for scripted use
- Logging via `slog` with level controlled by `LOG_LEVEL` env var or `--log-level` flag
- `slices.Contains` (stdlib) preferred over hand-rolled contains helpers
