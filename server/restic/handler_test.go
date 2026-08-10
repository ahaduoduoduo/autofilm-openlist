package restic

import (
	"encoding/base64"
	"testing"
)

func TestObjectPath(t *testing.T) {
	if got := objectPath("/backup/restic", "data", "abcdef"); got != "/backup/restic/data/ab/abcdef" {
		t.Fatalf("objectPath() = %q", got)
	}
	if got := objectPath("/backup/restic", "snapshots", "abcdef"); got != "/backup/restic/snapshots/abcdef" {
		t.Fatalf("objectPath() = %q", got)
	}
}

func TestParseTaskUsername(t *testing.T) {
	encoded := base64.RawURLEncoding.EncodeToString([]byte("nas-config"))
	policy, ok := parseTaskUsername("backrest~"+encoded+"~5368709120~2", "backrest")
	if !ok {
		t.Fatal("expected derived task username to be valid")
	}
	if policy.ID != "nas-config" || policy.DailyLimitBytes != 5368709120 || policy.Weight != 2 {
		t.Fatalf("parseTaskUsername() = %+v", policy)
	}
	for _, invalid := range []string{
		"other~" + encoded + "~5368709120~2",
		"backrest~***~5368709120~2",
		"backrest~" + encoded + "~0~2",
		"backrest~" + encoded + "~5368709120~0",
	} {
		if _, ok := parseTaskUsername(invalid, "backrest"); ok {
			t.Fatalf("expected %q to be invalid", invalid)
		}
	}
}

func TestValidObjectName(t *testing.T) {
	valid := "245bc4c430d393f74fbe7b13325e30dbde9fb0745e50caad57c446c93d20096b"
	if !validObjectName(valid) {
		t.Fatal("expected Restic object ID to be valid")
	}
	for _, invalid := range []string{"..", "abc", valid[:63], valid[:63] + "z"} {
		if validObjectName(invalid) {
			t.Fatalf("expected %q to be invalid", invalid)
		}
	}
}

func TestValidateInventoryObjects(t *testing.T) {
	validID := "245bc4c430d393f74fbe7b13325e30dbde9fb0745e50caad57c446c93d20096b"
	objects, err := validateInventoryObjects([]repositoryObjectSeed{
		{ObjectType: "config", Name: "config", Size: 155},
		{ObjectType: "data", Name: validID, Size: 1024},
	})
	if err != nil {
		t.Fatalf("validateInventoryObjects() error = %v", err)
	}
	if len(objects) != 2 || objects[1].Size != 1024 {
		t.Fatalf("validateInventoryObjects() = %+v", objects)
	}

	invalid := [][]repositoryObjectSeed{
		{{ObjectType: "data", Name: "invalid", Size: 1}},
		{{ObjectType: "data", Name: validID, Size: -1}},
		{
			{ObjectType: "index", Name: validID, Size: 1},
			{ObjectType: "index", Name: validID, Size: 1},
		},
	}
	for _, seeds := range invalid {
		if _, err := validateInventoryObjects(seeds); err == nil {
			t.Fatalf("expected invalid inventory seeds %+v to fail", seeds)
		}
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
