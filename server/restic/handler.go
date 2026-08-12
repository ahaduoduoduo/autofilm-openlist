package restic

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/db"
	"github.com/OpenListTeam/OpenList/v4/internal/errs"
	"github.com/OpenListTeam/OpenList/v4/internal/fs"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/internal/resticquota"
	"github.com/OpenListTeam/OpenList/v4/internal/stream"
	"github.com/OpenListTeam/OpenList/v4/pkg/http_range"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

const (
	mediaTypeV1 = "application/vnd.x.restic.rest.v1"
	mediaTypeV2 = "application/vnd.x.restic.rest.v2"
)

var repositoryTypes = map[string]struct{}{
	"data": {}, "index": {}, "keys": {}, "locks": {}, "snapshots": {},
}

var repositoryInventoryMu sync.Mutex

type Handler struct{}

type listEntry struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type inventorySeedRequest struct {
	Repositories []repositoryInventorySeed `json:"repositories"`
}

type repositoryInventorySeed struct {
	Name    string                 `json:"name"`
	Objects []repositoryObjectSeed `json:"objects"`
}

type repositoryObjectSeed struct {
	ObjectType string `json:"object_type"`
	Name       string `json:"name"`
	Size       int64  `json:"size"`
}

type releaseTaskRequest struct {
	Repository      string `json:"repository"`
	TaskID          string `json:"task_id"`
	DailyLimitBytes int64  `json:"daily_limit_bytes"`
	Weight          int    `json:"weight"`
}

type byteRange struct {
	start  int64
	length int64
}

func NewHandler() *Handler {
	return &Handler{}
}

func (h *Handler) Usage(c *gin.Context) {
	if _, ok := authenticate(c); !ok {
		return
	}
	usage, err := resticquota.Snapshot()
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, usage)
}

// SeedUsage imports an object inventory derived from a trusted local Restic
// index cache. It deliberately accepts a manifest instead of enumerating the
// provider because a sharded repository would otherwise require hundreds of
// directory-list requests on 115.
func (h *Handler) SeedUsage(c *gin.Context) {
	if _, ok := authenticate(c); !ok {
		return
	}
	var request inventorySeedRequest
	if err := c.ShouldBindJSON(&request); err != nil || len(request.Repositories) == 0 {
		c.String(http.StatusBadRequest, "invalid inventory manifest")
		return
	}
	repositoryInventoryMu.Lock()
	defer repositoryInventoryMu.Unlock()
	seenRepositories := make(map[string]struct{}, len(request.Repositories))
	validated := make(map[string][]model.ResticRepositoryObject, len(request.Repositories))
	for _, seed := range request.Repositories {
		if _, duplicate := seenRepositories[seed.Name]; duplicate {
			c.String(http.StatusBadRequest, "duplicate repository inventory")
			return
		}
		seenRepositories[seed.Name] = struct{}{}
		if _, ok := findRepository(seed.Name); !ok {
			c.String(http.StatusBadRequest, "unknown repository")
			return
		}
		objects, err := validateInventoryObjects(seed.Objects)
		if err != nil {
			c.String(http.StatusBadRequest, err.Error())
			return
		}
		validated[seed.Name] = objects
	}
	for repository, objects := range validated {
		if err := db.ReplaceResticRepositoryObjects(repository, objects); err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
	}
	usage, err := resticquota.Snapshot()
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, usage)
}

func validateInventoryObjects(seeds []repositoryObjectSeed) ([]model.ResticRepositoryObject, error) {
	objects := make([]model.ResticRepositoryObject, 0, len(seeds))
	seen := make(map[string]struct{}, len(seeds))
	for _, seed := range seeds {
		validConfig := seed.ObjectType == "config" && seed.Name == "config"
		_, validRepositoryType := repositoryTypes[seed.ObjectType]
		if !validConfig && (!validRepositoryType || !validObjectName(seed.Name)) {
			return nil, errors.New("invalid inventory object")
		}
		if seed.Size < 0 {
			return nil, errors.New("invalid inventory object size")
		}
		key := seed.ObjectType + "\x00" + seed.Name
		if _, duplicate := seen[key]; duplicate {
			return nil, errors.New("duplicate inventory object")
		}
		seen[key] = struct{}{}
		objects = append(objects, model.ResticRepositoryObject{
			ObjectType: seed.ObjectType,
			Name:       seed.Name,
			Size:       seed.Size,
		})
	}
	return objects, nil
}

