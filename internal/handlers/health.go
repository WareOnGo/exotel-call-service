package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (h *Handler) Health(c *gin.Context) {
	status := "ok"
	dbOK := true
	if sqlDB, err := h.DB.DB(); err != nil || sqlDB.Ping() != nil {
		dbOK = false
		status = "degraded"
	}
	code := http.StatusOK
	if !dbOK {
		code = http.StatusServiceUnavailable
	}
	c.JSON(code, gin.H{"status": status, "db": dbOK})
}
