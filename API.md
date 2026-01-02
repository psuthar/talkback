# TalkBack API Reference

Base URL: `http://localhost:8080` (or port specified in `PORT` environment variable)

## System Endpoints

### Health Check
**GET** `/health`

Check if the API server is running.

**Response:**
```json
{
  "status": "ok"
}
```

**Status Codes:**
- `200 OK` - Server is healthy

---

### Database Ping
**GET** `/db/ping`

Test the database connection.

**Response:**
```json
{
  "status": "success",
  "message": "Database connection successful"
}
```

**Status Codes:**
- `200 OK` - Database connection successful
- `500 Internal Server Error` - Database connection failed

---

## Artifact Endpoints

### Create Artifact
**POST** `/artifacts`

Create a new artifact.

**Request Body:**
```json
{
  "title": "My Artifact Title",
  "description": "Optional description"  // optional
}
```

**Response:** `201 Created`
```json
{
  "id": "uuid-string",
  "title": "My Artifact Title",
  "description": "Optional description",
  "status": "draft"
}
```

**Status Codes:**
- `201 Created` - Artifact created successfully
- `400 Bad Request` - Invalid request body or missing title
- `500 Internal Server Error` - Failed to create artifact

---

### Get Artifact
**GET** `/artifacts/{id}`

Retrieve an artifact with its associated materials and video sources.

**Path Parameters:**
- `id` (UUID) - The artifact ID

**Response:** `200 OK`
```json
{
  "artifact": {
    "id": "uuid-string",
    "title": "My Artifact Title",
    "description": "Optional description",
    "status": "draft",
    "created_at": "2025-12-30T13:00:00Z",
    "updated_at": "2025-12-30T13:00:00Z"
  },
  "materials": [
    {
      "id": "uuid-string",
      "artifact_id": "uuid-string",
      "kind": "document",
      "filename": "test.txt",
      "content_type": "text/plain",
      "storage_url": "data/uploads/.../test.txt",
      "text_status": "ready",
      "extracted_text": "File content...",
      "created_at": "2025-12-30T13:00:00Z"
    }
  ],
  "video_sources": [
    {
      "id": "uuid-string",
      "artifact_id": "uuid-string",
      "provider": "loom",
      "video_url": "https://www.loom.com/share/...",
      "transcript_status": "ready",
      "transcript_text": "Transcript content...",
      "created_at": "2025-12-30T13:00:00Z",
      "updated_at": "2025-12-30T13:00:00Z"
    }
  ]
}
```

**Status Codes:**
- `200 OK` - Artifact retrieved successfully
- `400 Bad Request` - Invalid artifact ID
- `500 Internal Server Error` - Failed to retrieve artifact

---

### Upload Material
**POST** `/artifacts/{id}/materials`

Upload a material file (document, slides, etc.) for an artifact.

**Path Parameters:**
- `id` (UUID) - The artifact ID

**Request:** `multipart/form-data`
- `file` (required) - The file to upload
- `kind` (optional) - Material type, defaults to `"document"`

**Supported File Types:**
- `text/plain` - Text extraction supported
- `application/pdf` - Text extraction marked as failed (TODO for Phase 1)
- Other types - Stored but text extraction not attempted

**Response:** `201 Created`
```json
{
  "id": "uuid-string",
  "artifact_id": "uuid-string",
  "kind": "document",
  "filename": "test.txt",
  "content_type": "text/plain",
  "storage_url": "data/uploads/{artifact_id}/test.txt",
  "text_status": "ready",
  "extracted_text": "File content...",
  "created_at": "2025-12-30T13:00:00Z"
}
```

**Text Status Values:**
- `pending` - Text extraction not yet attempted
- `ready` - Text successfully extracted
- `failed` - Text extraction failed (e.g., PDF not yet supported)

**Status Codes:**
- `201 Created` - Material uploaded successfully
- `400 Bad Request` - Invalid request or missing file
- `404 Not Found` - Artifact not found
- `500 Internal Server Error` - Failed to upload material

---

### Attach Video URL
**POST** `/artifacts/{id}/video`

Attach a video URL to an artifact.

**Path Parameters:**
- `id` (UUID) - The artifact ID

**Request Body:**
```json
{
  "provider": "loom",  // or "zoom" or "other"
  "video_url": "https://www.loom.com/share/example-video-id"
}
```

**Provider Values:**
- `loom` - Loom video
- `zoom` - Zoom recording
- `other` - Other video provider

**Response:** `201 Created`
```json
{
  "id": "uuid-string",
  "artifact_id": "uuid-string",
  "provider": "loom",
  "video_url": "https://www.loom.com/share/example-video-id",
  "transcript_status": "missing",
  "transcript_text": null,
  "created_at": "2025-12-30T13:00:00Z",
  "updated_at": "2025-12-30T13:00:00Z"
}
```

**Status Codes:**
- `201 Created` - Video URL attached successfully
- `400 Bad Request` - Invalid request body or missing fields
- `404 Not Found` - Artifact not found
- `500 Internal Server Error` - Failed to attach video URL

---

### Upload Transcript
**POST** `/artifacts/{id}/video/{video_id}/transcript`

Upload or paste a transcript for a video source.

**Path Parameters:**
- `id` (UUID) - The artifact ID
- `video_id` (UUID) - The video source ID

**Request Body:**
```json
{
  "transcript_text": "This is the full transcript text. It can be quite long and contain multiple paragraphs."
}
```

**Response:** `200 OK`
```json
{
  "id": "uuid-string",
  "artifact_id": "uuid-string",
  "provider": "loom",
  "video_url": "https://www.loom.com/share/example-video-id",
  "transcript_status": "ready",
  "transcript_text": "This is the full transcript text...",
  "created_at": "2025-12-30T13:00:00Z",
  "updated_at": "2025-12-30T13:00:00Z"
}
```

