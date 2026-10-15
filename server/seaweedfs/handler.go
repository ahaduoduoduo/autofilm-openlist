// Package seaweedfs exposes an exact-object gateway for immutable cloud-tier
// packs. It deliberately never calls fs.Get, fs.List, or fs.Link.
package seaweedfs

import (
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Resolver func(string) (driver.Driver, error)

type cachedLink struct {
	url     string
	expires time.Time
}

type Handler struct {
	config  conf.SeaweedFS
	db      *gorm.DB
	resolve Resolver
	spaces  map[string]conf.SeaweedFSSpace
	uploads chan struct{}
	mu      sync.Mutex
	links   map[string]cachedLink
}

var spaceName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

func NewHandler(config conf.SeaweedFS, database *gorm.DB, resolve Resolver) (*Handler, error) {
	if config.MaxObjectMiB == 0 {
		config.MaxObjectMiB = 64
	}
	if config.ReadIntervalMilliseconds == 0 {
		config.ReadIntervalMilliseconds = 5000
	}
	if config.CooldownSeconds == 0 {
		config.CooldownSeconds = 1800
	}
	if config.LinkCacheSeconds == 0 {
		config.LinkCacheSeconds = 300
	}
	if config.MaxObjectMiB < 1 || config.MaxObjectMiB > 4096 || config.ReadIntervalMilliseconds < 1 || config.CooldownSeconds < 1 || config.LinkCacheSeconds < 1 {
		return nil, fmt.Errorf("invalid SeaweedFS size or timing limits")
	}
	h := &Handler{config: config, db: database, resolve: resolve, spaces: map[string]conf.SeaweedFSSpace{}, uploads: make(chan struct{}, 1), links: map[string]cachedLink{}}
	parents := map[string]bool{}
	for _, s := range config.Spaces {
		parent := s.MountPath + "\x00" + s.ParentID
		if !spaceName.MatchString(s.Name) || len(s.Token) < 16 || !strings.HasPrefix(s.MountPath, "/") || s.ParentID == "" || parents[parent] {
			return nil, fmt.Errorf("invalid or shared SeaweedFS space configuration: %q", s.Name)
		}
		if _, exists := h.spaces[s.Name]; exists {
			return nil, fmt.Errorf("duplicate space: %q", s.Name)
		}
		h.spaces[s.Name] = s
		parents[parent] = true
	}
	return h, nil
}

func (h *Handler) Register(g *gin.RouterGroup) {
	g.Use(h.authenticate)
	g.GET("/:space/objects", h.list)
	g.GET("/:space/objects/:key", h.read)
	g.HEAD("/:space/objects/:key", h.head)
	g.PUT("/:space/objects/:key", h.put)
	g.DELETE("/:space/objects/:key", h.remove)
	g.POST("/:space/objects/:key/failure", h.failure)
	g.GET("/:space/inventory", h.exportInventory)
	g.POST("/:space/inventory", h.importInventory)
}

func (h *Handler) authenticate(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	s, ok := h.spaces[c.Param("space")]
	token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	if !ok || !strings.HasPrefix(c.GetHeader("Authorization"), "Bearer ") || subtle.ConstantTimeCompare([]byte(token), []byte(s.Token)) != 1 {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	if key := c.Param("key"); key != "" && !validKey(key) {
		c.AbortWithStatus(http.StatusBadRequest)
	}
}

func validKey(key string) bool {
	if len(key) != 64 || strings.ToLower(key) != key {
		return false
	}
	_, err := hex.DecodeString(key)
	return err == nil
}

func (h *Handler) provider(c *gin.Context) (driver.TierProvider, uint, bool) {
	s := h.spaces[c.Param("space")]
	d, err := h.resolve(s.MountPath)
	if err != nil {
		c.Status(http.StatusServiceUnavailable)
		return nil, 0, false
	}
	p, ok := d.(driver.TierProvider)
	if !ok {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "storage does not support exact-object tier access"})
		return nil, 0, false
	}
	return p, d.GetStorage().ID, true
}

func (h *Handler) object(c *gin.Context) (*model.SeaweedObject, bool) {
	var o model.SeaweedObject
	err := h.db.First(&o, "space = ? AND key = ?", c.Param("space"), c.Param("key")).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.Status(http.StatusNotFound)
		return nil, false
	}
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return nil, false
	}
	if o.State == "deleted" {
		c.Status(http.StatusNotFound)
		return nil, false
	}
	if o.State != "ready" {
		c.Header("Retry-After", "5")
		c.JSON(http.StatusConflict, gin.H{"error": "upload is not committed", "state": o.State})
		return nil, false
	}
	s := h.spaces[o.Space]
	if o.MountPath != s.MountPath || o.ParentID != s.ParentID {
		c.Status(http.StatusConflict)
		return nil, false
	}
	return &o, true
}

func (h *Handler) head(c *gin.Context) {
	o, ok := h.object(c)
	if !ok {
		return
	}
	c.Header("Content-Length", strconv.FormatInt(o.Size, 10))
	c.Header("ETag", `"`+o.Key+`"`)
	c.Header("Accept-Ranges", "bytes")
	c.Status(http.StatusOK)
}

func (h *Handler) read(c *gin.Context) {
	o, ok := h.object(c)
	if !ok {
		return
	}
	p, storageID, ok := h.provider(c)
	if !ok {
		return
	}
	if storageID != o.StorageID {
		c.Status(http.StatusConflict)
		return
	}
	// Serializes link resolution and persistent read reservations across spaces.
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.reserveRead(c, p.TierAccountKey()) {
		return
	}
	cacheKey := o.Space + "/" + o.Key
	link := h.links[cacheKey]
	if time.Now().After(link.expires) {
		resolved, err := p.TierLink(c.Request.Context(), []byte(o.Locator))
		if err != nil {
			if blockedError(err) {
				h.block(c, p.TierAccountKey())
				return
			}
			c.Header("Retry-After", "5")
			c.Status(http.StatusBadGateway)
			return
		}
		if resolved == nil {
			c.Status(http.StatusBadGateway)
			return
		}
		defer resolved.Close()
		u, err := url.Parse(resolved.URL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
			c.Status(http.StatusBadGateway)
			return
		}
		ttl := time.Duration(h.config.LinkCacheSeconds) * time.Second
		if resolved.Expiration != nil && *resolved.Expiration > 0 && *resolved.Expiration < ttl {
			ttl = *resolved.Expiration
		}
		link = cachedLink{url: resolved.URL, expires: time.Now().Add(ttl)}
		// Bound memory independently of repository size.
		if len(h.links) >= 1024 {
			clear(h.links)
		}
		h.links[cacheKey] = link
	}
	c.Redirect(http.StatusFound, link.url)
}

func (h *Handler) list(c *gin.Context) {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if err != nil || limit < 1 || limit > 1000 {
		c.Status(http.StatusBadRequest)
		return
	}
	var objects []model.SeaweedObject
	err = h.db.Select("space,key,size,state,created_at,updated_at").Where("space = ? AND state = ? AND key > ?", c.Param("space"), "ready", c.Query("after")).Order("key").Limit(limit).Find(&objects).Error
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.JSON(http.StatusOK, objects)
}
