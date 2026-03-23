// Package msgraph provides a minimal Microsoft Graph client for Teams meeting recordings (MVP).
package msgraph

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	graphV1  = "https://graph.microsoft.com/v1.0"
	graphBeta = "https://graph.microsoft.com/beta"
)

// GraphID escapes opaque Graph resource ids for URL path segments (slashes → %2F).
func GraphID(id string) string {
	return strings.ReplaceAll(strings.TrimSpace(id), "/", "%2F")
}

// TeamsOneDriveMeetingID is stored as meeting_id on jobs that import from OneDrive instead of Graph onlineMeetings.
// RecordingID is the drive item id (GET /me/drive/items/{id}).
const TeamsOneDriveMeetingID = "onedrive"

// RecordingListItem is one Teams recording the UI can import (flattened from Graph).
type RecordingListItem struct {
	MeetingID   string `json:"meeting_id"`
	RecordingID string `json:"recording_id"`
	Subject     string `json:"subject"`
	StartTime   string `json:"start_time,omitempty"`
	// Source is "graph" (default) when from onlineMeetings API, or "onedrive" for files under Microsoft Teams Recordings.
	Source string `json:"source,omitempty"`
}

// IsTeamsOneDriveMeeting reports whether meetingID refers to a OneDrive-backed import.
func IsTeamsOneDriveMeeting(meetingID string) bool {
	return strings.EqualFold(strings.TrimSpace(meetingID), TeamsOneDriveMeetingID)
}

// RecordingDetail is returned by GetRecording for download + transcript resolution.
type RecordingDetail struct {
	ID                  string
	Subject             string
	RecordingContentURL string
}

// oDataEscapeString escapes single quotes for OData string literals in $filter.
func oDataEscapeString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// Teams join links often appear only in the event body (chat / Meet now); onlineMeeting.joinUrl may be missing.
var teamsJoinURLRegexes = []*regexp.Regexp{
	regexp.MustCompile(`https://teams\.microsoft\.com/l/meetup-join/[^"'\s<>]+`),
	regexp.MustCompile(`https://teams\.microsoft\.com/meet/[^"'\s<>]+`),
	regexp.MustCompile(`https://teams\.live\.com/meet/[^"'\s<>]+`),
}

func htmlUnescapeMinimal(s string) string {
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&#58;", ":")
	return s
}

func trimTeamsJoinURLSuffix(u string) string {
	u = strings.TrimSpace(u)
	for {
		trimmed := strings.TrimRight(u, ".,;)]}\"'")
		if trimmed == u {
			break
		}
		u = strings.TrimSpace(trimmed)
	}
	return u
}