func (h *Handler) Handle(c *gin.Context) {
	task, ok := authenticate(c)
	if !ok {
		return
	}
	repository, ok := findRepository(c.Param("repository"))
	if !ok {
		c.String(http.StatusNotFound, "repository not found")
		return
	}
	requestPath := strings.Trim(c.Param("path"), "/")
	if requestPath == "" {
		h.handleRepository(c, repository)
		return
	}
	parts := strings.Split(requestPath, "/")
	if len(parts) == 1 && parts[0] == "config" {
		h.handleObject(c, repository, "config", "config", task)
		return
	}
	if _, valid := repositoryTypes[parts[0]]; !valid {
		c.String(http.StatusBadRequest, "invalid object type")
		return
	}
	if len(parts) == 1 {
		h.handleList(c, repository, parts[0])
		return
	}
	if len(parts) != 2 || !validObjectName(parts[1]) {
		c.String(http.StatusBadRequest, "invalid object name")
		return
	}
	h.handleObject(c, repository, parts[0], parts[1], task)
}

func validObjectName(name string) bool {
	if len(name) != 64 {
		return false
	}
	_, err := hex.DecodeString(name)
	return err == nil
}

func authenticate(c *gin.Context) (resticquota.TaskPolicy, bool) {
	wantUser := conf.Conf.Restic.Username
	wantPassword := conf.Conf.Restic.Password
	user, password, ok := c.Request.BasicAuth()
	if !ok || wantUser == "" || wantPassword == "" || subtle.ConstantTimeCompare([]byte(password), []byte(wantPassword)) != 1 {
		c.Header("WWW-Authenticate", `Basic realm="OpenList Restic"`)
		c.Status(http.StatusUnauthorized)
		return resticquota.TaskPolicy{}, false
	}
	if subtle.ConstantTimeCompare([]byte(user), []byte(wantUser)) == 1 {
		return resticquota.TaskPolicy{}, true
	}
	policy, valid := parseTaskUsername(user, wantUser)
	if !valid {
		c.Status(http.StatusUnauthorized)
		return resticquota.TaskPolicy{}, false
	}
	return policy, true
}

func parseTaskUsername(user, baseUser string) (resticquota.TaskPolicy, bool) {
	prefix := baseUser + "~"
	if !strings.HasPrefix(user, prefix) {
		return resticquota.TaskPolicy{}, false
	}
	parts := strings.Split(strings.TrimPrefix(user, prefix), "~")
	if len(parts) != 3 {
		return resticquota.TaskPolicy{}, false
	}
	taskBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	limit, limitErr := strconv.ParseInt(parts[1], 10, 64)
	weight, weightErr := strconv.Atoi(parts[2])
	if err != nil || limitErr != nil || weightErr != nil || len(taskBytes) == 0 || len(taskBytes) > 192 || limit <= 0 || weight <= 0 || weight > 1000 {
		return resticquota.TaskPolicy{}, false
	}
	return resticquota.TaskPolicy{ID: string(taskBytes), DailyLimitBytes: limit, Weight: weight}, true
}

