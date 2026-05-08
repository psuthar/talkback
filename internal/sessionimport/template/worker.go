package template

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/sessionimport"
	"github.com/psuthar/talkback/internal/storage"
)

// SCRUM-357: import job worker.
//
// The worker takes a Template + ElementMeta map (from Run preflight),
// fetches each element body, and threads the synthesized SourceData through
// the existing internal/sessionimport primitives. Every per-element step
// updates the job's elements_state so the status API surfaces progress.
//
// Failure semantics (per SCRUM-350 epic):
//   - Per-element runtime failure: preserve the partial session, mark the
//     element failed in elements_state, continue with remaining elements.
//   - Critical infrastructure failure: mark the job failed; partial session
//     stays.
//
// This is the MVP worker. Future improvements (SCRUM-353 phase): goroutine
// pool, retries, cancellation API.

// WorkerDeps wires the worker's runtime dependencies. Tests can pass a fake
// HTTPClient (e.g. backed by httptest); production passes nil so the
// preflight package's SSRF-aware default client is used.
type WorkerDeps struct {
	DB         *database.DB
	Storage    storage.Interface
	R2Prefix   string
	HTTPClient *http.Client       // nil = use SSRF-aware default
	Trigger    func(uuid.UUID)    // optional triggerIndex hook
}

// RunImportJob executes a queued ImportJob to terminal state.
//
// Caller has already done schema validation + preflight; this function
// fetches bodies, materializes session content, and updates the job
// record. It is safe to invoke as a goroutine kicked off by the API
// handler; it manages its own context lifecycle (ctx is the outer life,
// per-fetch deadlines are derived from it).
func RunImportJob(ctx context.Context, deps WorkerDeps, jobID uuid.UUID, t *Template, preflight PreflightResult, creatorEmail string) error {
	// Mark running.
	if err := deps.DB.UpdateImportJobState(ctx, jobID, models.ImportJobStateRunning, nil); err != nil {
		return fmt.Errorf("worker: mark running: %w", err)
	}

	// Initialize per-element state (every element starts queued).
	for _, el := range t.Elements {
		_ = deps.DB.SetImportJobElement(ctx, jobID, el.ID, models.ImportElementResult{
			Status: models.ImportElementStatusQueued,
		})
	}

	// Create the destination session row up-front so failed-mid-job state
	// has a session to anchor against (SCRUM-350 failure semantics: preserve
	// partial session).
	dst := &models.Session{
		ID:                 uuid.New(),
		Title:              t.Title,
		CreatedBy:          &creatorEmail,
		Status:             models.SessionStatusOpen,
		SourceProvider:     models.SessionSourceTemplate,
		SourceReferenceURL: t.SourceReferenceURL,
		Premise:            t.Premise,
		PrimaryDecision:    t.PrimaryDecision,
		DecisionOutcome:    t.DecisionOutcome,
	}
	if err := deps.DB.CreateSession(ctx, dst); err != nil {
		errMsg := err.Error()
		_ = deps.DB.UpdateImportJobState(ctx, jobID, models.ImportJobStateFailed, &errMsg)
		return fmt.Errorf("worker: create dst session: %w", err)
	}
	if err := deps.DB.SetImportJobSessionID(ctx, jobID, dst.ID); err != nil {
		log.Printf("worker: set import_job session_id: %v", err)
	}

	// Build the import context. R2Prefix passed through so storage keys
	// land under the right bucket prefix.
	importDeps := sessionimport.Deps{
		DB:           deps.DB,
		Storage:      deps.Storage,
		R2Prefix:     deps.R2Prefix,
		TriggerIndex: deps.Trigger,
	}
	c := sessionimport.NewCtx(importDeps, dst)

	// Materialize source-side data slices from the template + preflight.
	// Each fetch error becomes one element_failed; on success, we
	// synthesize the corresponding *models row. The returned
	// elementMapping carries template element_id → synthetic dst row id
	// so applyTemplatePrimary can remap by element id (SCRUM-362).
	src, em := buildSourceData(ctx, deps, t, preflight, c, jobID)

	// Run the import primitives in the canonical order.
	_ = sessionimport.ImportArtifacts(ctx, c, src.Artifacts)
	_ = sessionimport.ImportFileArtifacts(ctx, c, src.AllFileArtifacts, nil) // templates have no source primary FA pointer
	_ = sessionimport.ImportMaterials(ctx, c, src.Materials, "")             // no source storage namespace for templates
	_ = sessionimport.ImportSessionLinks(ctx, c, src.SessionLinks)
	_ = sessionimport.ImportVideoSources(ctx, c, src.VideoSources)
	if err := sessionimport.ImportTranscripts(ctx, c, src.Transcripts); err != nil {
		errMsg := err.Error()
		_ = deps.DB.UpdateImportJobState(ctx, jobID, models.ImportJobStateFailed, &errMsg)
		return err
	}
	if err := sessionimport.ImportSessionMetadata(ctx, c, dst); err != nil {
		errMsg := err.Error()
		_ = deps.DB.UpdateImportJobState(ctx, jobID, models.ImportJobStateFailed, &errMsg)
		return err
	}

	// Apply the primary content pointer (template-level Primary translates
	// to the primary remap, looking up the named element_id in em and
	// chaining through c.MaterialRemap/LinkRemap to the dst row id).
	if t.Primary != nil {
		applyTemplatePrimary(ctx, deps.DB, dst.ID, t.Primary, em, c)
	}

	if deps.Trigger != nil {
		deps.Trigger(dst.ID)
	}

	// Determine terminal state based on whether any element failed.
	terminal := models.ImportJobStateSucceeded
	if anyElementFailed(c.PartialFailures) || hasFailedElement(ctx, deps.DB, jobID) {
		terminal = models.ImportJobStatePartial
	}
	if err := deps.DB.UpdateImportJobState(ctx, jobID, terminal, nil); err != nil {
		return fmt.Errorf("worker: mark terminal: %w", err)
	}
	return nil
}

