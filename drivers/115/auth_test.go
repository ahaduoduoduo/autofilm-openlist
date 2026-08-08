package _115

import (
	"net/http"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
	driver115 "github.com/SheltonZhu/115driver/pkg/driver"
	"github.com/go-resty/resty/v2"
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

func TestGetAutoFilmAuthStateRequiresLoginWhenCredentialIsMissing(t *testing.T) {
	t.Parallel()

	for _, status := range []string{
		missingCredentialStatus,
		"bad cookie",
		"user not login",
	} {
		driver := &Pan115{
			Storage: model.Storage{Status: status},
		}
		state := driver.GetAutoFilmAuthState()
		if state.State != "error" ||
			state.Authenticated ||
			!state.RequiresReauthentication {
			t.Fatalf("unexpected state for %q: %+v", status, state)
		}
	}
}

func TestObserveProviderResponseIgnoresReplacedClient(t *testing.T) {
	t.Parallel()

	driver := &Pan115{}
	currentClient := driver115.New()
	replacedClient := driver115.New()
	driver.client = currentClient

	driver.observeProviderResponse(replacedClient.Client, &resty.Response{
		RawResponse: &http.Response{StatusCode: http.StatusMethodNotAllowed},
	})

	state := driver.GetAutoFilmAuthState()
	if state.State == "risk_controlled" || state.RequiresReauthentication {
		t.Fatalf("replaced client changed current authentication state: %+v", state)
	}
}
