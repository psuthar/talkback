# Testing Guide - Test Driven Development (TDD)

This project follows **Test Driven Development (TDD)** principles. All new code should be written using the TDD cycle:

1. **Red**: Write a failing test
2. **Green**: Write minimal code to make the test pass
3. **Refactor**: Improve the code while keeping tests green

## Running Tests

### Prerequisites

Before running tests, ensure:
1. **Postgres is running** (start **postgres** only so port `8080` is not taken by compose `api`):
   ```bash
   docker compose -f deploy/docker-compose.yml up -d postgres
   ```

2. **`DATABASE_URL` is available to tests** (pick one):
   - **Recommended:** copy [`.env.example`](.env.example) to `.env` at the repo root. `go test` helpers load `.env` and `.env.test` automatically from the repo root (see `internal/test.LoadTestEnvFiles`).
   - **Or** export in your shell:
   ```powershell
   $env:DATABASE_URL = "postgres://talkback:talkback@localhost:5432/talkback?sslmode=disable"
   ```
   ```bash
   export DATABASE_URL=postgres://talkback:talkback@localhost:5432/talkback?sslmode=disable
   ```

   **Note:** `TEST_DATABASE_URL` is optional — if not set, tests use `DATABASE_URL`.

### Run all tests

**Option 1: Using PowerShell script (Windows, recommended)**
```powershell
.\scripts\test.ps1
```

**Option 2: Using Bash script (macOS / Linux)**
```bash
chmod +x scripts/test.sh   # once
./scripts/test.sh
```

**Option 3: Manual**
```bash
export DATABASE_URL=postgres://talkback:talkback@localhost:5432/talkback?sslmode=disable
go test ./...
```

### Loom live integration test

`internal/utils` includes an optional test that calls Loom’s real GraphQL API. It runs only when **`TALKBACK_RUN_LOOM_LIVE_TEST=1`** (GitHub Actions sets this) or is skipped otherwise. Use `go test -short ./...` if you want a fast run and to skip other short-mode tests consistently with prior usage.

### Run tests with coverage
```bash
go test -cover ./...
```

### Run tests with verbose output
```bash
go test -v ./...
```

### Run specific package tests
```bash
go test ./internal/database
go test ./internal/handlers
```

### Run specific test
```bash
go test -v ./internal/database -run TestCreateArtifact
```

## Debugging Tests in Cursor/VS Code

### Quick Start

1. **Open a test file** (e.g., `internal/database/artifacts_test.go`)
2. **Set breakpoints** where you want to pause execution
3. **Open Debug panel** (F5 or View → Run and Debug)
4. **Select "Debug Current Test"** from the dropdown
5. **Enter the test name** when prompted (e.g., `TestCreateArtifact`)
6. **Press F5** to start debugging

### Debug Configurations

The project includes several test debugging configurations:

- **"Debug Current Test"** - Prompts for a test name to debug
- **"Debug Tests (Current Package)"** - Debugs all tests in the current file's package
- **"Debug Tests (All)"** - Debugs all tests in the workspace

### Using CodeLens

1. Open a test file
2. Look for **"run test"** or **"debug test"** links above test functions
3. Click **"debug test"** to debug that specific test

### Using Test Explorer

1. Open **Test Explorer** (View → Testing or `Ctrl+Shift+T`)
2. Right-click on a test and select **"Debug Test"**

### Environment Variables

Test debugging configurations automatically set:
- `DATABASE_URL` - Database connection string
- `TEST_DATABASE_URL` - Test database connection (optional, falls back to `DATABASE_URL`)

These are also configured in `.vscode/settings.json` for the Go test runner, so tests run from the terminal or Test Explorer will also have these variables set.

## Test Structure

### Test Helpers

Test helpers are located in `internal/test/testdb.go`:

- `SetupTestDB(t *testing.T)` - Creates a test database connection
- `CleanupTestDB(t *testing.T, db *database.DB)` - Closes database connection
- `TruncateTables(t *testing.T, db *database.DB)` - Cleans up test data

### Database Tests

Database tests are located alongside the code they test:
- `internal/database/artifacts_test.go`
- `internal/database/materials_test.go`
- `internal/database/video_sources_test.go`

