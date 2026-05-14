package storage

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/google/uuid"
)

// NormalizeFilename decodes URL-encoded characters (e.g. %20) and replaces spaces and other
// problematic characters with underscores so filenames are safe and display consistently.
func NormalizeFilename(raw string) string {
	if raw == "" {
		return "file"
	}
	decoded, err := url.QueryUnescape(raw)
	if err != nil {
		decoded = raw
	}
	base := path.Base(decoded)
	ext := strings.ToLower(path.Ext(base))
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	var b strings.Builder
	for _, r := range base {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r) || r == '.' || r == '-' || r == '_':
			b.WriteRune(r)
		case r == ' ' || unicode.IsSpace(r):
			b.WriteByte('_')
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "._")
	for strings.Contains(out, "__") {
		out = strings.ReplaceAll(out, "__", "_")
	}
	if out == "" {
		out = "file"
	}
	return out + ext
}

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

// SlidesPrefixFromArtifactKey returns the prefix for derived slide assets for a given artifact key.
// If the artifact key is "prefix/sessions/<sid>/data/uploads/Galaxy.pptx",
// the slides prefix is "prefix/sessions/<sid>/data/uploads/Galaxy.pptx_slides/".
func SlidesPrefixFromArtifactKey(artifactKey string) string {
	return artifactKey + "_slides/"
}

// SlidesManifestKeyFromArtifactKey returns the storage key for the slide manifest JSON for a given artifact key.
// Using the example above, this would be "prefix/sessions/<sid>/data/uploads/Galaxy.pptx_slides/manifest.json".
func SlidesManifestKeyFromArtifactKey(artifactKey string) string {
	return SlidesPrefixFromArtifactKey(artifactKey) + "manifest.json"
}

// SlidesProcessingKeyFromArtifactKey returns the storage key for the in-flight slide-derivation marker
// (SCRUM-443). Written on goroutine entry and deleted on terminal success/error so a goroutine killed
// mid-flight (OOM, instance restart) leaves the marker behind. GetSlidesStatus checks the marker's age
// to detect stranded conversions and lazily auto-fail them.
func SlidesProcessingKeyFromArtifactKey(artifactKey string) string {
	return SlidesPrefixFromArtifactKey(artifactKey) + "processing.json"
}

// SlideImageKeyFromArtifactKey returns the storage key for a specific slide PNG for a given artifact key and 1-based index.
// Using the example above and index=1, this would be "prefix/sessions/<sid>/data/uploads/Galaxy.pptx_slides/slide-001.png".
func SlideImageKeyFromArtifactKey(artifactKey string, index int) string {
	return fmt.Sprintf("%sslide-%03d.png", SlidesPrefixFromArtifactKey(artifactKey), index)
}
