package reconcile

import (
	"context"
	"log"
	"time"

	"gorm.io/gorm"

	"github.com/wareongo/exotel-call-service/internal/assign"
	"github.com/wareongo/exotel-call-service/internal/exotel"
	"github.com/wareongo/exotel-call-service/internal/models"
)

// Summary reports what a reconcile run did.
type Summary struct {
	Fetched  int       `json:"fetched"`
	Upserted int       `json:"upserted"`
	From     time.Time `json:"from"`
	To       time.Time `json:"to"`
}

// Sync re-fetches calls in [now-lookback, now] from Exotel and upserts them.
// This is the backstop that fills any gaps the StatusCallback webhook missed.
// Idempotent: re-running over the same window is safe.
func Sync(ctx context.Context, db *gorm.DB, client *exotel.Client, lookback time.Duration) (Summary, error) {
	to := time.Now()
	from := to.Add(-lookback)
	sum := Summary{From: from, To: to}

	nextURI := ""
	for {
		records, next, err := client.ListCallsPage(ctx, from, to, nextURI)
		if err != nil {
			return sum, err
		}
		for _, r := range records {
			sum.Fetched++
			call := &models.Call{
				ExotelSID:            r.Sid,
				FromNumber:           r.From,
				ToNumber:             r.To,
				Exophone:             r.PhoneNumber,
				Direction:            r.Direction,
				Status:               r.Status,
				StartTime:            exotel.ParseTime(r.StartTime),
				EndTime:              exotel.ParseTime(r.EndTime),
				Duration:             exotel.AsInt(r.Duration),
				ConversationDuration: exotel.AsInt(r.ConversationDuration),
				Price:                exotel.AsFloat(r.Price),
				RecordingURL:         r.RecordingURL,
			}

			// Enrich with talk-time + per-leg data (only on details=true).
			// Best-effort: if it fails we still persist the list-level record.
			if det, derr := client.GetCallDetails(ctx, r.Sid); derr != nil {
				log.Printf("reconcile: details %s failed: %v (keeping list data)", r.Sid, derr)
			} else {
				call.ConversationDuration = det.ConversationDuration
				call.Leg1Status = det.Leg1Status
				call.Leg2Status = det.Leg2Status
				call.Leg1Duration = det.Leg1Duration
				call.Leg2Duration = det.Leg2Duration
				if det.RecordingURL != "" {
					call.RecordingURL = det.RecordingURL
				}
			}
			// Throttle to stay well under the 200 req/min voice-API cap.
			time.Sleep(350 * time.Millisecond)

			if err := models.UpsertCall(db, call); err != nil {
				log.Printf("reconcile: upsert %s failed: %v", r.Sid, err)
				continue
			}
			sum.Upserted++

			// Inbound + outbound calls both seed sticky routing.
			if _, err := assign.FromCall(db, call); err != nil {
				log.Printf("reconcile: capture assignment %s: %v", r.Sid, err)
			}
		}
		if next == "" {
			break
		}
		nextURI = next
	}
	return sum, nil
}

// Runner periodically invokes Sync until its context is cancelled.
type Runner struct {
	DB       *gorm.DB
	Client   *exotel.Client
	Interval time.Duration
	Lookback time.Duration
}

func (r *Runner) Start(ctx context.Context) {
	log.Printf("reconcile: runner started (every %s, lookback %s)", r.Interval, r.Lookback)
	t := time.NewTicker(r.Interval)
	defer t.Stop()
	// kick once on boot to catch anything missed while down
	r.runOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			log.Printf("reconcile: runner stopped")
			return
		case <-t.C:
			r.runOnce(ctx)
		}
	}
}

func (r *Runner) runOnce(ctx context.Context) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	sum, err := Sync(cctx, r.DB, r.Client, r.Lookback)
	if err != nil {
		log.Printf("reconcile: run error: %v (fetched=%d upserted=%d)", err, sum.Fetched, sum.Upserted)
		return
	}
	log.Printf("reconcile: ok fetched=%d upserted=%d window=%s..%s",
		sum.Fetched, sum.Upserted, sum.From.Format(time.RFC3339), sum.To.Format(time.RFC3339))
}
