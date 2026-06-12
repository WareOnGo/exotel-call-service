package handlers

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/wareongo/exotel-call-service/internal/assign"
	"github.com/wareongo/exotel-call-service/internal/exotel"
	"github.com/wareongo/exotel-call-service/internal/models"
)

// CallStatusWebhook ingests Exotel StatusCallback events (fired when a call
// completes / changes state) and upserts the call record. Idempotent on
// exotel_sid, so Exotel retries / duplicates are safe.
func (h *Handler) CallStatusWebhook(c *gin.Context) {
	sid := param(c, "CallSid", "Sid")
	if sid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing CallSid"})
		return
	}

	call := &models.Call{
		ExotelSID:            sid,
		FromNumber:           param(c, "From", "CallFrom"),
		ToNumber:             param(c, "To", "CallTo"),
		Exophone:             param(c, "PhoneNumber", "DialWhomNumber"),
		Direction:            param(c, "Direction"),
		Status:               param(c, "Status", "CallStatus", "DialCallStatus"),
		StartTime:            exotel.ParseTime(param(c, "StartTime")),
		EndTime:              exotel.ParseTime(param(c, "EndTime")),
		Duration:             exotel.AsInt(param(c, "Duration", "DialCallDuration")),
		ConversationDuration: exotel.AsInt(param(c, "ConversationDuration")),
		Price:                exotel.AsFloat(param(c, "Price")),
		RecordingURL:         param(c, "RecordingUrl"),
	}

	// Only overwrite columns the webhook actually carries (avoid nuking
	// recording_url/price that may arrive via reconcile instead).
	cols := []string{"to_number", "from_number", "direction", "status",
		"end_time", "duration", "updated_at"}
	if call.RecordingURL != "" {
		cols = append(cols, "recording_url")
	}
	if call.StartTime != nil {
		cols = append(cols, "start_time")
	}

	if err := models.UpsertCall(h.DB, call, cols...); err != nil {
		log.Printf("webhook: upsert call %s failed: %v", sid, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "persist failed"})
		return
	}

	// Outbound POC→client calls seed sticky routing.
	if _, err := assign.CaptureOutbound(h.DB, call); err != nil {
		log.Printf("webhook: capture assignment %s: %v", sid, err)
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
