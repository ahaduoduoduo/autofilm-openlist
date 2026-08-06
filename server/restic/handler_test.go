package restic

import "testing"

func TestObjectPath(t *testing.T) {
	if got := objectPath("/backup/restic", "data", "abcdef"); got != "/backup/restic/data/ab/abcdef" {
		t.Fatalf("objectPath() = %q", got)
	}
	if got := objectPath("/backup/restic", "snapshots", "abcdef"); got != "/backup/restic/snapshots/abcdef" {
		t.Fatalf("objectPath() = %q", got)
	}
}

func TestParseRange(t *testing.T) {
	tests := []struct {
		value      string
		size       int64
		wantStart  int64
		wantLength int64
		wantError  bool
	}{
		{value: "bytes=10-19", size: 100, wantStart: 10, wantLength: 10},
		{value: "bytes=90-", size: 100, wantStart: 90, wantLength: 10},
		{value: "bytes=-5", size: 100, wantStart: 95, wantLength: 5},
		{value: "bytes=100-", size: 100, wantError: true},
		{value: "items=1-2", size: 100, wantError: true},
	}
	for _, tt := range tests {
		got, err := parseRange(tt.value, tt.size)
		if (err != nil) != tt.wantError {
			t.Fatalf("parseRange(%q) error = %v", tt.value, err)
		}
		if tt.wantError {
			continue
		}
		if got.start != tt.wantStart || got.length != tt.wantLength {
			t.Fatalf("parseRange(%q) = %+v", tt.value, got)
		}
	}
}
