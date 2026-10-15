package driver

import (
	"context"
	"encoding/json"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

// TierProvider accesses objects by durable provider identifiers. Implementations
// must not resolve paths by listing directories. Locators remain server-side.
type TierProvider interface {
	TierAccountKey() string
	TierPut(context.Context, string, model.FileStreamer) (json.RawMessage, error)
	TierLink(context.Context, json.RawMessage) (*model.Link, error)
	TierRemove(context.Context, json.RawMessage) error
}

type tierUploadKey struct{}

func WithTierUpload(ctx context.Context) context.Context {
	return context.WithValue(ctx, tierUploadKey{}, true)
}

func IsTierUpload(ctx context.Context) bool {
	v, _ := ctx.Value(tierUploadKey{}).(bool)
	return v
}