func extractTeamsJoinURLsFromText(s string) []string {
	s = htmlUnescapeMinimal(s)
	if s == "" {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	for _, re := range teamsJoinURLRegexes {
		for _, m := range re.FindAllString(s, -1) {
			u := trimTeamsJoinURLSuffix(m)
			if u == "" {
				continue
			}
			if _, ok := seen[u]; ok {
				continue
			}
			seen[u] = struct{}{}
			out = append(out, u)
		}
	}
	return out
}

// joinURLsFromCalendarEvent returns Teams join URLs from structured fields and from body/location (HTML-safe).
func joinURLsFromCalendarEvent(ev calendarEventRecord) []string {
	seen := make(map[string]struct{})
	var out []string
	push := func(raw string) {
		u := trimTeamsJoinURLSuffix(raw)
		if u == "" {
			return
		}
		if _, ok := seen[u]; ok {
			return
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	if ev.OnlineMeeting != nil {
		push(ev.OnlineMeeting.JoinURL)
	}
	var blob string
	if ev.Body != nil {
		blob += ev.Body.Content
	}
	if ev.Location != nil {
		blob += " " + ev.Location.DisplayName
	}
	blob += " " + ev.WebLink
	for _, u := range extractTeamsJoinURLsFromText(blob) {
		push(u)
	}
	return out
}

type calendarEventRecord struct {
	Subject         string `json:"subject"`
	Body            *struct {
		Content     string `json:"content"`
		ContentType string `json:"contentType"`
	} `json:"body"`
	Location *struct {
		DisplayName string `json:"displayName"`
	} `json:"location"`
	WebLink         string `json:"webLink"`
	Start           *struct{ DateTime string `json:"dateTime"` } `json:"start"`
	IsOnlineMeeting bool   `json:"isOnlineMeeting"`
	OnlineMeeting   *struct {
		JoinURL string `json:"joinUrl"`
	} `json:"onlineMeeting"`
}

type calendarViewPage struct {
	Value    []calendarEventRecord `json:"value"`
	NextLink string                `json:"@odata.nextLink"`
}

type onlineMeetingsFilterPage struct {
	Value []struct {
		ID            string `json:"id"`
		Subject       string `json:"subject"`
		// startDateTime on the beta onlineMeetings endpoint is a plain RFC3339 string,
		// NOT the {"dateTime":"...","timeZone":"..."} object used by calendar events.
		StartDateTime string `json:"startDateTime,omitempty"`
	} `json:"value"`
}

type recordingsPage struct {
	Value []struct {
		ID                  string `json:"id"`
		RecordingContentURL string `json:"recordingContentUrl"`
	} `json:"value"`
}

// RecordingsListDiag is optional troubleshooting info (populate via ListRecordingsDetailed or ?debug=1).
type RecordingsListDiag struct {
	GraphRecordingItems int      `json:"graph_recording_items"`
	OnedriveFolderFound bool     `json:"onedrive_folder_found"`
	OnedriveWalkItems   int      `json:"onedrive_walk_items"`
	OnedriveSearchItems int      `json:"onedrive_search_items"`
	Messages            []string `json:"messages,omitempty"`
}

// ListRecordings lists recordings using delegated auth only.
// getAllRecordings returns 412 "not supported in delegated context" on many tenants.
// Flow: (1) calendar events (Teams join URLs) → onlineMeeting by JoinWebUrl filter → recordings per meeting.
// (2) OneDrive folder "Microsoft Teams Recordings" — chat / Meet now often land here without a calendar join URL.
// Requires Calendars.Read + Files.Read (see teams_auth.go default scopes).
func ListRecordings(ctx context.Context, accessToken string, httpClient *http.Client) ([]RecordingListItem, error) {
	items, _, err := ListRecordingsDetailed(ctx, accessToken, httpClient)
	return items, err
}

// ListRecordingsDetailed is like ListRecordings but fills diagnostics for troubleshooting (e.g. empty lists).
func ListRecordingsDetailed(ctx context.Context, accessToken string, httpClient *http.Client) ([]RecordingListItem, RecordingsListDiag, error) {
	var diag RecordingsListDiag
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	// Run graph and OneDrive paths concurrently — they are independent.
	type graphResult struct {
		items []RecordingListItem
		err   error
	}
	type odResult struct {
		items []RecordingListItem
		diag  oneDriveListDiag
		err   error
	}
	graphCh := make(chan graphResult, 1)
	odCh := make(chan odResult, 1)

	log.Printf("[teams] ListRecordingsDetailed: starting graph and OneDrive paths concurrently")
	go func() {
		items, err := listRecordingsViaCalendarAndOnlineMeetings(ctx, accessToken, httpClient)
		graphCh <- graphResult{items, err}
	}()
	go func() {
		items, d, err := listOneDriveTeamsRecordingFilesDetailed(ctx, accessToken, httpClient)
		odCh <- odResult{items, d, err}
	}()

	gr := <-graphCh
	or_ := <-odCh

	if gr.err != nil {
		log.Printf("[teams] ListRecordingsDetailed: calendar+onlineMeetings path error: %v", gr.err)
		return nil, diag, gr.err
	}
	diag.GraphRecordingItems = len(gr.items)
	log.Printf("[teams] ListRecordingsDetailed: graph path returned %d recordings", len(gr.items))

	diag.OnedriveFolderFound = or_.diag.OnedriveFolderFound
	diag.OnedriveWalkItems = or_.diag.OnedriveWalkItems
	diag.OnedriveSearchItems = or_.diag.OnedriveSearchItems
	diag.Messages = append(diag.Messages, or_.diag.Messages...)
	log.Printf("[teams] ListRecordingsDetailed: OneDrive path returned %d recordings folder_found=%v messages=%v", len(or_.items), or_.diag.OnedriveFolderFound, or_.diag.Messages)
	if or_.err != nil {
		log.Printf("[teams] ListRecordingsDetailed: OneDrive path error (non-fatal, continuing with graph items): %v", or_.err)
		diag.Messages = append(diag.Messages, "onedrive: "+or_.err.Error())
		return gr.items, diag, nil
	}
	combined := append(gr.items, or_.items...)
	sortRecordingsGraphFirst(combined)
	log.Printf("[teams] ListRecordingsDetailed: combined total=%d (graph=%d onedrive=%d)", len(combined), len(gr.items), len(or_.items))
	return combined, diag, nil
}

// sortRecordingsGraphFirst lists calendar/onlineMeeting recordings before OneDrive files so the UI does not bury Graph results.
func sortRecordingsGraphFirst(items []RecordingListItem) {
	sort.SliceStable(items, func(i, j int) bool {
		rank := func(s string) int {
			if s == "onedrive" {
				return 1
			}
			return 0 // graph, empty, or unknown → first
		}
		ri, rj := rank(items[i].Source), rank(items[j].Source)
		if ri != rj {
			return ri < rj
		}
		return false
	})
}

func listRecordingsViaCalendarAndOnlineMeetings(ctx context.Context, accessToken string, httpClient *http.Client) ([]RecordingListItem, error) {
	end := time.Now().UTC().Add(24 * time.Hour)
	start := end.AddDate(0, 0, -180) // wider window so scheduled Teams meetings with recordings are discoverable via join URL
	joinToSubject := make(map[string]string)
	joinToStart := make(map[string]string)
	firstPageURL := graphV1 + "/me/calendar/calendarView?" + url.Values{
		"startDateTime": {start.Format(time.RFC3339)},
		"endDateTime":   {end.Format(time.RFC3339)},
		"$select":       {"subject,start,isOnlineMeeting,onlineMeeting,body,location,webLink"},
	}.Encode() // body/location: chat / Meet now often omit onlineMeeting.joinUrl
	log.Printf("[teams] calendarView: querying range %s → %s; url=%s", start.Format(time.RFC3339), end.Format(time.RFC3339), firstPageURL)
	next := firstPageURL
	const maxPages = 8
	totalCalEvents := 0
	for page := 0; page < maxPages && next != ""; page++ {
		log.Printf("[teams] calendarView: fetching page %d url=%s", page, next)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, next, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		res, err := httpClient.Do(req)
		if err != nil {
			log.Printf("[teams] calendarView: page %d HTTP error: %v (check network/token)", page, err)
			return nil, err
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		log.Printf("[teams] calendarView: page %d status=%d body_len=%d", page, res.StatusCode, len(body))
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			log.Printf("[teams] calendarView: page %d non-2xx body=%s", page, truncate(string(body), 500))
			return nil, fmt.Errorf("graph calendarView: status %d: %s", res.StatusCode, truncate(string(body), 500))
		}
		var cv calendarViewPage
		if err := json.Unmarshal(body, &cv); err != nil {
			log.Printf("[teams] calendarView: page %d parse error: %v body=%s", page, err, truncate(string(body), 300))
			return nil, fmt.Errorf("parse calendarView: %w", err)
		}
		totalCalEvents += len(cv.Value)
		log.Printf("[teams] calendarView: page %d events=%d nextLink=%q", page, len(cv.Value), cv.NextLink)
		for _, ev := range cv.Value {
			joinURLs := joinURLsFromCalendarEvent(ev)
			if len(joinURLs) == 0 {
				continue
			}
			for _, ju := range joinURLs {
				if ju == "" {
					continue
				}
				if _, ok := joinToSubject[ju]; ok {
					continue
				}
				subj := strings.TrimSpace(ev.Subject)
				if subj == "" {
					subj = "Teams meeting"
				}
				joinToSubject[ju] = subj
				st := ""
				if ev.Start != nil {
					st = strings.TrimSpace(ev.Start.DateTime)
				}
				joinToStart[ju] = st
			}
		}
		next = strings.TrimSpace(cv.NextLink)
		if next != "" {
			log.Printf("[teams] calendarView: following nextLink (page %d → %d)", page, page+1)
		}
	}
	log.Printf("[teams] calendarView: finished — total_events=%d unique_join_urls=%d (if 0, no Teams join URLs found in calendar; check Calendars.Read scope and calendar window)", totalCalEvents, len(joinToSubject))
	if len(joinToSubject) == 0 {
		log.Printf("[teams] calendarView: no join URLs found — skipping onlineMeetings filter; list will be empty from graph path (check that meetings have Teams join links and fall within the 180-day window)")
		return nil, nil
	}
	// Collect up to maxMeetings join URLs into a slice for parallel processing.
	const maxMeetings = 35
	type joinEntry struct {
		joinURL string
		subject string
		startAt string
	}
	entries := make([]joinEntry, 0, len(joinToSubject))
	for ju, subj := range joinToSubject {
		if len(entries) >= maxMeetings {
			log.Printf("[teams] onlineMeetings filter: reached maxMeetings=%d cap, capping input", maxMeetings)
			break
		}
		entries = append(entries, joinEntry{ju, subj, joinToStart[ju]})
	}

	// Fan-out: look up each meeting's recordings concurrently with a bounded worker pool.
	const concurrentMeetingLookups = 5
	results := make([][]RecordingListItem, len(entries))
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrentMeetingLookups)
	for i, e := range entries {
		wg.Add(1)
		go func(idx int, entry joinEntry) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[idx] = lookupMeetingRecordings(ctx, httpClient, accessToken, entry.joinURL, entry.subject, entry.startAt)
		}(i, e)
	}
	wg.Wait()

	var out []RecordingListItem
	for _, items := range results {
		out = append(out, items...)
	}
	log.Printf("[teams] listRecordingsViaCalendarAndOnlineMeetings: done — graph recordings found=%d", len(out))
	return out, nil
}

// lookupMeetingRecordings resolves a single join URL to its onlineMeeting and returns all its recordings.
// Errors are logged and an empty slice is returned so the caller can continue with other meetings.
func lookupMeetingRecordings(ctx context.Context, httpClient *http.Client, accessToken, joinURL, fallbackSubject, startAt string) []RecordingListItem {
	filter := fmt.Sprintf("JoinWebUrl eq '%s'", oDataEscapeString(joinURL))
	// NOTE: $filter by JoinWebUrl is only supported on the beta endpoint, not v1.0.
	listURL := graphBeta + "/me/onlineMeetings?$filter=" + url.QueryEscape(filter)
	log.Printf("[teams] onlineMeetings filter: url=%s", listURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		log.Printf("[teams] onlineMeetings filter: build request error: %v", err)
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	res, err := httpClient.Do(req)
	if err != nil {
		log.Printf("[teams] onlineMeetings filter: HTTP error: %v (check network)", err)
		return nil
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		log.Printf("[teams] onlineMeetings filter: non-2xx status=%d body=%s (check OnlineMeetings.Read scope or beta endpoint access)", res.StatusCode, truncate(string(body), 500))
		return nil
	}
	var omp onlineMeetingsFilterPage
	if err := json.Unmarshal(body, &omp); err != nil {
		log.Printf("[teams] onlineMeetings filter: parse error: %v body=%s", err, truncate(string(body), 300))
		return nil
	}
	if len(omp.Value) == 0 {
		log.Printf("[teams] onlineMeetings filter: no match for joinURL=%s (hosted by another organiser or filter unsupported)", joinURL)
		return nil
	}
	om := omp.Value[0]
	mid := strings.TrimSpace(om.ID)
	if mid == "" {
		log.Printf("[teams] onlineMeetings filter: matched meeting has empty ID — skipping")
		return nil
	}
	subj := strings.TrimSpace(om.Subject)
	if subj == "" {
		subj = fallbackSubject
	}
	st := startAt
	if strings.TrimSpace(om.StartDateTime) != "" {
		st = strings.TrimSpace(om.StartDateTime)
	}
	log.Printf("[teams] listRecordingsForMeeting: meetingID=%s subject=%q", mid, subj)
	recs, err := listRecordingsForMeeting(ctx, httpClient, accessToken, mid)
	if err != nil {
		log.Printf("[teams] listRecordingsForMeeting: meetingID=%s error: %v (check OnlineMeetingRecording.Read.All scope)", mid, err)
		return nil
	}
	log.Printf("[teams] listRecordingsForMeeting: meetingID=%s recordings=%d", mid, len(recs))
	var out []RecordingListItem
	for _, r := range recs {
		rid := strings.TrimSpace(r.ID)
		if rid == "" {
			continue
		}
		out = append(out, RecordingListItem{
			MeetingID:   mid,
			RecordingID: rid,
			Subject:     subj,
			StartTime:   st,
			Source:      "graph",
		})
	}
	return out
}

type driveItemLite struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	LastModifiedDateTime string `json:"lastModifiedDateTime"`
	CreatedDateTime      string `json:"createdDateTime"`
	Folder               *struct{} `json:"folder,omitempty"`
	File                 *struct {
		MimeType string `json:"mimeType"`
	} `json:"file,omitempty"`
}

type driveChildrenPage struct {
	Value    []driveItemLite `json:"value"`
	NextLink string          `json:"@odata.nextLink"`
}

const teamsOneDriveFolderName = "Microsoft Teams Recordings"

// teamsRecordingsFolderCandidates returns folder names to resolve under /me/drive/root.
// TEAMS_ONEDRIVE_RECORDINGS_FOLDER overrides (comma-separated). Otherwise we try English + common localized names.
func teamsRecordingsFolderCandidates() []string {
	if v := strings.TrimSpace(os.Getenv("TEAMS_ONEDRIVE_RECORDINGS_FOLDER")); v != "" {
		var out []string
		for _, p := range strings.Split(v, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return []string{
		teamsOneDriveFolderName,          // "Microsoft Teams Recordings" (older tenants / classic Teams)
		"Recordings",                      // newer Teams configurations store recordings here directly
		"Microsoft Teams-Aufzeichnungen", // de (common)
		"Microsoft Teams-opnames",         // nl
		"Enregistrements Microsoft Teams", // fr (pattern seen in some tenants)
	}
}

// oneDriveListDiag is internal counters for OneDrive listing.
type oneDriveListDiag struct {
	OnedriveFolderFound bool
	OnedriveWalkItems   int
	OnedriveSearchItems int
	Messages            []string
}

func isTeamsRecordingsFolderName(name string) bool {
	n := strings.TrimSpace(name)
	for _, c := range teamsRecordingsFolderCandidates() {
		if strings.EqualFold(n, c) {
			return true
		}
	}
	return false
}

func isDriveVideoFile(it driveItemLite) bool {
	if it.Folder != nil {
		return false
	}
	if it.File != nil {
		mt := strings.ToLower(strings.TrimSpace(it.File.MimeType))
		if strings.HasPrefix(mt, "image/") {
			return false
		}
	}
	n := strings.ToLower(strings.TrimSpace(it.Name))
	// Teams meeting recordings in this folder are MP4; avoid .mov/.webm from other apps.
	return strings.HasSuffix(n, ".mp4")
}

// driveItemByPathRequest builds GET .../me/drive/root:/path/to/folder (path segments URL-safe).
func driveItemByPathURL(pathUnderRoot string) string {
	pathUnderRoot = strings.TrimPrefix(strings.TrimSpace(pathUnderRoot), "/")
	if pathUnderRoot == "" {
		return graphV1 + "/me/drive/root"
	}
	segs := strings.Split(pathUnderRoot, "/")
	for i := range segs {
		segs[i] = url.PathEscape(segs[i])
	}
	return graphV1 + "/me/drive/root:/" + strings.Join(segs, "/")
}

// driveItemWithParent is used when listing /children to recover the folder id from parentReference.
type driveItemWithParent struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	ParentReference *struct {
		ID string `json:"id"`
	} `json:"parentReference"`
}

type driveChildrenPageWithParent struct {
	Value    []driveItemWithParent `json:"value"`
	NextLink string                  `json:"@odata.nextLink"`
}

func resolveTeamsRecordingsFolderID(ctx context.Context, accessToken string, httpClient *http.Client, diag *oneDriveListDiag) (folderID string, found bool) {
	candidates := teamsRecordingsFolderCandidates()
	log.Printf("[teams] resolveTeamsRecordingsFolderID: trying %d candidate folder names: %v", len(candidates), candidates)
	for _, folderName := range candidates {
		// 1) GET item metadata by path
		u := driveItemByPathURL(folderName)
		log.Printf("[teams] resolveTeamsRecordingsFolderID: GET path url=%s", u)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+accessToken)
			res, err := httpClient.Do(req)
			if err == nil {
				body, _ := io.ReadAll(res.Body)
				res.Body.Close()
				log.Printf("[teams] resolveTeamsRecordingsFolderID: path GET %q status=%d", folderName, res.StatusCode)
				if res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusUnauthorized {
					log.Printf("[teams] resolveTeamsRecordingsFolderID: path GET %q forbidden/unauthorized body=%s (check Files.Read scope)", folderName, truncate(string(body), 300))
					diag.Messages = append(diag.Messages, "onedrive folder GET "+folderName+": "+res.Status)
					continue
				}
				if res.StatusCode >= 200 && res.StatusCode < 300 {
					var it struct {
						ID string `json:"id"`
					}
					if json.Unmarshal(body, &it) == nil && strings.TrimSpace(it.ID) != "" {
						log.Printf("[teams] resolveTeamsRecordingsFolderID: resolved via path GET folderName=%q id=%s", folderName, it.ID)
						diag.Messages = append(diag.Messages, "resolved_teams_folder:path:"+folderName)
						return strings.TrimSpace(it.ID), true
					}
				} else {
					log.Printf("[teams] resolveTeamsRecordingsFolderID: path GET %q status=%d body=%s", folderName, res.StatusCode, truncate(string(body), 300))
				}
			} else {
				log.Printf("[teams] resolveTeamsRecordingsFolderID: path GET %q HTTP error: %v", folderName, err)
			}
		}
		// 2) Some tenants respond to .../children when metadata GET is flaky — recover folder id from parentReference.
		chURL := driveItemByPathURL(folderName) + ":/children"
		log.Printf("[teams] resolveTeamsRecordingsFolderID: GET children url=%s", chURL)
		req2, err := http.NewRequestWithContext(ctx, http.MethodGet, chURL, nil)
		if err != nil {
			continue
		}
		req2.Header.Set("Authorization", "Bearer "+accessToken)
		res2, err := httpClient.Do(req2)
		if err != nil {
			log.Printf("[teams] resolveTeamsRecordingsFolderID: children GET %q HTTP error: %v", folderName, err)
			continue
		}
		b2, _ := io.ReadAll(res2.Body)
		res2.Body.Close()
		log.Printf("[teams] resolveTeamsRecordingsFolderID: children GET %q status=%d", folderName, res2.StatusCode)
		if res2.StatusCode == http.StatusForbidden || res2.StatusCode == http.StatusUnauthorized {
			log.Printf("[teams] resolveTeamsRecordingsFolderID: children GET %q forbidden body=%s (check Files.Read scope)", folderName, truncate(string(b2), 300))
			diag.Messages = append(diag.Messages, "onedrive folder children "+folderName+": "+res2.Status)
			continue
		}
		if res2.StatusCode < 200 || res2.StatusCode >= 300 {
			log.Printf("[teams] resolveTeamsRecordingsFolderID: children GET %q non-2xx body=%s", folderName, truncate(string(b2), 300))
			continue
		}
		var ch driveChildrenPageWithParent
		if json.Unmarshal(b2, &ch) != nil {
			continue
		}
		for _, it := range ch.Value {
			if it.ParentReference != nil && strings.TrimSpace(it.ParentReference.ID) != "" {
				log.Printf("[teams] resolveTeamsRecordingsFolderID: resolved via children parentReference folderName=%q id=%s", folderName, it.ParentReference.ID)
				diag.Messages = append(diag.Messages, "resolved_teams_folder:children_parent_ref:"+folderName)
				return strings.TrimSpace(it.ParentReference.ID), true
			}
		}
	}
	// Scan drive root for a folder whose name matches the allowlist (exact, case-insensitive).
	log.Printf("[teams] resolveTeamsRecordingsFolderID: path attempts exhausted — falling back to root children scan")
	next := graphV1 + "/me/drive/root/children"
	for page := 0; page < 8 && next != ""; page++ {
		log.Printf("[teams] resolveTeamsRecordingsFolderID: root scan page %d url=%s", page, next)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, next, nil)
		if err != nil {
			break
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		res, err := httpClient.Do(req)
		if err != nil {
			log.Printf("[teams] resolveTeamsRecordingsFolderID: root scan page %d HTTP error: %v", page, err)
			break
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		log.Printf("[teams] resolveTeamsRecordingsFolderID: root scan page %d status=%d", page, res.StatusCode)
		if res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusUnauthorized {
			log.Printf("[teams] resolveTeamsRecordingsFolderID: root scan forbidden body=%s (check Files.Read scope)", truncate(string(b), 300))
			diag.Messages = append(diag.Messages, "onedrive root children: "+res.Status)
			return "", false
		}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			log.Printf("[teams] resolveTeamsRecordingsFolderID: root scan non-2xx body=%s", truncate(string(b), 300))
			break
		}
		var ch driveChildrenPage
		if json.Unmarshal(b, &ch) != nil {
			break
		}
		var folderNames []string
		for _, it := range ch.Value {
			if it.Folder != nil {
				folderNames = append(folderNames, it.Name)
			}
		}
		log.Printf("[teams] resolveTeamsRecordingsFolderID: root scan page %d items=%d folders=%v nextLink=%q", page, len(ch.Value), folderNames, ch.NextLink)
		for _, it := range ch.Value {
			if it.Folder == nil {
				continue
			}
			n := strings.TrimSpace(it.Name)
			if isTeamsRecordingsFolderName(n) {
				id := strings.TrimSpace(it.ID)
				if id != "" {
					log.Printf("[teams] resolveTeamsRecordingsFolderID: resolved via root scan folderName=%q id=%s", n, id)
					diag.Messages = append(diag.Messages, "resolved_teams_folder:root_scan:"+n)
					return id, true
				}
			}
		}
		next = strings.TrimSpace(ch.NextLink)
	}
	log.Printf("[teams] resolveTeamsRecordingsFolderID: folder not found — OneDrive path will return empty (folder may be named differently; set TEAMS_ONEDRIVE_RECORDINGS_FOLDER env to override)")
	diag.Messages = append(diag.Messages, "teams_recordings_folder_not_found_under_known_paths_or_root")
	return "", false
}

