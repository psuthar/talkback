package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// ResendSender sends invite emails via Resend HTTP API.
type ResendSender struct {
	APIKey         string
	FromAddress    string
	FromName       string
	InviteSubject  string
}

// NewResendSender returns a Resend sender. FromAddress and FromName should come from config.
func NewResendSender(apiKey, fromAddress, fromName string) *ResendSender {
	if fromName == "" {
		fromName = "TalkBack"
	}
	return &ResendSender{
		APIKey:        apiKey,
		FromAddress:   fromAddress,
		FromName:      fromName,
		InviteSubject: "You've been invited to a TalkBack session",
	}
}

// SendInviteEmail sends the invitation email via Resend API.
func (r *ResendSender) SendInviteEmail(ctx context.Context, params SendInviteParams) error {
	if r.APIKey == "" {
		return fmt.Errorf("resend: RESEND_API_KEY not set")
	}
	body := r.buildBody(params)
	reqBody := map[string]interface{}{
		"from":    r.FromName + " <" + r.FromAddress + ">",
		"to":      []string{params.ToEmail},
		"subject": r.InviteSubject,
		"html":    body,
	}
	jb, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(jb))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("resend API returned %d", resp.StatusCode)
	}
	return nil
}

func (r *ResendSender) buildBody(p SendInviteParams) string {
	var buf bytes.Buffer
	buf.WriteString("<p>")
	buf.WriteString(escapeHTML(p.InviterName))
	buf.WriteString(" invited you to join the TalkBack session \"")
	buf.WriteString(escapeHTML(p.SessionTitle))
	buf.WriteString("\" as a ")
	buf.WriteString(escapeHTML(p.InvitedRole))
	buf.WriteString(".</p><p>TalkBack lets you review session content and ask questions asynchronously.</p><p>Accept your invitation using the secure link below.</p>")
	buf.WriteString("<p><a href=\"")
	buf.WriteString(escapeHTML(p.AcceptURL))
	buf.WriteString("\" style=\"display:inline-block;padding:12px 24px;background:#2563eb;color:#fff;text-decoration:none;border-radius:6px;\">Accept invitation</a></p>")
	if p.ExpirationNote != "" {
		buf.WriteString("<p><small>")
		buf.WriteString(escapeHTML(p.ExpirationNote))
		buf.WriteString("</small></p>")
	}
	return buf.String()
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}