func anyElementFailed(partial []string) bool {
	return len(partial) > 0
}

// hasFailedElement reads the job's elements_state and returns true if any
// element ended up in the failed state.
func hasFailedElement(ctx context.Context, db *database.DB, jobID uuid.UUID) bool {
	job, err := db.GetImportJob(ctx, jobID)
	if err != nil || job == nil {
		return false
	}
	for _, el := range job.ElementsState {
		if el.Status == models.ImportElementStatusFailed {
			return true
		}
	}
	return false
}

// elementMapping carries template element_id → synthetic dst-row id, per
// kind. Used by applyTemplatePrimary to look up the row a template Primary
// pointer names. SCRUM-362.
type elementMapping struct {
	materials    map[string]uuid.UUID
	links        map[string]uuid.UUID
	videoSources map[string]uuid.UUID
	transcripts  map[string]uuid.UUID
}

func newElementMapping() elementMapping {
	return elementMapping{
		materials:    map[string]uuid.UUID{},
		links:        map[string]uuid.UUID{},
		videoSources: map[string]uuid.UUID{},
		transcripts:  map[string]uuid.UUID{},
	}
}

// buildSourceData fetches each element URL and synthesizes the appropriate
// *models row into a sessionimport.SourceData. Per-element fetch errors are
// recorded in the job and the corresponding row is omitted.
//
// Returns the populated SourceData and an elementMapping that records
// template element_id → synthetic dst row id for each successful element,
// used by applyTemplatePrimary.
func buildSourceData(ctx context.Context, deps WorkerDeps, t *Template, pre PreflightResult, c *sessionimport.Ctx, jobID uuid.UUID) (sessionimport.SourceData, elementMapping) {
	em := newElementMapping()
	src := sessionimport.SourceData{
		Session: c.Dst, // metadata copy will read this for premise/decision/primary pointer encoding
	}
	// Single-artifact bucket for materials/video_sources (templates don't
	// have a notion of multiple "artifacts" to group elements; one default
	// artifact owns everything).
	dstArtifactID := uuid.New()
	src.Artifacts = []*models.Artifact{{
		ID:        dstArtifactID,
		SessionID: c.Dst.ID,
		Title:     "Template content",
	}}
	c.ArtifactRemap[dstArtifactID] = dstArtifactID // identity remap; primitive will create a fresh artifact

	httpClient := deps.HTTPClient
	if httpClient == nil {
		httpClient = newDefaultClient(Config{PerFetchTimeout: 30 * time.Second, MaxRedirects: 5})
	}

	for i := range t.Elements {
		el := &t.Elements[i]
		switch el.Kind {
		case KindMaterial:
			body, observedCT, fetchErr := fetchBody(ctx, httpClient, deps, *el.URL, MaxMaterialBytes)
			if fetchErr != nil {
				recordElementFailure(ctx, deps.DB, jobID, el.ID, ReasonURLUnreachable, fetchErr.Error())
				continue
			}
			fa, err := uploadMaterialBody(ctx, deps, c.Dst.ID, dstArtifactID, baseFilename(*el.URL), observedCT, body)
			if err != nil {
				recordElementFailure(ctx, deps.DB, jobID, el.ID, ReasonURLUnreachable, err.Error())
				continue
			}
			titleStr := ""
			if el.Title != nil {
				titleStr = *el.Title
			}
			size := int64(len(body))
			matID := uuid.New()
			src.Materials = append(src.Materials, &models.Material{
				ID:              matID,
				ArtifactID:      dstArtifactID,
				SessionID:       c.Dst.ID,
				Kind:            "document",
				Filename:        filepath.Base(*el.URL),
				ContentType:     observedCT,
				StorageURL:      "",
				StorageProvider: "r2",
				StorageKey:      fa,
				SizeBytes:       &size,
				TextStatus:      "pending",
				Title:           &titleStr,
			})
			em.materials[el.ID] = matID
			recordElementSuccess(ctx, deps.DB, jobID, el.ID)
		case KindLink:
			linkID := uuid.New()
			src.SessionLinks = append(src.SessionLinks, &models.SessionLink{
				ID:        linkID,
				SessionID: c.Dst.ID,
				URL:       *el.URL,
				Title:     el.Title,
				Status:    models.SessionLinkStatusVerified,
			})
			em.links[el.ID] = linkID
			recordElementSuccess(ctx, deps.DB, jobID, el.ID)
		case KindVideoSource:
			provider := "other"
			if el.Provider != nil && *el.Provider != "" {
				provider = *el.Provider
			}
			vsID := uuid.New()
			vs := &models.VideoSource{
				ID:               vsID,
				ArtifactID:       dstArtifactID,
				SessionID:        c.Dst.ID,
				Provider:         provider,
				PlaybackMode:     "embed",
				SourceType:       "embed_url",
				TranscriptStatus: models.VideoTranscriptStatusMissing,
			}
			if el.EmbedURL != nil {
				vs.EmbedURL = el.EmbedURL
			}
			if el.VideoURL != nil {
				vs.VideoURL = *el.VideoURL
			}
			if el.DurationSeconds != nil {
				ds := *el.DurationSeconds
				vs.DurationSeconds = &ds
			}
			src.VideoSources = append(src.VideoSources, vs)
			em.videoSources[el.ID] = vsID
			recordElementSuccess(ctx, deps.DB, jobID, el.ID)
		case KindTranscript:
			text, err := fetchTranscriptText(ctx, httpClient, deps, el)
			if err != nil {
				recordElementFailure(ctx, deps.DB, jobID, el.ID, ReasonURLUnreachable, err.Error())
				continue
			}
			source := *el.Source
			rawText := text
			txID := uuid.New()
			src.Transcripts = append(src.Transcripts, &models.Transcript{
				ID:        txID,
				SessionID: c.Dst.ID,
				Source:    source,
				Status:    models.TranscriptStatusReady,
				RawText:   &rawText,
			})
			em.transcripts[el.ID] = txID
			recordElementSuccess(ctx, deps.DB, jobID, el.ID)
		}
	}
	// Ignore preflight (already validated; metadata-only).
	_ = pre
	return src, em
}

