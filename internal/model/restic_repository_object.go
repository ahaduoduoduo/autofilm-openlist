package model

import "time"

// ResticRepositoryObject records the size of an object committed through the
// Restic REST gateway. The inventory is local metadata; repository contents
// remain stored only on the configured provider.
type ResticRepositoryObject struct {
	Repository string    `json:"repository" gorm:"primaryKey;size:128"`
	ObjectType string    `json:"object_type" gorm:"primaryKey;size:16"`
	Name       string    `json:"name" gorm:"primaryKey;size:128"`
	Size       int64     `json:"size"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ResticRepositoryInventory marks whether the local object inventory was
// initialized when the repository was created or seeded from a trusted index.
type ResticRepositoryInventory struct {
	Repository  string    `json:"repository" gorm:"primaryKey;size:128"`
	Initialized bool      `json:"initialized"`
	ObjectCount int64     `json:"object_count"`
	RefreshedAt time.Time `json:"refreshed_at"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
