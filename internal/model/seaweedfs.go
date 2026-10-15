package model

import "time"

// SeaweedObject is a local receipt. Only ready receipts are readable; an
// interrupted upload can leave an unreferenced remote attempt, never a partial
// committed object. The primary key is the SHA-256 of the immutable payload.
type SeaweedObject struct {
	Space     string    `gorm:"primaryKey;size:128" json:"space"`
	Key       string    `gorm:"primaryKey;size:64" json:"key"`
	MountPath string    `json:"mount_path"`
	ParentID  string    `json:"parent_id"`
	StorageID uint      `json:"storage_id"`
	Size      int64     `json:"size"`
	State     string    `gorm:"size:32;index" json:"state"`
	Attempt   string    `json:"attempt"`
	Locator   string    `gorm:"type:text" json:"locator"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SeaweedReadGate survives a restart, so restart cannot clear a provider ban.
type SeaweedReadGate struct {
	Account      string `gorm:"primaryKey;size:128"`
	NextReadAt   time.Time
	BlockedUntil time.Time
}
