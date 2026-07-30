package handles

import (
	"context"
	"fmt"
	stdpath "path"
	"strconv"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/db"
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/errs"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/offline_download/tool"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/internal/sign"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	"github.com/OpenListTeam/OpenList/v4/server/common"
	"github.com/gin-gonic/gin"
)

type AutoFilmObjectReq struct {
	Path    string `json:"path" binding:"required"`
	Refresh bool   `json:"refresh"`
}

type AutoFilmDeleteObjectReq struct {
	Path string `json:"path" binding:"required"`
}

type AutoFilmObjectResp struct {
	Path         string    `json:"path"`
	Name         string    `json:"name"`
	Size         int64     `json:"size"`
	IsDir        bool      `json:"is_dir"`
	Modified     time.Time `json:"modified"`
	Created      time.Time `json:"created"`
	DownloadPath string    `json:"download_path,omitempty"`
}

type AutoFilmListResp struct {
	Objects []AutoFilmObjectResp `json:"objects"`
}

type AutoFilmStorageReq struct {
	StorageID uint `json:"storage_id" binding:"required"`
}

type AutoFilmOfflineTasksResp struct {
	Tasks []TaskInfo `json:"tasks"`
}

type AutoFilmOfflineTaskReq struct {
	TaskID string `json:"task_id" binding:"required"`
}

func autoFilmObjectResponse(
	obj model.Obj,
	fullPath string,
) AutoFilmObjectResp {
	return autoFilmObjectResponseValue(obj, fullPath)
}

func autoFilmVirtualObjectResponse(
	obj model.Obj,
	fullPath string,
) AutoFilmObjectResp {
	return autoFilmObjectResponseValue(obj, fullPath)
}

func autoFilmObjectResponseValue(
	obj model.Obj,
	fullPath string,
) AutoFilmObjectResp {
	obj = model.UnwrapObjName(obj)
	resp := AutoFilmObjectResp{
		Path:     utils.FixAndCleanPath(fullPath),
		Name:     obj.GetName(),
		Size:     obj.GetSize(),
		IsDir:    obj.IsDir(),
		Modified: obj.ModTime(),
		Created:  obj.CreateTime(),
	}
	if !obj.IsDir() {
		resp.DownloadPath = fmt.Sprintf(
			"/d%s?sign=%s",
			utils.EncodePath(resp.Path, true),
			sign.Sign(resp.Path),
		)
	}
	return resp
}

func autoFilmVirtualDirectoryResponse(fullPath string) AutoFilmObjectResp {
	cleanPath := utils.FixAndCleanPath(fullPath)
	return autoFilmVirtualObjectResponse(&model.Object{
		Name:     stdpath.Base(cleanPath),
		Path:     cleanPath,
		IsFolder: true,
	}, cleanPath)
}

func autoFilmVirtualListResponse(fullPath string, objects []model.Obj) AutoFilmListResp {
	content := make([]AutoFilmObjectResp, 0, len(objects))
	for _, obj := range objects {
		content = append(content, autoFilmVirtualObjectResponse(
			obj,
			stdpath.Join(fullPath, obj.GetName()),
		))
	}
	return AutoFilmListResp{
		Objects: content,
	}
}

func AutoFilmGetObject(c *gin.Context) {
	var req AutoFilmObjectReq
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	fullPath := utils.FixAndCleanPath(req.Path)
	storage, actualPath, err := op.GetStorageAndActualPath(fullPath)
	if err != nil {
		virtualObjects := op.GetStorageVirtualFilesByPath(fullPath)
		if len(virtualObjects) > 0 {
			common.SuccessResp(c, autoFilmVirtualDirectoryResponse(fullPath))
			return
		}
		common.ErrorResp(c, err, 404)
		return
	}
	obj, err := getAutoFilmObject(
		c.Request.Context(),
		storage,
		actualPath,
		req.Refresh,
	)
	if err != nil {
		common.ErrorResp(c, err, 404)
		return
	}
	common.SuccessResp(c, autoFilmObjectResponse(obj, fullPath))
}

