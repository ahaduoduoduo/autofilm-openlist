package model

import "time"

// ResticTrafficUsage stores the actual bytes read by a remote provider upload.
// One row is maintained per repository and local calendar day.
type ResticTrafficUsage struct {
	Repository string    `json:"repository" gorm:"primaryKey;size:128"`
	Day        string    `json:"day" gorm:"primaryKey;size:10"`
	Bytes      int64     `json:"bytes"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}
