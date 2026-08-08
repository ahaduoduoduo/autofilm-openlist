package _115

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	driver115 "github.com/SheltonZhu/115driver/pkg/driver"
	"github.com/go-resty/resty/v2"
	"github.com/google/uuid"
)

const (
	authSessionTTL      = 5 * time.Minute
	riskControlStatus   = "115 risk control detected (HTTP 405); QR re-authentication is required"
	riskControlHTTPCode = http.StatusMethodNotAllowed
	missingCredentialStatus = "missing cookie or qrcode account"
)

type authSession struct {
	id        string
	session   *driver115.QRCodeSession
	client    *driver115.Pan115Client
	qrCode    []byte
	state     string
	message   string
	expiresAt time.Time
}

func (s *authSession) response() *driver.AutoFilmAuthSession {
	return &driver.AutoFilmAuthSession{
		ID:        s.id,
		State:     s.state,
		ExpiresAt: s.expiresAt,
		Message:   s.message,
	}
}

func (d *Pan115) StartAutoFilmAuth(ctx context.Context) (*driver.AutoFilmAuthSession, error) {
	d.authMu.Lock()
	defer d.authMu.Unlock()

	now := time.Now()
	for id, session := range d.authSessions {
		if now.After(session.expiresAt) {
			delete(d.authSessions, id)
			continue
		}
		if session.state == "pending" || session.state == "scanned" {
			return session.response(), nil
		}
	}

	client := d.newClient()
	qrSession, err := client.QRCodeStart()
	if err != nil {
		return nil, err
	}
	qrCode, err := qrSession.QRCode()
	if err != nil {
		return nil, err
	}
	session := &authSession{
		id:        uuid.NewString(),
		session:   qrSession,
		client:    client,
		qrCode:    qrCode,
		state:     "pending",
		expiresAt: now.Add(authSessionTTL),
	}
	if d.authSessions == nil {
		d.authSessions = make(map[string]*authSession)
	}
	d.authSessions[session.id] = session
	return session.response(), nil
}

func (d *Pan115) GetAutoFilmAuth(
	ctx context.Context,
	sessionID string,
) (*driver.AutoFilmAuthSession, error) {
	d.authMu.Lock()
	defer d.authMu.Unlock()

	session, ok := d.authSessions[sessionID]
	if !ok {
		return nil, errors.New("authentication session not found")
	}
	if time.Now().After(session.expiresAt) {
		session.state = "expired"
		session.message = "QR code expired"
		return session.response(), nil
	}
	if session.state == "confirmed" || session.state == "expired" || session.state == "canceled" {
		return session.response(), nil
	}

	status, err := session.client.QRCodeStatus(session.session)
	if err != nil {
		session.state = "failed"
		session.message = err.Error()
		return session.response(), nil
	}
	switch {
	case status.IsWaiting():
		session.state = "pending"
	case status.IsScanned():
		session.state = "scanned"
	case status.IsExpired():
		session.state = "expired"
	case status.IsCanceled():
		session.state = "canceled"
	case status.IsAllowed():
		source := d.QRCodeSource
		if source == "" || source == "linux" {
			source = string(driver115.LoginAppWeb)
		}
		credential, loginErr := session.client.QRCodeLoginWithApp(
			session.session,
			driver115.LoginApp(source),
		)
		if loginErr != nil {
			session.state = "failed"
			session.message = loginErr.Error()
			return session.response(), nil
		}
		replacementClient, checkErr := d.newAuthenticatedClient(credential)
		if checkErr != nil {
			session.state = "failed"
			session.message = checkErr.Error()
			return session.response(), nil
		}

		// Replace the whole client only after both login and upload metadata have
		// been validated. Mutating the previous client's cookie jar leaves its
		// UserID/Userkey pair from the pre-scan session in memory, which makes the
		// next upload initialization fail even though reads already work.
		d.Cookie = credential.Cookie()
		d.QRCodeToken = ""
		d.client = replacementClient
		d.clearRiskControl()
		d.GetStorage().SetStatus(op.WORK)
		op.MustSaveDriverStorage(d)
		session.state = "confirmed"
		session.message = "storage authentication restored"
	}
	return session.response(), nil
}

func (d *Pan115) newAuthenticatedClient(
	credential *driver115.Credential,
) (*driver115.Pan115Client, error) {
	client := d.newClient()
	client.ImportCredential(credential)
	if err := client.CookieCheck(); err != nil {
		return nil, fmt.Errorf("validate refreshed 115 credential: %w", err)
	}
	available, err := client.UploadAvailable()
	if err != nil {
		return nil, fmt.Errorf("refresh 115 upload identity: %w", err)
	}
	if !available {
		return nil, errors.New("refreshed 115 credential cannot upload")
	}
	return client, nil
}