func getAutoFilmObject(
	ctx context.Context,
	storage driver.Driver,
	actualPath string,
	refresh bool,
) (model.Obj, error) {
	if !refresh || actualPath == "/" {
		return op.Get(ctx, storage, actualPath)
	}

	parentPath := stdpath.Dir(actualPath)
	objects, err := op.List(ctx, storage, parentPath, model.ListArgs{
		Refresh: true,
	})
	if err != nil {
		return nil, err
	}
	if obj := findAutoFilmObject(objects, stdpath.Base(actualPath)); obj != nil {
		return obj, nil
	}
	return nil, errs.ObjectNotFound
}

func findAutoFilmObject(objects []model.Obj, name string) model.Obj {
	for _, obj := range objects {
		if obj.GetName() == name {
			return obj
		}
	}
	return nil
}

func AutoFilmListObjects(c *gin.Context) {
	var req AutoFilmObjectReq
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	fullPath := utils.FixAndCleanPath(req.Path)
	storage, actualPath, err := op.GetStorageAndActualPath(fullPath)
	if err != nil {
		virtualObjects := op.GetStorageVirtualFilesByPath(fullPath)
		if len(virtualObjects) > 0 {
			common.SuccessResp(c, autoFilmVirtualListResponse(fullPath, virtualObjects))
			return
		}
		common.ErrorResp(c, err, 404)
		return
	}
	directory, err := op.Get(c.Request.Context(), storage, actualPath)
	if err != nil {
		common.ErrorResp(c, err, 404)
		return
	}
	if !directory.IsDir() {
		common.ErrorStrResp(c, "path is not a directory", 400)
		return
	}
	objects, err := op.List(c.Request.Context(), storage, actualPath, model.ListArgs{
		Refresh: req.Refresh,
	})
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	content := make([]AutoFilmObjectResp, 0, len(objects))
	for _, obj := range objects {
		content = append(content, autoFilmObjectResponse(
			obj,
			stdpath.Join(fullPath, obj.GetName()),
		))
	}
	common.SuccessResp(c, AutoFilmListResp{
		Objects: content,
	})
}

func AutoFilmDeleteObject(c *gin.Context) {
	var req AutoFilmDeleteObjectReq
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	fullPath := utils.FixAndCleanPath(req.Path)
	storage, actualPath, err := op.GetStorageAndActualPath(fullPath)
	if err != nil {
		common.ErrorResp(c, err, 404)
		return
	}
	if actualPath == "/" {
		common.ErrorStrResp(c, "storage root deletion is not allowed", 400)
		return
	}
	if err := op.Remove(c.Request.Context(), storage, actualPath); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	common.SuccessResp(c)
}

func autoFilmStorageProvider(
	c *gin.Context,
	storageID uint,
) (driver.Driver, bool) {
	storage, err := db.GetStorageById(storageID)
	if err != nil {
		common.ErrorResp(c, err, 404)
		return nil, false
	}
	provider, err := op.GetStorageByMountPath(storage.MountPath)
	if err != nil {
		common.ErrorResp(c, err, 503)
		return nil, false
	}
	return provider, true
}

func AutoFilmStartStorageAuth(c *gin.Context) {
	var req AutoFilmStorageReq
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	storage, ok := autoFilmStorageProvider(c, req.StorageID)
	if !ok {
		return
	}
	provider, ok := storage.(driver.AutoFilmAuthProvider)
	if !ok {
		common.ErrorStrResp(c, "storage does not support interactive authentication", 400)
		return
	}
	session, err := provider.StartAutoFilmAuth(c.Request.Context())
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	common.SuccessResp(c, session)
}

func AutoFilmGetStorageAuth(c *gin.Context) {
	storageID, err := strconv.ParseUint(c.Query("storage_id"), 10, 64)
	if err != nil || storageID == 0 {
		common.ErrorStrResp(c, "invalid storage_id", 400)
		return
	}
	sessionID := c.Query("session_id")
	if sessionID == "" {
		common.ErrorStrResp(c, "session_id is required", 400)
		return
	}
	storage, ok := autoFilmStorageProvider(c, uint(storageID))
	if !ok {
		return
	}
	provider, ok := storage.(driver.AutoFilmAuthProvider)
	if !ok {
		common.ErrorStrResp(c, "storage does not support interactive authentication", 400)
		return
	}
	session, err := provider.GetAutoFilmAuth(c.Request.Context(), sessionID)
	if err != nil {
		common.ErrorResp(c, err, 404)
		return
	}
	common.SuccessResp(c, session)
}

