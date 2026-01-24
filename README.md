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
   OPENAI_API_KEY=your_openai_api_key_here  # Required for Phase 2 Q&A
   RUN_MIGRATIONS=true  # Set to false to skip migrations
   
   # Dev-only: Enable reset endpoint (⚠️ WARNING: Allows deletion of all data)
   ALLOW_DEV_RESET=false  # Set to true to enable /admin/reset endpoint
   DEV_RESET_DELETE_FILES=false  # Set to true to also delete uploaded files on reset
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

### Web UI (Phase 2)

A minimal React SPA is available in the `web/` directory.

**Quick Start:**

```bash
cd web
npm install
npm run dev
```

The web UI will open at `http://localhost:3000` and allows you to:
- Create artifacts
- Upload materials
- Attach video URLs
- Submit transcripts
- Ask questions and view answers with citations
- View question history

See `web/README.md` for detailed instructions.

### Testing the API

Use the `requests.http` file in the repository root to test endpoints. This file contains example requests for all Phase 1 and Phase 2 endpoints.

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

  **Embed Mode (Loom/Zoom):**
  ```json
  {
    "provider": "loom|zoom|other",
    "playback_mode": "embed",
    "embed_url": "https://www.loom.com/share/..."
  }
  ```
  Or use backward-compatible format:
  ```json
  {
    "provider": "loom",
    "video_url": "https://www.loom.com/share/..."
  }
  ```

  **Direct Mode (MP4/WebM):**
  ```json
  {
    "provider": "other",
    "playback_mode": "direct",
    "media_url": "https://example.com/video.mp4",
    "poster_url": "https://example.com/poster.jpg",
    "duration_seconds": 1234
  }
  ```

  **Playback Modes:**
  - `embed`: Uses iframe for Loom/Zoom embeds (limited control, convenience)
  - `direct`: Uses HTML5 `<video>` for MP4/WebM (full control, play/pause/seek events)

  Returns: `201 Created` with video_source record JSON

- `POST /artifacts/{id}/video/{video_id}/transcript` - Upload transcript text
  ```json
  {
    "transcript_text": "Full transcript text..."
  }
  ```
  Sets `transcript_status=ready` and stores the transcript text.
  Returns: `200 OK` with updated video_source JSON

#### Phase 2: Q&A (RAG)
- `POST /artifacts/{id}/questions` - Ask a question about the artifact
  ```json
  {
    "question_text": "What is the main topic discussed?",
    "video_time_seconds": 120
  }
  ```
  Optional `video_time_seconds` field stores the playback timestamp when the question was asked (useful for direct playback mode).
  Returns: `201 Created` with question and answer JSON
  - Uses RAG (Retrieval-Augmented Generation) over `materials.extracted_text` and `video_sources.transcript_text`
  - Returns `answer_status`: "answered", "not_covered", or "error"
  - Includes citations with `chunk_id`, `source_type`, `source_id`, `locator`, and `snippet`
  - If no relevant chunks are found, returns `not_covered` without calling OpenAI

- `GET /artifacts/{id}/questions` - Get recent questions with their latest answers
  Returns: `200 OK` with JSON containing:
  ```json
  {
    "questions": [...],
    "answers": [...]
  }
  ```
  Returns up to 20 most recent questions for the artifact, each with its latest answer.

#### Phase 3: Sessions

Sessions provide a virtual meeting context around an artifact, allowing multiple participants to collaborate, track playback progress, and ask session-scoped questions.

**Key Concepts:**
- **Artifact**: The base content (video + materials + transcript)
- **Session**: A virtual meeting context around an artifact. One artifact can have multiple sessions.
- **Participant**: A user in a session (identified by `participant_ref`, e.g., "anonymous" for now)
- **Events**: Playback and interaction events (play, pause, seek, join, leave, question)

**Session Endpoints:**

- `POST /artifacts/{artifact_id}/sessions` - Create a new session
  ```json
  {
    "title": "Weekly review - Jan 2026",
    "created_by": "Paresh"
  }
  ```
  Returns: `201 Created` with session JSON

- `GET /artifacts/{artifact_id}/sessions` - List all sessions for an artifact
  Returns: `200 OK` with array of sessions

- `GET /sessions/{session_id}` - Get session details with artifact context
  Returns: `200 OK` with session, artifact, materials, video sources, and recent questions

- `POST /sessions/{session_id}/participants` - Join a session (or update heartbeat)
  ```json
  {
    "participant_ref": "anonymous"
  }
  ```
  Returns: `200 OK` with participant record (upserts if already exists)