func walkOneDriveFolderForVideos(ctx context.Context, accessToken string, httpClient *http.Client, rootFolderID, rootLabel string, seen map[string]struct{}, maxFiles int) ([]RecordingListItem, error) {
	var out []RecordingListItem
	var walk func(parentID, parentFolderName string, depth int) error
	walk = func(parentID, parentFolderName string, depth int) error {
		if depth > 8 {
			return nil
		}
		next := graphV1 + "/me/drive/items/" + GraphID(parentID) + "/children"
		for iter := 0; iter < 12 && next != ""; iter++ {
			req2, err := http.NewRequestWithContext(ctx, http.MethodGet, next, nil)
			if err != nil {
				return err
			}
			req2.Header.Set("Authorization", "Bearer "+accessToken)
			res2, err := httpClient.Do(req2)
			if err != nil {
				return err
			}
			b2, _ := io.ReadAll(res2.Body)
			res2.Body.Close()
			if res2.StatusCode == http.StatusForbidden || res2.StatusCode == http.StatusUnauthorized {
				return nil
			}
			if res2.StatusCode < 200 || res2.StatusCode >= 300 {
				return fmt.Errorf("drive children: status %d", res2.StatusCode)
			}
			var chPage driveChildrenPage
			if json.Unmarshal(b2, &chPage) != nil {
				return nil
			}
			for _, it := range chPage.Value {
				id := strings.TrimSpace(it.ID)
				name := strings.TrimSpace(it.Name)
				if id == "" || name == "" {
					continue
				}
				if it.Folder != nil {
					_ = walk(id, name, depth+1)
					continue
				}
				if !isDriveVideoFile(it) {
					continue
				}
				subject := recordingSubjectFromDrivePath(parentFolderName, name)
				st := strings.TrimSpace(it.LastModifiedDateTime)
				if st == "" {
					st = strings.TrimSpace(it.CreatedDateTime)
				}
				if _, ok := seen[id]; ok {
					continue
				}
				seen[id] = struct{}{}
				out = append(out, RecordingListItem{
					MeetingID:   TeamsOneDriveMeetingID,
					RecordingID: id,
					Subject:     subject,
					StartTime:   st,
					Source:      "onedrive",
				})
				if len(out) >= maxFiles {
					return nil
				}
			}
			next = strings.TrimSpace(chPage.NextLink)
		}
		return nil
	}
	if err := walk(rootFolderID, rootLabel, 0); err != nil {
		return nil, err
	}
	return out, nil
}