func (h *Handler) ReleaseTask(c *gin.Context) {
	task, ok := authenticate(c)
	if !ok {
		return
	}
	if task.ID != "" {
		c.Status(http.StatusForbidden)
		return
	}
	var request releaseTaskRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.Repository == "" || request.TaskID == "" || request.DailyLimitBytes <= 0 || request.Weight <= 0 || request.Weight > 1000 {
		c.String(http.StatusBadRequest, "invalid task allocation")
		return
	}
	if _, exists := findRepository(request.Repository); !exists {
		c.String(http.StatusNotFound, "repository not found")
		return
	}
	if err := resticquota.ReleaseTask(request.Repository, resticquota.TaskPolicy{
		ID: request.TaskID, DailyLimitBytes: request.DailyLimitBytes, Weight: request.Weight,
	}); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	usage, err := resticquota.Snapshot()
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, usage)
}

func findRepository(name string) (conf.ResticRepository, bool) {
	for _, repository := range conf.Conf.Restic.Repositories {
		if repository.Name == name && repository.Path != "" {
			return repository, true
		}
	}
	return conf.ResticRepository{}, false
}

func (h *Handler) handleRepository(c *gin.Context, repository conf.ResticRepository) {
	switch c.Request.Method {
	case http.MethodPost:
		if c.Query("create") != "true" {
			c.Status(http.StatusBadRequest)
			return
		}
		if err := createRepository(c, repository); err != nil {
			writeStorageError(c, err)
			return
		}
		c.Status(http.StatusOK)
	case http.MethodDelete:
		c.Status(http.StatusForbidden)
	default:
		c.Status(http.StatusMethodNotAllowed)
	}
}

func createRepository(c *gin.Context, repository conf.ResticRepository) error {
	for _, directory := range []string{"", "data", "index", "keys", "locks", "snapshots"} {
		if err := ensureDirectory(c, path.Join(repository.Path, directory)); err != nil {
			return err
		}
	}
	return db.ReplaceResticRepositoryObjects(repository.Name, nil)
}

func (h *Handler) handleList(c *gin.Context, repository conf.ResticRepository, objectType string) {
	if c.Request.Method != http.MethodGet {
		c.Status(http.StatusMethodNotAllowed)
		return
	}
	entries, err := listObjects(c, repository, objectType)
	if err != nil {
		writeStorageError(c, err)
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	if strings.Contains(c.GetHeader("Accept"), mediaTypeV2) {
		writeJSON(c, mediaTypeV2, entries)
		return
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name)
	}
	writeJSON(c, mediaTypeV1, names)
}