- `POST /sessions/{session_id}/events` - Record a session event
  ```json
  {
    "participant_ref": "anonymous",
    "event_type": "play|pause|seek|join|leave|question",
    "video_time_seconds": 120,
    "payload": {}
  }
  ```
  Returns: `201 Created` with event JSON

- `POST /sessions/{session_id}/questions` - Ask a question in session context
  ```json
  {
    "question_text": "What was discussed at the 5-minute mark?"
  }
  ```
  Returns: `201 Created` with question and answer (uses same RAG pipeline as artifact-level Q&A)
  - Questions are scoped to the session (stored with `session_id`)
  - Uses the same RAG/LLM pipeline as artifact-level questions

- `GET /sessions/{session_id}/questions` - Get questions for a session
  Returns: `200 OK` with questions and answers array (up to 20 most recent)

**Session vs Artifact Q&A:**
- Artifact-level questions (`POST /artifacts/{id}/questions`) are global to the artifact
- Session-level questions (`POST /sessions/{id}/questions`) are scoped to a specific session
- Both use the same RAG pipeline and retrieval logic
- Session questions include `session_id` for filtering and context

### Environment Variables

**RAG Debug Mode:**
- `RAG_DEBUG=true` - Enables detailed logging of retrieved chunks and scores for debugging RAG retrieval

**Loom Transcript Auto-Fetch:**
- `LOOM_API_KEY=your_loom_api_key_here` - (Optional) If set, attempts to fetch transcripts from Loom API when attaching Loom videos
  - When a Loom video is attached and `LOOM_API_KEY` is set, the system will attempt to fetch the transcript automatically
  - If fetching succeeds, the transcript is saved and `transcript_status` is set to `ready`
  - If fetching fails (API error, transcript not available, etc.), `transcript_status` remains `missing` or `pending`, and the transcript can be added manually later
  - **Note:** The Loom API endpoint structure may need to be adjusted based on Loom's actual API documentation. See `internal/utils/loom_transcript.go` for implementation details.

### Auto-Transcription Limitations

**Loom Video Auto-Transcription:**
- Auto-transcription of Loom videos requires a downloadable media URL
- Many Loom videos (especially private videos) use streaming protocols (HLS/DASH) and don't expose direct downloadable URLs
- For videos that can't be auto-transcribed, you can manually upload transcripts via the API or UI
- If you encounter "unable to resolve downloadable media URL" errors, the video likely requires:
  1. Loom API authentication (set `LOOM_API_KEY` if available)
  2. Manual transcript upload
  3. Or the video may use streaming protocols that aren't directly downloadable

**Workarounds:**
- For videos that fail auto-transcription, use the manual transcript upload feature
- Check if Loom provides an official API for downloading video files or transcripts
- Consider using video download tools (like `yt-dlp`) if you need to extract audio for transcription

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

**sessions (Phase 3):**
- `id` UUID (primary key)
- `title` TEXT NOT NULL
- `created_by` TEXT
- `status` TEXT NOT NULL DEFAULT 'open' (open, closed)
- `created_at` TIMESTAMPTZ NOT NULL DEFAULT now()
- `updated_at` TIMESTAMPTZ NOT NULL DEFAULT now() (auto-updated via trigger)
- Note: **Artifacts belong to sessions** (artifacts.session_id → sessions.id), not the other way around

**session_participants (Phase 3):**
- `id` UUID (primary key)
- `session_id` UUID NOT NULL (foreign key → sessions)
- `participant_ref` TEXT NOT NULL
- `joined_at` TIMESTAMPTZ NOT NULL DEFAULT now()
- `last_seen_at` TIMESTAMPTZ NOT NULL DEFAULT now()
- `watch_progress` REAL NOT NULL DEFAULT 0 (0.0-1.0)
- UNIQUE(session_id, participant_ref)

**session_events (Phase 3):**
- `id` UUID (primary key)
- `session_id` UUID NOT NULL (foreign key → sessions)
- `participant_ref` TEXT
- `event_type` TEXT NOT NULL (join, leave, play, pause, seek, question)
- `video_time_seconds` INT
- `payload` JSONB NOT NULL DEFAULT '{}'::jsonb
- `created_at` TIMESTAMPTZ NOT NULL DEFAULT now()

**questions (updated in Phase 3):**
- `session_id` UUID NOT NULL (foreign key → sessions, required in Phase 3)
- `video_time_seconds` INT NULL (timestamp when question was asked during playback, added in Phase 3)
- All other fields remain the same

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
- **Docker API version mismatch (500 Internal Server Error)**: 
  - Restart Docker Desktop: Right-click system tray icon → "Restart" or "Quit" then start again
  - Or run: `.\scripts\fix-docker-api.ps1`
  - This syncs the Docker CLI and Docker Desktop API versions

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
