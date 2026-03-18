# TalkBack Web UI - Phase 2

A minimal single-page React application for interacting with the TalkBack API.

## Prerequisites

- **Node.js 18+ and npm** - If you don't have Node.js installed:
  - Download from [https://nodejs.org/](https://nodejs.org/)
  - Choose the LTS (Long Term Support) version
  - The installer includes npm automatically
  - Verify installation: `node --version` and `npm --version`
- The TalkBack API server (default: `http://localhost:8080`)

## Setup

1. **Install dependencies:**

   ```bash
   cd web
   npm install
   ```

2. **Start the development server:**

   ```bash
   npm run dev
   ```

   The app will open at `http://localhost:3000` (or the next available port).

## Usage

1. **Set API Base URL** (optional for local dev)
   - When `VITE_API_BASE_URL` is unset, the UI uses same-origin-relative paths (Vite proxy -> `http://localhost:8080`).
   - In debug mode you can override in the UI; for production builds set `VITE_API_BASE_URL` to your API URL (see root README "Deploying on Render")

2. **Create an Artifact**
   - Enter a title (required) and optional description
   - Click "Create Artifact" to create a new artifact
   - The artifact ID will be displayed and stored for subsequent operations

3. **Upload Material**
   - Select a file (text files will have text extracted automatically)
   - Choose a kind (document, slides, diagram, other)
   - Click "Upload Material"

4. **Attach Video URL**
   - Select a provider (Loom, Zoom, Other)
   - Enter the video URL
   - Click "Attach Video"
   - The video ID will be stored for transcript submission

5. **Submit Transcript**
   - Paste transcript text in the textarea
   - Click "Submit Transcript"
   - This sets the transcript status to "ready"

6. **Ask a Question**
   - Enter your question in the textarea
   - Click "Ask Question"
   - The answer will be displayed with:
     - Answer status (answered, not_covered, or error)
     - Confidence score
     - Answer text
     - Citations (if available)

7. **View Question History**
   - Click "Load Questions" to fetch all questions for the current artifact
   - Questions are displayed with their latest answers and citations

## Features

- **Error Handling**: All errors display the full response body for debugging
- **CORS Support**: The Go API includes CORS middleware to allow cross-origin requests
- **Real-time Feedback**: Success/error messages and loading states
- **Citation Display**: Answers show citations with source type, ID, and snippets
- **Voice Questions (Mic-to-Question)**: Record a short voice question, transcribe it, review/edit, then submit as a normal session question

## Building for Production

```bash
npm run build
```

The built files will be in the `dist/` directory.

## Troubleshooting

**CORS Errors:**
- Ensure the Go API server is running and has CORS middleware enabled (already included)
- Check that the API Base URL is correct

**API Connection Errors:**
- Verify the API server is running: `curl http://localhost:8080/health` (or your API URL)
- Check the API Base URL in the UI (debug panel) matches your server, or set `VITE_API_BASE_URL` in `web/.env`

**Missing Artifact ID:**
- Create an artifact first before uploading materials or asking questions

**OpenAI API Errors:**
- Ensure `OPENAI_API_KEY` is set in your `.env` file for the Go API
- Questions will return `answer_status="error"` if the API key is missing

## Voice Questions – Local Whisper Setup (Backend)

Voice questions use a local Whisper transcription step via the Python `whisper` CLI (no audio is stored long-term).

### Prerequisites

- Python 3.10+
- `ffmpeg` available on PATH
- Python package `openai-whisper`

Example install:

- `pip install -U openai-whisper`
- Install `ffmpeg` (Windows: via Chocolatey/winget or manual install)

### Backend configuration

Set these environment variables for the Go API:

- `WHISPER_CLI` (default: `whisper`)
- `WHISPER_MODEL` (default: `base`)
- `WHISPER_LANGUAGE` (optional)
- `WHISPER_EXTRA_ARGS` (optional)
- `VOICE_MAX_UPLOAD_MB` (default: `25`)

## Reset All Data (Dev Only)

⚠️ **WARNING: This feature deletes ALL data!**

The web UI includes a "Reset All Data" section at the bottom of the page.

**To use:**

1. Enable the reset endpoint in your Go API `.env` file:
   ```
   ALLOW_DEV_RESET=true
   DEV_RESET_DELETE_FILES=false  # Set to true to also delete uploaded files
   ```

2. Restart the Go API server

3. In the web UI:
   - Scroll to the "⚠ Reset All Data (Dev Only)" section
   - Click "Show Reset Confirmation"
   - Type `RESET` exactly to confirm
   - Click "⚠ Reset All Data"

**What gets deleted:**
- All artifacts
- All materials
- All video sources
- All questions
- All answers
- Optionally: All uploaded files in `./data/uploads/` (if `DEV_RESET_DELETE_FILES=true`)

**Security:**
- The endpoint returns `403 Forbidden` if `ALLOW_DEV_RESET` is not `true`
- This is a dev-only feature - never enable in production

## Development

The app uses:
- **Vite** for fast development and building
- **React** for the UI framework
- **No additional dependencies** - minimal setup as requested

To modify the UI, edit `src/App.jsx` and `src/index.css`.
