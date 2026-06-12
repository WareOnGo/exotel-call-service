package handlers

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/wareongo/exotel-call-service/internal/models"
	"github.com/wareongo/exotel-call-service/internal/util"
)

// Route is the hot path: Exotel's Connect applet (dynamic "Application URL"
// mode) calls this, and we return the number(s) to dial.
//
// Decision:
//  1. Sticky: if the caller already has an assignment -> dial that contact.
//  2. Fresh:  pick a first-touch POC (lowest fallback_priority), else any active.
//  3. If the DB is slow/unavailable -> dial the static fallback (no dead air).
//
// The assignment write happens AFTER we respond, off the caller's latency path.
func (h *Handler) Route(c *gin.Context) {
	// Exotel POSTs the call as JSON: {"Call": {"From": "...", "Sid": "..."}}.
	customerRaw, callSid := extractCall(c)
	customer := util.NormalizePhone(customerRaw)

	if customer == "" {
		log.Printf("route: missing caller number (sid=%s) -> fallback", callSid)
		h.respondDial(c, h.fallbackNumbers())
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.Cfg.RouterDBTimeout)
	defer cancel()

	contact, fresh, err := h.resolveContact(ctx, customer)
	if err != nil || contact == nil {
		if err != nil {
			log.Printf("route: resolve failed (caller=%s sid=%s): %v -> fallback", customer, callSid, err)
		} else {
			log.Printf("route: no contact configured (caller=%s) -> fallback", customer)
		}
		h.respondDial(c, h.fallbackNumbers())
		return
	}

	log.Printf("route: caller=%s -> contact=%d (%s) fresh=%v sid=%s",
		customer, contact.ID, contact.Phone, fresh, callSid)

	// Persist the (sticky) assignment without blocking the response.
	go h.persistAssignment(customer, contact.ID)

	// Primary contact first, then the configured fallback as a backup leg —
	// Exotel tries the comma-separated numbers in order.
	dial := append([]string{contact.Phone}, h.fallbackNumbers()...)
	h.respondDial(c, dial)
}

// extractCall pulls the caller and call SID from Exotel's request. The
// confirmed Connect-applet format is a JSON body {"Call": {...}}; we fall back
// to query/form params for other setups.
func extractCall(c *gin.Context) (from, sid string) {
	var body struct {
		Call struct {
			Sid  string `json:"Sid"`
			From string `json:"From"`
		} `json:"Call"`
	}
	if err := c.ShouldBindJSON(&body); err == nil && body.Call.From != "" {
		return body.Call.From, body.Call.Sid
	}
	return param(c, "CallFrom", "From", "from"), param(c, "CallSid", "Sid")
}

// resolveContact returns the contact to dial and whether this was a fresh caller.
func (h *Handler) resolveContact(ctx context.Context, customer string) (*models.Contact, bool, error) {
	var asg models.Assignment
	err := h.DB.WithContext(ctx).
		Preload("Contact").
		Where("customer_phone = ?", customer).
		Limit(1).Find(&asg).Error
	if err != nil {
		return nil, false, err
	}
	if asg.ID != 0 && asg.Contact != nil && asg.Contact.Active {
		return asg.Contact, false, nil
	}

	// Fresh caller (or assigned contact is inactive): pick a first-touch POC.
	var contact models.Contact
	err = h.DB.WithContext(ctx).
		Where("active = ? AND is_first_touch = ?", true, true).
		Order("fallback_priority asc").
		Limit(1).Find(&contact).Error
	if err != nil {
		return nil, true, err
	}
	if contact.ID == 0 {
		// no dedicated first-touch contact -> any active contact by priority
		err = h.DB.WithContext(ctx).
			Where("active = ?", true).
			Order("fallback_priority asc").
			Limit(1).Find(&contact).Error
		if err != nil {
			return nil, true, err
		}
	}
	if contact.ID == 0 {
		return nil, true, nil
	}
	return &contact, true, nil
}

func (h *Handler) persistAssignment(customer string, contactID uint) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	asg := models.Assignment{
		CustomerPhone:   customer,
		ContactID:       contactID,
		LastContactedAt: time.Now(),
	}
	// Upsert on customer_phone.
	err := h.DB.WithContext(ctx).
		Where("customer_phone = ?", customer).
		Assign(map[string]any{"contact_id": contactID, "last_contacted_at": asg.LastContactedAt}).
		FirstOrCreate(&asg).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		log.Printf("route: persist assignment failed (caller=%s contact=%d): %v", customer, contactID, err)
	}
}

func (h *Handler) fallbackNumbers() []string {
	if h.Cfg.DefaultFallbackNumber == "" {
		return nil
	}
	return []string{h.Cfg.DefaultFallbackNumber}
}

// respondDial writes the dial target(s) in the format Exotel's Connect applet
// expects: numbers in +91 E.164, comma-separated (Exotel dials them in order).
// Confirmed with Exotel support (2026-06).
func (h *Handler) respondDial(c *gin.Context, numbers []string) {
	seen := make(map[string]bool, len(numbers))
	out := make([]string, 0, len(numbers))
	for _, n := range numbers {
		e := util.ToE164India(n)
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	if len(out) == 0 {
		// Nothing dialable. 404 lets the flow branch to a dashboard fallback.
		c.String(http.StatusNotFound, "")
		return
	}
	c.String(http.StatusOK, strings.Join(out, ","))
}
