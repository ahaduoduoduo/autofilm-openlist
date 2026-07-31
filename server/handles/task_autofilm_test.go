package handles

import (
	"testing"
	"time"

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

func TestGetTaskInfoDistinguishesProviderSubmission(t *testing.T) {
	submittedAt := time.Date(2026, 7, 31, 15, 24, 53, 0, time.UTC)
	task := &tool.DownloadTask{
		ProviderTaskID:      "provider-info-hash",
		ProviderSubmittedAt: &submittedAt,
	}

	info := getTaskInfo(task)

	if info.ProviderTaskID != "provider-info-hash" {
		t.Fatalf("unexpected provider task id: %q", info.ProviderTaskID)
	}
	if info.ProviderSubmittedAt == nil ||
		!info.ProviderSubmittedAt.Equal(submittedAt) {
		t.Fatalf(
			"unexpected provider submission time: %v",
			info.ProviderSubmittedAt,
		)
	}
}