// fetchBody GETs a URL with the SSRF-aware client, capping read at maxBytes.
func fetchBody(ctx context.Context, client *http.Client, deps WorkerDeps, url string, maxBytes int64) (body []byte, contentType string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("http %d", resp.StatusCode)
	}
	contentType = strings.TrimSpace(strings.SplitN(resp.Header.Get("Content-Type"), ";", 2)[0])
	if maxBytes <= 0 {
		maxBytes = 100 * 1024 * 1024
	}
	body, err = io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(body)) > maxBytes {
		return nil, "", fmt.Errorf("body exceeded %d bytes", maxBytes)
	}
	return body, contentType, nil
}

func fetchTranscriptText(ctx context.Context, client *http.Client, deps WorkerDeps, el *Element) (string, error) {
	url := ""
	if el.TextURL != nil {
		url = *el.TextURL
	} else if el.SegmentsURL != nil {
		url = *el.SegmentsURL
	}
	if url == "" {
		return "", fmt.Errorf("no transcript URL")
	}
	body, _, err := fetchBody(ctx, client, deps, url, MaxTranscriptBytes)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// uploadMaterialBody writes the fetched body to storage under a fresh
// destination key and returns that key.
func uploadMaterialBody(ctx context.Context, deps WorkerDeps, sessionID, artifactID uuid.UUID, filename, contentType string, body []byte) (string, error) {
	if deps.Storage == nil {
		// In tests without storage, we synthesize a stable in-memory key
		// shape so the SourceData rows still validate downstream.
		return fmt.Sprintf("sessions/%s/data/uploads/%s", sessionID, filepath.Base(filename)), nil
	}
	r2Prefix := strings.TrimSuffix(strings.TrimSpace(deps.R2Prefix), "/")
	key := storage.BuildArtifactStorageKey(r2Prefix, sessionID, artifactID, filename)
	_, _, err := deps.Storage.Put(ctx, key, bytes.NewReader(body), contentType, int64(len(body)))
	if err != nil {
		return "", err
	}
	return key, nil
}

func baseFilename(rawURL string) string {
	// Best-effort: strip query string and take last path segment.
	u := rawURL
	if idx := strings.Index(u, "?"); idx >= 0 {
		u = u[:idx]
	}
	if idx := strings.LastIndex(u, "/"); idx >= 0 && idx+1 < len(u) {
		return u[idx+1:]
	}
	return "file"
}

func recordElementSuccess(ctx context.Context, db *database.DB, jobID uuid.UUID, elementID string) {
	_ = db.SetImportJobElement(ctx, jobID, elementID, models.ImportElementResult{
		Status: models.ImportElementStatusSucceeded,
	})
}

func recordElementFailure(ctx context.Context, db *database.DB, jobID uuid.UUID, elementID, code, message string) {
	_ = db.SetImportJobElement(ctx, jobID, elementID, models.ImportElementResult{
		Status:    models.ImportElementStatusFailed,
		ErrorCode: code,
		Message:   message,
	})
}

// applyTemplatePrimary translates a template-level Primary to a primary
// content pointer on the destination session.
//
// SCRUM-362: looks up p.Ref in the elementMapping populated by
// buildSourceData (template element_id → synthetic *models row id), then
// chains through c.MaterialRemap / LinkRemap (synthetic → dst row UUID)
// to set the correct dst pointer. Replaces the SCRUM-357 MVP "first
// material on dst" stub which couldn't distinguish between multiple
// materials in the same template.
//
// If the named element didn't successfully import (no entry in em or no
// entry in the import primitive's remap), the primary pointer is left
// unset on the clone — matches CopySession's behavior when a primary
// references a child the copy couldn't reproduce (SCRUM-340).
func applyTemplatePrimary(ctx context.Context, db *database.DB, sessionID uuid.UUID, p *Primary, em elementMapping, c *sessionimport.Ctx) {
	switch p.Kind {
	case PrimaryKindDocument:
		synthID, ok := em.materials[p.Ref]
		if !ok {
			return
		}
		dstID, ok := c.MaterialRemap[synthID]
		if !ok {
			return
		}
		_ = db.UpdateSessionPrimary(ctx, sessionID, models.SessionPrimaryContentKindDocument, &dstID)
	case PrimaryKindLink:
		synthID, ok := em.links[p.Ref]
		if !ok {
			return
		}
		dstID, ok := c.LinkRemap[synthID]
		if !ok {
			return
		}
		_ = db.UpdateSessionPrimary(ctx, sessionID, models.SessionPrimaryContentKindLink, &dstID)
	case PrimaryKindVideo:
		// Video primary in v1 is stored on file_artifact, but templates use
		// embed-mode video_sources (no FA). Once URL→MP4 ingest lands as a
		// follow-up, this branch will look up via em.videoSources +
		// c.VideoSourceRemap with the corresponding FA id.
	}
	_ = json.RawMessage(nil) // keep encoding/json import alive when this file is the only consumer
}
