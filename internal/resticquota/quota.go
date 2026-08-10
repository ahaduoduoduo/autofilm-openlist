package resticquota

import (
	"context"
	"errors"
	"io"
	"math"
	"sync"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/db"
	"golang.org/x/time/rate"
)

const (
	bytesPerMiB   = 1024 * 1024
	bytesPerGiB   = 1024 * bytesPerMiB
	flushInterval = 8 * bytesPerMiB
)

var ErrQuotaExceeded = errors.New("restic upload quota reached")

type contextKey struct{}

type Tracker struct {
	manager    *manager
	repository string
	errMu      sync.Mutex
	err        error
}

type RepositoryUsage struct {
	Name               string `json:"name"`
	DayBytes           int64  `json:"day_bytes"`
	DayLimit           int64  `json:"day_limit"`
	MonthBytes         int64  `json:"month_bytes"`
	MonthLimit         int64  `json:"month_limit"`
	RateBytesSec       int64  `json:"rate_bytes_per_second"`
	StoredBytes        int64  `json:"stored_bytes"`
	StoredObjects      int64  `json:"stored_objects"`
	StorageInitialized bool   `json:"storage_initialized"`
	StorageUpdatedAt   string `json:"storage_updated_at,omitempty"`
}

type UsageSnapshot struct {
	Day                string            `json:"day"`
	Month              string            `json:"month"`
	DayBytes           int64             `json:"day_bytes"`
	DayLimit           int64             `json:"day_limit"`
	MonthBytes         int64             `json:"month_bytes"`
	MonthLimit         int64             `json:"month_limit"`
	RateBytesSec       int64             `json:"rate_bytes_per_second"`
	StoredBytes        int64             `json:"stored_bytes"`
	StoredObjects      int64             `json:"stored_objects"`
	StorageInitialized bool              `json:"storage_initialized"`
	Repositories       []RepositoryUsage `json:"repositories"`
}

type manager struct {
	mu              sync.Mutex
	day             string
	month           string
	dayBytes        map[string]int64
	monthBytes      map[string]int64
	dirty           map[string]int64
	dirtyBytes      int64
	reserved        map[string]int64
	globalLimiter   *rate.Limiter
	repositoryRates map[string]*rate.Limiter
}

var singleton = newManager()

func newManager() *manager {
	return &manager{
		dayBytes:        map[string]int64{},
		monthBytes:      map[string]int64{},
		dirty:           map[string]int64{},
		reserved:        map[string]int64{},
		repositoryRates: map[string]*rate.Limiter{},
	}
}

func NewTracker(repository string) *Tracker {
	return &Tracker{manager: singleton, repository: repository}
}

func WithTracker(ctx context.Context, tracker *Tracker) context.Context {
	return context.WithValue(ctx, contextKey{}, tracker)
}

func FromContext(ctx context.Context) *Tracker {
	tracker, _ := ctx.Value(contextKey{}).(*Tracker)
	return tracker
}

func WrapReader(ctx context.Context, reader io.Reader) io.Reader {
	tracker := FromContext(ctx)
	if tracker == nil {
		return reader
	}
	return &trackedReader{ctx: ctx, reader: reader, tracker: tracker}
}

func (t *Tracker) Err() error {
	t.errMu.Lock()
	defer t.errMu.Unlock()
	return t.err
}

func (t *Tracker) setErr(err error) {
	if err == nil {
		return
	}
	t.errMu.Lock()
	if t.err == nil {
		t.err = err
	}
	t.errMu.Unlock()
}

func (t *Tracker) Close() error {
	err := t.manager.flush()
	t.setErr(err)
	return err
}

type trackedReader struct {
	ctx     context.Context
	reader  io.Reader
	tracker *Tracker
}

