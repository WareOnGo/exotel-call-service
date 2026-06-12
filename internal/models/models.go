package models

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/wareongo/exotel-call-service/internal/util"
)

// Contact is an internal POC / agent that calls can be routed to.
type Contact struct {
	ID    uint   `gorm:"primaryKey" json:"id"`
	Name  string `json:"name"`
	Phone string `gorm:"uniqueIndex;not null" json:"phone"` // dialable number (E.164 or local)
	// PhoneKey is the normalized form of Phone (see util.NormalizePhone), kept in
	// sync by BeforeSave. It lets us match a call's From/To number to a POC
	// regardless of formatting. Internal — not exposed via JSON.
	PhoneKey string `gorm:"column:phone_key;index" json:"-"`
	Role     string `json:"role,omitempty"`

	// IsFirstTouch marks contacts eligible to receive brand-new callers.
	IsFirstTouch bool `gorm:"index" json:"is_first_touch"`
	// FallbackPriority orders selection; lower wins (0 = highest priority).
	FallbackPriority int `gorm:"index" json:"fallback_priority"`
	// No GORM default: a `default:true` tag would make an explicit active=false
	// be dropped on insert (zero value) and replaced by true. The create handler
	// defaults this to true instead, so false is honored.
	Active bool `gorm:"index" json:"active"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// BeforeSave keeps PhoneKey in sync with Phone on struct-based Create/Save.
// (The PATCH handler, which uses a column map, sets phone_key explicitly.)
func (c *Contact) BeforeSave(tx *gorm.DB) error {
	c.PhoneKey = util.NormalizePhone(c.Phone)
	return nil
}

// Assignment is the sticky routing ledger: which contact a customer is bound to.
// CustomerPhone is the normalized match key (see util.NormalizePhone).
type Assignment struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	CustomerPhone   string    `gorm:"uniqueIndex;not null" json:"customer_phone"`
	ContactID       uint      `gorm:"index;not null" json:"contact_id"`
	Contact         *Contact  `gorm:"foreignKey:ContactID" json:"contact,omitempty"`
	LastContactedAt time.Time `json:"last_contacted_at"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Call mirrors an Exotel call record (synced via webhook + reconcile).
type Call struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	ExotelSID  string `gorm:"column:exotel_sid;uniqueIndex;not null" json:"exotel_sid"`
	FromNumber string `gorm:"column:from_number;index" json:"from"`
	ToNumber   string `gorm:"column:to_number;index" json:"to"`
	Exophone   string `gorm:"column:exophone" json:"exophone,omitempty"`
	Direction  string `json:"direction,omitempty"`
	Status     string `gorm:"index" json:"status,omitempty"`

	StartTime *time.Time `json:"start_time,omitempty"`
	EndTime   *time.Time `json:"end_time,omitempty"`

	Duration             int     `json:"duration"`              // total seconds
	ConversationDuration int     `json:"conversation_duration"` // talk time (from details=true)
	Price                float64 `json:"price"`

	// Per-leg breakdown (from details=true). Leg 1 = the first party dialed,
	// Leg 2 = the second; OnCallDuration is each leg's connected seconds.
	Leg1Status   string `gorm:"column:leg1_status" json:"leg1_status,omitempty"`
	Leg2Status   string `gorm:"column:leg2_status" json:"leg2_status,omitempty"`
	Leg1Duration int    `gorm:"column:leg1_duration" json:"leg1_duration"`
	Leg2Duration int    `gorm:"column:leg2_duration" json:"leg2_duration"`

	RecordingURL string `gorm:"column:recording_url" json:"recording_url,omitempty"`

	// R2 archival (owned by the future archive worker — the reconciler never
	// touches these; they're absent from defaultCallUpdateCols on purpose).
	// Store the R2 object KEY (e.g. "recordings/<account_sid>/<sid>.mp3"), not a
	// URL — keys are storage-domain agnostic (no lock-in).
	RecordingR2Key           string     `gorm:"column:recording_r2_key;index" json:"recording_r2_key,omitempty"`
	RecordingArchivedAt      *time.Time `gorm:"column:recording_archived_at" json:"recording_archived_at,omitempty"`
	RecordingArchiveAttempts int        `gorm:"column:recording_archive_attempts" json:"recording_archive_attempts"`
	RecordingArchiveError    string     `gorm:"column:recording_archive_error" json:"recording_archive_error,omitempty"`

	AssignedContactID *uint    `gorm:"index" json:"assigned_contact_id,omitempty"`
	AssignedContact   *Contact `gorm:"foreignKey:AssignedContactID" json:"assigned_contact,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// defaultCallUpdateCols are refreshed on conflict during a full reconcile.
var defaultCallUpdateCols = []string{
	"from_number", "to_number", "exophone", "direction", "status",
	"start_time", "end_time", "duration", "conversation_duration",
	"price", "recording_url", "updated_at",
	"leg1_status", "leg2_status", "leg1_duration", "leg2_duration",
}

// UpsertCall inserts or updates a Call keyed by exotel_sid. Pass updateCols to
// restrict which columns are overwritten on conflict (used by the webhook,
// which has only partial data and must not clobber fields with empties).
func UpsertCall(db *gorm.DB, c *Call, updateCols ...string) error {
	cols := updateCols
	if len(cols) == 0 {
		cols = defaultCallUpdateCols
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "exotel_sid"}},
		DoUpdates: clause.AssignmentColumns(cols),
	}).Create(c).Error
}

// Migrate creates/updates tables. For production you may prefer pinned SQL
// migrations (golang-migrate/goose); AutoMigrate is convenient for dev and is
// additive-only (it never drops columns).
func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(&Contact{}, &Assignment{}, &Call{})
}
