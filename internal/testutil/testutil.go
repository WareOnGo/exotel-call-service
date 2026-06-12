// Package testutil provides shared test fixtures. It is imported only from
// _test.go files, so it is never compiled into the server binary.
package testutil

import (
	"os"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/wareongo/exotel-call-service/internal/db"
	"github.com/wareongo/exotel-call-service/internal/models"
)

// NewDB returns a migrated, empty database for a test. By default it uses an
// in-memory SQLite instance (hermetic, no external services). If TEST_DATABASE_URL
// is set, it uses that Postgres instead and truncates between tests — so the
// exact same suite can be run against the real engine in CI.
func NewDB(t *testing.T) *gorm.DB {
	t.Helper()

	if dsn := os.Getenv("TEST_DATABASE_URL"); dsn != "" {
		gdb, err := db.Open(dsn)
		if err != nil {
			t.Fatalf("open test postgres: %v", err)
		}
		if err := models.Migrate(gdb); err != nil {
			t.Fatalf("migrate: %v", err)
		}
		truncate(t, gdb)
		t.Cleanup(func() { truncate(t, gdb) })
		return gdb
	}

	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("sqlite handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1) // single conn keeps the :memory: DB alive for the test
	if err := models.Migrate(gdb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return gdb
}

func truncate(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	for _, tbl := range []string{"assignments", "calls", "contacts"} {
		if err := gdb.Exec("TRUNCATE TABLE " + tbl + " RESTART IDENTITY CASCADE").Error; err != nil {
			t.Fatalf("truncate %s: %v", tbl, err)
		}
	}
}