func (r *trackedReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	allowed, err := r.tracker.manager.reserve(r.tracker.repository, len(p))
	if err != nil {
		if errors.Is(err, ErrQuotaExceeded) {
			var probe [1]byte
			n, probeErr := r.reader.Read(probe[:])
			if n == 0 && errors.Is(probeErr, io.EOF) {
				return 0, io.EOF
			}
		}
		r.tracker.setErr(err)
		return 0, err
	}
	if err = r.tracker.manager.wait(r.ctx, r.tracker.repository, allowed); err != nil {
		r.tracker.manager.finishReservation(r.tracker.repository, int64(allowed), 0)
		r.tracker.setErr(err)
		return 0, err
	}
	n, readErr := r.reader.Read(p[:allowed])
	r.tracker.manager.finishReservation(r.tracker.repository, int64(allowed), int64(n))
	if flushErr := r.tracker.manager.flushIfNeeded(); flushErr != nil {
		r.tracker.setErr(flushErr)
		if n == 0 {
			return 0, flushErr
		}
	}
	return n, readErr
}

func (m *manager) reserve(repository string, requested int) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ensurePeriodLocked(); err != nil {
		return 0, err
	}
	allowed := int64(requested)
	repo := repositoryConfig(repository)
	allowed = minRemaining(allowed, bytesFromGiB(conf.Conf.Restic.DailyUploadGiB), total(m.dayBytes)+total(m.reserved))
	allowed = minRemaining(allowed, bytesFromGiB(conf.Conf.Restic.MonthlyUploadGiB), total(m.monthBytes)+total(m.reserved))
	allowed = minRemaining(allowed, bytesFromGiB(repo.DailyUploadGiB), m.dayBytes[repository]+m.reserved[repository])
	allowed = minRemaining(allowed, bytesFromGiB(repo.MonthlyUploadGiB), m.monthBytes[repository]+m.reserved[repository])
	if allowed <= 0 {
		return 0, ErrQuotaExceeded
	}
	m.reserved[repository] += allowed
	return int(allowed), nil
}

func minRemaining(requested, limit, used int64) int64 {
	if limit <= 0 {
		return requested
	}
	return min(requested, max(0, limit-used))
}

func (m *manager) finishReservation(repository string, reserved, consumed int64) {
	m.mu.Lock()
	m.reserved[repository] -= reserved
	m.dayBytes[repository] += consumed
	m.monthBytes[repository] += consumed
	m.dirty[repository] += consumed
	m.dirtyBytes += consumed
	m.mu.Unlock()
}

func (m *manager) wait(ctx context.Context, repository string, bytes int) error {
	m.mu.Lock()
	if err := m.ensureLimitersLocked(); err != nil {
		m.mu.Unlock()
		return err
	}
	globalLimiter := m.globalLimiter
	repositoryLimiter := m.repositoryRates[repository]
	m.mu.Unlock()
	if err := waitLimiter(ctx, globalLimiter, bytes); err != nil {
		return err
	}
	return waitLimiter(ctx, repositoryLimiter, bytes)
}

func waitLimiter(ctx context.Context, limiter *rate.Limiter, bytes int) error {
	if limiter == nil {
		return nil
	}
	for bytes > 0 {
		block := min(bytes, limiter.Burst())
		if err := limiter.WaitN(ctx, block); err != nil {
			return err
		}
		bytes -= block
	}
	return nil
}

func (m *manager) ensureLimitersLocked() error {
	if m.globalLimiter == nil {
		m.globalLimiter = newLimiter(conf.Conf.Restic.UploadMiBPerSecond)
	}
	for _, repo := range conf.Conf.Restic.Repositories {
		if _, ok := m.repositoryRates[repo.Name]; !ok {
			m.repositoryRates[repo.Name] = newLimiter(repo.UploadMiBPerSecond)
		}
	}
	return nil
}

func newLimiter(mibPerSecond float64) *rate.Limiter {
	bytesPerSecond := bytesFromMiB(mibPerSecond)
	if bytesPerSecond <= 0 {
		return nil
	}
	burst := int(min(bytesPerSecond, bytesPerMiB))
	burst = max(burst, 32*1024)
	return rate.NewLimiter(rate.Limit(bytesPerSecond), burst)
}

