package handlers

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/wareongo/exotel-call-service/internal/archive"
	"github.com/wareongo/exotel-call-service/internal/config"
	"github.com/wareongo/exotel-call-service/internal/exotel"
)

// Handler bundles dependencies shared across HTTP handlers.
type Handler struct {
	DB     *gorm.DB
	Exotel *exotel.Client
	Cfg    *config.Config

	// Archiver is nil when R2 is not configured; the archive endpoint then 503s.
	Archiver *archive.Archiver
}

func New(db *gorm.DB, ex *exotel.Client, cfg *config.Config) *Handler {
	return &Handler{DB: db, Exotel: ex, Cfg: cfg}
}

// param reads a value from query string first, then form body — Exotel uses
// either depending on the applet/version.
func param(c *gin.Context, keys ...string) string {
	for _, k := range keys {
		if v := c.Query(k); v != "" {
			return v
		}
		if v := c.PostForm(k); v != "" {
			return v
		}
	}
	return ""
}