**Transcript Status Values:**
- `missing` - No transcript available
- `pending` - Transcript processing pending
- `ready` - Transcript available and ready
- `failed` - Transcript processing failed

**Status Codes:**
- `200 OK` - Transcript uploaded successfully
- `400 Bad Request` - Invalid request body, missing transcript_text, or video source doesn't belong to artifact
- `404 Not Found` - Video source not found
- `500 Internal Server Error` - Failed to upload transcript

---

## Example Workflow

1. **Create an artifact:**
   ```bash
   POST /artifacts
   {
     "title": "My Project Documentation",
     "description": "Documentation for my project"
   }
   ```
   → Returns artifact ID

2. **Upload materials:**
   ```bash
   POST /artifacts/{artifact_id}/materials
   Content-Type: multipart/form-data
   file: [your-file.txt]
   kind: document
   ```

3. **Attach video:**
   ```bash
   POST /artifacts/{artifact_id}/video
   {
     "provider": "loom",
     "video_url": "https://www.loom.com/share/..."
   }
   ```
   → Returns video source ID

4. **Upload transcript:**
   ```bash
   POST /artifacts/{artifact_id}/video/{video_id}/transcript
   {
     "transcript_text": "Full transcript text..."
   }
   ```

5. **Get complete artifact:**
   ```bash
   GET /artifacts/{artifact_id}
   ```
   → Returns artifact with all materials and video sources

---

## Error Responses

All error responses follow this format:

```json
{
  "error": "Error message description"
}
```

Or plain text error messages for some endpoints.

**Common Status Codes:**
- `400 Bad Request` - Invalid request format or missing required fields
- `404 Not Found` - Resource not found
- `405 Method Not Allowed` - HTTP method not allowed for this endpoint
- `500 Internal Server Error` - Server error occurred

---

## Phase 2: Q&A Endpoints

### Ask Question
**POST** `/artifacts/{id}/questions`

Ask a question about an artifact. The system will use RAG (Retrieval Augmented Generation) to find relevant content from materials and transcripts, then generate a grounded answer.

**Path Parameters:**
- `id` (UUID) - The artifact ID

**Request Body:**
```json
{
  "question_text": "What is the main topic discussed in this artifact?"
}
```

**Response:** `201 Created`
```json
{
  "question": {
    "id": "uuid-string",
    "artifact_id": "uuid-string",
    "question_text": "What is the main topic discussed in this artifact?",
    "question_source": "text",
    "created_at": "2025-12-30T13:00:00Z"
  },
  "answer": {
    "id": "uuid-string",
    "question_id": "uuid-string",
    "answer_text": "The main topic is...",
    "answer_status": "answered",
    "confidence": 0.85,
    "citations": [
      {
        "source_type": "material",
        "source_id": "uuid-string",
        "locator": "",
        "snippet": "Relevant text snippet from the material..."
      }
    ],
    "model": "gpt-4o-mini",
    "created_at": "2025-12-30T13:00:00Z"
  }
}
```

**Answer Status Values:**
- `answered` - Answer was successfully generated from context
- `not_covered` - Question cannot be answered from available content (confidence < 0.55 or explicitly not covered)
- `error` - Error occurred during answer generation

**Status Codes:**
- `201 Created` - Question asked and answer generated
- `400 Bad Request` - Invalid request body or missing question_text
- `404 Not Found` - Artifact not found
- `500 Internal Server Error` - Failed to process question or generate answer

**Note:** If `OPENAI_API_KEY` is not set, the answer will have `answer_status="error"` with an appropriate error message.

---

### Get Questions
**GET** `/artifacts/{id}/questions`

Retrieve all questions and their latest answers for an artifact (up to 20 most recent).

**Path Parameters:**
- `id` (UUID) - The artifact ID

**Response:** `200 OK`
```json
{
  "questions": [
    {
      "id": "uuid-string",
      "artifact_id": "uuid-string",
      "question_text": "What is the main topic?",
      "question_source": "text",
      "created_at": "2025-12-30T13:00:00Z"
    }
  ],
  "answers": [
    {
      "id": "uuid-string",
      "question_id": "uuid-string",
      "answer_text": "The main topic is...",
      "answer_status": "answered",
      "confidence": 0.85,
      "citations": [...],
      "model": "gpt-4o-mini",
      "created_at": "2025-12-30T13:00:00Z"
    }
  ]
}
```

**Status Codes:**
- `200 OK` - Questions retrieved successfully
- `400 Bad Request` - Invalid artifact ID
- `404 Not Found` - Artifact not found
- `500 Internal Server Error` - Failed to retrieve questions

---

### Get Artifact (with Questions)

**GET** `/artifacts/{id}?include_questions=true`

Retrieve an artifact with optional questions and answers included.

**Query Parameters:**
- `include_questions` (optional) - Set to `true` to include recent questions and answers

**Response:** `200 OK` (same as regular Get Artifact, with optional `questions` and `answers` fields)

---

## Notes

- All UUIDs are in standard UUID format (e.g., `550e8400-e29b-41d4-a716-446655440000`)
- Timestamps are in RFC3339 format (ISO 8601)
- File uploads are stored in `./data/uploads/{artifact_id}/` directory
- Text extraction is currently supported for `text/plain` files only
- PDF text extraction is planned for future phases
- All endpoints return JSON unless otherwise specified
- **Phase 2:** Q&A uses lexical retrieval (keyword matching) for RAG. Embeddings-based retrieval will be added in Phase 3.
- **Phase 2:** Answers are generated using OpenAI GPT-4o-mini. Requires `OPENAI_API_KEY` environment variable.
