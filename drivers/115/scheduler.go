package _115

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/resticquota"
	driver115 "github.com/SheltonZhu/115driver/pkg/driver"
	"github.com/go-resty/resty/v2"
	"golang.org/x/time/rate"
)

const (
	defaultRequestRate         = 1.0
	defaultListConcurrency     = 1
	defaultMutationConcurrency = 1
	defaultUploadConcurrency   = 1
)

var accountSchedulers sync.Map

type accountScheduler struct {
	accountKey          string
	requestRate         float64
	listConcurrency     int
	mutationConcurrency int
	uploadConcurrency   int
	limiter             *rate.Limiter
	lists               chan struct{}
	mutations           chan struct{}
	uploads             *weightedUploadGate
	activeLists         atomic.Int64
	activeMutations     atomic.Int64
	activeUploads       atomic.Int64
}

func schedulerAccountKey(addition *Addition, storageID uint) string {
	if addition.Cookie != "" {
		credential := &driver115.Credential{}
		if credential.FromCookie(addition.Cookie) == nil && credential.UID != "" {
			return "uid:" + credential.UID
		}
	}
	return fmt.Sprintf("storage:%d", storageID)
}

func schedulerDisplayKey(accountKey string) string {
	sum := sha256.Sum256([]byte(accountKey))
	return hex.EncodeToString(sum[:6])
}

func getAccountScheduler(accountKey string, addition *Addition) *accountScheduler {
	requestRate := addition.LimitRate
	if requestRate <= 0 {
		requestRate = defaultRequestRate
	}
	listConcurrency := addition.ListConcurrency
	if listConcurrency <= 0 {
		listConcurrency = defaultListConcurrency
	}
	mutationConcurrency := addition.MutationConcurrency
	if mutationConcurrency <= 0 {
		mutationConcurrency = defaultMutationConcurrency
	}
	uploadConcurrency := addition.UploadConcurrency
	if uploadConcurrency <= 0 {
		uploadConcurrency = defaultUploadConcurrency
	}

	value, _ := accountSchedulers.LoadOrStore(accountKey, &accountScheduler{
		accountKey:          accountKey,
		requestRate:         requestRate,
		listConcurrency:     listConcurrency,
		mutationConcurrency: mutationConcurrency,
		uploadConcurrency:   uploadConcurrency,
		limiter:             rate.NewLimiter(rate.Limit(requestRate), 1),
		lists:               make(chan struct{}, listConcurrency),
		mutations:           make(chan struct{}, mutationConcurrency),
		uploads:             newWeightedUploadGate(uploadConcurrency),
	})
	return value.(*accountScheduler)
}

func (s *accountScheduler) installRequestLimiter(client *resty.Client) {
	client.OnBeforeRequest(func(_ *resty.Client, request *resty.Request) error {
		ctx := request.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		return s.limiter.Wait(ctx)
	})
}

func acquire(
	ctx context.Context,
	semaphore chan struct{},
	active *atomic.Int64,
) (func(), error) {
	select {
	case semaphore <- struct{}{}:
		active.Add(1)
		return func() {
			active.Add(-1)
			<-semaphore
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *accountScheduler) acquireList(ctx context.Context) (func(), error) {
	return acquire(ctx, s.lists, &s.activeLists)
}

func (s *accountScheduler) acquireMutation(ctx context.Context) (func(), error) {
	return acquire(ctx, s.mutations, &s.activeMutations)
}

func (s *accountScheduler) acquireUpload(ctx context.Context) (func(), error) {
	taskID := ""
	weight := 1
	if tracker := resticquota.FromContext(ctx); tracker != nil {
		taskID = tracker.TaskID()
		weight = tracker.Weight()
	}
	release, err := s.uploads.acquire(ctx, taskID, weight)
	if err != nil {
		return nil, err
	}
	s.activeUploads.Add(1)
	return func() {
		s.activeUploads.Add(-1)
		release()
	}, nil
}

func (s *accountScheduler) snapshot() driver.AutoFilmSchedulerSnapshot {
	return driver.AutoFilmSchedulerSnapshot{
		AccountKey:          schedulerDisplayKey(s.accountKey),
		RequestRate:         s.requestRate,
		ListConcurrency:     s.listConcurrency,
		MutationConcurrency: s.mutationConcurrency,
		UploadConcurrency:   s.uploadConcurrency,
		ActiveLists:         int(s.activeLists.Load()),
		ActiveMutations:     int(s.activeMutations.Load()),
		ActiveUploads:       int(s.activeUploads.Load()),
	}
}
