package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type connectRequest struct {
	From     string `json:"from" binding:"required"`
	To       string `json:"to" binding:"required"`
	CallerID string `json:"caller_id"`
}

// ConnectTwoNumbers exposes Exotel's "connect two numbers" (click-to-call):
// bridges From and To, presenting CallerID (an ExoPhone) to both legs.
func (h *Handler) ConnectTwoNumbers(c *gin.Context) {
	var req connectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	callerID := req.CallerID
	if callerID == "" {
		callerID = h.Cfg.DefaultCallerID
	}
	if callerID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "caller_id required (set DEFAULT_CALLER_ID or pass caller_id)"})
		return
	}

	res, err := h.Exotel.ConnectTwoNumbers(c.Request.Context(), req.From, req.To, callerID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}
