package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/sessionimport/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SCRUM-362: end-to-end coverage for top-level template metadata and the
// primary content pointer remap.
//
// The SCRUM-357 worker's original applyTemplatePrimary picked
// materials[0] / session_links[0] for kind=document/link, which couldn't
// distinguish between multiple elements in the same template. SCRUM-362
// rewires it to use a template element_id → synthetic dst row id
// mapping populated by buildSourceData, chained through the
// sessionimport remap maps. These tests assert the strict mapping holds
// for non-first elements.

func TestTemplateImport_E2E_TopLevelMetadata_RoundTrips(t *testing.T) {
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	addUserSessionCookie(t, h, httptest.NewRequest(http.MethodGet, "/", nil), "tmpl-meta@example.com")
	creator, err := h.DB.GetUserByEmail(ctx, "tmpl-meta@example.com")
	require.NoError(t, err)

	premise := "Should we ship feature X?"
	primaryDecision := "Adopt with caveats"
	decisionOutcome := "Approved with one dissent"
	srcRefURL := "https://templates.example.com/weekly-decision"
	tmpl := &template.Template{
		Version:            1,
		Title:              "Metadata Roundtrip Template",
		Premise:            &premise,
		PrimaryDecision:    &primaryDecision,
		DecisionOutcome:    &decisionOutcome,
		SourceReferenceURL: &srcRefURL,
		Elements: []template.Element{
			{Kind: template.KindLink, ID: "policy", URL: strPtr(srv.URL + "/policy")},
		},
	}

	pre := template.Run(ctx, tmpl, template.Config{AllowPrivateIPs: true, HTTPClient: srv.Client()})
	require.Empty(t, pre.Errors)

	descriptor, _ := json.Marshal(tmpl)
	job := &models.ImportJob{
		ID:                 uuid.New(),
		UserID:             &creator.ID,
		State:              models.ImportJobStateQueued,
		TemplateDescriptor: descriptor,
	}
	require.NoError(t, h.DB.CreateImportJob(ctx, job))
	deps := template.WorkerDeps{DB: h.DB, HTTPClient: srv.Client()}
	require.NoError(t, template.RunImportJob(ctx, deps, job.ID, tmpl, pre, creator.Email))

	got, err := h.DB.GetImportJob(ctx, job.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.SessionID)

	dstSession, err := h.DB.GetSession(ctx, *got.SessionID)
	require.NoError(t, err)
	require.NotNil(t, dstSession)
	require.NotNil(t, dstSession.Premise, "premise must round-trip")
	assert.Equal(t, premise, *dstSession.Premise)
	require.NotNil(t, dstSession.PrimaryDecision, "primary_decision must round-trip")
	assert.Equal(t, primaryDecision, *dstSession.PrimaryDecision)
	require.NotNil(t, dstSession.DecisionOutcome, "decision_outcome must round-trip")
	assert.Equal(t, decisionOutcome, *dstSession.DecisionOutcome)
	require.NotNil(t, dstSession.SourceReferenceURL, "source_reference_url must round-trip")
	assert.Equal(t, srcRefURL, *dstSession.SourceReferenceURL)
	assert.Equal(t, models.SessionSourceTemplate, dstSession.SourceProvider)
}

