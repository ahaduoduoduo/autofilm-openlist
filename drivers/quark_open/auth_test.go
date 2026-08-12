package quark_open

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/go-resty/resty/v2"
)

func TestMain(m *testing.M) {
	base.RestyClient = resty.New()
	os.Exit(m.Run())
}

func TestRefreshTokenDirectReturnsAllCredentials(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}

		var body struct {
			AuthType     int    `json:"authType"`
			RefreshToken string `json:"refreshToken"`
			TrimAppID    string `json:"trimAppId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.AuthType != 4 || body.RefreshToken != "old-refresh" || body.TrimAppID != "com.trim.cloudstorage" {
			t.Errorf("unexpected request body: %+v", body)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 0,
			"data": {
				"tokenInfo": {
					"accessToken": "new-access",
					"refreshToken": "new-refresh",
					"appId": "quark-app",
					"signKey": "quark-sign-key"
				}
			}
		}`))
	}))
	defer server.Close()

	driver := &QuarkOpen{Addition: Addition{RefreshToken: "old-refresh"}}
	credentials, err := driver.refreshTokenDirect(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("refreshTokenDirect() error = %v", err)
	}

	want := oauthCredentials{
		RefreshToken: "new-refresh",
		AccessToken:  "new-access",
		AppID:        "quark-app",
		SignKey:      "quark-sign-key",
	}
	if credentials != want {
		t.Fatalf("credentials = %+v, want %+v", credentials, want)
	}
}

func TestRefreshTokenDirectReturnsProviderMessageOnHTTPError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"msg":"refresh token rejected"}`))
	}))
	defer server.Close()

	driver := &QuarkOpen{Addition: Addition{RefreshToken: "secret-refresh"}}
	_, err := driver.refreshTokenDirect(context.Background(), server.URL)
	if err == nil {
		t.Fatal("refreshTokenDirect() error = nil, want HTTP error")
	}
	if got, want := err.Error(), "token refresh request failed with HTTP 401: refresh token rejected"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func TestRefreshTokenOncePreservesConfiguredSigningCredentials(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("refresh_ui"); got != "old-refresh" {
			t.Errorf("refresh_ui = %q, want old-refresh", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"refresh_token":"new-refresh","access_token":"new-access"}`))
	}))
	defer server.Close()

	driver := &QuarkOpen{Addition: Addition{
		UseOnlineAPI: true,
		APIAddress:   server.URL,
		RefreshToken: "old-refresh",
		AppID:        "configured-app",
		SignKey:      "configured-sign-key",
	}}
	credentials, err := driver.refreshTokenOnce(context.Background())
	if err != nil {
		t.Fatalf("refreshTokenOnce() error = %v", err)
	}

	want := oauthCredentials{
		RefreshToken: "new-refresh",
		AccessToken:  "new-access",
		AppID:        "configured-app",
		SignKey:      "configured-sign-key",
	}
	if credentials != want {
		t.Fatalf("credentials = %+v, want %+v", credentials, want)
	}
}

func TestValidateOAuthCredentialsRequiresSigningCredentials(t *testing.T) {
	t.Parallel()

	_, err := validateOAuthCredentials(oauthCredentials{
		RefreshToken: "refresh",
		AccessToken:  "access",
	}, true)
	if err == nil {
		t.Fatal("validateOAuthCredentials() error = nil, want missing signing credentials error")
	}
}
