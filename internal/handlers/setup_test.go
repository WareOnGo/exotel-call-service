package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/wareongo/exotel-call-service/internal/config"
	"github.com/wareongo/exotel-call-service/internal/exotel"
	"github.com/wareongo/exotel-call-service/internal/handlers"
	"github.com/wareongo/exotel-call-service/internal/models"
	"github.com/wareongo/exotel-call-service/internal/server"
	"github.com/wareongo/exotel-call-service/internal/testutil"
)

func init() { gin.SetMode(gin.TestMode) }

func testCfg() *config.Config {
	return &config.Config{
		RouterDBTimeout: 2 * time.Second,
		SyncLookback:    time.Hour,
		ExotelSubdomain: "api.exotel.com",
		ExotelSID:       "acct1",
	}
}

type harness struct {
	engine *gin.Engine
	db     *gorm.DB
	h      *handlers.Handler
	cfg    *config.Config
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	gdb := testutil.NewDB(t)
	cfg := testCfg()
	ex := exotel.NewWithBaseURL("k", "t", "acct1", "http://unused.invalid")
	h := handlers.New(gdb, ex, cfg)
	// server.New captures the *Handler pointer; mutating h.Exotel/h.Archiver
	// after this still affects requests (methods read the fields live).
	return &harness{engine: server.New(h), db: gdb, h: h, cfg: cfg}
}

func (hn *harness) withExotel(url string) {
	hn.h.Exotel = exotel.NewWithBaseURL("k", "t", "acct1", url)
}

func mockServer(t *testing.T, fn http.HandlerFunc) *httptest.Server {
	ts := httptest.NewServer(fn)
	t.Cleanup(ts.Close)
	return ts
}

func (hn *harness) req(method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	hn.engine.ServeHTTP(w, r)
	return w
}

func seedContact(t *testing.T, gdb *gorm.DB, c models.Contact) models.Contact {
	t.Helper()
	if err := gdb.Create(&c).Error; err != nil {
		t.Fatalf("seed contact: %v", err)
	}
	return c
}

// waitFor polls until cond is true (for async writes like the assignment upsert).
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met within deadline")
}
