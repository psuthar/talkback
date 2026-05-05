package handlers

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"

	"github.com/psuthar/talkback/internal/markitdown"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SCRUM-303 — image uploads with the feature flag OFF stay on the legacy
// path (text_status=ready immediately, no async job). This is the
// no-regression guarantee for environments that haven't opted in.
func TestImageUpload_FlagOffPreservesLegacyBehavior(t *testing.T) {
	// Note: cannot use t.Parallel() here because t.Setenv is incompatible
	// with parallel execution; tests still get an isolated DB via the
	// parallel helper.
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	t.Setenv("MARKITDOWN_IMAGE_EXTRACTION_ENABLED", "false")
	t.Setenv("MARKITDOWN_SIDECAR_URL", "https://sidecar.example")
	t.Setenv("MARKITDOWN_SIDECAR_SECRET", "test-secret")
	h.Markitdown = markitdown.NewClient()

	session := createTestSessionForHandlers(t, h.DB, "SCRUM-303 flag off")
	resp := uploadImagePNG(t, h, session.ID.String())
	assert.Equal(t, "ready", resp["text_status"], "flag OFF must keep today's text_status=ready behavior")
}

// SCRUM-303 — when the env-flag is ON but the sidecar client is not
// configured (URL/SECRET missing), the upload still succeeds and stays on
// the legacy path. This protects dev/test environments where the operator
// flipped the flag without deploying the sidecar.
func TestImageUpload_FlagOnButClientDisabledPreservesLegacy(t *testing.T) {
	// Note: cannot use t.Parallel() here because t.Setenv is incompatible
	// with parallel execution; tests still get an isolated DB via the
	// parallel helper.
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	t.Setenv("MARKITDOWN_IMAGE_EXTRACTION_ENABLED", "true")
	t.Setenv("MARKITDOWN_SIDECAR_URL", "")
	t.Setenv("MARKITDOWN_SIDECAR_SECRET", "")
	h.Markitdown = markitdown.NewClient() // .Enabled() will be false

	session := createTestSessionForHandlers(t, h.DB, "SCRUM-303 client disabled")
	resp := uploadImagePNG(t, h, session.ID.String())
	assert.Equal(t, "ready", resp["text_status"], "client disabled must keep today's behavior")
}

// SCRUM-303 — when both the flag is ON and the client is Enabled(), an
// image upload returns text_status=pending so the worker can populate
// extracted_text via the sidecar.
func TestImageUpload_FlagOnAndClientEnabledReturnsPending(t *testing.T) {
	// Note: cannot use t.Parallel() here because t.Setenv is incompatible
	// with parallel execution; tests still get an isolated DB via the
	// parallel helper.
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	t.Setenv("MARKITDOWN_IMAGE_EXTRACTION_ENABLED", "true")
	t.Setenv("MARKITDOWN_SIDECAR_URL", "https://sidecar.example")
	t.Setenv("MARKITDOWN_SIDECAR_SECRET", "test-secret")
	h.Markitdown = markitdown.NewClient()
	require.True(t, h.Markitdown.Enabled())

	session := createTestSessionForHandlers(t, h.DB, "SCRUM-303 pending")
	resp := uploadImagePNG(t, h, session.ID.String())
	assert.Equal(t, "pending", resp["text_status"], "flag+client enabled must defer to async worker")
}

// uploadImagePNG posts a tiny multipart PNG to /materials/upload and
// returns the decoded JSON response. Body is fake bytes; the upload
// handler does not parse pixel data.
func uploadImagePNG(t *testing.T, h *Handlers, sessionID string) map[string]interface{} {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="diagram.png"`)
	header.Set("Content-Type", "image/png")
	part, err := writer.CreatePart(header)
	require.NoError(t, err)
	// 8-byte PNG signature + arbitrary trailer is enough for the
	// magic-byte refinement helper to classify as image/png.
	_, err = part.Write([]byte("\x89PNG\r\n\x1a\nfake-image-bytes"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/sessions/"+sessionID+"/materials/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	h.SessionUploadMaterial(w, req)
	require.Equalf(t, http.StatusCreated, w.Code, "upload should succeed: %s", w.Body.String())

	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	return resp
}
