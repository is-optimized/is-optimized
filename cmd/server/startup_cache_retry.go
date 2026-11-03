package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"golang.org/x/crypto/acme/autocert"
)

const startupCacheWriteAttempts = 3

// This retry belongs only to the startup persistence probe. The GCS cache's
// existing ten-second Put deadline is unchanged, so three attempts and 1s/2s
// backoffs budget at most 33s for this write phase (excluding scheduling/logging
// overhead). Every attempt reuses the same unique object key and nonce: a timed
// out write may already have succeeded and must not create a second object.
// No read, delete, certificate, authorization or normal cache operation retries
// are added here. Only a typed context deadline while the parent remains alive
// qualifies; arbitrary network errors or matching error text never do.
func putStartupACMEProbe(ctx context.Context, cache autocert.Cache, key string, nonce []byte, wait func(context.Context, time.Duration) error) error {
	for attempt := 1; attempt <= startupCacheWriteAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		start := time.Now()
		slog.Info("startup stage", "stage", "https", "substage", "cache_write_attempt", "status", "starting", "attempt", attempt, "elapsed_ms", int64(0))
		err := cache.Put(ctx, key, nonce)
		if err == nil {
			slog.Info("startup stage", "stage", "https", "substage", "cache_write_attempt", "status", "complete", "attempt", attempt, "elapsed_ms", time.Since(start).Milliseconds())
			return ctx.Err()
		}
		slog.Error("startup stage", "stage", "https", "substage", "cache_write_attempt", "status", "failed", "attempt", attempt, "elapsed_ms", time.Since(start).Milliseconds(), "reason", startupErrorCategory(err))
		if parentErr := ctx.Err(); parentErr != nil {
			return parentErr
		}
		if !errors.Is(err, context.DeadlineExceeded) || attempt == startupCacheWriteAttempts {
			return err
		}
		delay := time.Duration(attempt) * time.Second
		slog.Info("startup stage", "stage", "https", "substage", "cache_write_retry", "status", "waiting", "attempt", attempt+1, "backoff_ms", delay.Milliseconds())
		if err := wait(ctx, delay); err != nil {
			return err
		}
	}
	return errors.New("startup cache probe attempts exhausted")
}

func startupCacheRetryWait(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}
