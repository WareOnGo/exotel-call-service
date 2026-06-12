package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// /api/* must reject requests without the token.
func TestApiRequiresToken(t *testing.T) {
	hn := newHarness(t)
	for _, p := range []string{"/api/contacts", "/api/calls", "/api/assignments"} {
		r := httptest.NewRequest("GET", p, nil) // no Authorization header
		w := httptest.NewRecorder()
		hn.engine.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s without token = %d, want 401", p, w.Code)
		}
	}
}

// A wrong token is rejected too.
func TestApiWrongToken(t *testing.T) {
	hn := newHarness(t)
	r := httptest.NewRequest("GET", "/api/contacts", nil)
	r.Header.Set("Authorization", "Bearer not-the-token")
	w := httptest.NewRecorder()
	hn.engine.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("wrong token = %d, want 401", w.Code)
	}
}

// X-Api-Key is accepted as an alternative to the bearer header.
func TestApiKeyHeader(t *testing.T) {
	hn := newHarness(t)
	r := httptest.NewRequest("GET", "/api/contacts", nil)
	r.Header.Set("X-Api-Key", testAPIToken)
	w := httptest.NewRecorder()
	hn.engine.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("X-Api-Key = %d, want 200", w.Code)
	}
}

// Fail closed: if no API_TOKEN is configured, /api is unavailable, not open.
func TestApiFailsClosedWhenUnconfigured(t *testing.T) {
	hn := newHarness(t)
	hn.cfg.APIToken = "" // simulate missing config
	r := httptest.NewRequest("GET", "/api/contacts", nil)
	r.Header.Set("Authorization", "Bearer "+testAPIToken)
	w := httptest.NewRecorder()
	hn.engine.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("unconfigured = %d, want 503", w.Code)
	}
}
