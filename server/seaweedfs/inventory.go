package seaweedfs

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type inventory struct {
	Version int                   `json:"version"`
	Space   string                `json:"space"`
	Objects []model.SeaweedObject `json:"objects"`
	Next    string                `json:"next,omitempty"`
}

// Export is paginated and entirely local. A full recovery point must also
// capture SeaweedFS's filer metadata, .idx/.vif files, and encryption keys.
func (h *Handler) exportInventory(c *gin.Context) {
	result := inventory{Version: 1, Space: c.Param("space")}
	err := h.db.Where("space = ? AND state IN ? AND key > ?", result.Space, []string{"ready", "deleted"}, c.Query("after")).Order("key").Limit(1000).Find(&result.Objects).Error
	if err != nil {
		c.Status(500)
		return
	}
	if len(result.Objects) == 1000 {
		result.Next = result.Objects[len(result.Objects)-1].Key
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) importInventory(c *gin.Context) {
	// Restore privileges are separate from a business space's storage token.
	if len(h.config.RecoveryToken) < 32 || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-SeaweedFS-Recovery-Token")), []byte(h.config.RecoveryToken)) != 1 {
		c.Status(403)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<20)
	var input inventory
	if c.ShouldBindJSON(&input) != nil || input.Version != 1 || input.Space != c.Param("space") || len(input.Objects) > 1000 {
		c.Status(400)
		return
	}
	_, storageID, ok := h.provider(c)
	if !ok {
		return
	}
	s := h.spaces[input.Space]
	seen := map[string]bool{}
	for i := range input.Objects {
		o := &input.Objects[i]
		if o.Space != s.Name || !validKey(o.Key) || seen[o.Key] || o.Size < 0 || o.MountPath != s.MountPath || o.ParentID != s.ParentID || (o.State != "ready" && o.State != "deleted") || !json.Valid([]byte(o.Locator)) {
			c.Status(400)
			return
		}
		seen[o.Key] = true
		o.StorageID = storageID
	}
	if !h.acquireUpload(c) {
		return
	}
	defer func() { <-h.uploads }()
	err := h.db.Transaction(func(tx *gorm.DB) error {
		for _, o := range input.Objects {
			var count int64
			if err := tx.Model(&model.SeaweedObject{}).Where("space = ? AND key = ?", o.Space, o.Key).Count(&count).Error; err != nil {
				return err
			}
			if count != 0 {
				return fmt.Errorf("restore would overwrite existing receipt")
			}
			if err := tx.Create(&o).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "inventory restore failed; existing receipts are never replaced"})
		return
	}
	c.Status(http.StatusCreated)
}
