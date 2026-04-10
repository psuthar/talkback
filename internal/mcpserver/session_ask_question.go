// ask_session and ask_session_question: session-scoped RAG Q&A for MCP agents (SCRUM-44, SCRUM-49, SCRUM-50).
// Mirrors POST /api/sessions/:id/ask: same rag.EnsureSessionIndex/RetrieveTopK, optional R2 storage for PDF indexing,
// utils.GenerateAnswer, citation.NormalizeCitations, and limits — thin MCP transport only.
// Uses the same ACL as get_session_metadata / search_session.
package mcpserver

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/psuthar/talkback/internal/auth"
	"github.com/psuthar/talkback/internal/citation"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/rag"
	"github.com/psuthar/talkback/internal/storage"
	"github.com/psuthar/talkback/internal/utils"
)

// mcpQAAutomationMinConfidence must stay aligned with the not-covered guardrail in
// internal/utils/qa.go (confidence < this forces not_covered and clears citations).
const mcpQAAutomationMinConfidence = 0.55

type askSessionQuestionInput struct {
	SessionID string `json:"session_id" jsonschema:"UUID of the TalkBack session"`
	Question  string `json:"question" jsonschema:"Question to answer from session content only"`
}

type askSessionCitationOut struct {
	CitationID string                   `json:"citation_id"`
	ChunkID    string                   `json:"chunk_id"`
	SourceType string                   `json:"source_type"`
	SourceID   string                   `json:"source_id"`
	Label      string                   `json:"label"`
	Excerpt    string                   `json:"excerpt"`
	Anchor     map[string]interface{}   `json:"anchor,omitempty"`
	Navigation *citation.CitationTarget `json:"navigation,omitempty"` // same resolver as POST /api/sessions/:id/ask (SCRUM-53)
}

type askSessionQuestionOutput struct {
	SessionID             string                  `json:"session_id"`
	QuestionID            string                  `json:"question_id"`
	AnswerID              string                  `json:"answer_id"`
	AnswerText            string                  `json:"answer_text"`
	AnswerStatus          string                  `json:"answer_status"`
	Confidence            float64                 `json:"confidence"`
	AutomationRecommended bool                    `json:"automation_recommended"`
	Citations             []askSessionCitationOut `json:"citations"`
	LLMModel              string                  `json:"llm_model,omitempty"`
	CachedRepeat          bool                    `json:"cached_repeat,omitempty"`
	QuestionSaved         string                  `json:"question_saved_at,omitempty"`
	AnswerSaved           string                  `json:"answer_saved_at,omitempty"`
}

func registerAskSessionQuestionTools(server *mcp.Server, db *database.DB, store storage.Interface) {
	const desc = "Ask a natural-language question about a single TalkBack session. Returns a grounded answer, citations (citation_id, label, excerpt, anchor, navigation — same normalization and navigation resolution as HTTP SessionAsk; see docs/mcp-server.md § Citations), confidence, answer_status (answered | not_covered | error), and automation_recommended. Uses the same RAG + guardrails as the web app (internal/utils QA). Requires DATABASE_URL, TALKBACK_MCP_ACTING_USER_ID, and OPENAI_API_KEY; enforces session read access and per-session question limits. Persists the Q&A like POST /api/sessions/:id/ask."
	registerAskSessionQuestionTool(server, db, store, ToolAskSession, desc)
	registerAskSessionQuestionTool(server, db, store, ToolAskSessionQuestion, "Same behavior as "+ToolAskSession+" (backward-compatible tool name). "+desc)
}

