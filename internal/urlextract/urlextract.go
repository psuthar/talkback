package urlextract

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	DefaultTimeout    = 15 * time.Second
	DefaultMaxBytes   = 2 * 1024 * 1024 // 2MB
	DefaultMaxRedirects = 5
)

// ValidateAndNormalizeURL returns a normalized URL if it is http or https; otherwise an error.
func ValidateAndNormalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("url is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("only http and https URLs are allowed")
	}
	if u.Host == "" {
		return "", fmt.Errorf("invalid url: missing host")
	}
	u.Fragment = ""
	u.RawQuery = ""
	return u.String(), nil
}

// FetchResult holds the result of fetching and extracting content from a URL.
type FetchResult struct {
	Title   string // page title from <title> or first <h1>
	Body    string // main text extracted from body
	FinalURL string // after redirects
}

// FetchAndExtract fetches the URL (with timeout and redirect limit), parses HTML, and extracts title and body text.
func FetchAndExtract(ctx context.Context, rawURL string, maxBytes int64) (*FetchResult, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	client := &http.Client{
		Timeout: DefaultTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= DefaultMaxRedirects {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	// Use a common browser User-Agent so sites (e.g. Wikipedia) serve full HTML rather than minimal or bot-specific content
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("url returned status %d", resp.StatusCode)
	}
	ct := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if ct != "" && !strings.Contains(ct, "text/html") && !strings.Contains(ct, "text/plain") {
		return nil, fmt.Errorf("unsupported content-type: %s", resp.Header.Get("Content-Type"))
	}
	body := io.LimitReader(resp.Body, maxBytes)
	raw, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	finalURL := resp.Request.URL.String()
	title, bodyText := extractFromHTML(raw)
	// Ensure valid UTF-8 for PostgreSQL and downstream (some pages serve Latin-1 or have invalid sequences)
	title = strings.ToValidUTF8(title, "\uFFFD")
	bodyText = strings.ToValidUTF8(bodyText, "\uFFFD")
	return &FetchResult{Title: title, Body: bodyText, FinalURL: finalURL}, nil
}

func extractFromHTML(raw []byte) (title, bodyText string) {
	doc, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return "", ""
	}
	title = findTitle(doc)
	bodyNode := findBody(doc)
	if bodyNode == nil {
		bodyNode = doc
	}
	// Prefer main article content (e.g. Wikipedia #mw-content-text) so we index the page body, not nav/sidebar
	mainNode := findMainContent(doc)
	extractNode := mainNode
	if mainNode == nil {
		extractNode = bodyNode
	}
	var buf strings.Builder
	collectText(extractNode, &buf)
	bodyText = strings.TrimSpace(buf.String())
	bodyText = normalizeSpace(bodyText)
	// If main content yielded very little, fall back to full body (e.g. site uses different structure)
	const minMainLen = 300
	if mainNode != nil && len(bodyText) < minMainLen {
		var fullBuf strings.Builder
		collectText(bodyNode, &fullBuf)
		fullBody := strings.TrimSpace(normalizeSpace(fullBuf.String()))
		if len(fullBody) > len(bodyText) {
			bodyText = fullBody
		}
	}
	return title, bodyText
}

func findTitle(doc *html.Node) string {
	var walk func(*html.Node) string
	walk = func(n *html.Node) string {
		if n.Type == html.ElementNode && n.Data == "title" {
			if n.FirstChild != nil {
				return strings.TrimSpace(collectTextSingle(n))
			}
			return ""
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if s := walk(c); s != "" {
				return s
			}
		}
		return ""
	}
	return walk(doc)
}

func findBody(doc *html.Node) *html.Node {
	var body *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "body" {
			body = n
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
			if body != nil {
				return
			}
		}
	}
	walk(doc)
	return body
}

// mainContentIDs are element IDs commonly used for the main article body (Wikipedia, etc.)
var mainContentIDs = map[string]bool{
	"mw-content-text": true, // Wikipedia
	"content":         true,
	"main-content":    true,
	"bodyContent":     true,
	"article-body":   true,
	"post-content":    true,
	"entry-content":   true,
}

func getAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// findMainContent returns a node that likely contains the main article text, or nil.
func findMainContent(doc *html.Node) *html.Node {
	var found *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found != nil {
			return
		}
		if n.Type == html.ElementNode {
			if id := getAttr(n, "id"); mainContentIDs[strings.TrimSpace(id)] {
				found = n
				return
			}
			switch n.Data {
			case "main", "article":
				found = n
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
			if found != nil {
				return
			}
		}
	}
	walk(doc)
	return found
}

func collectText(n *html.Node, buf *strings.Builder) {
	if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style" || n.Data == "noscript") {
		return
	}
	if n.Type == html.TextNode {
		buf.WriteString(n.Data)
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		collectText(c, buf)
	}
}

func collectTextSingle(n *html.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
	}
	return b.String()
}

func normalizeSpace(s string) string {
	var out strings.Builder
	lastSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if !lastSpace {
				out.WriteRune(' ')
				lastSpace = true
			}
			continue
		}
		lastSpace = false
		out.WriteRune(r)
	}
	return strings.TrimSpace(out.String())
}
