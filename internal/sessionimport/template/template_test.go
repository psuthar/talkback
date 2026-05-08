package template

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SCRUM-351: schema + validator.
//
// The validator is pure (no I/O). These tests cover happy path + every reason
// code in the catalog. Preflight-only reason codes (url_unreachable, etc.)
// are NOT tested here — they belong to SCRUM-352.

func TestParseAndValidate_HappyPath_FullExample(t *testing.T) {
	t.Parallel()
	body := []byte(`{
		"version": 1,
		"title": "Weekly Decision Review — Template",
		"premise": "Are we on track?",
		"primary_decision": "Ship / hold / extend",
		"source_reference_url": "https://templates.example.com/weekly",
		"elements": [
			{"kind":"material","id":"agenda","title":"Agenda","url":"https://example.com/a.pdf","content_type":"application/pdf"},
			{"kind":"link","id":"policy","title":"Policy","url":"https://wiki.example.com/policy"},
			{"kind":"video_source","id":"rec","embed_url":"https://recordings.example.com/embed/abc"},
			{"kind":"transcript","id":"tx","source":"whisper","text_url":"https://example.com/t.txt"}
		],
		"primary": {"kind":"document","ref":"agenda"}
	}`)
	tmpl, errs := ParseAndValidate(body)
	require.Empty(t, errs, "happy path must validate cleanly: %+v", errs)
	require.NotNil(t, tmpl)
	assert.Equal(t, 1, tmpl.Version)
	assert.Equal(t, "Weekly Decision Review — Template", tmpl.Title)
	assert.Len(t, tmpl.Elements, 4)
	require.NotNil(t, tmpl.Primary)
	assert.Equal(t, "document", tmpl.Primary.Kind)
	assert.Equal(t, "agenda", tmpl.Primary.Ref)
}

func TestParseAndValidate_VersionUnsupported(t *testing.T) {
	t.Parallel()
	body := []byte(`{"version": 2, "title": "x", "elements": []}`)
	_, errs := ParseAndValidate(body)
	require.NotEmpty(t, errs)
	assert.Equal(t, ReasonVersionUnsupported, errs[0].ReasonCode)
}

func TestParseAndValidate_UnknownTopLevelField(t *testing.T) {
	t.Parallel()
	body := []byte(`{"version":1,"title":"x","elements":[],"future_field":"oops"}`)
	_, errs := ParseAndValidate(body)
	require.NotEmpty(t, errs)
	assert.Equal(t, ReasonUnknownField, errs[0].ReasonCode)
}

func TestParseAndValidate_UnknownElementField(t *testing.T) {
	t.Parallel()
	body := []byte(`{"version":1,"title":"x","elements":[
		{"kind":"link","id":"l1","url":"https://example.com","oops":"future"}
	]}`)
	_, errs := ParseAndValidate(body)
	require.NotEmpty(t, errs)
	assert.Equal(t, ReasonUnknownField, errs[0].ReasonCode)
}

func TestParseAndValidate_TitleMissing(t *testing.T) {
	t.Parallel()
	body := []byte(`{"version":1,"elements":[]}`)
	_, errs := ParseAndValidate(body)
	require.NotEmpty(t, errs)
	assert.True(t, hasReason(errs, ReasonMissingRequired))
}

func TestParseAndValidate_TitleTooLong(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a", MaxTitleLen+1)
	body := []byte(`{"version":1,"title":"` + long + `","elements":[]}`)
	_, errs := ParseAndValidate(body)
	require.NotEmpty(t, errs)
	assert.True(t, hasReason(errs, ReasonTitleTooLong))
}

func TestParseAndValidate_DescriptorTooLarge(t *testing.T) {
	t.Parallel()
	body := make([]byte, MaxDescriptorBytes+1)
	for i := range body {
		body[i] = ' '
	}
	_, errs := ParseAndValidate(body)
	require.NotEmpty(t, errs)
	assert.Equal(t, ReasonDescriptorTooLarge, errs[0].ReasonCode)
}

