package seaweedfs

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Called with h.mu held. Persisting URLs prevents restarts or an in-memory
// eviction from spending the provider's link-generation budget again.
func (h *Handler) cachedOrResolve(c *gin.Context, o *model.SeaweedObject, p driver.TierProvider) (model.SeaweedLink, bool) {
	var cached model.SeaweedLink
	account := p.TierAccountKey()
	err := h.db.First(&cached, "space = ? AND key = ?", o.Space, o.Key).Error
	if err == nil && cached.Account == account && !cached.Invalid && (cached.ExpiresAt.IsZero() || time.Now().Before(cached.ExpiresAt)) {
		return cached, true
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		c.Status(500)
		return cached, false
	}
	if !h.reserveLink(c, account) {
		return cached, false
	}
	resolved, err := p.TierLink(c.Request.Context(), []byte(o.Locator))
	if err != nil {
		if blockedError(err) {
			h.block(c, account)
		} else {
			c.Header("Retry-After", "5")
			c.Status(http.StatusBadGateway)
		}
		return cached, false
	}
	if resolved == nil {
		c.Status(http.StatusBadGateway)
		return cached, false
	}
	defer resolved.Close()
	u, err := url.Parse(resolved.URL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		c.Status(http.StatusBadGateway)
		return cached, false
	}
	// Unknown expiry is not a guessed TTL. Reuse until a client reports an
	// actual rejection. Explicit provider validity can still be honored.
	var expires time.Time
	if resolved.Expiration != nil {
		if *resolved.Expiration <= 0 {
			c.Status(http.StatusBadGateway)
			return cached, false
		}
		expires = time.Now().Add(*resolved.Expiration)
	}
	failures := cached.Failures
	if cached.Account != account {
		failures = 0
	}
	cached = model.SeaweedLink{Space: o.Space, Key: o.Key, Account: account, URL: resolved.URL, ExpiresAt: expires, Failures: failures}
	if err = h.db.Save(&cached).Error; err != nil {
		c.Status(500)
		return cached, false
	}
	return cached, true
}