### Handler Tests

Handler tests test HTTP endpoints:
- `internal/handlers/handlers_test.go`

## Test Database Setup

Tests use the same database as development. Set `TEST_DATABASE_URL` environment variable to use a separate test database, or tests will use `DATABASE_URL`.

**Important**: 
- Tests automatically run migrations before each test run
- Tests automatically truncate tables between runs to ensure clean state
- Tests require Postgres to be running (use `docker compose -f deploy/docker-compose.yml up -d`)

## Troubleshooting

### Connection Errors

**Error: "TEST_DATABASE_URL or DATABASE_URL must be set"**
- Solution: Create a repo-root `.env` from `.env.example` (includes `DATABASE_URL`), or export:
  ```powershell
  $env:DATABASE_URL = "postgres://talkback:talkback@localhost:5432/talkback?sslmode=disable"
  ```
  ```bash
  export DATABASE_URL=postgres://talkback:talkback@localhost:5432/talkback?sslmode=disable
  ```

**Error: "connection refused" or "connection timeout"**
- Solution: Ensure Postgres is running:
  ```bash
  docker compose -f deploy/docker-compose.yml up -d
  docker compose -f deploy/docker-compose.yml ps
  ```

**Error: "column X does not exist"**
- Solution: Migrations may not have run. Tests automatically run migrations, but if you see this error, manually run:
  ```bash
  go run ./cmd/api
  ```
  This will run migrations on startup.

### Port Already in Use

**Error: "bind: Only one usage of each socket address"**
- Solution: The debug configuration uses port 8081 by default. If you need to change it:
  1. Update `.vscode/launch.json` to use a different port
  2. Or stop the process using port 8080:
     ```powershell
     # Find process
     netstat -ano | findstr :8080
     # Kill process (replace PID with actual process ID)
     taskkill /PID <PID> /F
     ```

## Writing New Tests (TDD Workflow)

### Example: Adding a new feature

1. **Write the test first** (Red phase):
```go
func TestNewFeature(t *testing.T) {
    db := test.SetupTestDB(t)
    defer test.CleanupTestDB(t, db)
    defer test.TruncateTables(t, db)

    t.Run("does something new", func(t *testing.T) {
        // Arrange
        // Act
        result, err := db.NewFeature(context.Background(), input)
        
        // Assert
        require.NoError(t, err)
        assert.Equal(t, expected, result)
    })
}
```

2. **Run the test** - it should fail (Red):
```bash
go test -v ./internal/database -run TestNewFeature
```

3. **Write minimal code** to make test pass (Green):
```go
func (db *DB) NewFeature(ctx context.Context, input string) (string, error) {
    // Minimal implementation
    return "expected", nil
}
```

4. **Refactor** if needed, ensuring tests still pass

## Test Best Practices

### Use Table-Driven Tests

For testing multiple scenarios:

```go
func TestFeatureWithMultipleInputs(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected string
        wantErr  bool
    }{
        {
            name:     "valid input",
            input:    "test",
            expected: "result",
            wantErr:  false,
        },
        {
            name:     "invalid input",
            input:    "",
            expected: "",
            wantErr:  true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test implementation
        })
    }
}
```

### Use testify for Assertions

```go
import (
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

// Use require for critical assertions (stops test on failure)
require.NoError(t, err)
require.NotNil(t, result)

// Use assert for non-critical assertions (continues test on failure)
assert.Equal(t, expected, actual)
assert.Contains(t, message, "expected text")
```

### Test Isolation

- Each test should be independent
- Use `defer test.TruncateTables(t, db)` to clean up
- Don't rely on test execution order

### Test Naming

- Test functions: `TestFunctionName`
- Subtests: `t.Run("describes what is being tested", func(t *testing.T) { ... })`

## Continuous Integration

Tests should be run before committing:

```bash
# Run all tests
go test ./...

# Check test coverage
go test -cover ./...

# Run with race detector
go test -race ./...
```

## Coverage Goals

- Aim for >80% code coverage
- Focus on testing business logic
- Don't test trivial getters/setters

## Mocking (Future)

For external dependencies, consider using:
- `github.com/stretchr/testify/mock` for mocking
- Interface-based design for testability

