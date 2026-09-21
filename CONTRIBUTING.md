<!-- snippet-lint-disable -->

# Contributing to tygor

Thanks for your interest in contributing to tygor!

## Development Setup

### Prerequisites

- Go 1.27.1 or later (that's it!)
- Docker (optional, for local CI testing)

All other tools (Bun, staticcheck, etc.) are managed automatically.

### Initial Setup

1. Clone the repository:
   ```bash
   git clone https://github.com/ahimsalabs/tygor.git
   cd tygor
   ```

2. Bootstrap the project (only requires Go):
   ```bash
   go run github.com/go-task/task/v3/cmd/task@latest setup
   ```

   This installs to `.tools/`:
   - `task` - the task runner (replaces Make)
   - `aqua` - tool version manager
   - `bun` - JavaScript runtime (via aqua)
   - `staticcheck` - Go linter (via aqua)
   - `jq` - JSON processor (via aqua)
   - All npm workspace packages

3. Run tests:
   ```bash
   .tools/task test
   ```

After setup, use `.tools/task` or add `.tools/` to your PATH.

## Development Workflow

### Task Commands

Run `.tools/task --list` to see all available commands:

```bash
.tools/task              # Run tests and lint (default)
.tools/task setup        # Bootstrap project
.tools/task test         # Run Go tests
.tools/task lint         # Run go vet and staticcheck
.tools/task fmt          # Format Go code
.tools/task fmt:check    # Check formatting without modifying
.tools/task gen          # Generate client error types
.tools/task precommit    # Run ALL checks before committing
.tools/task ci:local     # Run GitHub Actions locally via Docker
.tools/task release -- patch|minor|major  # Release packages
```

Or add `.tools/` to your PATH to use `task` directly.

### Before Committing

Always run `.tools/task precommit` before committing. This runs:
1. Format check (`gofmt`)
2. Go tests
3. Linters (`go vet`, `staticcheck`)
4. TypeScript type checks
5. Generated file verification

### Testing CI Locally

To test the GitHub Actions workflow locally before pushing:

```bash
.tools/task ci:local
```

This uses [act](https://github.com/nektos/act) to run the CI workflow in Docker.

## Project Structure

```
tygor/
├── .github/workflows/   # CI workflow
├── packages/
│   ├── client/          # @tygor/client npm package
│   └── vite-plugin/     # @tygor/vite-plugin npm package
├── examples/            # Example applications
├── middleware/          # Built-in middleware (CORS, logging)
├── tygorgen/            # Code generator
├── Taskfile.yml         # Task definitions (replaces Makefile)
├── aqua.yaml            # Tool versions (bun, staticcheck, etc.)
├── go.work              # Go workspace
└── *.go                 # Core framework files
```

### Tool Management

This repo uses [Task](https://taskfile.dev/) + [aqua](https://aquaproj.github.io/) for reproducible tooling:

- `Taskfile.yml` - defines all tasks ([Task docs](https://taskfile.dev/usage/))
- `aqua.yaml` - pins tool versions ([aqua docs](https://aquaproj.github.io/docs/tutorial/))
- `.tools/` - local tool installation (gitignored)

To update tool versions, edit `aqua.yaml` and run `.tools/task setup`.

To find packages for aqua: [aqua registry search](https://aquaproj.github.io/aqua-registry/)

### Multi-Module Repository

This repo uses a Go workspace (`go.work`) to manage multiple Go modules:

- **`/`** - Main tygor module
- **`/examples`** - Shared examples module
- **`/examples/react`** - Standalone React example

```bash
# Main module tests (uses GOWORK=off)
.tools/task test

# Build examples
cd examples && go build ./...
```

## Monorepo Setup

This repo uses bun workspaces to manage TypeScript packages:

- **`/packages/client`** - The `@tygor/client` package
- **`/packages/vite-plugin`** - The `@tygor/vite-plugin` package

During development, bun creates symlinks so packages use local code.

## Making Changes

### Go Code

1. Make your changes
2. Run tests: `.tools/task test`
3. Run linters: `.tools/task lint`
4. Format: `.tools/task fmt`

### TypeScript Client

1. Edit `packages/client/runtime.ts`
2. Run tests: `cd packages/client && bun test`
3. Build: `cd packages/client && bun run build`

### Code Generator

The generator lives in `tygorgen/`. Test by running examples and regenerating types.

## Testing

### Go Tests

```bash
.tools/task test       # All tests
go test -cover ./...   # With coverage
go test ./middleware   # Specific package
```

### TypeScript Tests

```bash
cd packages/client
bun test          # Run tests
bun test --watch  # Watch mode
```

## Publishing Packages

**Note**: Only maintainers can publish.

```bash
.tools/task release -- patch   # or minor, major
```

This handles version bumping, publishing, and tagging.

## Commit Messages

We use [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <description>
```

**Types:** `feat`, `fix`, `docs`, `refactor`, `test`, `chore`

**Scopes:** `client`, `vite-plugin`, `tygorgen`, `middleware`

**Examples:**
```
feat(client): add retry support for failed requests
fix(vite-plugin): pin tygor CLI to package version
docs: update installation instructions
```

## Pull Request Process

1. Create a feature branch from `main`
2. Make changes with clear commits
3. Run `.tools/task precommit`
4. Submit PR with clear description

CI runs `.tools/task precommit` automatically.

## Code Style

### Go
- Run `.tools/task fmt` to format code
- Keep handlers simple and focused
- Document exported types and functions

### TypeScript
- Use TypeScript strict mode
- Prefer functional style
- Keep the runtime small

## Questions?

Open an issue or discussion on GitHub!
