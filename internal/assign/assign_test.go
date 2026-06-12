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
	changed, err := assign.FromCall(gdb, call)
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

// Inbound: POC = To (the answered agent), client = From.
func TestFromCallInboundCapturesPOC(t *testing.T) {
	gdb := testutil.NewDB(t)
	gdb.Create(&models.Contact{Name: "POC", Phone: "07400184225", Active: true})
	call := &models.Call{
		ExotelSID: "in1", Direction: "inbound",
		FromNumber: "08076708542", // client (caller)
		ToNumber:   "07400184225", // POC (answered agent)
	}
	changed, err := assign.FromCall(gdb, call)
	if err != nil || !changed {
		t.Fatalf("expected inbound assignment, changed=%v err=%v", changed, err)
	}
	if err := gdb.First(&models.Assignment{}, "customer_phone = ?", "8076708542").Error; err != nil {
		t.Fatalf("inbound assignment not created: %v", err)
	}
}

// Directions we don't understand are ignored.
func TestFromCallIgnoresUnknownDirection(t *testing.T) {
	gdb := testutil.NewDB(t)
	gdb.Create(&models.Contact{Name: "POC", Phone: "07400184225", Active: true})
	call := &models.Call{ExotelSID: "x", Direction: "", FromNumber: "08076708542", ToNumber: "07400184225"}
	if changed, _ := assign.FromCall(gdb, call); changed {
		t.Error("unknown direction should be ignored")
	}
}

func TestCaptureOutboundIgnoresUnknownPOC(t *testing.T) {
	gdb := testutil.NewDB(t)
	// no contacts at all
	call := &models.Call{ExotelSID: "c1", Direction: "outbound-dial", FromNumber: "09812345678", ToNumber: "08318825478"}
	changed, _ := assign.FromCall(gdb, call)
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
	if _, err := assign.FromCall(gdb, &models.Call{
		ExotelSID: "n", Direction: "outbound-dial", FromNumber: "09800000002", ToNumber: "08318825478", StartTime: &newer,
	}); err != nil {
		t.Fatal(err)
	}
	// older call must NOT override the newer assignment
	if _, err := assign.FromCall(gdb, &models.Call{
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
	// 2 outbound (POC=From) + 1 inbound (POC=To) all map to the POC; 1 outbound
	// from a non-POC is ignored. -> 3 distinct clients assigned.
	t1 := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	gdb.Create(&models.Call{ExotelSID: "a", Direction: "outbound-dial", FromNumber: "09812345678", ToNumber: "08300000001", StartTime: &t1})
	gdb.Create(&models.Call{ExotelSID: "b", Direction: "outbound-api", FromNumber: "09812345678", ToNumber: "08300000002", StartTime: &t1})
	gdb.Create(&models.Call{ExotelSID: "c", Direction: "inbound", FromNumber: "08300000003", ToNumber: "09812345678", StartTime: &t1})
	gdb.Create(&models.Call{ExotelSID: "d", Direction: "outbound-dial", FromNumber: "09999999999", ToNumber: "08300000004", StartTime: &t1})

	written, err := assign.Backfill(gdb)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if written != 3 {
		t.Errorf("expected 3 assignments written, got %d", written)
	}
	var n int64
	gdb.Model(&models.Assignment{}).Count(&n)
	if n != 3 {
		t.Errorf("expected 3 assignments in db, got %d", n)
	}
}
