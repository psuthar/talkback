package storage

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// UploadRoot returns the base directory for session uploads. Use this when resolving
// SessionArtifactDir/SessionArtifactPath so writes and reads use the same root.
// If TALKBACK_UPLOAD_ROOT is set, that is used; otherwise the process working directory.
func UploadRoot() string {
	if root := os.Getenv("TALKBACK_UPLOAD_ROOT"); root != "" {
		return filepath.Clean(root)
	}
	wd, _ := os.Getwd()
	return wd
}

// SessionStorageRoot is the local filesystem root for session file content.
// Under this directory the only children must be {session_id} directories.
// Deleting a path containing the session ID removes all files for that session.
const SessionStorageRoot = "sessions"

// SafeFilename replaces unsafe characters for use in object keys.
var safeFilenameRe = regexp.MustCompile(`[^a-zA-Z0-9._-]`)

// BuildArtifactStorageKey returns a stable key for an artifact object (e.g. R2).
// Path format: sessions/{session_id}/data/uploads/{safe_filename}
func BuildArtifactStorageKey(prefix string, sessionID uuid.UUID, artifactID uuid.UUID, filename string) string {
	p := strings.TrimSuffix(strings.Trim(prefix, "/"), "/")
	if p != "" {
		p = p + "/"
	}
	safe := "file"
	if filename != "" {
		safe = safeFilenameRe.ReplaceAllString(path.Base(filename), "_")
		if safe == "" {
			safe = "file"
		}
	}
	return p + "sessions/" + sessionID.String() + "/data/uploads/" + safe
}

// SessionArtifactPath returns the relative on-disk path for a session material file.
// Path format: sessions/{session_id}/data/uploads/{filename}
func SessionArtifactPath(sessionID uuid.UUID, filename string) string {
	return filepath.Join(SessionStorageRoot, sessionID.String(), "data", "uploads", filename)
}

// SessionArtifactDir returns the directory for session uploads under SessionStorageRoot.
// Path format: sessions/{session_id}/data/uploads
func SessionArtifactDir(sessionID uuid.UUID) string {
	return filepath.Join(SessionStorageRoot, sessionID.String(), "data", "uploads")
}

// SessionUploadsAbsDir returns the absolute directory for writing session material uploads.
// Path: UploadRoot/sessions/{session_id}/data/uploads
func SessionUploadsAbsDir(sessionID uuid.UUID) string {
	return filepath.Join(UploadRoot(), "sessions", sessionID.String(), "data", "uploads")
}

// SessionVideosDir returns sessions/{session_id}/videos.
func SessionVideosDir(sessionID uuid.UUID) string {
	return filepath.Join(SessionStorageRoot, sessionID.String(), "videos")
}

// SessionVideoPath returns sessions/{session_id}/videos/{videoID}.ext.
func SessionVideoPath(sessionID uuid.UUID, videoID uuid.UUID, ext string) string {
	return filepath.Join(SessionStorageRoot, sessionID.String(), "videos", videoID.String()+ext)
}

// SessionTranscriptsDir returns sessions/{session_id}/transcripts.
func SessionTranscriptsDir(sessionID uuid.UUID) string {
	return filepath.Join(SessionStorageRoot, sessionID.String(), "transcripts")
}

// SessionTranscriptPath returns sessions/{session_id}/transcripts/{tempID}.mp4.
func SessionTranscriptPath(sessionID uuid.UUID, tempID uuid.UUID) string {
	return filepath.Join(SessionStorageRoot, sessionID.String(), "transcripts", tempID.String()+".mp4")
}
