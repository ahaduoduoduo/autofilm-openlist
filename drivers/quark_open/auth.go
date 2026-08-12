package quark_open

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	log "github.com/sirupsen/logrus"
)

const quarkOpenDirectRefreshURL = "https://oauth.fnnas.com/api/v1/oauth/refreshToken"

type oauthCredentials struct {
	RefreshToken string
	AccessToken  string
	AppID        string
	SignKey      string
}

type directRefreshResp struct {
	Message string `json:"message"`
	Msg     string `json:"msg"`
	Data    struct {
		TokenInfo struct {
			RefreshToken string `json:"refreshToken"`
			AccessToken  string `json:"accessToken"`
			AppID        string `json:"appId"`
			SignKey      string `json:"signKey"`
		} `json:"tokenInfo"`
	} `json:"data"`
}

func (d *QuarkOpen) needsInitialRefresh() bool {
	return strings.TrimSpace(d.AccessToken) == "" ||
		strings.TrimSpace(d.AppID) == "" ||
		strings.TrimSpace(d.SignKey) == ""
}

func (d *QuarkOpen) refreshToken(ctx context.Context) error {
	d.authMu.Lock()
	defer d.authMu.Unlock()

	var (
		credentials oauthCredentials
		err         error
	)
	for attempt := 1; attempt <= 3; attempt++ {
		credentials, err = d.refreshTokenOnce(ctx)
		if err == nil {
			break
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		log.Warnf("[quark_open] token refresh attempt %d failed: %s", attempt, err)
	}
	if err != nil {
		return err
	}

	d.RefreshToken = credentials.RefreshToken
	d.AccessToken = credentials.AccessToken
	d.AppID = credentials.AppID
	d.SignKey = credentials.SignKey
	op.MustSaveDriverStorage(d)
	log.Infof("[quark_open] OAuth credentials refreshed")
	return nil
}

func (d *QuarkOpen) refreshTokenOnce(ctx context.Context) (oauthCredentials, error) {
	missingRequestCredentials := strings.TrimSpace(d.AppID) == "" || strings.TrimSpace(d.SignKey) == ""
	if missingRequestCredentials {
		credentials, err := d.refreshTokenDirect(ctx, quarkOpenDirectRefreshURL)
		if err != nil {
			return oauthCredentials{}, err
		}
		return validateOAuthCredentials(credentials, true)
	}

	if !d.UseOnlineAPI || strings.TrimSpace(d.APIAddress) == "" {
		return oauthCredentials{}, errors.New("local refresh token logic is not implemented; enable the online API or clear AppID and SignKey to recover them from the refresh token")
	}

	credentials, err := d.refreshTokenOnline(ctx, d.APIAddress)
	if err != nil {
		return oauthCredentials{}, err
	}
	if credentials.AppID == "" {
		credentials.AppID = d.AppID
	}
	if credentials.SignKey == "" {
		credentials.SignKey = d.SignKey
	}
	return validateOAuthCredentials(credentials, true)
}

func (d *QuarkOpen) refreshTokenDirect(ctx context.Context, url string) (oauthCredentials, error) {
	var payload directRefreshResp
	res, err := base.RestyClient.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(map[string]any{
			"authType":     4,
			"refreshToken": d.RefreshToken,
			"trimAppId":    "com.trim.cloudstorage",
		}).
		SetResult(&payload).
		SetError(&payload).
		Post(url)
	if err != nil {
		return oauthCredentials{}, err
	}
	if res.IsError() {
		return oauthCredentials{}, refreshResponseError(res.StatusCode(), payload.Message, payload.Msg)
	}

	return oauthCredentials{
		RefreshToken: strings.TrimSpace(payload.Data.TokenInfo.RefreshToken),
		AccessToken:  strings.TrimSpace(payload.Data.TokenInfo.AccessToken),
		AppID:        strings.TrimSpace(payload.Data.TokenInfo.AppID),
		SignKey:      strings.TrimSpace(payload.Data.TokenInfo.SignKey),
	}, nil
}

func (d *QuarkOpen) refreshTokenOnline(ctx context.Context, url string) (oauthCredentials, error) {
	var payload RefreshTokenOnlineAPIResp
	res, err := base.RestyClient.R().
		SetContext(ctx).
		SetResult(&payload).
		SetError(&payload).
		SetQueryParams(map[string]string{
			"refresh_ui": d.RefreshToken,
			"server_use": "true",
			"driver_txt": "quarkyun_oa",
		}).
		Get(url)
	if err != nil {
		return oauthCredentials{}, err
	}
	if res.IsError() {
		return oauthCredentials{}, refreshResponseError(res.StatusCode(), payload.ErrorMessage)
	}
	if payload.ErrorMessage != "" && (payload.RefreshToken == "" || payload.AccessToken == "") {
		return oauthCredentials{}, fmt.Errorf("failed to refresh token: %s", payload.ErrorMessage)
	}

	return oauthCredentials{
		RefreshToken: strings.TrimSpace(payload.RefreshToken),
		AccessToken:  strings.TrimSpace(payload.AccessToken),
		AppID:        strings.TrimSpace(payload.AppID),
		SignKey:      strings.TrimSpace(payload.SignKey),
	}, nil
}

func validateOAuthCredentials(credentials oauthCredentials, requireRequestCredentials bool) (oauthCredentials, error) {
	if credentials.RefreshToken == "" || credentials.AccessToken == "" {
		return oauthCredentials{}, errors.New("token refresh response did not contain access_token and refresh_token")
	}
	if requireRequestCredentials && (credentials.AppID == "" || credentials.SignKey == "") {
		return oauthCredentials{}, errors.New("token refresh response did not contain app_id and sign_key")
	}
	return credentials, nil
}

func refreshResponseError(status int, messages ...string) error {
	for _, message := range messages {
		if message = strings.TrimSpace(message); message != "" {
			return fmt.Errorf("token refresh request failed with HTTP %d: %s", status, message)
		}
	}
	return fmt.Errorf("token refresh request failed with HTTP %d", status)
}

func (d *QuarkOpen) generateAuthCookie() string {
	return fmt.Sprintf("x_pan_client_id=%s; x_pan_access_token=%s", d.AppID, d.AccessToken)
}
