package quark_open

import (
	"encoding/hex"
	"strings"

	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
)

func contentHashInfo(name, value string) utils.HashInfo {
	normalizedName := strings.ToLower(strings.TrimSpace(name))
	if normalizedName != "" && normalizedName != "sha1" && normalizedName != "sha-1" {
		return utils.HashInfo{}
	}

	normalizedValue := strings.ToLower(strings.TrimSpace(value))
	if len(normalizedValue) != utils.SHA1.Width {
		return utils.HashInfo{}
	}
	if _, err := hex.DecodeString(normalizedValue); err != nil {
		return utils.HashInfo{}
	}

	return utils.NewHashInfo(utils.SHA1, normalizedValue)
}