func registerAskSessionQuestionTool(server *mcp.Server, db *database.DB, store storage.Interface, toolName string, description string) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        toolName,
		Description: description,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in askSessionQuestionInput) (*mcp.CallToolResult, askSessionQuestionOutput, error) {
		start := time.Now()
		tool := toolName
		var logSessionID string
		defer func() {
			extras := map[string]string{}
			if logSessionID != "" {
				extras["session_id"] = logSessionID
			}
			logMCPToolComplete(tool, time.Since(start), extras)
		}()

		actingID, ok := ActingUserID(ctx)
		if !ok {
			return nil, askSessionQuestionOutput{}, mcpToolErr(403, "acting user not configured; set TALKBACK_MCP_ACTING_USER_ID to a TalkBack user UUID")
		}

		sessionID, err := uuid.Parse(strings.TrimSpace(in.SessionID))
		if err != nil {
			return nil, askSessionQuestionOutput{}, mcpToolErr(400, "invalid session_id: must be a UUID")
		}
		logSessionID = sessionID.String()

		qtext := strings.TrimSpace(in.Question)
		if qtext == "" {
			return nil, askSessionQuestionOutput{}, mcpToolErr(400, "question is required")
		}

		user, err := db.GetUserByID(ctx, actingID)
		if err != nil {
			return nil, askSessionQuestionOutput{}, err
		}
		if user == nil {
			return nil, askSessionQuestionOutput{}, mcpToolErr(403, "acting user not found in database")
		}

		session, err := db.GetSession(ctx, sessionID)
		if err != nil || session == nil {
			if err != nil && strings.Contains(err.Error(), "not found") {
				return nil, askSessionQuestionOutput{}, mcpToolErr(404, "session not found")
			}
			if err != nil {
				return nil, askSessionQuestionOutput{}, err
			}
			return nil, askSessionQuestionOutput{}, mcpToolErr(404, "session not found")
		}

		if allowed, err := userMayReadSessionMCP(ctx, db, session, user); err != nil {
			return nil, askSessionQuestionOutput{}, err
		} else if !allowed {
			return nil, askSessionQuestionOutput{}, mcpToolErr(403, "you do not have access to this session")
		}

		artifacts, err := db.GetArtifactsBySessionID(ctx, sessionID)
		if err != nil || len(artifacts) == 0 {
			return nil, askSessionQuestionOutput{}, mcpToolErr(400, "session has no artifacts; create an artifact first")
		}
		artifactID := artifacts[0].ID

		existingQ, existingA, _ := db.FindExistingQuestionByText(ctx, sessionID, qtext, nil)
		if existingQ != nil && existingA != nil {
			out := mcpBuildAskOutput(ctx, db, sessionID, existingQ, existingA)
			out.CachedRepeat = true
			return nil, out, nil
		}

		embedder := &rag.OpenAIEmbedder{}
		if err := rag.EnsureSessionIndex(ctx, db, embedder, sessionID, store); err != nil {
			log.Printf("%s EnsureSessionIndex: %v", tool, err)
		}

		videoSources, _ := db.GetVideoSourcesBySessionID(ctx, sessionID)
		primary, _ := resolveEffectivePrimaryAndAdditional(videoSources)
		var primaryVideoID *uuid.UUID
		if primary != nil {
			primaryVideoID = &primary.ID
		}

		questionEmbedding, err := embedder.Embed(ctx, []string{qtext})
		if err != nil || len(questionEmbedding) == 0 {
			log.Printf("%s Embed: %v", tool, err)
		}
		var sessionChunks []models.SessionChunk
		if len(questionEmbedding) > 0 {
			sessionChunks, err = rag.RetrieveTopK(ctx, db, sessionID, questionEmbedding[0], rag.DefaultTopK, primaryVideoID)
			if err != nil {
				log.Printf("%s RetrieveTopK: %v", tool, err)
			}
			if len(sessionChunks) == 0 && mcpSessionHasIndexableContent(ctx, db, sessionID) {
				_ = db.DeleteChunkEmbeddingsBySessionID(ctx, sessionID)
				_ = db.DeleteSessionChunksBySessionID(ctx, sessionID)
				if reindexErr := rag.IndexSession(ctx, db, embedder, sessionID, store); reindexErr != nil {
					log.Printf("%s reindex after 0 chunks: %v", tool, reindexErr)
				} else {
					sessionChunks, err = rag.RetrieveTopK(ctx, db, sessionID, questionEmbedding[0], rag.DefaultTopK, primaryVideoID)
					if err != nil {
						log.Printf("%s RetrieveTopK after reindex: %v", tool, err)
					}
				}
			}
		}

		count, err := db.CountQuestionsBySessionID(ctx, sessionID)
		if err != nil {
			log.Printf("%s CountQuestionsBySessionID: %v", tool, err)
			return nil, askSessionQuestionOutput{}, mcpToolErr(500, "failed to check question limit")
		}
		if count >= auth.Config.MaxQuestionsPerSession {
			return nil, askSessionQuestionOutput{}, mcpToolErr(429, fmt.Sprintf("session question limit reached (max %d)", auth.Config.MaxQuestionsPerSession))
		}

		var askedByPtr *string
		if user.Email != "" {
			email := strings.TrimSpace(user.Email)
			if email != "" {
				askedByPtr = &email
			}
		}
		question := &models.Question{
			ID:               uuid.New(),
			ArtifactID:       artifactID,
			SessionID:        sessionID,
			ParentQuestionID: nil,
			AskedBy:          askedByPtr,
			QuestionText:     qtext,
			QuestionSource:   models.QuestionSourceText,
		}
		if err := db.CreateQuestion(ctx, question); err != nil {
			log.Printf("%s CreateQuestion: %v", tool, err)
			return nil, askSessionQuestionOutput{}, mcpToolErr(500, "failed to create question")
		}

		if os.Getenv("ASK_TRACE") == "1" {
			mcpTraceAskLookup(qtext, sessionChunks)
		}

		chunks := mcpSessionChunksToUtilsChunks(sessionChunks)
		emptyChunkMessage := ""
		if len(chunks) == 0 {
			if msg := mcpSessionEmptyChunkMessage(ctx, db, sessionID); msg != "" {
				emptyChunkMessage = msg
			}
		}

		priorQA := mcpBuildThreadPriorQA(ctx, db, nil)
		priorQuestions, priorAnswers, _ := db.GetQuestionsBySessionID(ctx, sessionID, 10)
		priorQA = append(priorQA, mcpBuildPriorQA(priorQuestions, priorAnswers, question.ID)...)

		sessionCtx := utils.SessionContext{
			Premise:         session.Premise,
			PrimaryDecision: session.PrimaryDecision,
		}
		qaResponse, _, err := utils.GenerateAnswer(ctx, qtext, chunks, session.Title, sessionCtx, priorQA)
		if emptyChunkMessage != "" && qaResponse != nil {
			qaResponse.AnswerText = emptyChunkMessage
		}
		if err != nil {
			log.Printf("%s GenerateAnswer: %v", tool, err)
			qaResponse = &utils.QAResponse{
				AnswerStatus: "error",
				AnswerText:   fmt.Sprintf("Failed to generate answer: %v", err),
				Confidence:   0,
				Citations:    []models.Citation{},
			}
		}

		chunkMap := make(citation.ChunkByID)
		for _, c := range sessionChunks {
			chunkMap[c.ID.String()] = c
		}
		materialLabels := make(citation.MaterialLabels)
		if materials, _ := db.GetActiveMaterialsBySessionID(ctx, sessionID); materials != nil {
			for _, m := range materials {
				name := m.Filename
				if m.Title != nil && *m.Title != "" {
					name = *m.Title
				}
				if name != "" {
					materialLabels[m.ID.String()] = name
				}
			}
		}
		qaResponse.Citations = citation.NormalizeCitations(qaResponse.Citations, chunkMap, materialLabels)

		answer, err := utils.ConvertQAResponseToAnswer(question.ID, qaResponse, embedder.ModelName())
		if err != nil {
			log.Printf("%s ConvertQAResponseToAnswer: %v", tool, err)
			return nil, askSessionQuestionOutput{}, mcpToolErr(500, "failed to process answer")
		}
		if err := db.CreateAnswer(ctx, answer); err != nil {
			log.Printf("%s CreateAnswer: %v", tool, err)
			return nil, askSessionQuestionOutput{}, mcpToolErr(500, "failed to save answer")
		}

		out := mcpBuildAskOutput(ctx, db, sessionID, question, answer)
		return nil, out, nil
	})
}

