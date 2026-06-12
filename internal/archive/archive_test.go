package archive_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/wareongo/exotel-call-service/internal/archive"
	"github.com/wareongo/exotel-call-service/internal/exotel"
	"github.com/wareongo/exotel-call-service/internal/models"
	"github.com/wareongo/exotel-call-service/internal/testutil"
)

// fakeStore implements archive.ObjectStore in memory; failKey forces a failure.
type fakeStore struct {
	mu      sync.Mutex
	puts    map[string][]byte
	failKey string
}

func (f *fakeStore) Put(ctx context.Context, key string, body io.Reader, size int64, ct string) error {
	if key == f.failKey {
		return errors.New("simulated R2 failure")
	}
	b, _ := io.ReadAll(body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.puts[key] = b
	return nil
}

func TestArchiveRun(t *testing.T) {
	gdb := testutil.NewDB(t)

	// Recording host: 200 for c1/c6, 404 for c3.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "c1"), strings.Contains(r.URL.Path, "c6"):
			_, _ = w.Write([]byte("AUDIO"))
		default:
			w.WriteHeader(404)
		}
	}))
	defer ts.Close()

	rec := func(sid string) string { return ts.URL + "/rec/" + sid + ".mp3" }

	// Seed fixtures.
	gdb.Create(&models.Call{ExotelSID: "c1", RecordingURL: rec("c1")})                              // -> archived
	gdb.Create(&models.Call{ExotelSID: "c2", RecordingURL: rec("c2"), RecordingR2Key: "already"})   // -> skipped (has key)
	gdb.Create(&models.Call{ExotelSID: "c3", RecordingURL: rec("c3")})                              // -> download 404 -> fail
	gdb.Create(&models.Call{ExotelSID: "c4"})                                                       // -> skipped (no url)
	gdb.Create(&models.Call{ExotelSID: "c5", RecordingURL: rec("c5"), RecordingArchiveAttempts: 5}) // -> skipped (max attempts)
	gdb.Create(&models.Call{ExotelSID: "c6", RecordingURL: rec("c6")})                              // -> store Put fails

	store := &fakeStore{puts: map[string][]byte{}, failKey: "recordings/acct1/c6.mp3"}
	a := &archive.Archiver{
		DB:          gdb,
		Exotel:      exotel.NewWithBaseURL("k", "t", "acct1", ts.URL),
		Store:       store,
		KeyPrefix:   "recordings",
		AccountSID:  "acct1",
		BatchSize:   50,
		MaxAttempts: 5,
		Workers:     2,
	}

	sum, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	// Considered: c1, c3, c6 (c2 has key, c4 no url, c5 maxed out).
	if sum.Considered != 3 || sum.Archived != 1 || sum.Failed != 2 {
		t.Fatalf("summary = %+v, want considered=3 archived=1 failed=2", sum)
	}

	// c1 archived
	var c1 models.Call
	gdb.First(&c1, "exotel_sid = ?", "c1")
	if c1.RecordingR2Key != "recordings/acct1/c1.mp3" || c1.RecordingArchivedAt == nil {
		t.Errorf("c1 not archived: %+v", c1)
	}
	if string(store.puts["recordings/acct1/c1.mp3"]) != "AUDIO" {
		t.Errorf("c1 bytes not uploaded")
	}

	// c3 failed (download 404)
	var c3 models.Call
	gdb.First(&c3, "exotel_sid = ?", "c3")
	if c3.RecordingR2Key != "" || c3.RecordingArchiveAttempts != 1 || c3.RecordingArchiveError == "" {
		t.Errorf("c3 failure not recorded: %+v", c3)
	}

	// c6 failed (store put)
	var c6 models.Call
	gdb.First(&c6, "exotel_sid = ?", "c6")
	if c6.RecordingR2Key != "" || c6.RecordingArchiveAttempts != 1 {
		t.Errorf("c6 failure not recorded: %+v", c6)
	}

	// c5 untouched (already at max attempts)
	var c5 models.Call
	gdb.First(&c5, "exotel_sid = ?", "c5")
	if c5.RecordingArchiveAttempts != 5 || c5.RecordingR2Key != "" {
		t.Errorf("c5 should be untouched: %+v", c5)
	}
}

func TestArchiveRunNothingToDo(t *testing.T) {
	gdb := testutil.NewDB(t)
	a := &archive.Archiver{DB: gdb, Store: &fakeStore{puts: map[string][]byte{}}, MaxAttempts: 5, Workers: 1, BatchSize: 10}
	sum, err := a.Run(context.Background())
	if err != nil || sum.Considered != 0 {
		t.Fatalf("expected no-op, got sum=%+v err=%v", sum, err)
	}
}
