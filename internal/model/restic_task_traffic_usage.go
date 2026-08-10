package model

import "time"

// ResticTaskTrafficUsage stores provider upload bytes and allocation state for
// one Backrest plan on one local calendar day.
type ResticTaskTrafficUsage struct {
	Repository      string    `json:"repository" gorm:"primaryKey;size:128"`
	Task            string    `json:"task" gorm:"primaryKey;size:192"`
	Day             string    `json:"day" gorm:"primaryKey;size:10"`
	Bytes           int64     `json:"bytes"`
	DailyLimitBytes int64     `json:"daily_limit_bytes"`
	Weight          int       `json:"weight"`
	Released        bool      `json:"released"`
	ReleasedAtBytes int64     `json:"released_at_bytes"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}