func AutoFilmGetStorageAuthQRCode(c *gin.Context) {
	storageID, err := strconv.ParseUint(c.Query("storage_id"), 10, 64)
	if err != nil || storageID == 0 {
		common.ErrorStrResp(c, "invalid storage_id", 400)
		return
	}
	sessionID := c.Query("session_id")
	if sessionID == "" {
		common.ErrorStrResp(c, "session_id is required", 400)
		return
	}
	storage, ok := autoFilmStorageProvider(c, uint(storageID))
	if !ok {
		return
	}
	provider, ok := storage.(driver.AutoFilmAuthProvider)
	if !ok {
		common.ErrorStrResp(c, "storage does not support interactive authentication", 400)
		return
	}
	qrCode, err := provider.GetAutoFilmAuthQRCode(sessionID)
	if err != nil {
		common.ErrorResp(c, err, 404)
		return
	}
	c.Data(200, "image/png", qrCode)
}

func AutoFilmGetStorageAuthHealth(c *gin.Context) {
	// Compatibility alias. This endpoint intentionally returns the passive
	// state and must never issue a request to the storage provider.
	AutoFilmGetStorageAuthState(c)
}

func AutoFilmGetStorageAuthState(c *gin.Context) {
	storageID, err := strconv.ParseUint(c.Query("storage_id"), 10, 64)
	if err != nil || storageID == 0 {
		common.ErrorStrResp(c, "invalid storage_id", 400)
		return
	}
	storage, ok := autoFilmStorageProvider(c, uint(storageID))
	if !ok {
		return
	}
	provider, ok := storage.(driver.AutoFilmAuthStateProvider)
	if !ok {
		common.ErrorStrResp(c, "storage does not expose authentication state", 400)
		return
	}
	common.SuccessResp(c, provider.GetAutoFilmAuthState())
}

func AutoFilmGetScheduler(c *gin.Context) {
	storageID, err := strconv.ParseUint(c.Query("storage_id"), 10, 64)
	if err != nil || storageID == 0 {
		common.ErrorStrResp(c, "invalid storage_id", 400)
		return
	}
	storage, ok := autoFilmStorageProvider(c, uint(storageID))
	if !ok {
		return
	}
	provider, ok := storage.(driver.AutoFilmSchedulerProvider)
	if !ok {
		common.ErrorStrResp(c, "storage does not expose a scheduler", 400)
		return
	}
	common.SuccessResp(c, provider.AutoFilmSchedulerSnapshot())
}

// AutoFilmListOfflineTasks returns an in-memory snapshot only. It does not
// list storage objects, call a cloud provider, or expose task mutations.
func AutoFilmListOfflineTasks(c *gin.Context) {
	tasks := make([]TaskInfo, 0)
	if tool.DownloadTaskManager != nil {
		downloadTasks := tool.DownloadTaskManager.GetByCondition(
			func(_ *tool.DownloadTask) bool { return true },
		)
		tasks = append(tasks, getTaskInfos(downloadTasks)...)
	}
	if tool.TransferTaskManager != nil {
		transferTasks := tool.TransferTaskManager.GetByCondition(
			func(_ *tool.TransferTask) bool { return true },
		)
		tasks = append(tasks, getTaskInfos(transferTasks)...)
	}
	common.SuccessResp(c, AutoFilmOfflineTasksResp{Tasks: tasks})
}

// AutoFilmDeleteOfflineTask cancels an OpenList download task and removes the
// corresponding provider-side task. It deliberately does not accept transfer
// task IDs or perform any storage object deletion.
func AutoFilmDeleteOfflineTask(c *gin.Context) {
	var req AutoFilmOfflineTaskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	if tool.DownloadTaskManager == nil {
		common.ErrorStrResp(c, "offline download task manager is unavailable", 503)
		return
	}
	downloadTask, ok := tool.DownloadTaskManager.GetByID(req.TaskID)
	if !ok {
		common.ErrorStrResp(c, "offline download task not found", 404)
		return
	}
	tool.DownloadTaskManager.Cancel(req.TaskID)
	if err := downloadTask.RemoveRemote(); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	tool.DownloadTaskManager.Remove(req.TaskID)
	common.SuccessResp(c)
}
