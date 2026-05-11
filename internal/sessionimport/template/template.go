// Package template defines the v1 session-template schema and a pure-function
// validator. It is consumed by SCRUM-352 (preflight URL fetching) and
// SCRUM-357 (async import worker).
//
// The validator is I/O-free: it does not fetch URLs, hit the database, or
// touch storage. URL accessibility is the preflight stage's job; this package
// only enforces the structural / type contract.
//
// See docs/specs/session-templates-v1.md for the canonical schema.
package template

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
)

// Version is the only supported template descriptor version in v1.
const Version = 1

// Limits are the parse-time / validator-enforced caps. Preflight (SCRUM-352)
// and the worker (SCRUM-357) enforce additional runtime limits per their own
// docstrings.
const (
	MaxDescriptorBytes = 256 * 1024
	MaxElements        = 100
	MaxTitleLen        = 512
	MaxElementTitleLen = 256
)

// elementIDPattern matches the stable element-ID format from the spec.
var elementIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// Reason codes — keep in sync with docs/specs/session-templates-v1.md.
const (
	ReasonUnknownField                  = "unknown_field"
	ReasonMissingRequired               = "missing_required"
	ReasonVersionUnsupported            = "version_unsupported"
	ReasonTitleTooLong                  = "title_too_long"
	ReasonElementIDInvalid              = "element_id_invalid"
	ReasonElementIDDuplicate            = "element_id_duplicate"
	ReasonElementKindUnknown            = "element_kind_unknown"
	ReasonMaterialContentTypeUnsupported = "material_content_type_unsupported"
	ReasonURLSchemeInvalid              = "url_scheme_invalid"
	ReasonTranscriptSourceDuplicate     = "transcript_source_duplicate"
	ReasonPrimaryRefUnknown             = "primary_ref_unknown"
	ReasonPrimaryKindMismatch           = "primary_kind_mismatch"
	ReasonVideoSourceURLMissing         = "video_source_url_missing"
	ReasonTranscriptURLMissing          = "transcript_url_missing"
	ReasonProviderUnsupported           = "provider_unsupported"
	ReasonDescriptorTooLarge            = "descriptor_too_large"
	ReasonTooManyElements               = "too_many_elements"
)

// Allowed material content types per spec.
var materialContentTypes = map[string]bool{
	"application/pdf":                                                              true,
	"application/vnd.openxmlformats-officedocument.presentationml.presentation":    true,
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":      true,
	"text/csv":                            true,
	"text/plain":                          true,
	"application/vnd.ms-excel":            true,
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": true,
	"image/png":  true,
	"image/jpeg": true,
}

// Allowed video_source providers — must match the
// video_sources_provider_check constraint exactly.
var videoSourceProviders = map[string]bool{
	"zoom":        true,
	"other":       true,
	"teams":       true,
	"google_meet": true,
}

// Element kinds.
const (
	KindMaterial     = "material"
	KindLink         = "link"
	KindVideoSource  = "video_source"
	KindTranscript   = "transcript"
)

// Primary content kinds (mirror models.SessionPrimaryContentKind values).
const (
	PrimaryKindDocument = "document"
	PrimaryKindLink     = "link"
	PrimaryKindVideo    = "video"
)

// primaryKindToElementKind maps a primary.kind to the elements[].kind it must
// reference. Mismatch is ReasonPrimaryKindMismatch.
var primaryKindToElementKind = map[string]string{
	PrimaryKindDocument: KindMaterial,
	PrimaryKindLink:     KindLink,
	PrimaryKindVideo:    KindVideoSource,
}

// Template is the parsed v1 descriptor.
type Template struct {
	Version            int       `json:"version"`
	Title              string    `json:"title"`
	Premise            *string   `json:"premise,omitempty"`
	PrimaryDecision    *string   `json:"primary_decision,omitempty"`
	DecisionOutcome    *string   `json:"decision_outcome,omitempty"`
	SourceReferenceURL *string   `json:"source_reference_url,omitempty"`
	Elements           []Element `json:"elements"`
	Primary            *Primary  `json:"primary,omitempty"`
}

