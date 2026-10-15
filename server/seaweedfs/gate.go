package seaweedfs

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func retry(c *gin.Context, status int, duration time.Duration) {
	seconds := max(1, int64((duration+time.Second-1)/time.Second))
	c.Header("Retry-After", strconv.FormatInt(seconds, 10))
	c.JSON(status, gin.H{"error": "remote request budget unavailable"})
}

// Called with h.mu held. A reservation is persisted before provider access.
func (h *Handler) reserveLink(c *gin.Context, account string) bool {
	gate := model.SeaweedReadGate{Account: account}
	err := h.db.First(&gate, "account = ?", account).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		c.Status(500)
		return false
	}
	now := time.Now()
	if now.Before(gate.BlockedUntil) {
		retry(c, http.StatusServiceUnavailable, gate.BlockedUntil.Sub(now))
		return false
	}
	if now.Before(gate.NextReadAt) {
		retry(c, http.StatusTooManyRequests, gate.NextReadAt.Sub(now))
		return false
	}
	gate.NextReadAt = now.Add(time.Duration(h.config.ReadIntervalMilliseconds) * time.Millisecond)
	if err := h.db.Save(&gate).Error; err != nil {
		c.Status(500)
		return false
	}
	return true
}

func (h *Handler) block(c *gin.Context, account string) {
	duration := time.Duration(h.config.CooldownSeconds) * time.Second
	gate := model.SeaweedReadGate{Account: account}
	err := h.db.First(&gate, "account = ?", account).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		c.Status(500)
		return
	}
	gate.BlockedUntil = time.Now().Add(duration)
	if err := h.db.Save(&gate).Error; err != nil {
		c.Status(500)
		return
	}
	retry(c, http.StatusServiceUnavailable, duration)
}

func blockedError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "429") || strings.Contains(message, "request has been blocked") || strings.Contains(message, "访问被阻断") || strings.Contains(message, "<title>405</title>")
}

// Uploads share the persistent account cooldown, but use the driver's existing
// upload pacing rather than consuming a new-link reservation.
func (h *Handler) allowUpload(c *gin.Context, account string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	var gate model.SeaweedReadGate
	err := h.db.First(&gate, "account = ?", account).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		c.Status(500)
		return false
	}
	if remaining := time.Until(gate.BlockedUntil); remaining > 0 {
		retry(c, http.StatusServiceUnavailable, remaining)
		return false
	}
	return true
}

// The gateway cannot observe a CDN response after a 302. Trusted tier clients
// report it, allowing the account-wide cooldown to include direct downloads.
func (h *Handler) failure(c *gin.Context) {
	var report struct {
		Status int    `json:"status"`
		URL    string `json:"url,omitempty"`
	}
	if c.ShouldBindJSON(&report) != nil {
		c.Status(400)
		return
	}
	switch report.Status {
	case 200, 206, 401, 403, 404, 405, 410, 429:
	default:
		c.Status(http.StatusBadRequest)
		return
	}
	if _, ok := h.object(c); !ok {
		return
	}
	p, _, ok := h.provider(c)
	if !ok {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	var cached model.SeaweedLink
	err := h.db.First(&cached, "space = ? AND key = ?", c.Param("space"), c.Param("key")).Error
	// Ignore duplicate reports and reports for an old URL, including success
	// from a transfer that started before that URL was invalidated.
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && (cached.Invalid || (report.URL != "" && cached.URL != report.URL))) {
		c.Status(http.StatusNoContent)
		return
	}
	if err != nil {
		c.Status(500)
		return
	}
	if report.Status == 200 || report.Status == 206 {
		if cached.Failures > 0 {
			if err := h.db.Model(&cached).Update("failures", 0).Error; err != nil {
				c.Status(500)
				return
			}
		}
		c.Status(http.StatusNoContent)
		return
	}
	// Keep a tombstone so simultaneous failures of one URL neither extend
	// cooldown repeatedly nor invalidate a replacement URL.
	failures := cached.Failures + 1
	if err := h.db.Model(&cached).Updates(map[string]any{"invalid": true, "failures": failures}).Error; err != nil {
		c.Status(500)
		return
	}
	switch report.Status {
	case 405, 429:
		h.block(c, p.TierAccountKey())
	case 403:
		// An isolated 403 can simply mean URL expiry. A replacement URL that
		// also fails (without a successful transfer) requires account cooldown.
		if failures > 1 {
			h.block(c, p.TierAccountKey())
		} else {
			c.Status(http.StatusNoContent)
		}
	default:
		c.Status(http.StatusNoContent)
	}
}
