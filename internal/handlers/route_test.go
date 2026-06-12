package handlers_test

import (
	"testing"
	"time"

	"github.com/wareongo/exotel-call-service/internal/models"
)

func TestRouteFreshCallerToFirstTouch(t *testing.T) {
	hn := newHarness(t)
	seedContact(t, hn.db, models.Contact{Name: "Sales", Phone: "09812345678", IsFirstTouch: true, Active: true})

	w := hn.req("POST", "/route", `{"Call":{"Sid":"s1","From":"09876543210"}}`, nil)
	if w.Code != 200 {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != "+919812345678" {
		t.Errorf("dial = %q, want +919812345678", got)
	}
	// assignment is written asynchronously
	waitFor(t, func() bool {
		var n int64
		hn.db.Model(&models.Assignment{}).Where("customer_phone = ?", "9876543210").Count(&n)
		return n == 1
	})
}

func TestRoutePrimaryPlusFallback(t *testing.T) {
	hn := newHarness(t)
	hn.cfg.DefaultFallbackNumber = "09999900002"
	seedContact(t, hn.db, models.Contact{Name: "Sales", Phone: "09812345678", IsFirstTouch: true, Active: true})

	w := hn.req("POST", "/route", `{"Call":{"From":"09876543210"}}`, nil)
	if got := w.Body.String(); got != "+919812345678,+919999900002" {
		t.Errorf("dial = %q, want primary,fallback", got)
	}
}

func TestRouteFirstTouchPriorityOrder(t *testing.T) {
	hn := newHarness(t)
	seedContact(t, hn.db, models.Contact{Name: "Low", Phone: "09800000001", IsFirstTouch: true, FallbackPriority: 5, Active: true})
	seedContact(t, hn.db, models.Contact{Name: "High", Phone: "09800000002", IsFirstTouch: true, FallbackPriority: 0, Active: true})

	w := hn.req("POST", "/route", `{"Call":{"From":"09876543210"}}`, nil)
	if got := w.Body.String(); got != "+919800000002" {
		t.Errorf("dial = %q, want highest-priority (+919800000002)", got)
	}
}

func TestRouteSticky(t *testing.T) {
	hn := newHarness(t)
	c := seedContact(t, hn.db, models.Contact{Name: "Owner", Phone: "09833333333", Active: true})
	// existing assignment for normalized key
	hn.db.Create(&models.Assignment{CustomerPhone: "9876543210", ContactID: c.ID, LastContactedAt: time.Now()})

	// Caller arrives with +91 prefix — must normalize to the same key.
	w := hn.req("POST", "/route", `{"Call":{"From":"+919876543210"}}`, nil)
	if got := w.Body.String(); got != "+919833333333" {
		t.Errorf("dial = %q, want sticky owner (+919833333333)", got)
	}
}

func TestRouteInactiveAssignedFallsBackToFreshPath(t *testing.T) {
	hn := newHarness(t)
	inactive := seedContact(t, hn.db, models.Contact{Name: "Gone", Phone: "09800000009", Active: false})
	seedContact(t, hn.db, models.Contact{Name: "FirstTouch", Phone: "09811111111", IsFirstTouch: true, Active: true})
	hn.db.Create(&models.Assignment{CustomerPhone: "9876543210", ContactID: inactive.ID, LastContactedAt: time.Now()})

	w := hn.req("POST", "/route", `{"Call":{"From":"09876543210"}}`, nil)
	if got := w.Body.String(); got != "+919811111111" {
		t.Errorf("dial = %q, want first-touch since assigned contact inactive", got)
	}
}

func TestRouteNoContactNoFallback404(t *testing.T) {
	hn := newHarness(t) // no contacts, no fallback
	w := hn.req("POST", "/route", `{"Call":{"From":"09876543210"}}`, nil)
	if w.Code != 404 {
		t.Errorf("code = %d, want 404 when nothing to dial", w.Code)
	}
}

func TestRouteNoContactButFallback(t *testing.T) {
	hn := newHarness(t)
	hn.cfg.DefaultFallbackNumber = "09999900002"
	w := hn.req("POST", "/route", `{"Call":{"From":"09876543210"}}`, nil)
	if w.Code != 200 || w.Body.String() != "+919999900002" {
		t.Errorf("code=%d dial=%q, want fallback", w.Code, w.Body.String())
	}
}

func TestRouteMissingCaller(t *testing.T) {
	hn := newHarness(t)
	hn.cfg.DefaultFallbackNumber = "09999900002"
	// empty Call.From -> falls back
	w := hn.req("POST", "/route", `{"Call":{"From":""}}`, nil)
	if w.Code != 200 || w.Body.String() != "+919999900002" {
		t.Errorf("code=%d dial=%q, want fallback on missing caller", w.Code, w.Body.String())
	}
}

func TestRouteQueryParamFallbackPath(t *testing.T) {
	hn := newHarness(t)
	seedContact(t, hn.db, models.Contact{Name: "Sales", Phone: "09812345678", IsFirstTouch: true, Active: true})
	// No JSON body: Exotel-alt setups send query params (CallFrom).
	w := hn.req("GET", "/route?CallFrom=09876543210", "", nil)
	if w.Code != 200 || w.Body.String() != "+919812345678" {
		t.Errorf("code=%d dial=%q, want resolution via query param", w.Code, w.Body.String())
	}
}
