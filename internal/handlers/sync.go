package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/wareongo/exotel-call-service/internal/reconcile"
)

// Sync manually triggers a reconcile run. Guarded by SYNC_SECRET (sent as the
// X-Sync-Secret header) when configured. Useful for cron platforms that hit an
// HTTP endpoint, or for manual gap-filling.
func (h *Handler) Sync(c *gin.Context) {
	if h.Cfg.SyncSecret != "" && c.GetHeader("X-Sync-Secret") != h.Cfg.SyncSecret {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid sync secret"})
		return
	}
	sum, err := reconcile.Sync(c.Request.Context(), h.DB, h.Exotel, h.Cfg.SyncLookback)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error(), "summary": sum})
		return
	}
	c.JSON(http.StatusOK, sum)
}
