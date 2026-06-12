package exotel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/wareongo/exotel-call-service/internal/config"
)

const dateFmt = "2006-01-02 15:04:05"

// Exotel returns timestamps without a zone; for this account they are IST.
// IST has no DST, so a fixed +05:30 offset is always correct (and avoids
// depending on tzdata being present in the container).
var istLoc = time.FixedZone("IST", 5*3600+30*60)

// Client is a thin wrapper over the Exotel Voice v1 REST API.
type Client struct {
	http    *http.Client
	key     string
	token   string
	sid     string
	baseURL string // scheme + host, no trailing slash, e.g. https://api.exotel.com
}

func New(cfg *config.Config) *Client {
	return NewWithBaseURL(cfg.ExotelKey, cfg.ExotelToken, cfg.ExotelSID,
		"https://"+cfg.ExotelSubdomain)
}

// NewWithBaseURL builds a client against an explicit base URL (host w/ scheme).
// Useful for pointing at a staging host or a test server.
func NewWithBaseURL(key, token, sid, baseURL string) *Client {
	return &Client{
		http:    &http.Client{Timeout: 30 * time.Second},
		key:     key,
		token:   token,
		sid:     sid,
		baseURL: strings.TrimRight(baseURL, "/"),
	}
}

func (c *Client) base() string {
	return fmt.Sprintf("%s/v1/Accounts/%s", c.baseURL, c.sid)
}

func (c *Client) do(ctx context.Context, method, rawurl string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawurl, body)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.key, c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return c.http.Do(req)
}

// ConnectTwoNumbers triggers an outbound call that bridges `from` and `to`,
// presenting `callerID` (an ExoPhone) to both legs.
// POST /v1/Accounts/{sid}/Calls/connect.json
func (c *Client) ConnectTwoNumbers(ctx context.Context, from, to, callerID string) (map[string]any, error) {
	form := url.Values{}
	form.Set("From", from)
	form.Set("To", to)
	form.Set("CallerId", callerID)

	endpoint := c.base() + "/Calls/connect.json"
	resp, err := c.do(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("exotel connect: status %d: %s", resp.StatusCode, string(raw))
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("exotel connect: decode: %w (body=%s)", err, string(raw))
	}
	return out, nil
}

// DownloadRecording fetches a recording's bytes. The recordings host requires
// the same HTTP Basic auth as the API (key:token) — c.do() sets it. Returns the
// raw bytes and the Content-Type.
//
// Note: Go strips the Authorization header on a cross-host *redirect*. This call
// sets auth directly on the request to the recordings host, so it works as long
// as that host serves the file directly (it does). If Exotel ever switches to
// redirecting to a pre-signed URL, that target needs no auth anyway.
func (c *Client) DownloadRecording(ctx context.Context, recURL string) ([]byte, string, error) {
	resp, err := c.do(ctx, http.MethodGet, recURL, nil)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("download recording: status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	return data, resp.Header.Get("Content-Type"), nil
}

// --- bulk call listing (for reconcile) ---

type CallRecord struct {
	Sid                  string `json:"Sid"`
	From                 string `json:"From"`
	To                   string `json:"To"`
	PhoneNumber          string `json:"PhoneNumber"`
	Direction            string `json:"Direction"`
	Status               string `json:"Status"`
	StartTime            string `json:"StartTime"`
	EndTime              string `json:"EndTime"`
	Duration             any    `json:"Duration"` // API returns number or string across versions
	Price                any    `json:"Price"`
	RecordingURL         string `json:"RecordingUrl"`
	ConversationDuration any    `json:"ConversationDuration"`
}

type callsResponse struct {
	Metadata struct {
		NextPageURI string `json:"NextPageUri"`
	} `json:"Metadata"`
	Calls []CallRecord `json:"Calls"`
}

// ListCallsPage fetches one page of calls in [from, to]. Pass the previous
// page's NextPageURI as `nextURI` to paginate; empty for the first page.
// Returns the records and the next cursor URI ("" when exhausted).
func (c *Client) ListCallsPage(ctx context.Context, from, to time.Time, nextURI string) ([]CallRecord, string, error) {
	var endpoint string
	if nextURI != "" {
		endpoint = c.baseURL + nextURI
	} else {
		q := url.Values{}
		q.Set("DateCreated", fmt.Sprintf("gte:%s;lte:%s", from.Format(dateFmt), to.Format(dateFmt)))
		q.Set("PageSize", "100")
		q.Set("SortBy", "DateCreated:asc")
		q.Set("RecordingUrlValidity", "60")
		endpoint = c.base() + "/Calls.json?" + q.Encode()
	}
	resp, err := c.do(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("exotel list: status %d: %s", resp.StatusCode, string(raw))
	}
	var out callsResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, "", fmt.Errorf("exotel list: decode: %w", err)
	}
	return out.Calls, out.Metadata.NextPageURI, nil
}

// CallDetails holds the richer, leg-level fields only available from the
// single-call endpoint with details=true (not returned by the bulk list).
type CallDetails struct {
	ConversationDuration int
	Leg1Status           string
	Leg2Status           string
	Leg1Duration         int
	Leg2Duration         int
	RecordingURL         string
}

// GetCallDetails fetches GET /Calls/{sid}.json?details=true for talk-time and
// per-leg breakdown.
func (c *Client) GetCallDetails(ctx context.Context, sid string) (*CallDetails, error) {
	endpoint := c.base() + "/Calls/" + url.PathEscape(sid) + ".json?details=true"
	resp, err := c.do(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("exotel details: status %d: %s", resp.StatusCode, string(raw))
	}
	var wrap struct {
		Call struct {
			RecordingURL string `json:"RecordingUrl"`
			Details      struct {
				ConversationDuration int    `json:"ConversationDuration"`
				Leg1Status           string `json:"Leg1Status"`
				Leg2Status           string `json:"Leg2Status"`
				Legs                 []struct {
					Leg struct {
						ID             int `json:"Id"`
						OnCallDuration int `json:"OnCallDuration"`
					} `json:"Leg"`
				} `json:"Legs"`
			} `json:"Details"`
		} `json:"Call"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, fmt.Errorf("exotel details: decode: %w", err)
	}
	d := &CallDetails{
		ConversationDuration: wrap.Call.Details.ConversationDuration,
		Leg1Status:           wrap.Call.Details.Leg1Status,
		Leg2Status:           wrap.Call.Details.Leg2Status,
		RecordingURL:         wrap.Call.RecordingURL,
	}
	for _, l := range wrap.Call.Details.Legs {
		switch l.Leg.ID {
		case 1:
			d.Leg1Duration = l.Leg.OnCallDuration
		case 2:
			d.Leg2Duration = l.Leg.OnCallDuration
		}
	}
	return d, nil
}

// ParseTime parses Exotel's "2006-01-02 15:04:05" timestamps as IST; returns
// nil on empty/bad input.
func ParseTime(s string) *time.Time {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	t, err := time.ParseInLocation(dateFmt, s, istLoc)
	if err != nil {
		return nil
	}
	return &t
}

// AsInt coerces the API's number-or-string fields to int.
func AsInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(x))
		return n
	}
	return 0
}

// AsFloat coerces the API's number-or-string fields to float64.
func AsFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f
	}
	return 0
}
