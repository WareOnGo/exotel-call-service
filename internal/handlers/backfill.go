package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/wareongo/exotel-call-service/internal/assign"
)

// BackfillAssignments scans existing outbound calls and seeds sticky
// assignments (client -> the POC who called them). Run once after configuring
// your POC contacts. Guarded by SYNC_SECRET. Idempotent.
func (h *Handler) BackfillAssignments(c *gin.Context) {
	if h.Cfg.SyncSecret != "" && c.GetHeader("X-Sync-Secret") != h.Cfg.SyncSecret {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid sync secret"})
		return
	}
	n, err := assign.Backfill(h.DB)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error(), "assignments_written": n})
		return
	}
	c.JSON(http.StatusOK, gin.H{"assignments_written": n})
}
