package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/gin-gonic/gin"
)

type counters struct {
	Uploads             int     `json:"uploads"`
	Links               int     `json:"links"`
	Downloads           int     `json:"downloads"`
	SuccessfulDownloads int     `json:"successful_downloads"`
	Lists               int     `json:"lists"`
	CredentialLeaks     int     `json:"credential_leaks"`
	UserAgentMismatches int     `json:"user_agent_mismatches"`
	ActiveDownloads     int     `json:"active_downloads"`
	MaxActiveDownloads  int     `json:"max_active_downloads"`
	LinkRequestStarts   []int64 `json:"link_request_starts_ms"`
	DownloadStarts      []int64 `json:"download_starts_ms"`
}
type controls struct {
	RequireEmptyUserAgent     bool  `json:"require_empty_user_agent"`
	ExpireLinks               bool  `json:"expire_links"`
	ExpiredStatus             int   `json:"expired_status"`
	LinkTTLMilliseconds       int64 `json:"link_ttl_ms"`
	DisableCORS               bool  `json:"disable_cors"`
	FailUpload                bool  `json:"fail_upload"`
	LoseUploadReceipt         bool  `json:"lose_upload_receipt"`
	CDNStatus                 int   `json:"cdn_status"`
	BlockLink                 bool  `json:"block_link"`
	DownloadDelayMilliseconds int64 `json:"download_delay_ms"`
}
type locator struct {
	Parent string `json:"parent"`
	Name   string `json:"name"`
}

// Embedding Driver lets unexpected methods fail immediately. List and Link are
// explicitly rejected: the production gateway must use durable exact locators.
type simulator struct {
	driver.Driver
	mu            sync.Mutex
	root, base    string
	stats         counters
	control       controls
	rejectBefore  int64
	expiredStatus int
}

func (p *simulator) GetStorage() *model.Storage { return &model.Storage{ID: 1, MountPath: "/fake115"} }
func (p *simulator) TierAccountKey() string     { return "fake115:one-account" }
func (p *simulator) List(context.Context, model.Obj, model.ListArgs) ([]model.Obj, error) {
	p.mu.Lock()
	p.stats.Lists++
	p.mu.Unlock()
	return nil, fmt.Errorf("remote directory enumeration is forbidden in this lab")
}
func (p *simulator) Link(context.Context, model.Obj, model.LinkArgs) (*model.Link, error) {
	return nil, fmt.Errorf("ordinary path resolution is forbidden in this lab")
}
func (p *simulator) TierPut(ctx context.Context, parent string, file model.FileStreamer) (json.RawMessage, error) {
	p.mu.Lock()
	p.stats.Uploads++
	control := p.control
	p.mu.Unlock()
	if control.FailUpload {
		return nil, fmt.Errorf("simulated upload failure")
	}
	if !safeName(parent) || !safeName(file.GetName()) {
		return nil, fmt.Errorf("invalid lab path")
	}
	dir := filepath.Join(p.root, parent)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := io.Copy(f, file); err != nil {
		return nil, err
	}
	if err := f.Sync(); err != nil {
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(f.Name(), filepath.Join(dir, file.GetName())); err != nil {
		return nil, err
	}
	if control.LoseUploadReceipt {
		return nil, fmt.Errorf("simulated lost receipt after upload")
	}
	return json.Marshal(locator{Parent: parent, Name: file.GetName()})
}
func (p *simulator) TierLink(ctx context.Context, data json.RawMessage) (*model.Link, error) {
	p.mu.Lock()
	p.stats.Links++
	p.stats.LinkRequestStarts = append(p.stats.LinkRequestStarts, time.Now().UnixMilli())
	ttl := time.Duration(p.control.LinkTTLMilliseconds) * time.Millisecond
	blocked := p.control.BlockLink
	requireEmptyUA := p.control.RequireEmptyUserAgent
	p.mu.Unlock()
	if blocked {
		return nil, fmt.Errorf("request has been blocked")
	}
	var l locator
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, err
	}
	if !safeName(l.Parent) || !safeName(l.Name) {
		return nil, fmt.Errorf("invalid locator")
	}
	link := &model.Link{URL: p.base + "/cdn/" + l.Parent + "/" + l.Name + "?issued=" + strconv.FormatInt(time.Now().UnixNano(), 10)}
	if requireEmptyUA {
		link.URL += "&empty_ua=1"
		link.Header = http.Header{"User-Agent": {""}}
	}
	if ttl > 0 {
		link.Expiration = &ttl
	}
	return link, nil
}
func (p *simulator) TierRemove(context.Context, json.RawMessage) error {
	return fmt.Errorf("physical removal is not part of the online protocol")
}
func safeName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\\")
}
func (p *simulator) download(c *gin.Context) {
	p.mu.Lock()
	p.stats.Downloads++
	p.stats.ActiveDownloads++
	p.stats.MaxActiveDownloads = max(p.stats.MaxActiveDownloads, p.stats.ActiveDownloads)
	p.stats.DownloadStarts = append(p.stats.DownloadStarts, time.Now().UnixMilli())
	if c.GetHeader("Authorization") != "" {
		p.stats.CredentialLeaks++
	}
	cors := !p.control.DisableCORS
	status := p.control.CDNStatus
	if c.Query("empty_ua") == "1" && c.GetHeader("User-Agent") != "" {
		p.stats.UserAgentMismatches++
		status = http.StatusForbidden
	}
	issued, _ := strconv.ParseInt(c.Query("issued"), 10, 64)
	if status == 0 && issued < p.rejectBefore {
		status = p.expiredStatus
	}
	delay := p.control.DownloadDelayMilliseconds
	p.mu.Unlock()
	if cors {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Expose-Headers", "Content-Range, Content-Length")
	}
	defer func() { p.mu.Lock(); p.stats.ActiveDownloads--; p.mu.Unlock() }()
	if delay > 0 {
		timer := time.NewTimer(time.Duration(min(delay, 10000)) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-c.Request.Context().Done():
			return
		}
	}
	if status != 0 {
		c.Status(status)
		return
	}
	parent, name := c.Param("parent"), c.Param("name")
	if !safeName(parent) || !safeName(name) {
		c.Status(400)
		return
	}
	f, err := os.Open(filepath.Join(p.root, parent, name))
	if err != nil {
		c.Status(404)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		c.Status(500)
		return
	}
	http.ServeContent(c.Writer, c.Request, name, st.ModTime(), f)
	if c.Writer.Status() == http.StatusOK || c.Writer.Status() == http.StatusPartialContent {
		p.mu.Lock()
		p.stats.SuccessfulDownloads++
		p.mu.Unlock()
	}
}