func TestParseAndValidate_TooManyElements(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString(`{"version":1,"title":"x","elements":[`)
	for i := 0; i < MaxElements+1; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"kind":"link","id":"l`)
		b.WriteString(itoa(i))
		b.WriteString(`","url":"https://example.com"}`)
	}
	b.WriteString(`]}`)
	_, errs := ParseAndValidate([]byte(b.String()))
	require.NotEmpty(t, errs)
	assert.True(t, hasReason(errs, ReasonTooManyElements))
}

func TestParseAndValidate_ElementIDInvalid(t *testing.T) {
	t.Parallel()
	body := []byte(`{"version":1,"title":"x","elements":[{"kind":"link","id":"has spaces","url":"https://example.com"}]}`)
	_, errs := ParseAndValidate(body)
	require.NotEmpty(t, errs)
	assert.True(t, hasReason(errs, ReasonElementIDInvalid))
}

func TestParseAndValidate_ElementIDDuplicate(t *testing.T) {
	t.Parallel()
	body := []byte(`{"version":1,"title":"x","elements":[
		{"kind":"link","id":"dup","url":"https://example.com"},
		{"kind":"link","id":"dup","url":"https://example.com"}
	]}`)
	_, errs := ParseAndValidate(body)
	require.NotEmpty(t, errs)
	assert.True(t, hasReason(errs, ReasonElementIDDuplicate))
}

func TestParseAndValidate_ElementKindUnknown(t *testing.T) {
	t.Parallel()
	body := []byte(`{"version":1,"title":"x","elements":[{"kind":"emoji","id":"e1"}]}`)
	_, errs := ParseAndValidate(body)
	require.NotEmpty(t, errs)
	assert.True(t, hasReason(errs, ReasonElementKindUnknown))
}

func TestParseAndValidate_MaterialContentTypeUnsupported(t *testing.T) {
	t.Parallel()
	body := []byte(`{"version":1,"title":"x","elements":[
		{"kind":"material","id":"m","title":"M","url":"https://example.com","content_type":"application/x-bogus"}
	]}`)
	_, errs := ParseAndValidate(body)
	require.NotEmpty(t, errs)
	assert.True(t, hasReason(errs, ReasonMaterialContentTypeUnsupported))
}

func TestParseAndValidate_URLSchemeInvalid_HTTP(t *testing.T) {
	t.Parallel()
	body := []byte(`{"version":1,"title":"x","elements":[
		{"kind":"link","id":"l","url":"http://example.com"}
	]}`)
	_, errs := ParseAndValidate(body)
	require.NotEmpty(t, errs)
	assert.True(t, hasReason(errs, ReasonURLSchemeInvalid))
}

func TestParseAndValidate_URLSchemeInvalid_FTP(t *testing.T) {
	t.Parallel()
	body := []byte(`{"version":1,"title":"x","elements":[
		{"kind":"link","id":"l","url":"ftp://example.com/file"}
	]}`)
	_, errs := ParseAndValidate(body)
	require.NotEmpty(t, errs)
	assert.True(t, hasReason(errs, ReasonURLSchemeInvalid))
}

func TestParseAndValidate_VideoSourceMissingURL(t *testing.T) {
	t.Parallel()
	body := []byte(`{"version":1,"title":"x","elements":[{"kind":"video_source","id":"v"}]}`)
	_, errs := ParseAndValidate(body)
	require.NotEmpty(t, errs)
	assert.True(t, hasReason(errs, ReasonVideoSourceURLMissing))
}

func TestParseAndValidate_VideoSourceProviderUnsupported(t *testing.T) {
	t.Parallel()
	body := []byte(`{"version":1,"title":"x","elements":[
		{"kind":"video_source","id":"v","embed_url":"https://example.com/embed","provider":"vimeo"}
	]}`)
	_, errs := ParseAndValidate(body)
	require.NotEmpty(t, errs)
	assert.True(t, hasReason(errs, ReasonProviderUnsupported))
}

