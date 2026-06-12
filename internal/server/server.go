package server

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/wareongo/exotel-call-service/internal/config"
	"github.com/wareongo/exotel-call-service/internal/handlers"
)

// apiAuth guards the admin /api/* endpoints with a bearer token (or X-Api-Key).
// Fails CLOSED: if API_TOKEN isn't configured, the endpoints are unavailable
// rather than silently open.
func apiAuth(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		if cfg.APIToken == "" {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable,
				gin.H{"error": "API auth not configured (set API_TOKEN)"})
			return
		}
		tok := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		if tok == "" {
			tok = c.GetHeader("X-Api-Key")
		}
		if subtle.ConstantTimeCompare([]byte(tok), []byte(cfg.APIToken)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}

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

	// --- Admin / dashboard API (token-guarded) ---
	api := r.Group("/api", apiAuth(h.Cfg))
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
