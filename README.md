# TalkBack

## Local Development

### Prerequisites

- Go 1.21 or later
- Docker and Docker Compose
- Windows 11 (or compatible environment)

### Setup

1. **Start Postgres database:**

   ```bash
   docker compose -f deploy/docker-compose.yml up -d
   ```

   This will start a Postgres 16 container with:
   - User: `talkback`
   - Password: `talkback`
   - Database: `talkback`
   - Port: `5432`
   - Persistent data volume

2. **Configure environment variables:**

   Copy the example environment file:
   ```bash
   copy .env.example .env
   ```

   The `.env` file should contain:
   ```
   DATABASE_URL=postgres://talkback:talkback@localhost:5432/talkback?sslmode=disable
   ```

   The `.env` file is automatically loaded by the application using `godotenv`. If the file is missing, the application will log a warning and continue using environment variables.

3. **Run migrations:**

   Migrations run automatically on server startup when `RUN_MIGRATIONS=true` (default). To skip migrations, set `RUN_MIGRATIONS=false`.

   Migrations are located in `db/migrations/` and are embedded in the binary for reliable execution.

4. **Run the API server:**

   ```bash
   go run ./cmd/api
   ```

   The server will:
   - Automatically load `.env` file (if present)
   - Run database migrations on startup (if `RUN_MIGRATIONS=true`)
   - Start on port `8080` by default (or the port specified in the `PORT` environment variable)

### Testing the API

Use the `requests.http` file in the repository root to test endpoints. This file contains example requests for all Phase 1 endpoints.

**Sample curl commands:**

```bash
# Create an artifact
curl -X POST http://localhost:8080/artifacts \
  -H "Content-Type: application/json" \
  -d '{"title": "My Artifact", "description": "Test artifact"}'

# Get artifact (replace {id} with actual artifact ID)
curl http://localhost:8080/artifacts/{id}

# Upload a material file
curl -X POST http://localhost:8080/artifacts/{id}/materials \
  -F "file=@test.txt" \
  -F "kind=document"

# Attach video URL
curl -X POST http://localhost:8080/artifacts/{id}/video \
  -H "Content-Type: application/json" \
  -d '{"provider": "loom", "video_url": "https://www.loom.com/share/example"}'

# Upload transcript (replace {video_id} with actual video source ID)
curl -X POST http://localhost:8080/artifacts/{id}/video/{video_id}/transcript \
  -H "Content-Type: application/json" \
  -d '{"transcript_text": "Full transcript text here..."}'
```

### API Endpoints

#### Health & Status
- `GET /health` - Returns `200 OK` with a status message
- `GET /db/ping` - Tests the database connection and returns success/failure JSON

#### Artifacts
- `POST /artifacts` - Create a new artifact
  ```json
  {
    "title": "My Artifact",
    "description": "Optional description"
  }
  ```
  Returns: `201 Created` with artifact JSON including `id`, `title`, `description`, `status`

- `GET /artifacts/{id}` - Get artifact details
  Returns: `200 OK` with artifact, materials array, and video_sources array

#### Materials
- `POST /artifacts/{id}/materials` - Upload a material file (multipart/form-data)
  - Form field: `file` (required) - the file to upload
  - Form field: `kind` (optional, default: "document") - one of: document, slides, diagram, other
  - Files are stored in `./data/uploads/{artifact_id}/`
  - Text extraction:
    - `text/plain` files: automatically extracted, `text_status=ready`
    - PDF files: extraction not yet implemented, `text_status=failed` (logged)
  - Returns: `201 Created` with material record JSON

#### Video Sources
- `POST /artifacts/{id}/video` - Attach a video URL
  ```json
  {
    "provider": "loom|zoom|other",
    "video_url": "https://..."
  }
  ```
  Returns: `201 Created` with video_source record JSON

- `POST /artifacts/{id}/video/{video_id}/transcript` - Upload transcript text
  ```json
  {
    "transcript_text": "Full transcript text..."
  }
  ```
  Sets `transcript_status=ready` and stores the transcript text.
  Returns: `200 OK` with updated video_source JSON

### Database Schema

**artifacts:**
- `id` UUID (primary key)
- `title` TEXT NOT NULL
- `description` TEXT
- `status` TEXT NOT NULL DEFAULT 'draft' (values: draft, ready)
- `created_at` TIMESTAMPTZ NOT NULL DEFAULT now()
- `updated_at` TIMESTAMPTZ NOT NULL DEFAULT now() (auto-updated via trigger)

