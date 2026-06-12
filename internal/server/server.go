package server

import (
	"github.com/gin-gonic/gin"

	"github.com/wareongo/exotel-call-service/internal/handlers"
)

// New builds the Gin engine and wires routes.
func New(h *handlers.Handler) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	// --- Exotel-facing (public; Exotel calls these) ---
	r.GET("/healthz", h.Health)
	// Connect applet dynamic URL — Exotel may use GET or POST depending on setup.
	r.GET("/route", h.Route)
	r.POST("/route", h.Route)
	// StatusCallback ingestion.
	r.POST("/webhooks/call-status", h.CallStatusWebhook)

	// --- Ops / cron (guard with SYNC_SECRET) ---
	r.POST("/sync", h.Sync)
	r.POST("/archive-recordings", h.ArchiveRecordings)
	r.POST("/backfill-assignments", h.BackfillAssignments)

	// --- Admin / dashboard API ---
	api := r.Group("/api")
	{
		api.POST("/calls/connect", h.ConnectTwoNumbers)
		api.GET("/calls", h.ListCalls)

		api.GET("/contacts", h.ListContacts)
		api.POST("/contacts", h.CreateContact)
		api.PATCH("/contacts/:id", h.UpdateContact)

		api.GET("/assignments", h.ListAssignments)
		api.PUT("/assignments", h.UpsertAssignment)
	}

	return r
}
