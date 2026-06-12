package config

import (
	"log"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

// Config holds all runtime configuration, loaded from environment / .env.
type Config struct {
	Port string

	DatabaseURL string

	ExotelKey       string
	ExotelToken     string
	ExotelSID       string
	ExotelSubdomain string

	// DefaultFallbackNumber is dialed when routing cannot resolve a contact
	// (DB slow/down, or no POC configured). Keeps callers from hearing dead air.
	DefaultFallbackNumber string

	// RouterDBTimeout caps the assignment lookup on the hot /route path.
	RouterDBTimeout time.Duration

	// Reconcile settings.
	SyncInterval time.Duration // how often the background reconciler runs
	SyncLookback time.Duration // how far back each reconcile re-fetches
	SyncSecret   string        // shared secret to guard POST /sync

	// CallerID is the ExoPhone used as the caller id for outbound connects
	// when the request does not specify one.
	DefaultCallerID string

	// Cloudflare R2 (S3-compatible) recording archive.
	R2Endpoint        string // full endpoint; overrides R2AccountID if set
	R2AccountID       string // used to derive endpoint when R2Endpoint is empty
	R2AccessKeyID     string
	R2SecretAccessKey string
	R2Bucket          string
	R2KeyPrefix       string

	ArchiveInterval    time.Duration
	ArchiveBatchSize   int
	ArchiveMaxAttempts int
	ArchiveWorkers     int
}

// R2EndpointURL returns the configured endpoint, deriving it from the account
// id when an explicit endpoint isn't given.
func (c *Config) R2EndpointURL() string {
	if c.R2Endpoint != "" {
		return c.R2Endpoint
	}
	if c.R2AccountID != "" {
		return "https://" + c.R2AccountID + ".r2.cloudflarestorage.com"
	}
	return ""
}

// R2Configured reports whether enough is set to talk to R2.
func (c *Config) R2Configured() bool {
	return c.R2Bucket != "" && c.R2AccessKeyID != "" &&
		c.R2SecretAccessKey != "" && c.R2EndpointURL() != ""
}

func Load() *Config {
	if err := godotenv.Load(); err != nil {
		log.Printf("config: no .env loaded (%v) — relying on real env", err)
	}
	cfg := &Config{
		Port:                  getenv("PORT", "8080"),
		DatabaseURL:           os.Getenv("DATABASE_URL"),
		ExotelKey:             os.Getenv("EXOTEL_API_KEY"),
		ExotelToken:           os.Getenv("EXOTEL_API_TOKEN"),
		ExotelSID:             os.Getenv("EXOTEL_ACCOUNT_SID"),
		ExotelSubdomain:       getenv("EXOTEL_SUBDOMAIN", "api.exotel.com"),
		DefaultFallbackNumber: os.Getenv("DEFAULT_FALLBACK_NUMBER"),
		RouterDBTimeout:       time.Duration(getint("ROUTER_DB_TIMEOUT_MS", 150)) * time.Millisecond,
		SyncInterval:          time.Duration(getint("SYNC_INTERVAL_MIN", 15)) * time.Minute,
		SyncLookback:          time.Duration(getint("SYNC_LOOKBACK_MIN", 90)) * time.Minute,
		SyncSecret:            os.Getenv("SYNC_SECRET"),
		DefaultCallerID:       os.Getenv("DEFAULT_CALLER_ID"),

		R2Endpoint:         os.Getenv("R2_ENDPOINT"),
		R2AccountID:        os.Getenv("R2_ACCOUNT_ID"),
		R2AccessKeyID:      os.Getenv("R2_ACCESS_KEY_ID"),
		R2SecretAccessKey:  os.Getenv("R2_SECRET_ACCESS_KEY"),
		R2Bucket:           os.Getenv("R2_BUCKET"),
		R2KeyPrefix:        getenv("R2_KEY_PREFIX", "recordings"),
		ArchiveInterval:    time.Duration(getint("ARCHIVE_INTERVAL_HOURS", 24)) * time.Hour,
		ArchiveBatchSize:   getint("ARCHIVE_BATCH_SIZE", 200),
		ArchiveMaxAttempts: getint("ARCHIVE_MAX_ATTEMPTS", 5),
		ArchiveWorkers:     getint("ARCHIVE_WORKERS", 4),
	}
	return cfg
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getint(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
