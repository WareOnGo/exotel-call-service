package db

import (
	"sync"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Open connects GORM to Postgres (Supabase) with a tuned connection pool.
//
// For a long-running container, use Supabase's DIRECT connection string
// (port 5432) so this pool owns the connections. If you ever move to
// serverless, switch to the transaction pooler (port 6543) instead.
func Open(dsn string) (*gorm.DB, error) {
	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger:                 logger.Default.LogMode(logger.Warn),
		SkipDefaultTransaction: true, // small latency win; we manage txns explicitly
		PrepareStmt:            true, // cache prepared statements (faster hot path)
	})
	if err != nil {
		return nil, err
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	return gdb, nil
}

// WarmPool eagerly opens up to n connections so the first requests don't pay
// TLS/connection setup latency (important when the DB is a remote pooler).
func WarmPool(gdb *gorm.DB, n int) {
	sqlDB, err := gdb.DB()
	if err != nil {
		return
	}
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = sqlDB.Ping() // concurrent pings force distinct connections into the pool
		}()
	}
	wg.Wait()
}
