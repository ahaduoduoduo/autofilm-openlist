package resticquota

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
)

func useTestConfig(t *testing.T) {
	previous := conf.Conf
	conf.Conf = &conf.Config{}
	t.Cleanup(func() {
		conf.Conf = previous
	})
}

func TestMinRemaining(t *testing.T) {
	tests := []struct {
		name      string
		requested int64
		limit     int64
		used      int64
		want      int64
	}{
		{name: "unlimited", requested: 100, want: 100},
		{name: "within quota", requested: 100, limit: 200, used: 50, want: 100},
		{name: "partial quota", requested: 100, limit: 200, used: 150, want: 50},
		{name: "exhausted", requested: 100, limit: 200, used: 200, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := minRemaining(tt.requested, tt.limit, tt.used); got != tt.want {
				t.Fatalf("minRemaining() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestTaskAllocationSharesOnlyReleasedRemainder(t *testing.T) {
	m := newManager()
	m.day = "2026-08-11"
	m.month = "2026-08"

	nas := taskKey{repository: "115-offsite", task: "nas-config"}
	timeMachine := taskKey{repository: "115-offsite", task: "time-machine"}
	m.rememberTaskLocked(nas, TaskPolicy{ID: nas.task, DailyLimitBytes: 5, Weight: 2})
	m.rememberTaskLocked(timeMachine, TaskPolicy{ID: timeMachine.task, DailyLimitBytes: 45, Weight: 1})
	m.taskBytes[nas] = 2
	m.taskBytes[timeMachine] = 45

	if got := m.taskAvailableLocked(timeMachine); got != 0 {
		t.Fatalf("time-machine available before release = %d, want 0", got)
	}
	m.taskReleased[nas] = true
	m.taskReleasedAt[nas] = 2
	if got := m.taskAvailableLocked(timeMachine); got != 3 {
		t.Fatalf("time-machine available after release = %d, want 3", got)
	}
	m.taskBytes[timeMachine] = 47
	if got := m.taskAvailableLocked(timeMachine); got != 1 {
		t.Fatalf("time-machine available after borrowing = %d, want 1", got)
	}
}

func TestReserveExactRejectsWholeObject(t *testing.T) {
	useTestConfig(t)
	m := newManager()
	now := time.Now().In(configuredLocation())
	m.day = now.Format("2006-01-02")
	m.month = now.Format("2006-01")
	policy := TaskPolicy{ID: "time-machine", DailyLimitBytes: 10, Weight: 1}

	if err := m.reserveExact("115-offsite", policy, 8); err != nil {
		t.Fatalf("reserveExact(8) error = %v", err)
	}
	if err := m.reserveExact("115-offsite", policy, 3); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("reserveExact(3) error = %v, want ErrQuotaExceeded", err)
	}
	key := taskKey{repository: "115-offsite", task: "time-machine"}
	if got := m.reserved["115-offsite"]; got != 8 {
		t.Fatalf("repository reservation = %d, want 8", got)
	}
	if got := m.taskReserved[key]; got != 8 {
		t.Fatalf("task reservation = %d, want 8", got)
	}
}

func TestReservedReaderUsesExistingReservation(t *testing.T) {
	useTestConfig(t)
	m := newManager()
	tracker := &Tracker{
		manager:          m,
		metered:          true,
		fixedReservation: true,
		reservedBytes:    4,
	}
	ctx := WithTracker(context.Background(), tracker)
	data, err := io.ReadAll(WrapReader(ctx, strings.NewReader("test")))
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(data) != "test" {
		t.Fatalf("ReadAll() = %q, want test", data)
	}
	if tracker.consumedBytes != 4 {
		t.Fatalf("consumed bytes = %d, want 4", tracker.consumedBytes)
	}
}

func TestUnmeteredReaderDoesNotReserveQuota(t *testing.T) {
	useTestConfig(t)
	m := newManager()
	tracker := &Tracker{manager: m, metered: false}
	ctx := WithTracker(context.Background(), tracker)
	data, err := io.ReadAll(WrapReader(ctx, strings.NewReader("metadata")))
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(data) != "metadata" {
		t.Fatalf("ReadAll() = %q, want metadata", data)
	}
	if got := total(m.reserved); got != 0 {
		t.Fatalf("reserved bytes = %d, want 0", got)
	}
}

func TestReservedReaderAccountsForProviderRetry(t *testing.T) {
	useTestConfig(t)
	m := newManager()
	now := time.Now().In(configuredLocation())
	m.day = now.Format("2006-01-02")
	m.month = now.Format("2006-01")
	policy := TaskPolicy{ID: "time-machine", DailyLimitBytes: 8, Weight: 1}
	key := taskKey{repository: "115-offsite", task: policy.ID}
	m.reserved["115-offsite"] = 4
	m.taskReserved[key] = 4
	tracker := &Tracker{
		manager:          m,
		repository:       "115-offsite",
		task:             policy,
		metered:          true,
		fixedReservation: true,
		reservedBytes:    4,
	}
	ctx := WithTracker(context.Background(), tracker)
	for _, body := range []string{"test", "redo"} {
		data, err := io.ReadAll(WrapReader(ctx, strings.NewReader(body)))
		if err != nil {
			t.Fatalf("ReadAll(%q) error = %v", body, err)
		}
		if string(data) != body {
			t.Fatalf("ReadAll(%q) = %q", body, data)
		}
	}
	m.finishReservation("115-offsite", policy, 4, tracker.consumedBytes)
	if got := m.taskBytes[key]; got != 8 {
		t.Fatalf("task bytes = %d, want 8", got)
	}
}
