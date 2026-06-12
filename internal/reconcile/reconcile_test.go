package reconcile_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wareongo/exotel-call-service/internal/exotel"
	"github.com/wareongo/exotel-call-service/internal/models"
	"github.com/wareongo/exotel-call-service/internal/reconcile"
	"github.com/wareongo/exotel-call-service/internal/testutil"
)

// mockExotel serves the list endpoint and the per-call details endpoint.
func mockExotel(t *testing.T) *httptest.Server {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		switch {
		case strings.Contains(r.URL.Path, "/Calls/c1.json"): // details
			_, _ = w.Write([]byte(`{"Call":{"Details":{"ConversationDuration":144,
				"Leg1Status":"completed","Leg2Status":"completed",
				"Legs":[{"Leg":{"Id":1,"OnCallDuration":156}},{"Leg":{"Id":2,"OnCallDuration":144}}]}}}`))
		case strings.HasSuffix(r.URL.Path, "/Calls.json"): // list
			_, _ = w.Write([]byte(`{"Metadata":{"NextPageUri":""},"Calls":[
				{"Sid":"c1","From":"091","To":"092","Status":"completed","Duration":169,
				 "StartTime":"2026-06-11 15:12:07","RecordingUrl":"http://rec/c1.mp3"}]}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestSyncFetchesAndEnriches(t *testing.T) {
	gdb := testutil.NewDB(t)
	ts := mockExotel(t)
	client := exotel.NewWithBaseURL("k", "t", "acct1", ts.URL)

	sum, err := reconcile.Sync(context.Background(), gdb, client, time.Hour)
	if err != nil {
		t.Fatalf("sync error: %v", err)
	}
	if sum.Fetched != 1 || sum.Upserted != 1 {
		t.Fatalf("summary = %+v", sum)
	}

	var call models.Call
	if err := gdb.First(&call, "exotel_sid = ?", "c1").Error; err != nil {
		t.Fatalf("call not stored: %v", err)
	}
	if call.Status != "completed" || call.Duration != 169 {
		t.Errorf("bad list fields: %+v", call)
	}
	if call.ConversationDuration != 144 || call.Leg1Duration != 156 || call.Leg2Duration != 144 {
		t.Errorf("detail enrichment missing: %+v", call)
	}
	// IST timestamp: 15:12 IST == 09:42 UTC
	if call.StartTime == nil || call.StartTime.UTC().Hour() != 9 {
		t.Errorf("start_time not parsed as IST: %v", call.StartTime)
	}
}

func TestSyncIdempotent(t *testing.T) {
	gdb := testutil.NewDB(t)
	ts := mockExotel(t)
	client := exotel.NewWithBaseURL("k", "t", "acct1", ts.URL)

	_, _ = reconcile.Sync(context.Background(), gdb, client, time.Hour)
	_, _ = reconcile.Sync(context.Background(), gdb, client, time.Hour)

	var count int64
	gdb.Model(&models.Call{}).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 row after two syncs, got %d", count)
	}
}
