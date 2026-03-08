# Mission: Primary Video – Notes and checklist

## Manual test checklist

Use or create a demo session with: 2 videos, 2 PDFs/docs, 2 images, 1 link.

1. Session page loads without errors.
2. Primary video appears under **Presentation**.
3. Additional video(s) appear under **Additional Videos**.
4. Primary video auto-loads into player.
5. Transcript shown matches selected video.
6. Clicking additional video switches player + transcript.
7. Documents/Slides/Images still render in their sections.
8. Old session data with untyped videos still shows one effective primary video.
9. If there are no videos, page still renders gracefully.
10. Creator: "Set as Primary" on an additional video promotes it and demotes current primary; refetch updates UI.

---

## Current backend model (pre-mission)

- **Session** (`models.Session`): Has `PrimaryVideoArtifactID` (UUID) pointing to a **file_artifact** (Zoom/downloaded MP4). No direct link to video_sources.
- **VideoSource** (`models.VideoSource`): One row per session-level video (Zoom import or session video upload). Has `transcript_status`, `transcript_text`, `transcript_segments`, `stored_video_object_key`. **No primary/secondary flag.** Multiple video_sources per session possible.
- **Material** (`models.Material`): Has `kind` (document | slides | diagram | other). Video files uploaded as materials get `kind=other`, `content_type=video/mp4`, `extracted_text` (Whisper transcript). **No video_role.**
- **FileArtifact**: Stores binary file; `session.primary_video_artifact_id` points to the “main” video file (Zoom). Link to video_source is implicit (Zoom pipeline creates both).

## Where video type is inferred

- **Playback**: `GetSession` builds `video_access_url` from session’s `primary_video_artifact_id` (file_artifact) when it’s ready and video content-type. Frontend also uses `video_sources[]` for player list.
- **Transcript**: Stored per **video_source** (`transcript_text`, `transcript_segments`). For **materials** (uploaded video), transcript is `extracted_text` on the material.
- **UI**: Left rail uses `video_sources` for “Video” section and `materials` for Documents/Slides; materials with `kind=other` are currently grouped with documents.

## Transcript association

- **Session-level videos** (video_sources): transcript on the row (`transcript_text`, etc.).
- **Video materials** (kind=other): transcript in `material.extracted_text`.
- So transcript is already per-video/per-material; no session-level transcript blob.

## Ordering / featured logic

- No explicit ordering. `primary_video_artifact_id` identifies the “main” file for playback; which video_source corresponds to it is not stored (Zoom creates one video_source per session for that flow).
