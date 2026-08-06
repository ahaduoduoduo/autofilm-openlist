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
