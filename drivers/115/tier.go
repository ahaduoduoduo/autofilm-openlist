package _115

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

var _ driver.TierProvider = (*Pan115)(nil)

func (d *Pan115) TierAccountKey() string {
	return "115:" + schedulerDisplayKey(schedulerAccountKey(&d.Addition, d.Storage.ID))
}

func (d *Pan115) TierPut(ctx context.Context, parentID string, file model.FileStreamer) (json.RawMessage, error) {
	obj, err := d.Put(driver.WithTierUpload(ctx), &model.Object{ID: parentID, IsFolder: true}, file, func(float64) {})
	if err != nil {
		return nil, err
	}
	if obj == nil {
		return nil, fmt.Errorf("115 upload returned no durable locator")
	}
	locator, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	if _, err := decodeTierLocator(locator); err != nil {
		return nil, err
	}
	return locator, nil
}

func (d *Pan115) TierLink(ctx context.Context, locator json.RawMessage) (*model.Link, error) {
	obj, err := decodeTierLocator(locator)
	if err != nil {
		return nil, err
	}
	link, err := d.Link(ctx, obj, model.LinkArgs{})
	if err == nil && link != nil {
		if link.Header == nil {
			link.Header = make(map[string][]string)
		}
		// This exact-object endpoint requests one canonical empty-UA URL per pack.
		// Native clients must not replace it with their HTTP library's default UA.
		link.Header.Set("User-Agent", "")
	}
	return link, err
}

func (d *Pan115) TierRemove(ctx context.Context, locator json.RawMessage) error {
	obj, err := decodeTierLocator(locator)
	if err != nil {
		return err
	}
	return d.Remove(ctx, obj)
}

func decodeTierLocator(data json.RawMessage) (*FileObj, error) {
	var obj FileObj
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, err
	}
	if obj.GetID() == "" || obj.PickCode == "" || obj.IsDir() {
		return nil, fmt.Errorf("invalid 115 tier locator")
	}
	return &obj, nil
}