func mcpBuildAskOutput(ctx context.Context, db *database.DB, sessionID uuid.UUID, q *models.Question, a *models.Answer) askSessionQuestionOutput {
	var videoSources []*models.VideoSource
	var materials []*models.Material
	var links []*models.SessionLink
	if db != nil {
		videoSources, _ = db.GetVideoSourcesBySessionID(ctx, sessionID)
		materials, _ = db.GetActiveMaterialsBySessionID(ctx, sessionID)
		links, _ = db.GetSessionLinksBySessionID(ctx, sessionID)
	}

	cites := make([]askSessionCitationOut, 0, len(a.Citations))
	for _, c := range a.Citations {
		cite := askSessionCitationOut{
			CitationID: c.CitationID,
			ChunkID:    c.ChunkID,
			SourceType: c.SourceType,
			SourceID:   c.SourceID,
			Label:      c.Label,
			Excerpt:    c.Excerpt,
		}
		if cite.CitationID == "" {
			cite.CitationID = "C?"
		}
		if cite.Label == "" {
			cite.Label = c.SourceType + " " + c.ChunkID
		}
		if cite.Excerpt == "" {
			cite.Excerpt = c.Snippet
		}
		if c.Anchor != nil {
			cite.Anchor = mcpCitationAnchorToMap(c.Anchor)
		}
		nav := citation.ResolveCitationTarget(c, videoSources, materials, links, nil)
		cite.Navigation = &nav
		cites = append(cites, cite)
	}

	modelStr := ""
	if a.Model != nil {
		modelStr = *a.Model
	}

	automationRecommended := a.AnswerStatus == models.AnswerStatusAnswered &&
		float64(a.Confidence) >= mcpQAAutomationMinConfidence

	return askSessionQuestionOutput{
		SessionID:             sessionID.String(),
		QuestionID:            q.ID.String(),
		AnswerID:              a.ID.String(),
		AnswerText:            a.AnswerText,
		AnswerStatus:          string(a.AnswerStatus),
		Confidence:            float64(a.Confidence),
		AutomationRecommended: automationRecommended,
		Citations:             cites,
		LLMModel:              modelStr,
		QuestionSaved:         q.CreatedAt.UTC().Format(time.RFC3339),
		AnswerSaved:           a.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func mcpCitationAnchorToMap(a *models.CitationAnchor) map[string]interface{} {
	if a == nil {
		return nil
	}
	m := map[string]interface{}{"type": a.Type}
	if a.URL != "" {
		m["url"] = a.URL
	}
	if a.StartMs != nil {
		m["start_ms"] = *a.StartMs
	}
	if a.EndMs != nil {
		m["end_ms"] = *a.EndMs
	}
	if a.Page != nil {
		m["page"] = *a.Page
	}
	if a.Section != "" {
		m["section"] = a.Section
	}
	if a.Block != nil {
		m["block"] = *a.Block
	}
	return m
}

func mcpSessionChunksToUtilsChunks(chunks []models.SessionChunk) []utils.Chunk {
	out := make([]utils.Chunk, 0, len(chunks))
	for _, c := range chunks {
		locator := mcpFormatAnchorLocator(c.AnchorJSON)
		sourceID := ""
		if c.SourceID != nil {
			sourceID = c.SourceID.String()
		}
		out = append(out, utils.Chunk{
			ChunkID:    c.ID.String(),
			SourceType: c.SourceType,
			SourceID:   sourceID,
			Locator:    locator,
			Text:       c.Text,
		})
	}
	return out
}

func mcpFormatAnchorLocator(anchor map[string]interface{}) string {
	if anchor == nil {
		return ""
	}
	startMs, _ := anchor["start_ms"].(float64)
	endMs, _ := anchor["end_ms"].(float64)
	if startMs == 0 && endMs == 0 {
		if block, ok := anchor["block"].(float64); ok {
			return fmt.Sprintf("block %d", int(block))
		}
		return ""
	}
	sMin := int(startMs) / 60000
	sSec := (int(startMs) % 60000) / 1000
	eMin := int(endMs) / 60000
	eSec := (int(endMs) % 60000) / 1000
	return fmt.Sprintf("%d:%02d–%d:%02d", sMin, sSec, eMin, eSec)
}

func mcpBuildThreadPriorQA(ctx context.Context, db *database.DB, parentQuestionID *uuid.UUID) []utils.PriorQAPair {
	if parentQuestionID == nil {
		return nil
	}
	parentQ, err := db.GetQuestionByID(ctx, *parentQuestionID)
	if err != nil || parentQ == nil {
		return nil
	}
	parentA, err := db.GetLatestAnswerByQuestionID(ctx, parentQ.ID)
	if err != nil || parentA == nil {
		return nil
	}
	labels := make([]string, 0, len(parentA.Citations))
	for _, c := range parentA.Citations {
		if c.CitationID != "" && c.Label != "" {
			labels = append(labels, c.CitationID+": "+c.Label)
		}
	}
	out := []utils.PriorQAPair{{
		Question:       parentQ.QuestionText,
		Answer:         parentA.AnswerText,
		CitationLabels: labels,
	}}
	if parentQ.ParentQuestionID != nil {
		gpQ, _ := db.GetQuestionByID(ctx, *parentQ.ParentQuestionID)
		if gpQ != nil {
			gpA, _ := db.GetLatestAnswerByQuestionID(ctx, gpQ.ID)
			if gpA != nil {
				gpLabels := make([]string, 0, len(gpA.Citations))
				for _, c := range gpA.Citations {
					if c.CitationID != "" && c.Label != "" {
						gpLabels = append(gpLabels, c.CitationID+": "+c.Label)
					}
				}
				out = append([]utils.PriorQAPair{{
					Question:       gpQ.QuestionText,
					Answer:         gpA.AnswerText,
					CitationLabels: gpLabels,
				}}, out...)
			}
		}
	}
	return out
}

func mcpBuildPriorQA(questions []*models.Question, answers []*models.Answer, excludeQuestionID uuid.UUID) []utils.PriorQAPair {
	answerMap := make(map[uuid.UUID]*models.Answer)
	for _, a := range answers {
		answerMap[a.QuestionID] = a
	}
	var out []utils.PriorQAPair
	for _, q := range questions {
		if q.ID == excludeQuestionID {
			continue
		}
		if a, ok := answerMap[q.ID]; ok && a != nil {
			labels := make([]string, 0, len(a.Citations))
			for _, c := range a.Citations {
				if c.CitationID != "" && c.Label != "" {
					labels = append(labels, c.CitationID+": "+c.Label)
				}
			}
			out = append(out, utils.PriorQAPair{
				Question:       q.QuestionText,
				Answer:         a.AnswerText,
				CitationLabels: labels,
			})
		}
	}
	return out
}

func mcpSessionEmptyChunkMessage(ctx context.Context, db *database.DB, sessionID uuid.UUID) string {
	job, err := db.GetIngestionJobBySessionID(ctx, sessionID)
	if err != nil || job == nil {
		return ""
	}
	switch job.State {
	case "queued", "fetching":
		return "Transcript or content is still being imported. Please wait a moment and try again."
	}
	return ""
}

func mcpSessionHasIndexableContent(ctx context.Context, db *database.DB, sessionID uuid.UUID) bool {
	trans, err := db.GetTranscriptBySessionID(ctx, sessionID, "")
	if err == nil && trans != nil && trans.Status == models.TranscriptStatusReady {
		segments, err := db.ListSegmentsByTranscriptID(ctx, trans.ID)
		if err == nil && len(segments) > 0 {
			return true
		}
	}
	videoSources, err := db.GetVideoSourcesBySessionID(ctx, sessionID)
	if err == nil {
		for _, vs := range videoSources {
			if vs.TranscriptText != nil && *vs.TranscriptText != "" {
				return true
			}
			if len(vs.TranscriptSegments) > 0 {
				return true
			}
		}
	}
	links, err := db.GetVerifiedSessionLinksBySessionID(ctx, sessionID)
	if err == nil {
		for _, link := range links {
			if link.ExtractedText != nil && *link.ExtractedText != "" {
				return true
			}
		}
	}
	mats, err := db.GetActiveMaterialsBySessionID(ctx, sessionID)
	if err == nil {
		for _, m := range mats {
			if m.ExtractedText != nil && *m.ExtractedText != "" {
				return true
			}
		}
	}
	return false
}

const mcpTracePreviewLen = 400

func mcpTraceAskLookup(question string, chunks []models.SessionChunk) {
	log.Printf("[ASK_TRACE] question: %q", question)
	log.Printf("[ASK_TRACE] retrieved %d chunks", len(chunks))
	for i, c := range chunks {
		sourceID := ""
		if c.SourceID != nil {
			sourceID = c.SourceID.String()
		}
		url := ""
		if c.AnchorJSON != nil {
			if u, _ := c.AnchorJSON["url"].(string); u != "" {
				url = u
			}
		}
		preview := c.Text
		if len(preview) > mcpTracePreviewLen {
			preview = preview[:mcpTracePreviewLen] + "..."
		}
		log.Printf("[ASK_TRACE]   chunk %d: source_type=%s source_id=%s url=%s", i+1, c.SourceType, sourceID, url)
		log.Printf("[ASK_TRACE]     text_preview: %s", preview)
	}
}