func (d *Pan115) GetAutoFilmAuthQRCode(sessionID string) ([]byte, error) {
	d.authMu.Lock()
	defer d.authMu.Unlock()

	session, ok := d.authSessions[sessionID]
	if !ok {
		return nil, errors.New("authentication session not found")
	}
	if time.Now().After(session.expiresAt) {
		return nil, errors.New("authentication session expired")
	}
	return append([]byte(nil), session.qrCode...), nil
}

func (d *Pan115) GetAutoFilmAuthState() driver.AutoFilmAuthState {
	d.healthMu.RLock()
	detectedAt := d.riskDetectedAt
	message := d.riskMessage
	d.healthMu.RUnlock()

	status := d.GetStorage().Status
	if !detectedAt.IsZero() || isRiskControlError(status) {
		if message == "" {
			message = riskControlStatus
		}
		state := driver.AutoFilmAuthState{
			Authenticated:            false,
			State:                    "risk_controlled",
			RequiresReauthentication: true,
			StatusCode:               riskControlHTTPCode,
			Message:                  message,
		}
		if !detectedAt.IsZero() {
			state.DetectedAt = &detectedAt
		}
		return state
	}
	if status == op.WORK {
		return driver.AutoFilmAuthState{
			Authenticated: true,
			State:         "authenticated",
		}
	}
	if strings.EqualFold(strings.TrimSpace(status), missingCredentialStatus) {
		return driver.AutoFilmAuthState{
			Authenticated:            false,
			State:                    "error",
			RequiresReauthentication: true,
			Message:                  status,
		}
	}
	return driver.AutoFilmAuthState{
		Authenticated: false,
		State:         "error",
		Message:       status,
	}
}

func (d *Pan115) observeProviderResponse(
	client *resty.Client,
	response *resty.Response,
) {
	if response == nil {
		return
	}
	if response.StatusCode() >= http.StatusOK &&
		response.StatusCode() < http.StatusMultipleChoices &&
		d.client != nil &&
		client == d.client.Client {
		d.clearRiskControl()
		return
	}
	if response.StatusCode() != riskControlHTTPCode {
		return
	}
	requestURL := ""
	if response.Request != nil && response.Request.RawRequest != nil &&
		response.Request.RawRequest.URL != nil {
		requestURL = response.Request.RawRequest.URL.Host +
			response.Request.RawRequest.URL.Path
	}
	d.markRiskControl(fmt.Sprintf(
		"115 request was blocked by HTTP 405%s; scan the QR code to replace the current credential",
		formatRiskRequestURL(requestURL),
	))
}

func (d *Pan115) observeProviderError(err error) {
	if err == nil || !isRiskControlError(err.Error()) {
		return
	}
	d.markRiskControl(
		"115 request was blocked by HTTP 405; scan the QR code to replace the current credential",
	)
}

func (d *Pan115) markRiskControl(message string) {
	d.healthMu.Lock()
	alreadyMarked := !d.riskDetectedAt.IsZero()
	if !alreadyMarked {
		d.riskDetectedAt = time.Now()
	}
	d.riskMessage = message
	d.healthMu.Unlock()

	if d.GetStorage().Status != riskControlStatus {
		d.GetStorage().SetStatus(riskControlStatus)
		op.MustSaveDriverStorage(d)
	}
}

func (d *Pan115) clearRiskControl() {
	d.healthMu.Lock()
	wasMarked := !d.riskDetectedAt.IsZero() ||
		isRiskControlError(d.GetStorage().Status)
	d.riskDetectedAt = time.Time{}
	d.riskMessage = ""
	d.healthMu.Unlock()

	if wasMarked {
		d.GetStorage().SetStatus(op.WORK)
		op.MustSaveDriverStorage(d)
	}
}

func isRiskControlError(message string) bool {
	normalized := strings.ToLower(message)
	if !strings.Contains(normalized, "405") {
		return false
	}
	return strings.Contains(normalized, "method not allowed") ||
		strings.Contains(normalized, "status code") ||
		strings.Contains(normalized, "statuscode") ||
		strings.Contains(normalized, "aliyun") ||
		strings.Contains(normalized, "阿里云") ||
		strings.Contains(normalized, "risk control") ||
		strings.Contains(normalized, "风控")
}

func formatRiskRequestURL(value string) string {
	if value == "" {
		return ""
	}
	return " at " + value
}

var _ driver.AutoFilmAuthProvider = (*Pan115)(nil)
var _ driver.AutoFilmAuthStateProvider = (*Pan115)(nil)
var _ driver.AutoFilmSchedulerProvider = (*Pan115)(nil)