// Element is the union of all element kinds. Only fields relevant to the
// declared kind should be populated; the validator enforces which are
// required per kind.
type Element struct {
	Kind            string  `json:"kind"`
	ID              string  `json:"id"`
	Title           *string `json:"title,omitempty"`
	URL             *string `json:"url,omitempty"`             // material, link
	ContentType     *string `json:"content_type,omitempty"`    // material
	EmbedURL        *string `json:"embed_url,omitempty"`       // video_source
	VideoURL        *string `json:"video_url,omitempty"`       // video_source
	DurationSeconds *int    `json:"duration_seconds,omitempty"`// video_source
	Provider        *string `json:"provider,omitempty"`        // video_source (default "other")
	Source          *string `json:"source,omitempty"`          // transcript
	TextURL         *string `json:"text_url,omitempty"`        // transcript
	SegmentsURL     *string `json:"segments_url,omitempty"`    // transcript
}

// Primary is the optional explicit primary-content pointer.
type Primary struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}

// ValidationError is one structured error from the validator. Errors are
// accumulated, not short-circuited, so callers can present all problems in
// one round-trip.
type ValidationError struct {
	ElementID    string `json:"element_id,omitempty"`
	DeclaredType string `json:"declared_type,omitempty"`
	ObservedType string `json:"observed_type,omitempty"` // populated by preflight, not the schema validator
	ReasonCode   string `json:"reason_code"`
	Message      string `json:"message"`
}

