package exotel_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wareongo/exotel-call-service/internal/exotel"
)

func newClient(baseURL string) *exotel.Client {
	return exotel.NewWithBaseURL("KEY", "TOKEN", "acct1", baseURL)
}

func timeZero() time.Time { return time.Time{} }

func TestParseTimeIST(t *testing.T) {
	got := exotel.ParseTime("2026-06-11 15:12:07")
	if got == nil {
		t.Fatal("expected parsed time, got nil")
	}
	// 15:12:07 IST == 09:42:07 UTC
	if u := got.UTC(); u.Hour() != 9 || u.Minute() != 42 {
		t.Errorf("expected 09:42 UTC, got %02d:%02d", u.Hour(), u.Minute())
	}
	if exotel.ParseTime("") != nil || exotel.ParseTime("garbage") != nil {
		t.Error("expected nil for empty/garbage input")
	}
}

func TestAsIntAsFloat(t *testing.T) {
	if exotel.AsInt(float64(169)) != 169 || exotel.AsInt("169") != 169 || exotel.AsInt(nil) != 0 {
		t.Error("AsInt coercion wrong")
	}
	if exotel.AsFloat(float64(4.5)) != 4.5 || exotel.AsFloat("4.5") != 4.5 || exotel.AsFloat(nil) != 0 {
		t.Error("AsFloat coercion wrong")
	}
}

func TestConnectTwoNumbers(t *testing.T) {
	var gotAuth, gotForm string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = r.ParseForm()
		gotForm = r.Form.Get("From") + "|" + r.Form.Get("To") + "|" + r.Form.Get("CallerId")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"Call":{"Sid":"abc","Status":"queued"}}`))
	}))
	defer ts.Close()

	out, err := newClient(ts.URL).ConnectTwoNumbers(context.Background(), "0911", "0922", "0933")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if call, ok := out["Call"].(map[string]any); !ok || call["Sid"] != "abc" {
		t.Errorf("unexpected response: %+v", out)
	}
	if !strings.HasPrefix(gotAuth, "Basic ") {
		t.Errorf("expected basic auth header, got %q", gotAuth)
	}
	if gotForm != "0911|0922|0933" {
		t.Errorf("form fields wrong: %q", gotForm)
	}
}

func TestConnectTwoNumbersError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`bad request`))
	}))
	defer ts.Close()
	if _, err := newClient(ts.URL).ConnectTwoNumbers(context.Background(), "a", "b", "c"); err == nil {
		t.Fatal("expected error on 400")
	}
}

func TestListCallsPagePagination(t *testing.T) {
	var calls int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(200)
		if r.URL.Query().Get("After") == "" && !strings.Contains(r.URL.RawQuery, "After") {
			// first page -> has NextPageUri (note: Duration as string, exercises AsInt)
			_, _ = w.Write([]byte(`{"Metadata":{"NextPageUri":"/v1/Accounts/acct1/Calls.json?After=xyz"},
				"Calls":[{"Sid":"c1","Duration":"30","Price":"1.5"}]}`))
			return
		}
		// second page -> no next
		_, _ = w.Write([]byte(`{"Metadata":{"NextPageUri":""},"Calls":[{"Sid":"c2","Duration":45}]}`))
	}))
	defer ts.Close()

	c := newClient(ts.URL)
	page1, next, err := c.ListCallsPage(context.Background(), timeZero(), timeZero(), "")
	if err != nil || len(page1) != 1 || next == "" {
		t.Fatalf("page1: err=%v len=%d next=%q", err, len(page1), next)
	}
	if exotel.AsInt(page1[0].Duration) != 30 {
		t.Errorf("expected Duration 30, got %v", page1[0].Duration)
	}
	page2, next2, err := c.ListCallsPage(context.Background(), timeZero(), timeZero(), next)
	if err != nil || len(page2) != 1 || next2 != "" {
		t.Fatalf("page2: err=%v len=%d next=%q", err, len(page2), next2)
	}
	if calls != 2 {
		t.Errorf("expected 2 HTTP calls, got %d", calls)
	}
}

func TestGetCallDetails(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "details=true") {
			t.Errorf("expected details=true, got %q", r.URL.RawQuery)
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"Call":{"RecordingUrl":"http://rec/x.mp3","Details":{
			"ConversationDuration":144,"Leg1Status":"completed","Leg2Status":"completed",
			"Legs":[{"Leg":{"Id":1,"OnCallDuration":156}},{"Leg":{"Id":2,"OnCallDuration":144}}]}}}`))
	}))
	defer ts.Close()

	d, err := newClient(ts.URL).GetCallDetails(context.Background(), "sid1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.ConversationDuration != 144 || d.Leg1Duration != 156 || d.Leg2Duration != 144 {
		t.Errorf("bad details: %+v", d)
	}
	if d.Leg1Status != "completed" || d.RecordingURL != "http://rec/x.mp3" {
		t.Errorf("bad details: %+v", d)
	}
}

func TestDownloadRecording(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("ID3audio-bytes"))
	}))
	defer ts.Close()

	data, ct, err := newClient(ts.URL).DownloadRecording(context.Background(), ts.URL+"/rec.mp3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "ID3audio-bytes" || ct != "audio/mpeg" {
		t.Errorf("bad download: data=%q ct=%q", data, ct)
	}
}

func TestDownloadRecordingErrorStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer ts.Close()
	if _, _, err := newClient(ts.URL).DownloadRecording(context.Background(), ts.URL+"/missing.mp3"); err == nil {
		t.Fatal("expected error on 404")
	}
}
