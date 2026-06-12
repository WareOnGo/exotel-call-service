// Package assign derives sticky customer→POC assignments from call records.
//
// Inbound calls create assignments via the router. Outbound calls (a POC dialing
// a client) establish the same relationship — so we capture them here at call
// ingestion (reconcile + webhook), and can backfill existing history.
package assign

import (
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/wareongo/exotel-call-service/internal/models"
	"github.com/wareongo/exotel-call-service/internal/util"
)

// CaptureOutbound binds a client to the POC who called them, for an outbound
// call. Convention: for outbound calls From = the POC (dialer), To = the client.
// No-op unless the call is outbound and From matches an active contact.
// Returns whether an assignment was created/updated.
func CaptureOutbound(db *gorm.DB, call *models.Call) (bool, error) {
	if !strings.HasPrefix(strings.ToLower(call.Direction), "outbound") {
		return false, nil
	}
	pocKey := util.NormalizePhone(call.FromNumber)
	clientKey := util.NormalizePhone(call.ToNumber)
	if pocKey == "" || clientKey == "" {
		return false, nil
	}

	var poc models.Contact
	if err := db.Where("phone_key = ? AND active = ?", pocKey, true).
		Limit(1).Find(&poc).Error; err != nil {
		return false, err
	}
	if poc.ID == 0 {
		return false, nil // From isn't one of our POCs — ignore
	}

	when := time.Now()
	if call.StartTime != nil {
		when = *call.StartTime
	}
	return upsertLatest(db, clientKey, poc.ID, when)
}

// upsertLatest binds clientKey -> contactID, but never lets an older call
// overwrite a more recent assignment (important when reconcile replays history).
func upsertLatest(db *gorm.DB, clientKey string, contactID uint, when time.Time) (bool, error) {
	var asg models.Assignment
	if err := db.Where("customer_phone = ?", clientKey).Limit(1).Find(&asg).Error; err != nil {
		return false, err
	}
	if asg.ID == 0 {
		err := db.Create(&models.Assignment{
			CustomerPhone:   clientKey,
			ContactID:       contactID,
			LastContactedAt: when,
		}).Error
		return err == nil, err
	}
	if when.Before(asg.LastContactedAt) {
		return false, nil // a more recent call already set this assignment
	}
	err := db.Model(&asg).Updates(map[string]any{
		"contact_id":        contactID,
		"last_contacted_at": when,
	}).Error
	return err == nil, err
}

// Backfill applies CaptureOutbound to every outbound call already stored, oldest
// first so the most recent POC wins per client. Returns assignments written.
func Backfill(db *gorm.DB) (int, error) {
	var calls []models.Call
	if err := db.Where("LOWER(direction) LIKE 'outbound%'").
		Order("start_time asc nulls last").
		Find(&calls).Error; err != nil {
		return 0, err
	}
	written := 0
	for i := range calls {
		changed, err := CaptureOutbound(db, &calls[i])
		if err != nil {
			return written, err
		}
		if changed {
			written++
		}
	}
	return written, nil
}