func TestTemplateImport_E2E_PrimaryPointer_DocumentRemapsToNamedElement(t *testing.T) {
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := []byte("PDFCONTENT")
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if r.Method == http.MethodGet {
			_, _ = w.Write(body)
		}
	}))
	defer srv.Close()

	addUserSessionCookie(t, h, httptest.NewRequest(http.MethodGet, "/", nil), "tmpl-primary-doc@example.com")
	creator, err := h.DB.GetUserByEmail(ctx, "tmpl-primary-doc@example.com")
	require.NoError(t, err)

	// Three materials. Primary points at the SECOND one. A "first match"
	// regression would resolve to materials[0] (which Postgres ordering
	// makes deterministic only for this test setup).
	tmpl := &template.Template{
		Version: 1,
		Title:   "Primary Pointer Document Test",
		Elements: []template.Element{
			{Kind: template.KindMaterial, ID: "first-doc", Title: strPtr("First"), URL: strPtr(srv.URL + "/first.pdf"), ContentType: strPtr("application/pdf")},
			{Kind: template.KindMaterial, ID: "agenda", Title: strPtr("Agenda"), URL: strPtr(srv.URL + "/agenda.pdf"), ContentType: strPtr("application/pdf")},
			{Kind: template.KindMaterial, ID: "third-doc", Title: strPtr("Third"), URL: strPtr(srv.URL + "/third.pdf"), ContentType: strPtr("application/pdf")},
		},
		Primary: &template.Primary{Kind: template.PrimaryKindDocument, Ref: "agenda"},
	}

	pre := template.Run(ctx, tmpl, template.Config{AllowPrivateIPs: true, HTTPClient: srv.Client()})
	require.Empty(t, pre.Errors, "preflight: %+v", pre.Errors)

	descriptor, _ := json.Marshal(tmpl)
	job := &models.ImportJob{
		ID:                 uuid.New(),
		UserID:             &creator.ID,
		State:              models.ImportJobStateQueued,
		TemplateDescriptor: descriptor,
	}
	require.NoError(t, h.DB.CreateImportJob(ctx, job))
	deps := template.WorkerDeps{DB: h.DB, HTTPClient: srv.Client()}
	require.NoError(t, template.RunImportJob(ctx, deps, job.ID, tmpl, pre, creator.Email))

	got, err := h.DB.GetImportJob(ctx, job.ID)
	require.NoError(t, err)
	require.NotNil(t, got.SessionID)

	dstSession, err := h.DB.GetSession(ctx, *got.SessionID)
	require.NoError(t, err)
	require.NotNil(t, dstSession.PrimaryContentKind, "primary_content_kind must be set")
	assert.Equal(t, models.SessionPrimaryContentKindDocument, *dstSession.PrimaryContentKind)
	require.NotNil(t, dstSession.PrimaryMaterialID, "primary_material_id must be set")

	// The named element's title is "Agenda" — assert dst.PrimaryMaterialID
	// resolves to a material with that title, NOT just the first material.
	mats, err := h.DB.GetActiveMaterialsBySessionID(ctx, *got.SessionID)
	require.NoError(t, err)
	require.Len(t, mats, 3)

	var primaryRow *models.Material
	for _, m := range mats {
		if m.ID == *dstSession.PrimaryMaterialID {
			primaryRow = m
			break
		}
	}
	require.NotNil(t, primaryRow, "primary_material_id must resolve to a row in the dst session")
	require.NotNil(t, primaryRow.Title)
	assert.Equal(t, "Agenda", *primaryRow.Title,
		"primary must be the named element 'agenda' (title 'Agenda'), not whichever material happened to be first; SCRUM-362 worker fix")
}

func TestTemplateImport_E2E_PrimaryPointer_LinkRemapsToNamedElement(t *testing.T) {
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	addUserSessionCookie(t, h, httptest.NewRequest(http.MethodGet, "/", nil), "tmpl-primary-link@example.com")
	creator, err := h.DB.GetUserByEmail(ctx, "tmpl-primary-link@example.com")
	require.NoError(t, err)

	policyTitle := "Decision policy"
	tmpl := &template.Template{
		Version: 1,
		Title:   "Primary Pointer Link Test",
		Elements: []template.Element{
			{Kind: template.KindLink, ID: "background-link", URL: strPtr(srv.URL + "/background")},
			{Kind: template.KindLink, ID: "policy", Title: &policyTitle, URL: strPtr(srv.URL + "/policy")},
			{Kind: template.KindLink, ID: "tail-link", URL: strPtr(srv.URL + "/tail")},
		},
		Primary: &template.Primary{Kind: template.PrimaryKindLink, Ref: "policy"},
	}

	pre := template.Run(ctx, tmpl, template.Config{AllowPrivateIPs: true, HTTPClient: srv.Client()})
	require.Empty(t, pre.Errors)

	descriptor, _ := json.Marshal(tmpl)
	job := &models.ImportJob{
		ID:                 uuid.New(),
		UserID:             &creator.ID,
		State:              models.ImportJobStateQueued,
		TemplateDescriptor: descriptor,
	}
	require.NoError(t, h.DB.CreateImportJob(ctx, job))
	deps := template.WorkerDeps{DB: h.DB, HTTPClient: srv.Client()}
	require.NoError(t, template.RunImportJob(ctx, deps, job.ID, tmpl, pre, creator.Email))

	got, err := h.DB.GetImportJob(ctx, job.ID)
	require.NoError(t, err)
	require.NotNil(t, got.SessionID)

	dstSession, err := h.DB.GetSession(ctx, *got.SessionID)
	require.NoError(t, err)
	require.NotNil(t, dstSession.PrimaryContentKind)
	assert.Equal(t, models.SessionPrimaryContentKindLink, *dstSession.PrimaryContentKind)
	require.NotNil(t, dstSession.PrimarySessionLinkID)

	// Find the row pointed at and assert its title matches the named element.
	links, err := h.DB.GetSessionLinksBySessionID(ctx, *got.SessionID)
	require.NoError(t, err)
	require.Len(t, links, 3)
	var primaryRow *models.SessionLink
	for _, link := range links {
		if link.ID == *dstSession.PrimarySessionLinkID {
			primaryRow = link
			break
		}
	}
	require.NotNil(t, primaryRow, "primary_session_link_id must resolve to a dst row")
	require.NotNil(t, primaryRow.Title)
	assert.Equal(t, "Decision policy", *primaryRow.Title,
		"primary must be the named link element, not whichever happened to be first; SCRUM-362 worker fix")
}
