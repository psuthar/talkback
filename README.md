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
- Create sessions and add content (Zoom video, upload documents, transcripts)
- Optionally create extra artifacts to group materials
- Ask questions and view answers with citations
- View question history
- Ask by voice (mic): after transcription, you can **Clean up** (remove fillers/repetition) or **Polish with AI** before submitting

**Data model note:** The main content in a session is Zoom video, uploaded documents, and transcripts. These are all first-class content. "Artifacts" are an optional grouping container; each session gets a default one, and you can create more only if you want to organize materials into separate groups.

See `web/README.md` for detailed instructions.

### Local Auth Check (Mission 1)

To verify TalkBack auth (users, login sessions, signup/login/logout, bootstrap admin) locally, run:

```bash
make auth-check
```

Or directly: `bash scripts/auth_check.sh` (on Windows, use Git Bash or WSL).

**Requirements:** API server running (e.g. `go run ./cmd/api` on port 8081), Postgres with migrations applied. For full checks (DB tables, disabled-user test), set `DATABASE_URL` (e.g. from `.env`) or `TB_PSQL_DSN`. Optional env: `TB_BASE_URL` (default `http://localhost:8081`), `TB_COOKIE_NAME` (default `tb_login`), `TB_TEST_EMAIL` / `TB_TEST_PASSWORD`, `TB_ADMIN_EMAIL` / `TB_ADMIN_PASSWORD`. For Step 7 (bootstrap admin), start the server with `TALKBACK_BOOTSTRAP_ADMIN_EMAIL=<admin-email>`.

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

- `POST /api/sessions/{session_id}/questions/polish` - Polish spoken question text (voice flow)
  Request: `{"text": "um so what was uh the point"}`. Optional query: `?mode=llm` for AI rewrite.
  Returns: `200 OK` with `{"polished_text": "so what was the point"}`. Use before submitting a voice question so fillers and repetition are removed.

**Session vs Artifact Q&A:**
- Artifact-level questions (`POST /artifacts/{id}/questions`) are global to the artifact
- Session-level questions (`POST /sessions/{id}/questions`) are scoped to a specific session
- Both use the same RAG pipeline and retrieval logic
- Session questions include `session_id` for filtering and context

### Deploying on Render

