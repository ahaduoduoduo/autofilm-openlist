package resticquota

import "testing"

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
