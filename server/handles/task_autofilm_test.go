package handles

import (
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/offline_download/tool"
)

func TestGetTaskInfoIncludesOfflineResultPath(t *testing.T) {
	task := &tool.DownloadTask{
		TempDir:    "/115/nvideo/movie/2026-07",
		ResultName: "Colony (2026) WEB-DL 1080p.mkv",
	}

	info := getTaskInfo(task)

	if info.ResultPath != "/115/nvideo/movie/2026-07/Colony (2026) WEB-DL 1080p.mkv" {
		t.Fatalf("unexpected result path: %q", info.ResultPath)
	}
}
