package _115

import (
	"context"
	"errors"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	driver115 "github.com/SheltonZhu/115driver/pkg/driver"
	"github.com/google/uuid"
)

const authSessionTTL = 5 * time.Minute

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
		d.client.ImportCredential(credential)
		if checkErr := d.client.CookieCheck(); checkErr != nil {
			session.state = "failed"
			session.message = checkErr.Error()
			return session.response(), nil
		}
		d.Cookie = credential.Cookie()
		d.QRCodeToken = ""
		d.GetStorage().SetStatus(op.WORK)
		op.MustSaveDriverStorage(d)
		session.state = "confirmed"
		session.message = "storage authentication restored"
	}
	return session.response(), nil
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

func (d *Pan115) CheckAutoFilmAuth(ctx context.Context) driver.AutoFilmAuthHealth {
	d.authMu.Lock()
	defer d.authMu.Unlock()

	checkedAt := time.Now()
	if err := ctx.Err(); err != nil {
		return driver.AutoFilmAuthHealth{
			Authenticated: false,
			CheckedAt:     checkedAt,
			Message:       err.Error(),
		}
	}
	if err := d.client.CookieCheck(); err != nil {
		return driver.AutoFilmAuthHealth{
			Authenticated: false,
			CheckedAt:     checkedAt,
			Message:       err.Error(),
		}
	}
	return driver.AutoFilmAuthHealth{
		Authenticated: true,
		CheckedAt:     checkedAt,
	}
}

var _ driver.AutoFilmAuthProvider = (*Pan115)(nil)
var _ driver.AutoFilmAuthHealthProvider = (*Pan115)(nil)
var _ driver.AutoFilmSchedulerProvider = (*Pan115)(nil)