**materials:**
- `id` UUID (primary key)
- `artifact_id` UUID NOT NULL (foreign key → artifacts)
- `kind` TEXT NOT NULL (document, slides, diagram, other)
- `filename` TEXT NOT NULL
- `content_type` TEXT NOT NULL
- `storage_url` TEXT NOT NULL
- `text_status` TEXT NOT NULL DEFAULT 'pending' (pending, ready, failed)
- `extracted_text` TEXT
- `created_at` TIMESTAMPTZ NOT NULL DEFAULT now()

**video_sources:**
- `id` UUID (primary key)
- `artifact_id` UUID NOT NULL (foreign key → artifacts)
- `provider` TEXT NOT NULL (loom, zoom, other)
- `video_url` TEXT NOT NULL
- `transcript_status` TEXT NOT NULL DEFAULT 'missing' (missing, pending, ready, failed)
- `transcript_text` TEXT
- `created_at` TIMESTAMPTZ NOT NULL DEFAULT now()

### Stopping Services

To stop the Postgres container:

```bash
docker compose -f deploy/docker-compose.yml down
```

To stop and remove the data volume:

```bash
docker compose -f deploy/docker-compose.yml down -v
```

### Testing

This project follows **Test Driven Development (TDD)** principles. See [TESTING.md](TESTING.md) for detailed testing guidelines.

**Quick Start:**

```powershell
# Ensure Postgres is running
docker compose -f deploy/docker-compose.yml up -d

# Set DATABASE_URL
$env:DATABASE_URL = "postgres://talkback:talkback@localhost:5432/talkback?sslmode=disable"

# Run all tests
go test ./...

# Or use the test script
.\scripts\test.ps1
```

**Troubleshooting:**
- **Connection errors**: Ensure Postgres is running and `DATABASE_URL` is set
- **Port 8080 in use**: Use `.\scripts\kill-port.ps1` or change port in launch.json to 8081

**Test Structure:**
- Database tests: `internal/database/*_test.go`
- Handler tests: `internal/handlers/*_test.go`
- Test helpers: `internal/test/testdb.go`

**TDD Workflow:**
1. **Red**: Write a failing test first
2. **Green**: Write minimal code to make test pass
3. **Refactor**: Improve code while keeping tests green

All new code should follow this TDD cycle. See `TESTING.md` for examples and best practices.

### Debugging

The project is configured for debugging with Delve (dlv) in VS Code/Cursor.

**Prerequisites:**
- Delve debugger is installed (automatically installed with Go extension)
- `.env` file is configured (see Setup step 2)
- Postgres is running (see Setup step 1)

#### Debugging the API Server

1. **Using VS Code/Cursor Debug Panel:**
   - Open the Debug panel (F5 or View → Run and Debug)
   - Select "Debug TalkBack API" from the dropdown
   - Set breakpoints by clicking in the gutter next to line numbers
   - Press F5 or click the green play button to start debugging

2. **Debug Configurations:**
   - **"Debug TalkBack API"** - Uses environment variables from system
   - **"Debug TalkBack API (with .env file)"** - Automatically loads `.env` file before debugging
   - **"Attach to Process"** - Attach to a running Go process (enter process ID when prompted)

#### Debugging Tests

**Method 1: Using Debug Configurations (Recommended)**

1. **Open the test file** you want to debug (e.g., `internal/database/artifacts_test.go`)
2. **Set breakpoints** in your test code or the code being tested
3. **Open Debug panel** (F5 or View → Run and Debug)
4. **Select a test debug configuration:**
   - **"Debug Current Test"** - Prompts for test name (e.g., `TestCreateArtifact`)
   - **"Debug Tests (Current Package)"** - Debugs all tests in the current file's package
   - **"Debug Tests (All)"** - Debugs all tests in the workspace
5. **Press F5** to start debugging

**Method 2: Using CodeLens (Test Functions)**

1. **Open a test file** (e.g., `internal/database/artifacts_test.go`)
2. **Look for "run test" or "debug test" links** above test functions (CodeLens)
3. **Click "debug test"** to debug that specific test function

**Method 3: Using Test Explorer**

1. **Open Test Explorer** (View → Testing or `Ctrl+Shift+T`)
2. **Right-click on a test** and select "Debug Test"

**Environment Variables for Tests:**

The debug configurations automatically set:
- `DATABASE_URL` - Main database connection
- `TEST_DATABASE_URL` - Test database connection (falls back to `DATABASE_URL` if not set)

These are also configured in `.vscode/settings.json` for the Go test runner, so tests run from the terminal or Test Explorer will also have these variables set.

**Debugging Tips:**
- Set breakpoints in test code or production code
- Use the Debug Console to evaluate expressions
- Check Variables panel to inspect test state
- Tests run sequentially (`-p 1`) to avoid database deadlocks
- Step over (F10), step into (F11), step out (Shift+F11)
