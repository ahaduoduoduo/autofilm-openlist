package restic

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultDownloadCooldown = 30 * time.Minute

type downloadCircuitState struct {
	blockedUntil time.Time
	probing      bool
}

type downloadAccess struct {
	allowed    bool
	probe      bool
	retryAfter time.Duration
}

type downloadCircuitEvent struct {
	blocked    bool
	recovered  bool
	retryAfter time.Duration
}

// downloadCircuit prevents Restic retries from repeatedly reaching a remote
// provider after its WAF has temporarily blocked download-link requests.
// State is isolated per repository and only affects the Restic REST endpoint.
type downloadCircuit struct {
	mu       sync.Mutex
	cooldown time.Duration
	now      func() time.Time
	states   map[string]downloadCircuitState
}

func newDownloadCircuit(cooldown time.Duration) *downloadCircuit {
	if cooldown <= 0 {
		cooldown = defaultDownloadCooldown
	}
	return &downloadCircuit{
		cooldown: cooldown,
		now:      time.Now,
		states:   make(map[string]downloadCircuitState),
	}
}

func (d *downloadCircuit) begin(repository string) downloadAccess {
	d.mu.Lock()
	defer d.mu.Unlock()

	state, blocked := d.states[repository]
	if !blocked {
		return downloadAccess{allowed: true}
	}
	now := d.now()
	if now.Before(state.blockedUntil) {
		return downloadAccess{retryAfter: state.blockedUntil.Sub(now)}
	}
	if state.probing {
		return downloadAccess{retryAfter: time.Second}
	}
	state.probing = true
	d.states[repository] = state
	return downloadAccess{allowed: true, probe: true}
}

func (d *downloadCircuit) complete(repository string, access downloadAccess, err error) downloadCircuitEvent {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := d.now()
	if isBlockedDownloadError(err) {
		blockedUntil := now.Add(d.cooldown)
		if state, ok := d.states[repository]; ok && state.blockedUntil.After(blockedUntil) {
			blockedUntil = state.blockedUntil
		}
		d.states[repository] = downloadCircuitState{blockedUntil: blockedUntil}
		return downloadCircuitEvent{blocked: true, retryAfter: blockedUntil.Sub(now)}
	}
	if !access.probe {
		return downloadCircuitEvent{}
	}
	state, ok := d.states[repository]
	if !ok {
		return downloadCircuitEvent{}
	}
	if err == nil {
		delete(d.states, repository)
		return downloadCircuitEvent{recovered: true}
	}
	state.probing = false
	state.blockedUntil = now.Add(time.Minute)
	d.states[repository] = state
	return downloadCircuitEvent{retryAfter: time.Minute}
}

func isBlockedDownloadError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	if !strings.Contains(message, "405") {
		return false
	}
	return strings.Contains(message, "errors.aliyun.com") ||
		strings.Contains(message, "request has been blocked") ||
		strings.Contains(message, "访问被阻断") ||
		strings.Contains(message, "<title>405</title>")
}

func writeDownloadUnavailable(c interface {
	Header(string, string)
	String(int, string, ...any)
}, retryAfter time.Duration) {
	seconds := max(1, int64((retryAfter+time.Second-1)/time.Second))
	c.Header("Retry-After", strconv.FormatInt(seconds, 10))
	c.String(http.StatusServiceUnavailable, "remote storage temporarily unavailable")
}
