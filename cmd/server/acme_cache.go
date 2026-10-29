package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"time"

	"cloud.google.com/go/storage"
	"golang.org/x/crypto/acme/autocert"
)

const maxACMECacheBytes = 128 << 10

// The dedicated bucket contains certificate private keys and ACME account
// keys. It must remain private, without public report routes or expiry rules.
// GCS supplies durable, atomically replaced objects and at-rest encryption;
// no private data is placed in the Nanos image or instance metadata.
type gcsACMECache struct {
	bucket *storage.BucketHandle
	prefix string
}

func newGCSACMECache(ctx context.Context, bucket, prefix string) (autocert.Cache, func() error, error) {
	client, err := storage.NewClient(ctx, storage.WithJSONReads())
	if err != nil {
		return nil, nil, errors.New("TLS cache identity unavailable")
	}
	return &gcsACMECache{bucket: client.Bucket(bucket), prefix: prefix}, client.Close, nil
}
func (c *gcsACMECache) object(key string) *storage.ObjectHandle {
	hash := sha256.Sum256([]byte(key))
	return c.bucket.Object(c.prefix + hex.EncodeToString(hash[:]))
}
func (c *gcsACMECache) Get(ctx context.Context, key string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	r, err := c.object(key).NewReader(ctx)
	if errors.Is(err, storage.ErrObjectNotExist) {
		return nil, autocert.ErrCacheMiss
	}
	if err != nil {
		return nil, errors.New("TLS cache read failed")
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, maxACMECacheBytes+1))
	if err != nil {
		return nil, errors.New("TLS cache read incomplete")
	}
	if len(data) > maxACMECacheBytes {
		return nil, errors.New("TLS cache entry exceeds size limit")
	}
	return data, nil
}
func (c *gcsACMECache) Put(ctx context.Context, key string, data []byte) error {
	if len(data) > maxACMECacheBytes {
		return errors.New("TLS cache entry exceeds size limit")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	w := c.object(key).NewWriter(ctx)
	w.ContentType = "application/octet-stream"
	w.CacheControl = "private, no-store"
	w.ChunkSize = 0
	if _, err := w.Write(data); err != nil {
		_ = w.CloseWithError(err)
		slog.Error("TLS cache write failed; certificate persistence at risk")
		return errors.New("TLS cache write failed")
	}
	if err := w.Close(); err != nil {
		slog.Error("TLS cache write failed; certificate persistence at risk")
		return errors.New("TLS cache write failed")
	}
	return nil
}
func (c *gcsACMECache) Delete(ctx context.Context, key string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err := c.object(key).Delete(ctx)
	if errors.Is(err, storage.ErrObjectNotExist) {
		return nil
	}
	if err != nil {
		return errors.New("TLS cache delete failed")
	}
	return nil
}
