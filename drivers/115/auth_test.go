package _115

import (
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

func TestIsRiskControlError(t *testing.T) {
	t.Parallel()

	for _, message := range []string{
		"405 Method Not Allowed",
		"aliyun request failed with status code 405",
		"阿里云 405 风控",
	} {
		if !isRiskControlError(message) {
			t.Fatalf("expected risk-control error for %q", message)
		}
	}
	for _, message := range []string{
		"404 object not found",
		"405 is a file identifier",
		"network timeout",
	} {
		if isRiskControlError(message) {
			t.Fatalf("unexpected risk-control error for %q", message)
		}
	}
}

func TestGetAutoFilmAuthStateFromPersistedStatus(t *testing.T) {
	t.Parallel()

	driver := &Pan115{
		Storage: model.Storage{Status: riskControlStatus},
	}
	state := driver.GetAutoFilmAuthState()
	if state.State != "risk_controlled" ||
		!state.RequiresReauthentication ||
		state.StatusCode != riskControlHTTPCode {
		t.Fatalf("unexpected state: %+v", state)
	}
}
