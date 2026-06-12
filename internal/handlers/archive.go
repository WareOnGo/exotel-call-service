package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ArchiveRecordings copies a batch of un-archived recordings to R2. Designed to
// be hit by a daily cron (guard with SYNC_SECRET via X-Sync-Secret), and also
// runs as an in-process nightly ticker. Idempotent — safe to call repeatedly.
func (h *Handler) ArchiveRecordings(c *gin.Context) {
	if h.Cfg.SyncSecret != "" && c.GetHeader("X-Sync-Secret") != h.Cfg.SyncSecret {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid sync secret"})
		return
	}
	if h.Archiver == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "R2 not configured (set R2_* env vars)"})
		return
	}
	sum, err := h.Archiver.Run(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error(), "summary": sum})
		return
	}
	c.JSON(http.StatusOK, sum)
}
