package handlers_test

import (
	"testing"

	"github.com/wareongo/exotel-call-service/internal/models"
)

func TestWebhookInsert(t *testing.T) {
	hn := newHarness(t)
	w := hn.req("POST", "/webhooks/call-status?CallSid=sidA&Status=completed&From=091&To=092&Duration=169", "", nil)
	if w.Code != 200 {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var call models.Call
	if err := hn.db.First(&call, "exotel_sid = ?", "sidA").Error; err != nil {
		t.Fatalf("call not stored: %v", err)
	}
	if call.Status != "completed" || call.Duration != 169 {
		t.Errorf("bad call: %+v", call)
	}
}

func TestWebhookIdempotentUpdate(t *testing.T) {
	hn := newHarness(t)
	hn.req("POST", "/webhooks/call-status?CallSid=sidA&Status=in-progress", "", nil)
	hn.req("POST", "/webhooks/call-status?CallSid=sidA&Status=completed&Duration=42", "", nil)

	var count int64
	hn.db.Model(&models.Call{}).Where("exotel_sid = ?", "sidA").Count(&count)
	if count != 1 {
		t.Fatalf("expected 1 row, got %d", count)
	}
	var call models.Call
	hn.db.First(&call, "exotel_sid = ?", "sidA")
	if call.Status != "completed" || call.Duration != 42 {
		t.Errorf("expected updated row, got %+v", call)
	}
}

func TestWebhookMissingSid(t *testing.T) {
	hn := newHarness(t)
	w := hn.req("POST", "/webhooks/call-status?Status=completed", "", nil)
	if w.Code != 400 {
		t.Errorf("code = %d, want 400 for missing CallSid", w.Code)
	}
}

// An outbound POC→client call ingested via the webhook seeds a sticky
// assignment (client -> that POC).
func TestWebhookOutboundSeedsAssignment(t *testing.T) {
	hn := newHarness(t)
	poc := seedContact(t, hn.db, models.Contact{Name: "POC", Phone: "09812345678", Active: true})

	w := hn.req("POST", "/webhooks/call-status?CallSid=ob1&Direction=outbound-dial&From=09812345678&To=08318825478&Status=completed", "", nil)
	if w.Code != 200 {
		t.Fatalf("code=%d", w.Code)
	}
	var asg models.Assignment
	if err := hn.db.First(&asg, "customer_phone = ?", "8318825478").Error; err != nil {
		t.Fatalf("assignment not seeded: %v", err)
	}
	if asg.ContactID != poc.ID {
		t.Errorf("assignment -> %d, want POC %d", asg.ContactID, poc.ID)
	}
}

func TestBackfillEndpoint(t *testing.T) {
	hn := newHarness(t)
	seedContact(t, hn.db, models.Contact{Name: "POC", Phone: "09812345678", Active: true})
	hn.db.Create(&models.Call{ExotelSID: "a", Direction: "outbound-dial", FromNumber: "09812345678", ToNumber: "08300000001"})

	w := hn.req("POST", "/backfill-assignments", "", nil)
	if w.Code != 200 {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var n int64
	hn.db.Model(&models.Assignment{}).Count(&n)
	if n != 1 {
		t.Errorf("expected 1 assignment after backfill, got %d", n)
	}
}

// A webhook without RecordingUrl must not blank a recording_url set elsewhere
// (e.g. by reconcile).
func TestWebhookNoClobberRecording(t *testing.T) {
	hn := newHarness(t)
	hn.db.Create(&models.Call{ExotelSID: "sidB", RecordingURL: "http://rec/x.mp3", Status: "completed"})

	hn.req("POST", "/webhooks/call-status?CallSid=sidB&Status=completed&From=1&To=2", "", nil)

	var call models.Call
	hn.db.First(&call, "exotel_sid = ?", "sidB")
	if call.RecordingURL != "http://rec/x.mp3" {
		t.Errorf("recording_url clobbered: %q", call.RecordingURL)
	}
}