func listObjects(c *gin.Context, repository conf.ResticRepository, objectType string) ([]listEntry, error) {
	directory := path.Join(repository.Path, objectType)
	objects, err := listDirectory(c, directory)
	if errs.IsObjectNotFound(err) || errs.IsNotFoundError(err) {
		return []listEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	entries := make([]listEntry, 0, len(objects))
	for _, object := range objects {
		if !object.IsDir() {
			entries = append(entries, listEntry{Name: object.GetName(), Size: object.GetSize()})
			continue
		}
		if objectType != "data" {
			continue
		}
		shardObjects, shardErr := listDirectory(c, path.Join(directory, object.GetName()))
		if shardErr != nil {
			return nil, shardErr
		}
		for _, shardObject := range shardObjects {
			if !shardObject.IsDir() {
				entries = append(entries, listEntry{Name: shardObject.GetName(), Size: shardObject.GetSize()})
			}
		}
	}
	return entries, nil
}

func listDirectory(c *gin.Context, directory string) ([]model.Obj, error) {
	ctx := withMeta(c, directory)
	return fs.List(ctx, directory, &fs.ListArgs{NoLog: true})
}

func (h *Handler) handleObject(c *gin.Context, repository conf.ResticRepository, objectType, name string, task resticquota.TaskPolicy) {
	objectPath := objectPath(repository.Path, objectType, name)
	switch c.Request.Method {
	case http.MethodHead:
		h.headObject(c, objectPath)
	case http.MethodGet:
		h.getObject(c, objectPath, name)
	case http.MethodPost:
		h.putObject(c, repository, objectType, objectPath, name, task)
	case http.MethodDelete:
		if err := fs.Remove(withMeta(c, objectPath), objectPath); err != nil {
			writeStorageError(c, err)
			return
		}
		repositoryInventoryMu.Lock()
		err := db.DeleteResticRepositoryObject(repository.Name, objectType, name)
		repositoryInventoryMu.Unlock()
		if err != nil {
			log.WithError(err).WithFields(log.Fields{
				"repository": repository.Name,
				"type":       objectType,
				"name":       name,
			}).Error("failed to update Restic repository inventory after delete")
		}
		c.Status(http.StatusOK)
	default:
		c.Status(http.StatusMethodNotAllowed)
	}
}

func (h *Handler) headObject(c *gin.Context, objectPath string) {
	object, err := fs.Get(withMeta(c, objectPath), objectPath, &fs.GetArgs{NoLog: true})
	if err != nil || object.IsDir() {
		writeStorageError(c, err)
		return
	}
	c.Header("Content-Length", strconv.FormatInt(object.GetSize(), 10))
	c.Header("Accept-Ranges", "bytes")
	c.Status(http.StatusOK)
}

func (h *Handler) getObject(c *gin.Context, objectPath, name string) {
	ctx := withMeta(c, objectPath)
	link, object, err := fs.Link(ctx, objectPath, model.LinkArgs{Header: c.Request.Header})
	if err != nil {
		writeStorageError(c, err)
		return
	}
	defer link.Close()
	size := link.ContentLength
	if size <= 0 {
		size = object.GetSize()
	}
	requestedRange, err := parseRange(c.GetHeader("Range"), size)
	if err != nil {
		c.Header("Content-Range", fmt.Sprintf("bytes */%d", size))
		c.Status(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	rangeReader, err := stream.GetRangeReaderFromLink(size, link)
	if err != nil {
		writeStorageError(c, err)
		return
	}
	readRange := http_range.Range{Length: -1}
	status := http.StatusOK
	contentLength := size
	if requestedRange != nil {
		readRange = http_range.Range{Start: requestedRange.start, Length: requestedRange.length}
		status = http.StatusPartialContent
		contentLength = requestedRange.length
		c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", requestedRange.start, requestedRange.start+requestedRange.length-1, size))
	}
	reader, err := rangeReader.RangeRead(ctx, readRange)
	if err != nil {
		writeStorageError(c, err)
		return
	}
	defer reader.Close()
	c.Header("Accept-Ranges", "bytes")
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Length", strconv.FormatInt(contentLength, 10))
	c.Header("ETag", `"`+name+`"`)
	c.Status(status)
	_, _ = io.Copy(c.Writer, reader)
}

func (h *Handler) putObject(c *gin.Context, repository conf.ResticRepository, objectType, objectPath, name string, task resticquota.TaskPolicy) {
	if c.Request.ContentLength < 0 {
		c.String(http.StatusLengthRequired, "content length required")
		return
	}
	tracker, err := uploadTracker(repository.Name, objectType, c.Request.ContentLength, task)
	if errors.Is(err, resticquota.ErrQuotaExceeded) {
		c.Header("Retry-After", secondsUntilTomorrow())
		c.String(http.StatusTooManyRequests, resticquota.ErrQuotaExceeded.Error())
		return
	}
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	defer tracker.Close()
	directory := path.Dir(objectPath)
	if err := ensureDirectory(c, directory); err != nil {
		writeStorageError(c, err)
		return
	}
	ctx := resticquota.WithTracker(withMeta(c, objectPath), tracker)
	object := &model.Object{
		Name:     name,
		Size:     c.Request.ContentLength,
		Modified: time.Now(),
		Ctime:    time.Now(),
	}
	fileStream := &stream.FileStream{
		Ctx:      ctx,
		Obj:      object,
		Reader:   c.Request.Body,
		Mimetype: "application/octet-stream",
	}
	if err := fs.PutDirectly(ctx, directory, fileStream); err != nil {
		if errors.Is(tracker.Err(), resticquota.ErrQuotaExceeded) {
			c.Header("Retry-After", secondsUntilTomorrow())
			c.String(http.StatusTooManyRequests, resticquota.ErrQuotaExceeded.Error())
			return
		}
		writeStorageError(c, err)
		return
	}
	if err := tracker.Close(); err != nil {
		writeStorageError(c, err)
		return
	}
	repositoryInventoryMu.Lock()
	err = db.UpsertResticRepositoryObject(
		repository.Name,
		objectType,
		name,
		c.Request.ContentLength,
	)
	repositoryInventoryMu.Unlock()
	if err != nil {
		log.WithError(err).WithFields(log.Fields{
			"repository": repository.Name,
			"type":       objectType,
			"name":       name,
		}).Error("failed to update Restic repository inventory after upload")
	}
	c.Status(http.StatusOK)
}

func uploadTracker(repository, objectType string, size int64, task resticquota.TaskPolicy) (*resticquota.Tracker, error) {
	if objectType == "data" {
		return resticquota.NewReservedTracker(repository, size, task)
	}
	return resticquota.NewUnmeteredTracker(repository, task), nil
}

func objectPath(root, objectType, name string) string {
	if objectType == "config" {
		return path.Join(root, "config")
	}
	if objectType == "data" && len(name) >= 2 {
		return path.Join(root, objectType, name[:2], name)
	}
	return path.Join(root, objectType, name)
}

func ensureDirectory(c *gin.Context, directory string) error {
	cleaned := path.Clean("/" + strings.TrimSpace(directory))
	current := "/"
	for _, segment := range strings.Split(strings.Trim(cleaned, "/"), "/") {
		if segment == "" {
			continue
		}
		current = path.Join(current, segment)
		ctx := withMeta(c, current)
		object, err := fs.Get(ctx, current, &fs.GetArgs{NoLog: true})
		if err == nil {
			if !object.IsDir() {
				return fmt.Errorf("%s is not a directory", current)
			}
			continue
		}
		if !errs.IsObjectNotFound(err) && !errs.IsNotFoundError(err) {
			return err
		}
		if err = fs.MakeDir(ctx, current); err != nil {
			return err
		}
	}
	return nil
}

func withMeta(c *gin.Context, objectPath string) context.Context {
	meta, _ := op.GetNearestMeta(objectPath)
	return context.WithValue(c.Request.Context(), conf.MetaKey, meta)
}

func parseRange(value string, size int64) (*byteRange, error) {
	if value == "" {
		return nil, nil
	}
	if !strings.HasPrefix(value, "bytes=") || strings.Contains(value, ",") || size < 0 {
		return nil, errors.New("invalid range")
	}
	parts := strings.Split(strings.TrimPrefix(value, "bytes="), "-")
	if len(parts) != 2 {
		return nil, errors.New("invalid range")
	}
	if parts[0] == "" {
		suffix, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || suffix <= 0 {
			return nil, errors.New("invalid range")
		}
		suffix = min(suffix, size)
		return &byteRange{start: size - suffix, length: suffix}, nil
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || start < 0 || start >= size {
		return nil, errors.New("invalid range")
	}
	end := size - 1
	if parts[1] != "" {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil || end < start {
			return nil, errors.New("invalid range")
		}
		end = min(end, size-1)
	}
	return &byteRange{start: start, length: end - start + 1}, nil
}

func writeJSON(c *gin.Context, contentType string, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Data(http.StatusOK, contentType, data)
}

func writeStorageError(c *gin.Context, err error) {
	if err == nil || errs.IsObjectNotFound(err) || errs.IsNotFoundError(err) {
		c.Status(http.StatusNotFound)
		return
	}
	c.String(http.StatusInternalServerError, err.Error())
}

func secondsUntilTomorrow() string {
	location := time.Local
	if configured, err := time.LoadLocation(conf.Conf.Restic.Timezone); err == nil {
		location = configured
	}
	now := time.Now().In(location)
	tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, location)
	return strconv.FormatInt(max(1, int64(time.Until(tomorrow).Seconds())), 10)
}