func listOneDriveTeamsRecordingFilesDetailed(ctx context.Context, accessToken string, httpClient *http.Client) ([]RecordingListItem, oneDriveListDiag, error) {
	var diag oneDriveListDiag
	folderID, found := resolveTeamsRecordingsFolderID(ctx, accessToken, httpClient, &diag)
	diag.OnedriveFolderFound = found
	if !found {
		// Do not search the whole drive — that lists unrelated MP4s. Only the Teams folder tree is allowed.
		return nil, diag, nil
	}
	seen := make(map[string]struct{})
	items, err := walkOneDriveFolderForVideos(ctx, accessToken, httpClient, folderID, teamsOneDriveFolderName, seen, 50)
	if err != nil {
		return nil, diag, err
	}
	diag.OnedriveWalkItems = len(items)
	return items, diag, nil
}

func recordingSubjectFromDrivePath(parentTitle, fileName string) string {
	base := fileName
	if i := strings.LastIndex(base, "."); i > 0 {
		base = base[:i]
	}
	base = strings.TrimSpace(base)
	if parentTitle != "" && parentTitle != teamsOneDriveFolderName {
		if strings.EqualFold(base, "recording") || strings.EqualFold(base, "video") {
			return parentTitle
		}
		return parentTitle + " — " + base
	}
	if base == "" {
		return "Teams recording"
	}
	return base
}

