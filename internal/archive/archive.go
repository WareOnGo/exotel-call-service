package archive

import (
	"bytes"
	"context"
	"log"
	"path"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/wareongo/exotel-call-service/internal/exotel"
	"github.com/wareongo/exotel-call-service/internal/models"
)

// Summary reports an archive run.
type Summary struct {
	Considered int `json:"considered"`
	Archived   int `json:"archived"`
	Failed     int `json:"failed"`
}

// Archiver copies un-archived call recordings from Exotel into an ObjectStore
// (R2) and records the destination key on the call row. Idempotent and
// resumable: it only picks rows where the recording exists but R2 key is null,
// and skips rows that have already failed MaxAttempts times.
type Archiver struct {
	DB          *gorm.DB
	Exotel      *exotel.Client
	Store       ObjectStore
	KeyPrefix   string // e.g. "recordings"
	AccountSID  string // e.g. "<account_sid>" — part of the object key
	BatchSize   int
	MaxAttempts int
	Workers     int
}

// Run archives one batch of recordings.
func (a *Archiver) Run(ctx context.Context) (Summary, error) {
	var calls []models.Call
	err := a.DB.WithContext(ctx).
		// GORM stores unset strings as '' (not NULL), so check both.
		Where("recording_url <> '' AND (recording_r2_key IS NULL OR recording_r2_key = '')").
		// COALESCE: rows that predate the column (added via AutoMigrate) have a
		// NULL attempts, and `NULL < n` is NULL (not true) — which would silently
		// exclude them forever. Treat NULL as 0.
		Where("COALESCE(recording_archive_attempts, 0) < ?", a.MaxAttempts).
		Order("start_time asc nulls last"). // oldest first — closest to Exotel's ~6mo expiry
		Limit(a.BatchSize).
		Find(&calls).Error
	if err != nil {
		return Summary{}, err
	}
	sum := Summary{Considered: len(calls)}
	if len(calls) == 0 {
		return sum, nil
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, max(1, a.Workers))
	for i := range calls {
		call := calls[i]
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if err := a.archiveOne(ctx, call); err != nil {
				log.Printf("archive: %s failed: %v", call.ExotelSID, err)
				mu.Lock()
				sum.Failed++
				mu.Unlock()
				return
			}
			mu.Lock()
			sum.Archived++
			mu.Unlock()
		}()
	}
	wg.Wait()
	return sum, nil
}

func (a *Archiver) archiveOne(ctx context.Context, call models.Call) error {
	key := path.Join(a.KeyPrefix, a.AccountSID, call.ExotelSID+".mp3")

	data, ct, err := a.Exotel.DownloadRecording(ctx, call.RecordingURL)
	if err != nil {
		a.markFailure(call.ID, err)
		return err
	}
	if ct == "" {
		ct = "audio/mpeg"
	}
	if err := a.Store.Put(ctx, key, bytes.NewReader(data), int64(len(data)), ct); err != nil {
		a.markFailure(call.ID, err)
		return err
	}
	return a.DB.WithContext(ctx).Model(&models.Call{}).
		Where("id = ?", call.ID).
		Updates(map[string]any{
			"recording_r2_key":        key,
			"recording_archived_at":   time.Now(),
			"recording_archive_error": "",
		}).Error
}

func (a *Archiver) markFailure(id uint, cause error) {
	msg := cause.Error()
	if len(msg) > 500 {
		msg = msg[:500]
	}
	a.DB.Model(&models.Call{}).Where("id = ?", id).Updates(map[string]any{
		"recording_archive_attempts": gorm.Expr("recording_archive_attempts + 1"),
		"recording_archive_error":    msg,
	})
}

// Runner invokes the archiver on an interval (nightly by default).
type Runner struct {
	Archiver *Archiver
	Interval time.Duration
}

func (r *Runner) Start(ctx context.Context) {
	log.Printf("archive: runner started (every %s)", r.Interval)
	t := time.NewTicker(r.Interval)
	defer t.Stop()
	r.runOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			log.Printf("archive: runner stopped")
			return
		case <-t.C:
			r.runOnce(ctx)
		}
	}
}

func (r *Runner) runOnce(ctx context.Context) {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	sum, err := r.Archiver.Run(cctx)
	if err != nil {
		log.Printf("archive: run error: %v", err)
		return
	}
	if sum.Considered > 0 {
		log.Printf("archive: considered=%d archived=%d failed=%d",
			sum.Considered, sum.Archived, sum.Failed)
	}
}
