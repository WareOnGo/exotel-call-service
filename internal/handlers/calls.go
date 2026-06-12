package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/wareongo/exotel-call-service/internal/models"
)

// ListCalls returns recent synced calls (most recent first), for a dashboard.
// Query params: limit (default 50, max 200), offset, status, from (number).
func (h *Handler) ListCalls(c *gin.Context) {
	limit := 50
	if v, err := strconv.Atoi(c.Query("limit")); err == nil && v > 0 {
		limit = v
	}
	if limit > 200 {
		limit = 200
	}
	offset, _ := strconv.Atoi(c.Query("offset"))

	q := h.DB.Model(&models.Call{})
	if s := c.Query("status"); s != "" {
		q = q.Where("status = ?", s)
	}
	if f := c.Query("from"); f != "" {
		q = q.Where("from_number = ?", f)
	}

	var calls []models.Call
	if err := q.Order("start_time desc nulls last").
		Limit(limit).Offset(offset).Find(&calls).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"calls": calls, "count": len(calls)})
}
