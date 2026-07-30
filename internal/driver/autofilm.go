package driver

import (
	"context"
	"time"
)

// AutoFilmAuthSession is a channel-neutral view of an interactive storage
// authentication session. Credentials never leave the storage driver.
type AutoFilmAuthSession struct {
	ID        string    `json:"session_id"`
	State     string    `json:"state"`
	ExpiresAt time.Time `json:"expires_at"`
	Message   string    `json:"message,omitempty"`
}

// AutoFilmAuthProvider exposes interactive authentication without coupling
// the API layer to a concrete storage driver.
type AutoFilmAuthProvider interface {
	StartAutoFilmAuth(ctx context.Context) (*AutoFilmAuthSession, error)
	GetAutoFilmAuth(ctx context.Context, sessionID string) (*AutoFilmAuthSession, error)
	GetAutoFilmAuthQRCode(sessionID string) ([]byte, error)
}

// AutoFilmAuthHealth is an explicit provider credential check. The API layer
// returns this machine-readable result instead of asking callers to interpret
// provider error text.
type AutoFilmAuthHealth struct {
	Authenticated bool      `json:"authenticated"`
	CheckedAt     time.Time `json:"checked_at"`
	Message       string    `json:"message,omitempty"`
}

// AutoFilmAuthHealthProvider verifies the current storage credential.
type AutoFilmAuthHealthProvider interface {
	CheckAutoFilmAuth(ctx context.Context) AutoFilmAuthHealth
}

// AutoFilmSchedulerSnapshot contains non-sensitive scheduler diagnostics.
type AutoFilmSchedulerSnapshot struct {
	AccountKey          string  `json:"account_key"`
	RequestRate         float64 `json:"request_rate"`
	ListConcurrency     int     `json:"list_concurrency"`
	MutationConcurrency int     `json:"mutation_concurrency"`
	UploadConcurrency   int     `json:"upload_concurrency"`
	ActiveLists         int     `json:"active_lists"`
	ActiveMutations     int     `json:"active_mutations"`
	ActiveUploads       int     `json:"active_uploads"`
}

// AutoFilmSchedulerProvider exposes scheduler state for diagnostics.
type AutoFilmSchedulerProvider interface {
	AutoFilmSchedulerSnapshot() AutoFilmSchedulerSnapshot
}
