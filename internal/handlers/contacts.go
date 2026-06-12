package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/wareongo/exotel-call-service/internal/models"
	"github.com/wareongo/exotel-call-service/internal/util"
)

func (h *Handler) ListContacts(c *gin.Context) {
	var contacts []models.Contact
	if err := h.DB.Order("fallback_priority asc").Find(&contacts).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, contacts)
}

type createContactRequest struct {
	Name             string `json:"name"`
	Phone            string `json:"phone" binding:"required"`
	Role             string `json:"role"`
	IsFirstTouch     bool   `json:"is_first_touch"`
	FallbackPriority int    `json:"fallback_priority"`
	Active           *bool  `json:"active"` // pointer: omitted -> default true; false -> honored
}

func (h *Handler) CreateContact(c *gin.Context) {
	var req createContactRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	contact := models.Contact{
		Name:             req.Name,
		Phone:            req.Phone,
		Role:             req.Role,
		IsFirstTouch:     req.IsFirstTouch,
		FallbackPriority: req.FallbackPriority,
		Active:           active,
	}
	if err := h.DB.Create(&contact).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, contact)
}

func (h *Handler) UpdateContact(c *gin.Context) {
	id := c.Param("id")
	var contact models.Contact
	if err := h.DB.First(&contact, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "contact not found"})
		return
	}
	var patch map[string]any
	if err := c.ShouldBindJSON(&patch); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	delete(patch, "id")        // never let the PK be patched
	delete(patch, "phone_key") // derived column, not directly settable
	// Keep phone_key in sync when the phone is changed (the map-based update
	// path bypasses the BeforeSave hook).
	if p, ok := patch["phone"].(string); ok {
		patch["phone_key"] = util.NormalizePhone(p)
	}
	if err := h.DB.Model(&contact).Updates(patch).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, contact)
}

func (h *Handler) ListAssignments(c *gin.Context) {
	var asgs []models.Assignment
	if err := h.DB.Preload("Contact").Order("last_contacted_at desc").Limit(500).Find(&asgs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, asgs)
}

type upsertAssignmentRequest struct {
	CustomerPhone string `json:"customer_phone" binding:"required"`
	ContactID     uint   `json:"contact_id" binding:"required"`
}

// UpsertAssignment manually binds (or reassigns) a customer to a contact.
func (h *Handler) UpsertAssignment(c *gin.Context) {
	var req upsertAssignmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	key := util.NormalizePhone(req.CustomerPhone)
	// Pre-populate CustomerPhone so FirstOrCreate writes it on insert (a raw
	// string Where doesn't seed column values for the created row).
	asg := models.Assignment{CustomerPhone: key}
	err := h.DB.Where("customer_phone = ?", key).
		Assign(map[string]any{"contact_id": req.ContactID}).
		FirstOrCreate(&asg).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, asg)
}
