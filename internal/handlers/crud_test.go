package handlers_test

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/wareongo/exotel-call-service/internal/models"
)

func TestContactsCreateListUpdate(t *testing.T) {
	hn := newHarness(t)

	// create
	w := hn.req("POST", "/api/contacts", `{"name":"Sales","phone":"09812345678","is_first_touch":true,"active":true}`, nil)
	if w.Code != 201 {
		t.Fatalf("create code=%d body=%s", w.Code, w.Body.String())
	}
	var created models.Contact
	json.Unmarshal(w.Body.Bytes(), &created)
	if created.ID == 0 {
		t.Fatal("expected an id")
	}

	// list
	w = hn.req("GET", "/api/contacts", "", nil)
	var list []models.Contact
	json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 1 {
		t.Fatalf("expected 1 contact, got %d", len(list))
	}

	// update (PATCH)
	w = hn.req("PATCH", "/api/contacts/1", `{"active":false}`, nil)
	if w.Code != 200 {
		t.Fatalf("patch code=%d", w.Code)
	}
	var got models.Contact
	hn.db.First(&got, 1)
	if got.Active {
		t.Error("expected active=false after patch")
	}
}

func TestContactCreateRequiresPhone(t *testing.T) {
	hn := newHarness(t)
	w := hn.req("POST", "/api/contacts", `{"name":"NoPhone"}`, nil)
	if w.Code != 400 {
		t.Errorf("code=%d, want 400 without phone", w.Code)
	}
}

func TestAssignmentsUpsertAndReassign(t *testing.T) {
	hn := newHarness(t)
	c1 := seedContact(t, hn.db, models.Contact{Name: "A", Phone: "09800000001", Active: true})
	c2 := seedContact(t, hn.db, models.Contact{Name: "B", Phone: "09800000002", Active: true})

	// create assignment (note un-normalized phone in request -> normalized key)
	w := hn.req("PUT", "/api/assignments", `{"customer_phone":"+919876543210","contact_id":`+itoa(c1.ID)+`}`, nil)
	if w.Code != 200 {
		t.Fatalf("upsert code=%d body=%s", w.Code, w.Body.String())
	}
	var asg models.Assignment
	hn.db.First(&asg, "customer_phone = ?", "9876543210")
	if asg.ContactID != c1.ID {
		t.Fatalf("expected contact %d, got %d", c1.ID, asg.ContactID)
	}

	// reassign to c2
	hn.req("PUT", "/api/assignments", `{"customer_phone":"09876543210","contact_id":`+itoa(c2.ID)+`}`, nil)
	var count int64
	hn.db.Model(&models.Assignment{}).Where("customer_phone = ?", "9876543210").Count(&count)
	if count != 1 {
		t.Fatalf("expected 1 assignment after reassign, got %d", count)
	}
	hn.db.First(&asg, "customer_phone = ?", "9876543210")
	if asg.ContactID != c2.ID {
		t.Errorf("expected reassignment to %d, got %d", c2.ID, asg.ContactID)
	}
}

func TestListCallsFilters(t *testing.T) {
	hn := newHarness(t)
	hn.db.Create(&models.Call{ExotelSID: "c1", Status: "completed", FromNumber: "091"})
	hn.db.Create(&models.Call{ExotelSID: "c2", Status: "failed", FromNumber: "092"})

	w := hn.req("GET", "/api/calls?status=completed", "", nil)
	var resp struct {
		Calls []models.Call `json:"calls"`
		Count int           `json:"count"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Count != 1 || resp.Calls[0].ExotelSID != "c1" {
		t.Errorf("status filter wrong: %+v", resp)
	}

	w = hn.req("GET", "/api/calls?from=092", "", nil)
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Count != 1 || resp.Calls[0].ExotelSID != "c2" {
		t.Errorf("from filter wrong: %+v", resp)
	}
}

func itoa(u uint) string {
	return strconv.FormatUint(uint64(u), 10)
}
