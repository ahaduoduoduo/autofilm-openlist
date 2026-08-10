package _115

import (
	"context"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/resticquota"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
)

func TestShouldUseOrdinaryOSSUpload(t *testing.T) {
	t.Parallel()

	resticContext := resticquota.WithTracker(
		context.Background(),
		resticquota.NewTracker("test-repository"),
	)
	tests := []struct {
		name string
		ctx  context.Context
		size int64
		want bool
	}{
		{name: "generic at ordinary limit", ctx: context.Background(), size: 10 * utils.MB, want: true},
		{name: "generic above ordinary limit", ctx: context.Background(), size: 10*utils.MB + 1, want: false},
		{name: "generic restic-sized pack", ctx: context.Background(), size: 20 * utils.MB, want: false},
		{name: "restic pack", ctx: resticContext, size: 20 * utils.MB, want: true},
		{name: "restic at ordinary limit", ctx: resticContext, size: 64 * utils.MB, want: true},
		{name: "restic above ordinary limit", ctx: resticContext, size: 64*utils.MB + 1, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldUseOrdinaryOSSUpload(tt.ctx, tt.size); got != tt.want {
				t.Fatalf("shouldUseOrdinaryOSSUpload(size=%d) = %v, want %v", tt.size, got, tt.want)
			}
		})
	}
}