func TestParseAndValidate_TranscriptMissingURL(t *testing.T) {
	t.Parallel()
	body := []byte(`{"version":1,"title":"x","elements":[{"kind":"transcript","id":"t","source":"whisper"}]}`)
	_, errs := ParseAndValidate(body)
	require.NotEmpty(t, errs)
	assert.True(t, hasReason(errs, ReasonTranscriptURLMissing))
}

func TestParseAndValidate_TranscriptSourceDuplicate(t *testing.T) {
	t.Parallel()
	body := []byte(`{"version":1,"title":"x","elements":[
		{"kind":"transcript","id":"t1","source":"whisper","text_url":"https://example.com/a.txt"},
		{"kind":"transcript","id":"t2","source":"whisper","text_url":"https://example.com/b.txt"}
	]}`)
	_, errs := ParseAndValidate(body)
	require.NotEmpty(t, errs)
	assert.True(t, hasReason(errs, ReasonTranscriptSourceDuplicate))
}

func TestParseAndValidate_PrimaryRefUnknown(t *testing.T) {
	t.Parallel()
	body := []byte(`{"version":1,"title":"x","elements":[
		{"kind":"link","id":"l","url":"https://example.com"}
	],"primary":{"kind":"document","ref":"missing"}}`)
	_, errs := ParseAndValidate(body)
	require.NotEmpty(t, errs)
	assert.True(t, hasReason(errs, ReasonPrimaryRefUnknown))
}

func TestParseAndValidate_PrimaryKindMismatch(t *testing.T) {
	t.Parallel()
	// primary kind=document but ref points to a link
	body := []byte(`{"version":1,"title":"x","elements":[
		{"kind":"link","id":"l","url":"https://example.com"}
	],"primary":{"kind":"document","ref":"l"}}`)
	_, errs := ParseAndValidate(body)
	require.NotEmpty(t, errs)
	assert.True(t, hasReason(errs, ReasonPrimaryKindMismatch))
}

func TestParseAndValidate_PrimaryKindInvalid(t *testing.T) {
	t.Parallel()
	body := []byte(`{"version":1,"title":"x","elements":[
		{"kind":"link","id":"l","url":"https://example.com"}
	],"primary":{"kind":"unicorn","ref":"l"}}`)
	_, errs := ParseAndValidate(body)
	require.NotEmpty(t, errs)
	assert.True(t, hasReason(errs, ReasonPrimaryKindMismatch))
}

func TestParseAndValidate_AccumulatesMultipleErrors(t *testing.T) {
	t.Parallel()
	// Title missing + bad version + invalid element id + duplicate id
	body := []byte(`{"version":2,"elements":[
		{"kind":"link","id":"bad id","url":"https://example.com"},
		{"kind":"link","id":"l","url":"http://example.com"},
		{"kind":"link","id":"l","url":"https://example.com"}
	]}`)
	_, errs := ParseAndValidate(body)
	require.NotEmpty(t, errs)
	// We expect at least: version_unsupported, missing_required (title),
	// element_id_invalid, url_scheme_invalid, element_id_duplicate.
	assert.True(t, hasReason(errs, ReasonVersionUnsupported))
	assert.True(t, hasReason(errs, ReasonMissingRequired))
	assert.True(t, hasReason(errs, ReasonElementIDInvalid))
	assert.True(t, hasReason(errs, ReasonURLSchemeInvalid))
	assert.True(t, hasReason(errs, ReasonElementIDDuplicate))
	// Validator must accumulate, not short-circuit.
	assert.GreaterOrEqual(t, len(errs), 5)
}

// --- helpers ---

func hasReason(errs []ValidationError, reason string) bool {
	for _, e := range errs {
		if e.ReasonCode == reason {
			return true
		}
	}
	return false
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