To run the API on [Render.com](https://render.com) as a Web Service.

**Local vs Render — env contract**

| | Local dev | Render (production) |
|---|-----------|----------------------|
| **ENV** | Not set (or `dev`) | `production` |
| **.env** | Loaded if present; warning if missing | Not loaded, no dotenv warnings |
| **PORT** | Defaults to `8080` if unset | Set by Render |
| **CORS_ALLOWED_ORIGINS** | Defaults to `*` if unset | Set to your frontend URL |
| **DATABASE_URL** | From `.env` or env | Set in Render dashboard |
| **Zoom redirect** | N/A or from `BASE_URL` | `ZOOM_REDIRECT_URL` = `https://<api-host>/auth/zoom/callback` (must match app callback route) |

**Test URLs:** `GET /health`, `GET /healthz`, `GET /db/ping` — all return JSON; use `/health` for Render health check.

---

1. **Create a PostgreSQL** instance on Render and note the internal `DATABASE_URL` (use "Internal" URL; Render adds `?sslmode=require`).

2. **Create a Web Service** connected to your repo. Set:
   - **Build command:** `go build -o app ./cmd/api`
   - **Start command:** `./app`
   - **Health check path:** `/health`

3. **Environment variables** (set in Render dashboard):

   | Variable | Required | Notes |
   |----------|----------|--------|
   | `DATABASE_URL` | Yes | From Render Postgres (auto if linked) |
   | `ENV` | Yes | Set to `production` (skips .env load, no noisy logs) |
   | `RUN_MIGRATIONS` | Yes | Set to `true` |
   | `CORS_ALLOWED_ORIGINS` | Yes | Your frontend origin, e.g. `https://your-frontend.onrender.com` (default `*` if unset) |
   | `OPENAI_API_KEY` | Yes | For RAG and Q&A |
   | `BASE_URL` | Yes | Full API URL, e.g. `https://your-api.onrender.com` (used for post-OAuth redirect) |
   | `ZOOM_REDIRECT_URL` | If using Zoom | **Absolute** callback URL, e.g. `https://your-api.onrender.com/auth/zoom/callback` (required in production; local fallback is `http://localhost:8081/auth/zoom/callback`) |
   | `ZOOM_CLIENT_ID` | If using Zoom | From Zoom Marketplace app |
   | `ZOOM_CLIENT_SECRET` | If using Zoom | From Zoom Marketplace app |
   | `APP_REDIRECT_URL` | If using Zoom | Frontend URL for post-OAuth redirect, e.g. `https://your-frontend.onrender.com` |
   | `ENCRYPTION_KEY` | If using Zoom | 32-byte key for token encryption (production value) |
   | `TRANSCRIPT_WORKERS` | No | Default 2 |
   | `ALLOW_DEV_RESET` | No | Set to `false` in production |

4. **Zoom OAuth:** Set `ZOOM_REDIRECT_URL` to your API’s absolute callback URL (e.g. `https://talkback-895n.onrender.com/auth/zoom/callback`). In Zoom Marketplace app, set the OAuth Redirect URL to the same value. Relative URLs are rejected by Zoom (invalid redirect 4,700).

5. **Health and test endpoints:**
   - `GET /health` or `GET /healthz` — returns `200` with `{"status":"ok"}`
   - `GET /db/ping` — tests database connectivity

Render sets `PORT` automatically; the app uses it by default.

**Reset database and deploy on Render**

To wipe the database and then deploy (e.g. for staging or a clean slate):

1. **Reset from your machine (recommended)**  
   Use Render’s **external** PostgreSQL URL (Dashboard → Postgres → Connect → “External connection string”). Then run:
   ```bash
   DATABASE_URL="postgres://...?sslmode=require" go run ./cmd/reset-db
   ```
   After the reset, trigger a deploy (or push a commit). On startup the API will run migrations from scratch.

2. **Optional: reset during Render release (use with care)**  
   To reset automatically on each deploy for a **non-production** service you can either use the repo’s **Blueprint** or set commands manually:
   - **Blueprint:** The repo includes a `render.yaml` that builds both binaries and runs `./reset-db` in a pre-deploy step only when `RENDER_RESET_DB_ON_RELEASE` is set. Deploy via Render’s “Blueprint” flow; then set `RENDER_RESET_DB_ON_RELEASE=true` only for that service in the dashboard.
   - **Manual:** Build command: `go build -o app ./cmd/api && go build -o reset-db ./cmd/reset-db`. Release / pre-deploy command: `if [ -n "$RENDER_RESET_DB_ON_RELEASE" ]; then ./reset-db; fi`
   Leave `RENDER_RESET_DB_ON_RELEASE` unset (or delete it) for production so the DB is never wiped on deploy.

**Frontend (Static Site on Render)**

To deploy the React SPA as a Render **Static Site**:

1. Connect the repo and set **Root Directory** to `web` (or build from `web`).
2. **Build command:** `npm ci && npm run build`
3. **Publish directory:** `dist`
4. **Environment variable** (required so the app talks to your API, not localhost):
   - `VITE_API_BASE_URL=https://<your-api-service>.onrender.com`  
   Example: if your API is `https://talkback-api.onrender.com`, set `VITE_API_BASE_URL=https://talkback-api.onrender.com` (no trailing slash).
5. **Rebuild:** Changing `VITE_API_BASE_URL` (or any `VITE_*` var) requires a new build; Render will redeploy when env vars change.

Local dev uses `http://localhost:8081` by default when `VITE_API_BASE_URL` is not set (see `web/.env.example`).

**Render checklist (recent changes)**  
- **Backend:** Migrations run on startup from embedded files (`internal/migrations/migrations/`). New migrations (e.g. 000021) run automatically when `RUN_MIGRATIONS=true`. No new env vars required.  
- **Frontend:** Ensure `VITE_API_BASE_URL` is set to your API URL so the app and docx viewer (mammoth) fetch from the API. `mammoth` is in `dependencies` and is installed by `npm ci`; commit `package-lock.json` so Render installs it.  
- **CORS:** API must allow your frontend origin (`CORS_ALLOWED_ORIGINS`) so the browser can fetch material files (e.g. for Word document formatted view).

**TalkBack Auth (users + login sessions)**  
- Native user accounts and cookie-based sessions. Endpoints: `POST /api/auth/signup`, `POST /api/auth/login`, `POST /api/auth/logout`, `GET /api/me` (requires auth).
- Cookie name and TTL: `TB_SESSION_COOKIE_NAME` (default `tb_login`), `TB_SESSION_TTL_HOURS` (default 168). For cross-origin cookies set `TB_COOKIE_SECURE=true` and optionally `TB_COOKIE_DOMAIN`.
- First-user admin bootstrap: set `TALKBACK_BOOTSTRAP_ADMIN_EMAIL`; the first signup or login with that email gets `global_role=admin`.
- Origin check: for mutating auth routes (signup/login/logout), if the request sends an `Origin` header it must be in `TB_ALLOWED_ORIGINS` (comma-separated). Omit to allow any origin (e.g. local dev).
- Frontend: call `GET /api/me` with `credentials: 'include'`; the app shows "Logged in as …" when the cookie is valid. For cross-site (e.g. Render frontend + API), set `CORS_ALLOWED_ORIGINS` to the frontend URL and use `corsWithCredentials` (already wired for `/api/auth/*` and `/api/me`).

### Environment Variables

**Speech-to-text (UI mic — ask/answer with voice):**
- By default the app uses **OpenAI Whisper API** when `OPENAI_API_KEY` is set (faster, no local CLI). Fallback: **Whisper CLI** (openai-whisper) when API is unavailable or `STT_PREFER_CLI=true`.
- `STT_MODE` — `api` | `cli` | `hybrid` (default `hybrid`: try API then CLI).
- `STT_PREFER_CLI` — set to `true` to force CLI even when API key is set.
- `STT_MAX_AUDIO_SECONDS` — max duration for mic uploads (default 30); over-long returns 413.
- `STT_TIMEOUT_MS` — timeout for mic transcription (default 15000).
- `STT_API_MODEL` — Whisper API model for mic (default `whisper-1`).
- `STT_CLI_MODEL` — Whisper CLI model for mic (default `tiny` for speed).
- `STT_DAILY_MAX_SECONDS` — optional daily cap; when exceeded, returns 429.
- `STT_COST_PER_MIN_USD` — for cost logging (default 0.006).
- `WHISPER_CLI` — path to `whisper` CLI when using CLI; `FFMPEG_BIN_DIR` if ffmpeg not on PATH.

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
   - **"Debug TalkBack API"** - Uses environment variables from launch config (sets `PORT=8081`, `BIND_ADDRESS=127.0.0.1` so you’ll see *Server starting on 127.0.0.1:8081*)
   - **"Debug TalkBack API (with .env file)"** - Same, but loads `.env` first
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
