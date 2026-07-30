package handles

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	stdpath "path"
	"strings"
	"time"

	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	"github.com/OpenListTeam/OpenList/v4/server/common"
	"github.com/gin-gonic/gin"
)

const (
	autoFilmJellyfinURLEnvironment    = "AUTOFILM_JELLYFIN_URL"
	autoFilmJellyfinAPIKeyEnvironment = "AUTOFILM_JELLYFIN_API_KEY"
	autoFilmJellyfinResponseLimit     = 1024 * 1024
)

var autoFilmJellyfinHTTPClient = &http.Client{Timeout: 90 * time.Second}

type AutoFilmJellyfinScanReq struct {
	Path       string `json:"path" binding:"required"`
	Refresh    bool   `json:"refresh"`
	Recursive  bool   `json:"recursive"`
	ForceProbe bool   `json:"force_probe"`
}

type autoFilmJellyfinRefreshReq struct {
	Path       string `json:"path"`
	Refresh    bool   `json:"refresh"`
	Recursive  bool   `json:"recursive"`
	ForceProbe bool   `json:"force_probe"`
}

// AutoFilmScanJellyfin explicitly asks Jellyfin to import or refresh one
// OpenList path. Normal OpenList filesystem mutations never call this handler.
func AutoFilmScanJellyfin(c *gin.Context) {
	var req AutoFilmJellyfinScanReq
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ErrorResp(c, err, http.StatusBadRequest)
		return
	}

	cleanPath, err := normalizeAutoFilmJellyfinPath(req.Path)
	if err != nil {
		common.ErrorResp(c, err, http.StatusBadRequest)
		return
	}

	result, err := requestAutoFilmJellyfinRefresh(
		c,
		cleanPath,
		req,
	)
	if err != nil {
		common.ErrorResp(c, err, http.StatusBadGateway)
		return
	}
	common.SuccessResp(c, result)
}

func normalizeAutoFilmJellyfinPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || !strings.HasPrefix(value, "/") {
		return "", fmt.Errorf("path must be an absolute OpenList path")
	}
	cleanPath := utils.FixAndCleanPath(value)
	if cleanPath == "/" || cleanPath == "." ||
		strings.Contains(value, "/../") ||
		strings.HasSuffix(value, "/..") {
		return "", fmt.Errorf("path must identify a media file or directory")
	}
	return stdpath.Clean(cleanPath), nil
}

func requestAutoFilmJellyfinRefresh(
	c *gin.Context,
	cleanPath string,
	req AutoFilmJellyfinScanReq,
) (any, error) {
	baseURL := strings.TrimRight(
		strings.TrimSpace(os.Getenv(autoFilmJellyfinURLEnvironment)),
		"/",
	)
	apiKey := strings.TrimSpace(os.Getenv(
		autoFilmJellyfinAPIKeyEnvironment,
	))
	if baseURL == "" || apiKey == "" {
		return nil, fmt.Errorf(
			"%s and %s must be configured",
			autoFilmJellyfinURLEnvironment,
			autoFilmJellyfinAPIKeyEnvironment,
		)
	}

	body, err := json.Marshal(autoFilmJellyfinRefreshReq{
		Path:       cleanPath,
		Refresh:    req.Refresh,
		Recursive:  req.Recursive,
		ForceProbe: req.ForceProbe,
	})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(
		c.Request.Context(),
		http.MethodPost,
		baseURL+"/AutoFilm/RemoteRefresh",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(
		"Authorization",
		`MediaBrowser Client="OpenList", Device="Server", `+
			`DeviceId="autofilm-openlist", Version="1.0", Token="`+
			strings.ReplaceAll(apiKey, `"`, "")+`"`,
	)

	response, err := autoFilmJellyfinHTTPClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request Jellyfin refresh: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(
		response.Body,
		autoFilmJellyfinResponseLimit,
	))
	if err != nil {
		return nil, fmt.Errorf("read Jellyfin response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf(
			"Jellyfin returned HTTP %d: %s",
			response.StatusCode,
			strings.TrimSpace(string(responseBody)),
		)
	}

	var result any
	if len(responseBody) == 0 {
		return map[string]any{"path": cleanPath}, nil
	}
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return nil, fmt.Errorf("decode Jellyfin response: %w", err)
	}
	return result, nil
}