func listRecordingsForMeeting(ctx context.Context, httpClient *http.Client, accessToken, meetingID string) ([]struct {
	ID                  string
	RecordingContentURL string
}, error) {
	u := fmt.Sprintf("%s/me/onlineMeetings/%s/recordings", graphBeta, GraphID(meetingID))
	log.Printf("[teams] listRecordingsForMeeting: GET %s", u)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	res, err := httpClient.Do(req)
	if err != nil {
		log.Printf("[teams] listRecordingsForMeeting: HTTP error meetingID=%s: %v", meetingID, err)
		return nil, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	log.Printf("[teams] listRecordingsForMeeting: meetingID=%s status=%d body_len=%d", meetingID, res.StatusCode, len(body))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		log.Printf("[teams] listRecordingsForMeeting: meetingID=%s non-2xx body=%s (check OnlineMeetingRecording.Read.All scope; 403 often means missing permission or meeting not owned by this user)", meetingID, truncate(string(body), 400))
		return nil, fmt.Errorf("recordings %d: %s", res.StatusCode, truncate(string(body), 400))
	}
	var page recordingsPage
	if err := json.Unmarshal(body, &page); err != nil {
		log.Printf("[teams] listRecordingsForMeeting: meetingID=%s parse error: %v", meetingID, err)
		return nil, err
	}
	log.Printf("[teams] listRecordingsForMeeting: meetingID=%s recordings_in_page=%d", meetingID, len(page.Value))
	var out []struct {
		ID                  string
		RecordingContentURL string
	}
	for _, v := range page.Value {
		out = append(out, struct {
			ID                  string
			RecordingContentURL string
		}{ID: v.ID, RecordingContentURL: v.RecordingContentURL})
	}
	return out, nil
}

