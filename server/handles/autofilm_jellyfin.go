package handles

import (
	"bytes"
	"context"
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
	ScanMode   string `json:"scan_mode"`
}

type autoFilmJellyfinRefreshReq struct {
	Path       string `json:"path"`
	Refresh    bool   `json:"refresh"`
	Recursive  bool   `json:"recursive"`
	ForceProbe bool   `json:"force_probe"`
	ScanMode   string `json:"scan_mode"`
}

type autoFilmJellyfinVirtualFolder struct {
	Name      string   `json:"Name"`
	Locations []string `json:"Locations"`
}

type AutoFilmJellyfinPathStatusResp struct {
	Path         string `json:"path"`
	Configured   bool   `json:"configured"`
	LibraryName  string `json:"library_name,omitempty"`
	MatchingRoot string `json:"matching_root,omitempty"`
	Message      string `json:"message,omitempty"`
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
	req.ScanMode, err = normalizeAutoFilmJellyfinScanMode(req.ScanMode)
	if err != nil {
		common.ErrorResp(c, err, http.StatusBadRequest)
		return
	}

	status, err := requestAutoFilmJellyfinPathStatus(
		c.Request.Context(),
		cleanPath,
	)
	if err != nil {
		common.ErrorResp(c, err, http.StatusBadGateway)
		return
	}
	if !status.Configured {
		common.ErrorStrResp(c, status.Message, http.StatusConflict)
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

// AutoFilmGetJellyfinPathStatus checks only Jellyfin's configured library
// roots. It does not list the selected OpenList directory.
func AutoFilmGetJellyfinPathStatus(c *gin.Context) {
	cleanPath, err := normalizeAutoFilmJellyfinPath(c.Query("path"))
	if err != nil {
		common.ErrorResp(c, err, http.StatusBadRequest)
		return
	}
	status, err := requestAutoFilmJellyfinPathStatus(
		c.Request.Context(),
		cleanPath,
	)
	if err != nil {
		common.ErrorResp(c, err, http.StatusBadGateway)
		return
	}
	common.SuccessResp(c, status)
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

func normalizeAutoFilmJellyfinScanMode(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || value == "new" {
		return "new", nil
	}
	if value == "full" {
		return "full", nil
	}
	return "", fmt.Errorf("scan_mode must be new or full")
}

func requestAutoFilmJellyfinRefresh(
	c *gin.Context,
	cleanPath string,
	req AutoFilmJellyfinScanReq,
) (any, error) {
	baseURL, apiKey, err := autoFilmJellyfinConfiguration()
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(autoFilmJellyfinRefreshReq{
		Path:       cleanPath,
		Refresh:    req.Refresh,
		Recursive:  req.Recursive,
		ForceProbe: req.ForceProbe,
		ScanMode:   req.ScanMode,
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
	setAutoFilmJellyfinHeaders(request, apiKey)

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

func requestAutoFilmJellyfinPathStatus(
	ctx context.Context,
	cleanPath string,
) (AutoFilmJellyfinPathStatusResp, error) {
	result := AutoFilmJellyfinPathStatusResp{Path: cleanPath}
	baseURL, apiKey, err := autoFilmJellyfinConfiguration()
	if err != nil {
		return result, err
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		baseURL+"/Library/VirtualFolders",
		nil,
	)
	if err != nil {
		return result, err
	}
	setAutoFilmJellyfinHeaders(request, apiKey)
	response, err := autoFilmJellyfinHTTPClient.Do(request)
	if err != nil {
		return result, fmt.Errorf("request Jellyfin libraries: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(
		response.Body,
		autoFilmJellyfinResponseLimit,
	))
	if err != nil {
		return result, fmt.Errorf("read Jellyfin libraries: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return result, fmt.Errorf(
			"Jellyfin returned HTTP %d: %s",
			response.StatusCode,
			strings.TrimSpace(string(responseBody)),
		)
	}
	var libraries []autoFilmJellyfinVirtualFolder
	if err := json.Unmarshal(responseBody, &libraries); err != nil {
		return result, fmt.Errorf("decode Jellyfin libraries: %w", err)
	}
	for _, library := range libraries {
		for _, location := range library.Locations {
			root, ok := openListPathFromJellyfinLocation(location)
			if !ok || !isPathWithinRoot(cleanPath, root) {
				continue
			}
			result.Configured = true
			result.LibraryName = library.Name
			result.MatchingRoot = root
			return result, nil
		}
	}
	result.Message = fmt.Sprintf(
		"OpenList path %q is not under a configured Jellyfin remote library root",
		cleanPath,
	)
	return result, nil
}

func autoFilmJellyfinConfiguration() (string, string, error) {
	baseURL := strings.TrimRight(
		strings.TrimSpace(os.Getenv(autoFilmJellyfinURLEnvironment)),
		"/",
	)
	apiKey := strings.TrimSpace(os.Getenv(
		autoFilmJellyfinAPIKeyEnvironment,
	))
	if baseURL == "" || apiKey == "" {
		return "", "", fmt.Errorf(
			"%s and %s must be configured",
			autoFilmJellyfinURLEnvironment,
			autoFilmJellyfinAPIKeyEnvironment,
		)
	}
	return baseURL, apiKey, nil
}

func setAutoFilmJellyfinHeaders(request *http.Request, apiKey string) {
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(
		"Authorization",
		`MediaBrowser Client="OpenList", Device="Server", `+
			`DeviceId="autofilm-openlist", Version="1.0", Token="`+
			strings.ReplaceAll(apiKey, `"`, "")+`"`,
	)
}

func openListPathFromJellyfinLocation(value string) (string, bool) {
	const prefix = "openlist://"
	if !strings.HasPrefix(value, prefix) {
		return "", false
	}
	pathValue := strings.TrimPrefix(value, prefix)
	if !strings.HasPrefix(pathValue, "/") {
		pathValue = "/" + pathValue
	}
	return utils.FixAndCleanPath(pathValue), true
}

func isPathWithinRoot(candidate string, root string) bool {
	return candidate == root || strings.HasPrefix(candidate, root+"/")
}
