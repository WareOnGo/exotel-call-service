package assign_test

import (
	"testing"
	"time"

	"github.com/wareongo/exotel-call-service/internal/assign"
	"github.com/wareongo/exotel-call-service/internal/models"
	"github.com/wareongo/exotel-call-service/internal/testutil"
)

func TestCaptureOutboundCreatesAssignment(t *testing.T) {
	gdb := testutil.NewDB(t)
	// POC phone differs in format from the call's From — phone_key must match.
	gdb.Create(&models.Contact{Name: "POC", Phone: "09812345678", Active: true})

	call := &models.Call{
		ExotelSID: "c1", Direction: "outbound-dial",
		FromNumber: "+919812345678", // POC
		ToNumber:   "08318825478",   // client
	}
	changed, err := assign.CaptureOutbound(gdb, call)
	if err != nil || !changed {
		t.Fatalf("expected assignment created, changed=%v err=%v", changed, err)
	}
	var asg models.Assignment
	if err := gdb.First(&asg, "customer_phone = ?", "8318825478").Error; err != nil {
		t.Fatalf("assignment not created: %v", err)
	}
	var c models.Contact
	gdb.First(&c, "phone = ?", "09812345678")
	if asg.ContactID != c.ID {
		t.Errorf("assignment -> contact %d, want %d", asg.ContactID, c.ID)
	}
}

func TestCaptureOutboundIgnoresInbound(t *testing.T) {
	gdb := testutil.NewDB(t)
	gdb.Create(&models.Contact{Name: "POC", Phone: "09812345678", Active: true})
	call := &models.Call{ExotelSID: "c1", Direction: "inbound", FromNumber: "09812345678", ToNumber: "08318825478"}
	changed, _ := assign.CaptureOutbound(gdb, call)
	var n int64
	gdb.Model(&models.Assignment{}).Count(&n)
	if changed || n != 0 {
		t.Errorf("inbound should be ignored; changed=%v count=%d", changed, n)
	}
}

func TestCaptureOutboundIgnoresUnknownPOC(t *testing.T) {
	gdb := testutil.NewDB(t)
	// no contacts at all
	call := &models.Call{ExotelSID: "c1", Direction: "outbound-dial", FromNumber: "09812345678", ToNumber: "08318825478"}
	changed, _ := assign.CaptureOutbound(gdb, call)
	if changed {
		t.Error("From is not a known POC; should be ignored")
	}
}

func TestCaptureOutboundKeepsLatest(t *testing.T) {
	gdb := testutil.NewDB(t)
	gdb.Create(&models.Contact{Name: "Old", Phone: "09800000001", Active: true})
	gdb.Create(&models.Contact{Name: "New", Phone: "09800000002", Active: true})

	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	// newer call first -> binds to "New"
	if _, err := assign.CaptureOutbound(gdb, &models.Call{
		ExotelSID: "n", Direction: "outbound-dial", FromNumber: "09800000002", ToNumber: "08318825478", StartTime: &newer,
	}); err != nil {
		t.Fatal(err)
	}
	// older call must NOT override the newer assignment
	if _, err := assign.CaptureOutbound(gdb, &models.Call{
		ExotelSID: "o", Direction: "outbound-dial", FromNumber: "09800000001", ToNumber: "08318825478", StartTime: &older,
	}); err != nil {
		t.Fatal(err)
	}

	var asg models.Assignment
	gdb.Preload("Contact").First(&asg, "customer_phone = ?", "8318825478")
	if asg.Contact.Name != "New" {
		t.Errorf("expected latest POC 'New', got %q", asg.Contact.Name)
	}
}

func TestBackfill(t *testing.T) {
	gdb := testutil.NewDB(t)
	gdb.Create(&models.Contact{Name: "POC", Phone: "09812345678", Active: true})
	// two outbound calls to two clients + one inbound (ignored) + one outbound from a non-POC (ignored)
	t1 := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	gdb.Create(&models.Call{ExotelSID: "a", Direction: "outbound-dial", FromNumber: "09812345678", ToNumber: "08300000001", StartTime: &t1})
	gdb.Create(&models.Call{ExotelSID: "b", Direction: "outbound-api", FromNumber: "09812345678", ToNumber: "08300000002", StartTime: &t1})
	gdb.Create(&models.Call{ExotelSID: "c", Direction: "inbound", FromNumber: "08300000003", ToNumber: "09812345678", StartTime: &t1})
	gdb.Create(&models.Call{ExotelSID: "d", Direction: "outbound-dial", FromNumber: "09999999999", ToNumber: "08300000004", StartTime: &t1})

	written, err := assign.Backfill(gdb)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if written != 2 {
		t.Errorf("expected 2 assignments written, got %d", written)
	}
	var n int64
	gdb.Model(&models.Assignment{}).Count(&n)
	if n != 2 {
		t.Errorf("expected 2 assignments in db, got %d", n)
	}
}