// GetDriveItemRecordingDetail returns a download URL for a OneDrive file (Teams chat recordings).
// meetingID must be TeamsOneDriveMeetingID; recordingID is the drive item id.
func GetDriveItemRecordingDetail(ctx context.Context, accessToken, meetingID, itemID string, httpClient *http.Client) (*RecordingDetail, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if !IsTeamsOneDriveMeeting(meetingID) {
		return nil, fmt.Errorf("not a OneDrive Teams import")
	}
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return nil, fmt.Errorf("missing drive item id")
	}
	uu, err := url.Parse(graphV1 + "/me/drive/items/" + GraphID(itemID))
	if err != nil {
		return nil, err
	}
	q := uu.Query()
	q.Set("$select", "id,name,@microsoft.graph.downloadUrl")
	uu.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uu.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("get drive item: status %d: %s", res.StatusCode, truncate(string(body), 500))
	}
	var raw struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		DownloadURL string `json:"@microsoft.graph.downloadUrl"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	dl := strings.TrimSpace(raw.DownloadURL)
	if dl == "" {
		// @microsoft.graph.downloadUrl is absent (e.g. sensitivity label, shared file, or
		// SharePoint-hosted item). Fall back to the /content endpoint which redirects to the
		// actual Azure Storage pre-signed URL. DownloadHTTP will add the Bearer token
		// (URL contains graph.microsoft.com) and Go's HTTP client follows the 302 redirect,
		// stripping the Authorization header when it crosses to blob.core.windows.net.
		dl = graphV1 + "/me/drive/items/" + GraphID(itemID) + "/content"
		log.Printf("[teams] GetDriveItemRecordingDetail: no @microsoft.graph.downloadUrl for item %s, falling back to /content endpoint", itemID)
	}
	return &RecordingDetail{
		ID:                  strings.TrimSpace(raw.ID),
		Subject:             strings.TrimSpace(raw.Name),
		RecordingContentURL: dl,
	}, nil
}

// GetRecording fetches a single recording resource (beta) for MP4 URL resolution.
func GetRecording(ctx context.Context, accessToken, meetingID, recordingID string, httpClient *http.Client) (*RecordingDetail, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	u := fmt.Sprintf("%s/me/onlineMeetings/%s/recordings/%s", graphBeta, GraphID(meetingID), GraphID(recordingID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("get recording: status %d: %s", res.StatusCode, truncate(string(body), 500))
	}
	var raw struct {
		ID                  string `json:"id"`
		RecordingContentURL string `json:"recordingContentUrl"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	return &RecordingDetail{
		ID:                  raw.ID,
		RecordingContentURL: strings.TrimSpace(raw.RecordingContentURL),
	}, nil
}

