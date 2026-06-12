package handlers_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestHealthz(t *testing.T) {
	hn := newHarness(t)
	w := hn.req("GET", "/healthz", "", nil)
	if w.Code != 200 {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"status":"ok"`) {
		t.Errorf("unexpected body: %s", w.Body.String())
	}
}

func TestConnectTwoNumbersSuccess(t *testing.T) {
	hn := newHarness(t)
	ts := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/Calls/connect.json") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"Call":{"Sid":"newcall"}}`))
	})
	hn.withExotel(ts.URL)

	w := hn.req("POST", "/api/calls/connect", `{"from":"091","to":"092","caller_id":"093"}`, nil)
	if w.Code != 200 {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "newcall") {
		t.Errorf("expected exotel response passthrough, got %s", w.Body.String())
	}
}

func TestConnectBadBody(t *testing.T) {
	hn := newHarness(t)
	// missing required to/from
	w := hn.req("POST", "/api/calls/connect", `{"from":"091"}`, nil)
	if w.Code != 400 {
		t.Errorf("code=%d, want 400", w.Code)
	}
}

func TestConnectMissingCallerID(t *testing.T) {
	hn := newHarness(t) // no DefaultCallerID configured, none in body
	w := hn.req("POST", "/api/calls/connect", `{"from":"091","to":"092"}`, nil)
	if w.Code != 400 {
		t.Errorf("code=%d, want 400 when no caller id", w.Code)
	}
}

func TestSyncSecretGuard(t *testing.T) {
	hn := newHarness(t)
	hn.cfg.SyncSecret = "s3cret"
	w := hn.req("POST", "/sync", "", nil) // no header
	if w.Code != 401 {
		t.Errorf("code=%d, want 401 without secret", w.Code)
	}
}

func TestSyncSuccessEmpty(t *testing.T) {
	hn := newHarness(t)
	ts := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"Metadata":{"NextPageUri":""},"Calls":[]}`))
	})
	hn.withExotel(ts.URL)

	w := hn.req("POST", "/sync", "", nil)
	if w.Code != 200 {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"fetched":0`) {
		t.Errorf("unexpected summary: %s", w.Body.String())
	}
}

func TestArchiveSecretGuard(t *testing.T) {
	hn := newHarness(t)
	hn.cfg.SyncSecret = "s3cret"
	w := hn.req("POST", "/archive-recordings", "", nil)
	if w.Code != 401 {
		t.Errorf("code=%d, want 401 without secret", w.Code)
	}
}

func TestArchiveNotConfigured(t *testing.T) {
	hn := newHarness(t) // Archiver is nil
	w := hn.req("POST", "/archive-recordings", "", nil)
	if w.Code != 503 {
		t.Errorf("code=%d, want 503 when R2 not configured", w.Code)
	}
}
