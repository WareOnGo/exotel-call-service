package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wareongo/exotel-call-service/internal/archive"
	"github.com/wareongo/exotel-call-service/internal/config"
	"github.com/wareongo/exotel-call-service/internal/db"
	"github.com/wareongo/exotel-call-service/internal/exotel"
	"github.com/wareongo/exotel-call-service/internal/handlers"
	"github.com/wareongo/exotel-call-service/internal/models"
	"github.com/wareongo/exotel-call-service/internal/reconcile"
	"github.com/wareongo/exotel-call-service/internal/server"
)

func main() {
	cfg := config.Load()
	if cfg.DatabaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	gdb, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	if err := models.Migrate(gdb); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	db.WarmPool(gdb, 5) // pre-open connections so early /route calls skip TLS setup

	ex := exotel.New(cfg)
	h := handlers.New(gdb, ex, cfg)
	engine := server.New(h)

	// Root context cancelled on shutdown signal.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Background reconciler (real-time ingestion is the webhook; this fills gaps).
	if cfg.ExotelSID != "" {
		runner := &reconcile.Runner{
			DB: gdb, Client: ex,
			Interval: cfg.SyncInterval, Lookback: cfg.SyncLookback,
		}
		go runner.Start(ctx)
	} else {
		log.Printf("EXOTEL_ACCOUNT_SID unset — reconciler disabled")
	}

	// Recording archive (R2). Disabled until R2_* env vars are set.
	if cfg.R2Configured() {
		store, err := archive.NewR2Store(context.Background(),
			cfg.R2EndpointURL(), cfg.R2AccessKeyID, cfg.R2SecretAccessKey, cfg.R2Bucket)
		if err != nil {
			log.Fatalf("r2 init: %v", err)
		}
		h.Archiver = &archive.Archiver{
			DB: gdb, Exotel: ex, Store: store,
			KeyPrefix: cfg.R2KeyPrefix, AccountSID: cfg.ExotelSID,
			BatchSize: cfg.ArchiveBatchSize, MaxAttempts: cfg.ArchiveMaxAttempts,
			Workers: cfg.ArchiveWorkers,
		}
		go (&archive.Runner{Archiver: h.Archiver, Interval: cfg.ArchiveInterval}).Start(ctx)
	} else {
		log.Printf("R2 not configured — recording archive disabled")
	}

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      engine,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	go func() {
		log.Printf("listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Printf("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
	os.Exit(0)
}