// DownloadHTTP GETs a URL, adding Bearer auth when the URL is on graph.microsoft.com.
func DownloadHTTP(ctx context.Context, downloadURL, accessToken string, httpClient *http.Client) (*http.Response, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, err
	}
	if strings.Contains(downloadURL, "graph.microsoft.com") {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
	return httpClient.Do(req)
}

// FetchTranscriptVTTBestEffort tries to fetch meeting transcript as WebVTT (beta APIs). Returns ("", nil) if none.
func FetchTranscriptVTTBestEffort(ctx context.Context, accessToken, meetingID, recordingID string, httpClient *http.Client) (vtt string, err error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	// Requires OnlineMeetingTranscript.Read.All (delegated).
	// The correct path is /me/onlineMeetings/{id}/transcripts — transcripts are per-meeting, not per-recording.
	// The path /me/onlineMeetings/{id}/recordings/{rid}/transcripts does not exist.
	base := fmt.Sprintf("%s/me/onlineMeetings/%s/transcripts", graphBeta, GraphID(meetingID))
	log.Printf("[teams] FetchTranscriptVTTBestEffort: GET transcripts url=%s meetingID=%s", base, meetingID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	res, err := httpClient.Do(req)
	if err != nil {
		log.Printf("[teams] FetchTranscriptVTTBestEffort: transcripts list HTTP error meetingID=%s: %v", meetingID, err)
		return "", err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	log.Printf("[teams] FetchTranscriptVTTBestEffort: transcripts list meetingID=%s status=%d body_len=%d", meetingID, res.StatusCode, len(body))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		log.Printf("[teams] FetchTranscriptVTTBestEffort: transcripts list non-2xx meetingID=%s body=%s (check OnlineMeetingTranscript.Read.All scope; returning empty transcript)", meetingID, truncate(string(body), 400))
		return "", nil
	}
	var page struct {
		Value []struct {
			ID string `json:"id"`
		} `json:"value"`
	}
	if json.Unmarshal(body, &page) != nil || len(page.Value) == 0 {
		log.Printf("[teams] FetchTranscriptVTTBestEffort: no transcripts found for meetingID=%s (parse_err=%v value_len=%d)", meetingID, json.Unmarshal(body, &page), len(page.Value))
		return "", nil
	}
	tid := strings.TrimSpace(page.Value[0].ID)
	if tid == "" {
		log.Printf("[teams] FetchTranscriptVTTBestEffort: first transcript has empty ID meetingID=%s", meetingID)
		return "", nil
	}
	contentURL := fmt.Sprintf("%s/me/onlineMeetings/%s/transcripts/%s/content", graphBeta, GraphID(meetingID), GraphID(tid))
	log.Printf("[teams] FetchTranscriptVTTBestEffort: GET transcript content url=%s", contentURL)
	req2, err := http.NewRequestWithContext(ctx, http.MethodGet, contentURL, nil)
	if err != nil {
		return "", err
	}
	req2.Header.Set("Authorization", "Bearer "+accessToken)
	req2.Header.Set("Accept", "text/vtt")
	res2, err := httpClient.Do(req2)
	if err != nil {
		log.Printf("[teams] FetchTranscriptVTTBestEffort: transcript content HTTP error meetingID=%s transcriptID=%s: %v", meetingID, tid, err)
		return "", err
	}
	defer res2.Body.Close()
	b2, _ := io.ReadAll(res2.Body)
	log.Printf("[teams] FetchTranscriptVTTBestEffort: transcript content meetingID=%s transcriptID=%s status=%d body_len=%d", meetingID, tid, res2.StatusCode, len(b2))
	if res2.StatusCode < 200 || res2.StatusCode >= 300 {
		log.Printf("[teams] FetchTranscriptVTTBestEffort: transcript content non-2xx meetingID=%s body=%s (returning empty transcript)", meetingID, truncate(string(b2), 400))
		return "", nil
	}
	s := string(b2)
	if strings.TrimSpace(s) == "" {
		log.Printf("[teams] FetchTranscriptVTTBestEffort: transcript content empty after trim meetingID=%s transcriptID=%s", meetingID, tid)
		return "", nil
	}
	log.Printf("[teams] FetchTranscriptVTTBestEffort: transcript fetched OK meetingID=%s transcriptID=%s vtt_len=%d", meetingID, tid, len(s))
	return s, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// DefaultHTTPClient returns a client with a reasonable timeout for Graph + CDN downloads.
func DefaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 60 * time.Minute}
}