func (m *manager) ensurePeriodLocked() error {
	now := time.Now().In(configuredLocation())
	day := now.Format("2006-01-02")
	month := now.Format("2006-01")
	if m.day == day && m.month == month {
		return nil
	}
	if err := m.flushLocked(); err != nil {
		return err
	}
	m.day = day
	m.month = month
	m.dayBytes = map[string]int64{}
	m.monthBytes = map[string]int64{}
	for _, repo := range conf.Conf.Restic.Repositories {
		dayBytes, err := db.SumResticTrafficUsage(repo.Name, day)
		if err != nil {
			return err
		}
		monthBytes, err := db.SumResticTrafficUsage(repo.Name, month+"-")
		if err != nil {
			return err
		}
		m.dayBytes[repo.Name] = dayBytes
		m.monthBytes[repo.Name] = monthBytes
	}
	return nil
}

func (m *manager) flushIfNeeded() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.dirtyBytes < flushInterval {
		return nil
	}
	return m.flushLocked()
}

func (m *manager) flush() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.flushLocked()
}

func (m *manager) flushLocked() error {
	if m.day == "" {
		return nil
	}
	for repository, bytes := range m.dirty {
		if bytes <= 0 {
			continue
		}
		if err := db.AddResticTrafficUsage(repository, m.day, bytes); err != nil {
			return err
		}
		delete(m.dirty, repository)
	}
	m.dirtyBytes = 0
	return nil
}

func Snapshot() (UsageSnapshot, error) {
	m := singleton
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ensurePeriodLocked(); err != nil {
		return UsageSnapshot{}, err
	}
	snapshot := UsageSnapshot{
		Day:                m.day,
		Month:              m.month,
		DayBytes:           total(m.dayBytes),
		DayLimit:           bytesFromGiB(conf.Conf.Restic.DailyUploadGiB),
		MonthBytes:         total(m.monthBytes),
		MonthLimit:         bytesFromGiB(conf.Conf.Restic.MonthlyUploadGiB),
		RateBytesSec:       bytesFromMiB(conf.Conf.Restic.UploadMiBPerSecond),
		StorageInitialized: len(conf.Conf.Restic.Repositories) > 0,
	}
	for _, repo := range conf.Conf.Restic.Repositories {
		storage, err := db.ResticRepositoryStorageUsage(repo.Name)
		if err != nil {
			return UsageSnapshot{}, err
		}
		repositoryUsage := RepositoryUsage{
			Name:               repo.Name,
			DayBytes:           m.dayBytes[repo.Name],
			DayLimit:           bytesFromGiB(repo.DailyUploadGiB),
			MonthBytes:         m.monthBytes[repo.Name],
			MonthLimit:         bytesFromGiB(repo.MonthlyUploadGiB),
			RateBytesSec:       bytesFromMiB(repo.UploadMiBPerSecond),
			StoredBytes:        storage.StoredBytes,
			StoredObjects:      storage.ObjectCount,
			StorageInitialized: storage.Initialized,
		}
		if !storage.LastVerified.IsZero() {
			repositoryUsage.StorageUpdatedAt = storage.LastVerified.Format(time.RFC3339)
		}
		snapshot.Repositories = append(snapshot.Repositories, repositoryUsage)
		snapshot.StoredBytes += storage.StoredBytes
		snapshot.StoredObjects += storage.ObjectCount
		snapshot.StorageInitialized = snapshot.StorageInitialized && storage.Initialized
	}
	return snapshot, nil
}

func repositoryConfig(name string) conf.ResticRepository {
	for _, repo := range conf.Conf.Restic.Repositories {
		if repo.Name == name {
			return repo
		}
	}
	return conf.ResticRepository{Name: name}
}

func configuredLocation() *time.Location {
	location, err := time.LoadLocation(conf.Conf.Restic.Timezone)
	if err != nil {
		return time.Local
	}
	return location
}

func total(values map[string]int64) int64 {
	var result int64
	for _, value := range values {
		result += value
	}
	return result
}

func bytesFromGiB(value float64) int64 {
	return int64(math.Round(value * bytesPerGiB))
}

func bytesFromMiB(value float64) int64 {
	return int64(math.Round(value * bytesPerMiB))
}
