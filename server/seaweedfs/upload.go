package seaweedfs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/stream"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func (h *Handler) acquireUpload(c *gin.Context) bool {
	select {
	case h.uploads <- struct{}{}:
		return true
	case <-c.Request.Context().Done():
		c.Status(http.StatusRequestTimeout)
		return false
	}
}

func (h *Handler) put(c *gin.Context) {
	size := c.Request.ContentLength
	if size < 0 {
		c.Status(http.StatusLengthRequired)
		return
	}
	if size > h.config.MaxObjectMiB<<20 {
		c.Status(http.StatusRequestEntityTooLarge)
		return
	}
	if !h.acquireUpload(c) {
		return
	}
	defer func() { <-h.uploads }()
	s := h.spaces[c.Param("space")]
	key := c.Param("key")
	p, storageID, ok := h.provider(c)
	if !ok {
		return
	}
	var object model.SeaweedObject
	err := h.db.First(&object, "space = ? AND key = ?", s.Name, key).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		c.Status(500)
		return
	}
	if err == nil {
		if object.Size != size || object.StorageID != storageID || object.MountPath != s.MountPath || object.ParentID != s.ParentID || object.State == "deleted" {
			c.Status(http.StatusConflict)
			return
		}
		if object.State == "ready" {
			// Drain and verify retries before replying. Early success on a large
			// HTTP/1 request body can close the connection while it is still sent.
			if err := verifyBody(c.Request.Body, io.Discard, size, key); err != nil {
				c.Status(http.StatusBadRequest)
				return
			}
			c.JSON(http.StatusOK, gin.H{"key": key, "size": size, "state": "ready"})
			return
		}
	}
	if !h.allowUpload(c, p.TierAccountKey()) {
		return
	}
	// Hash before any provider request. Disk use is bounded by one active body.
	f, err := os.CreateTemp("", "openlist-seaweedfs-*")
	if err != nil {
		c.Status(500)
		return
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := verifyBody(c.Request.Body, f, size, key); err != nil {
		c.JSON(400, gin.H{"error": "payload size or SHA-256 mismatch"})
		return
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		c.Status(500)
		return
	}
	object = model.SeaweedObject{Space: s.Name, Key: key, MountPath: s.MountPath, ParentID: s.ParentID, StorageID: storageID, Size: size, State: "uploading", Attempt: uuid.NewString()}
	if err := h.db.Save(&object).Error; err != nil {
		c.Status(500)
		return
	}
	// Every ambiguous retry uses a new remote name. A lost provider response may
	// leave an orphan, but cannot overwrite the sole committed copy of a pack.
	file := &stream.FileStream{Ctx: c.Request.Context(), Obj: &model.Object{Name: key + "." + object.Attempt + ".pack", Size: size, Modified: time.Now()}, Reader: f, Mimetype: "application/octet-stream"}
	defer file.Close()
	locator, err := p.TierPut(c.Request.Context(), s.ParentID, file)
	if err != nil || !json.Valid(locator) {
		if updateErr := h.db.Model(&object).Update("state", "pending").Error; updateErr != nil {
			c.Status(500)
			return
		}
		if blockedError(err) {
			h.mu.Lock()
			h.block(c, p.TierAccountKey())
			h.mu.Unlock()
			return
		}
		c.Header("Retry-After", "30")
		c.JSON(http.StatusBadGateway, gin.H{"error": "upload not committed; retry the same key"})
		return
	}
	// A failed receipt commit is an error. Never acknowledge a cloud object that
	// would become unreachable after an OpenList restart.
	if err := h.db.Model(&object).Updates(map[string]any{"state": "ready", "locator": string(locator)}).Error; err != nil {
		c.Status(500)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"key": key, "size": size, "state": "ready"})
}

func verifyBody(body io.Reader, target io.Writer, size int64, key string) error {
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(target, hash), io.LimitReader(body, size+1))
	if err != nil {
		return err
	}
	if n != size || hex.EncodeToString(hash.Sum(nil)) != key {
		return fmt.Errorf("payload size or SHA-256 mismatch")
	}
	return nil
}

func (h *Handler) remove(c *gin.Context) {
	if !h.acquireUpload(c) {
		return
	}
	defer func() { <-h.uploads }()
	o, ok := h.object(c)
	if !ok {
		return
	}
	// A logical deletion intentionally retains remote bytes. Physical reclaim
	// requires a retention-aware maintenance pass, not an ordinary API request.
	if err := h.db.Model(o).Update("state", "deleted").Error; err != nil {
		c.Status(500)
		return
	}
	h.mu.Lock()
	delete(h.links, o.Space+"/"+o.Key)
	h.mu.Unlock()
	c.Status(http.StatusNoContent)
}