// ParseAndValidate parses the JSON descriptor and runs the v1 validator.
// On success returns the parsed Template and an empty error slice. On
// failure returns nil and a non-empty error slice.
//
// The function is pure: no I/O, no DB, no fetches.
func ParseAndValidate(jsonBytes []byte) (*Template, []ValidationError) {
	if len(jsonBytes) > MaxDescriptorBytes {
		return nil, []ValidationError{{
			ReasonCode: ReasonDescriptorTooLarge,
			Message:    fmt.Sprintf("template descriptor exceeds %d bytes", MaxDescriptorBytes),
		}}
	}
	var t Template
	dec := json.NewDecoder(bytes.NewReader(jsonBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		return nil, []ValidationError{translateDecodeError(err)}
	}
	errs := validate(&t)
	if len(errs) > 0 {
		return nil, errs
	}
	return &t, nil
}

func translateDecodeError(err error) ValidationError {
	msg := err.Error()
	// json.Decoder DisallowUnknownFields error format:
	//   "json: unknown field \"foo\""
	// We surface that as ReasonUnknownField for the API contract.
	if startsWith(msg, `json: unknown field`) {
		return ValidationError{ReasonCode: ReasonUnknownField, Message: msg}
	}
	return ValidationError{ReasonCode: ReasonMissingRequired, Message: "invalid JSON: " + msg}
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func validate(t *Template) []ValidationError {
	var errs []ValidationError

	// Version
	if t.Version != Version {
		errs = append(errs, ValidationError{
			ReasonCode: ReasonVersionUnsupported,
			Message:    fmt.Sprintf("version must be %d, got %d", Version, t.Version),
		})
	}

	// Title
	if t.Title == "" {
		errs = append(errs, ValidationError{
			ReasonCode: ReasonMissingRequired,
			Message:    "title is required",
		})
	} else if len(t.Title) > MaxTitleLen {
		errs = append(errs, ValidationError{
			ReasonCode: ReasonTitleTooLong,
			Message:    fmt.Sprintf("title exceeds %d chars", MaxTitleLen),
		})
	}

	// source_reference_url scheme
	if t.SourceReferenceURL != nil {
		if !isHTTPSURL(*t.SourceReferenceURL) {
			errs = append(errs, ValidationError{
				ReasonCode: ReasonURLSchemeInvalid,
				Message:    "source_reference_url must be https://",
			})
		}
	}

	// Elements
	if len(t.Elements) > MaxElements {
		errs = append(errs, ValidationError{
			ReasonCode: ReasonTooManyElements,
			Message:    fmt.Sprintf("too many elements: %d (max %d)", len(t.Elements), MaxElements),
		})
	}
	seenIDs := map[string]bool{}
	seenTranscriptSources := map[string]bool{}
	elementsByID := map[string]*Element{}
	for i := range t.Elements {
		el := &t.Elements[i]
		errs = append(errs, validateElement(el, seenIDs, seenTranscriptSources)...)
		if el.ID != "" {
			elementsByID[el.ID] = el
		}
	}

	// Primary pointer
	if t.Primary != nil {
		errs = append(errs, validatePrimary(t.Primary, elementsByID)...)
	}

	return errs
}

func validateElement(el *Element, seenIDs, seenTranscriptSources map[string]bool) []ValidationError {
	var errs []ValidationError

	// id
	if el.ID == "" {
		errs = append(errs, ValidationError{
			ReasonCode: ReasonMissingRequired,
			Message:    "element id is required",
		})
	} else if !elementIDPattern.MatchString(el.ID) {
		errs = append(errs, ValidationError{
			ElementID:  el.ID,
			ReasonCode: ReasonElementIDInvalid,
			Message:    "element id must match ^[a-zA-Z0-9_-]{1,64}$",
		})
	} else if seenIDs[el.ID] {
		errs = append(errs, ValidationError{
			ElementID:  el.ID,
			ReasonCode: ReasonElementIDDuplicate,
			Message:    "element id is duplicated",
		})
	} else {
		seenIDs[el.ID] = true
	}

	// kind
	switch el.Kind {
	case KindMaterial:
		errs = append(errs, validateMaterial(el)...)
	case KindLink:
		errs = append(errs, validateLink(el)...)
	case KindVideoSource:
		errs = append(errs, validateVideoSource(el)...)
	case KindTranscript:
		errs = append(errs, validateTranscript(el, seenTranscriptSources)...)
	default:
		errs = append(errs, ValidationError{
			ElementID:  el.ID,
			ReasonCode: ReasonElementKindUnknown,
			Message:    fmt.Sprintf("unknown element kind %q", el.Kind),
		})
	}

	// title length (when present)
	if el.Title != nil && len(*el.Title) > MaxElementTitleLen {
		errs = append(errs, ValidationError{
			ElementID:  el.ID,
			ReasonCode: ReasonTitleTooLong,
			Message:    fmt.Sprintf("element title exceeds %d chars", MaxElementTitleLen),
		})
	}

	return errs
}

func validateMaterial(el *Element) []ValidationError {
	var errs []ValidationError
	if el.Title == nil || *el.Title == "" {
		errs = append(errs, ValidationError{ElementID: el.ID, ReasonCode: ReasonMissingRequired, Message: "material.title is required"})
	}
	if el.URL == nil || *el.URL == "" {
		errs = append(errs, ValidationError{ElementID: el.ID, ReasonCode: ReasonMissingRequired, Message: "material.url is required"})
	} else if !isHTTPSURL(*el.URL) {
		errs = append(errs, ValidationError{ElementID: el.ID, ReasonCode: ReasonURLSchemeInvalid, Message: "material.url must be https://"})
	}
	if el.ContentType == nil || *el.ContentType == "" {
		errs = append(errs, ValidationError{ElementID: el.ID, ReasonCode: ReasonMissingRequired, Message: "material.content_type is required"})
	} else if !materialContentTypes[*el.ContentType] {
		errs = append(errs, ValidationError{
			ElementID:    el.ID,
			DeclaredType: *el.ContentType,
			ReasonCode:   ReasonMaterialContentTypeUnsupported,
			Message:      fmt.Sprintf("material.content_type %q is not in the v1 allowlist", *el.ContentType),
		})
	}
	return errs
}

func validateLink(el *Element) []ValidationError {
	var errs []ValidationError
	if el.URL == nil || *el.URL == "" {
		errs = append(errs, ValidationError{ElementID: el.ID, ReasonCode: ReasonMissingRequired, Message: "link.url is required"})
	} else if !isHTTPSURL(*el.URL) {
		errs = append(errs, ValidationError{ElementID: el.ID, ReasonCode: ReasonURLSchemeInvalid, Message: "link.url must be https://"})
	}
	return errs
}

func validateVideoSource(el *Element) []ValidationError {
	var errs []ValidationError
	hasURL := (el.EmbedURL != nil && *el.EmbedURL != "") || (el.VideoURL != nil && *el.VideoURL != "")
	if !hasURL {
		errs = append(errs, ValidationError{ElementID: el.ID, ReasonCode: ReasonVideoSourceURLMissing, Message: "video_source requires embed_url or video_url"})
	}
	if el.EmbedURL != nil && *el.EmbedURL != "" && !isHTTPSURL(*el.EmbedURL) {
		errs = append(errs, ValidationError{ElementID: el.ID, ReasonCode: ReasonURLSchemeInvalid, Message: "video_source.embed_url must be https://"})
	}
	if el.VideoURL != nil && *el.VideoURL != "" && !isHTTPSURL(*el.VideoURL) {
		errs = append(errs, ValidationError{ElementID: el.ID, ReasonCode: ReasonURLSchemeInvalid, Message: "video_source.video_url must be https://"})
	}
	if el.Provider != nil && *el.Provider != "" && !videoSourceProviders[*el.Provider] {
		errs = append(errs, ValidationError{ElementID: el.ID, ReasonCode: ReasonProviderUnsupported, Message: fmt.Sprintf("video_source.provider %q is not in the v1 allowlist", *el.Provider)})
	}
	return errs
}

func validateTranscript(el *Element, seenSources map[string]bool) []ValidationError {
	var errs []ValidationError
	if el.Source == nil || *el.Source == "" {
		errs = append(errs, ValidationError{ElementID: el.ID, ReasonCode: ReasonMissingRequired, Message: "transcript.source is required"})
	} else if seenSources[*el.Source] {
		errs = append(errs, ValidationError{ElementID: el.ID, ReasonCode: ReasonTranscriptSourceDuplicate, Message: fmt.Sprintf("transcript.source %q is duplicated within the template", *el.Source)})
	} else {
		seenSources[*el.Source] = true
	}
	hasURL := (el.TextURL != nil && *el.TextURL != "") || (el.SegmentsURL != nil && *el.SegmentsURL != "")
	if !hasURL {
		errs = append(errs, ValidationError{ElementID: el.ID, ReasonCode: ReasonTranscriptURLMissing, Message: "transcript requires text_url or segments_url"})
	}
	if el.TextURL != nil && *el.TextURL != "" && !isHTTPSURL(*el.TextURL) {
		errs = append(errs, ValidationError{ElementID: el.ID, ReasonCode: ReasonURLSchemeInvalid, Message: "transcript.text_url must be https://"})
	}
	if el.SegmentsURL != nil && *el.SegmentsURL != "" && !isHTTPSURL(*el.SegmentsURL) {
		errs = append(errs, ValidationError{ElementID: el.ID, ReasonCode: ReasonURLSchemeInvalid, Message: "transcript.segments_url must be https://"})
	}
	return errs
}

func validatePrimary(p *Primary, elementsByID map[string]*Element) []ValidationError {
	var errs []ValidationError
	if p.Kind == "" {
		errs = append(errs, ValidationError{ReasonCode: ReasonMissingRequired, Message: "primary.kind is required"})
		return errs
	}
	wantElementKind, ok := primaryKindToElementKind[p.Kind]
	if !ok {
		errs = append(errs, ValidationError{ReasonCode: ReasonPrimaryKindMismatch, Message: fmt.Sprintf("primary.kind %q is not one of document/link/video", p.Kind)})
		return errs
	}
	if p.Ref == "" {
		errs = append(errs, ValidationError{ReasonCode: ReasonMissingRequired, Message: "primary.ref is required"})
		return errs
	}
	target, ok := elementsByID[p.Ref]
	if !ok {
		errs = append(errs, ValidationError{ReasonCode: ReasonPrimaryRefUnknown, Message: fmt.Sprintf("primary.ref %q does not match any element", p.Ref)})
		return errs
	}
	if target.Kind != wantElementKind {
		errs = append(errs, ValidationError{
			ElementID:  p.Ref,
			ReasonCode: ReasonPrimaryKindMismatch,
			Message:    fmt.Sprintf("primary.kind=%q expects element kind %q but ref points to %q", p.Kind, wantElementKind, target.Kind),
		})
	}
	return errs
}

func isHTTPSURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return u.Scheme == "https" && u.Host != ""
}
