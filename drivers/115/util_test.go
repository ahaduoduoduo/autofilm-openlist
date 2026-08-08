package _115

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	driver115 "github.com/SheltonZhu/115driver/pkg/driver"
)

func TestRetryUploadInitResponseRetriesDecodeError(t *testing.T) {
	t.Parallel()

	attempts := 0
	result, err := retryUploadInitResponse(
		context.Background(),
		3,
		0,
		func() (*driver115.UploadInitResp, error) {
			attempts++
			if attempts == 1 {
				return nil, &uploadInitResponseDecodeError{err: errors.New("malformed response")}
			}
			return &driver115.UploadInitResp{Status: 1}, nil
		},
	)
	if err != nil {
		t.Fatalf("retry upload initialization response: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if result.Status != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestRetryUploadInitResponseStopsOnNonDecodeError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("provider rejected request")
	attempts := 0
	_, err := retryUploadInitResponse(
		context.Background(),
		3,
		0,
		func() (*driver115.UploadInitResp, error) {
			attempts++
			return nil, wantErr
		},
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected provider error, got %v", err)
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", attempts)
	}
}

func TestRetryUploadInitResponseReportsExhaustion(t *testing.T) {
	t.Parallel()

	attempts := 0
	_, err := retryUploadInitResponse(
		context.Background(),
		3,
		0,
		func() (*driver115.UploadInitResp, error) {
			attempts++
			return nil, &uploadInitResponseDecodeError{err: errors.New("slice bounds out of range")}
		},
	)
	if err == nil || !strings.Contains(err.Error(), "failed after 3 attempts") {
		t.Fatalf("expected exhaustion error, got %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestRetryUploadInitResponseHonorsCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	_, err := retryUploadInitResponse(
		ctx,
		3,
		time.Hour,
		func() (*driver115.UploadInitResp, error) {
			attempts++
			cancel()
			return nil, &uploadInitResponseDecodeError{err: errors.New("malformed response")}
		},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", attempts)
	}
}
