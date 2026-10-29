package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Cloud struct {
	db      *firestore.Client
	objects *storage.Client
	bucket  string
}

func NewCloud(ctx context.Context, project, bucket string) (*Cloud, error) {
	db, e := firestore.NewClient(ctx, project)
	if e != nil {
		return nil, e
	}
	s, e := storage.NewClient(ctx)
	if e != nil {
		db.Close()
		return nil, e
	}
	return &Cloud{db, s, bucket}, nil
}
func (c *Cloud) Get(ctx context.Context, k string, v any) error {
	if !KeyOK(k) {
		return errors.New("invalid key")
	}
	d, e := c.db.Collection("records").Doc(k).Get(ctx)
	if status.Code(e) == codes.NotFound {
		return ErrNotFound
	}
	if e != nil {
		return e
	}
	s, ok := d.Data()["json"].(string)
	if !ok {
		return errors.New("corrupt record")
	}
	return json.Unmarshal([]byte(s), v)
}
func (c *Cloud) Update(ctx context.Context, k string, fn func(json.RawMessage) (any, error)) error {
	if !KeyOK(k) {
		return errors.New("invalid key")
	}
	ref := c.db.Collection("records").Doc(k)
	return c.db.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		d, e := tx.Get(ref)
		var raw json.RawMessage
		if e == nil {
			s, ok := d.Data()["json"].(string)
			if !ok {
				return errors.New("corrupt record")
			}
			raw = []byte(s)
		} else if status.Code(e) != codes.NotFound {
			return e
		}
		v, e := fn(raw)
		if e != nil {
			return e
		}
		b, e := json.Marshal(v)
		if e != nil {
			return e
		}
		// A transaction with no writes still validates the snapshot without
		// sending a document write or changing its timestamp on idle polls.
		if bytes.Equal(raw, b) {
			return nil
		}
		if len(b) > 850000 {
			return errors.New("record too large")
		}
		kind := strings.SplitN(k, "_", 2)[0]
		var fields map[string]any
		if e := json.Unmarshal(b, &fields); e != nil {
			return e
		}
		return tx.Set(ref, map[string]any{"json": string(b), "kind": kind, "state": fields["State"], "updated": time.Now().UTC()})
	})
}
func (c *Cloud) List(ctx context.Context, prefix string) ([]json.RawMessage, error) {
	kind := strings.TrimSuffix(prefix, "_")
	query := c.db.Collection("records").Where("kind", "==", kind)
	if kind == "request" {
		query = query.Where("state", "in", []string{"pending", "sending"})
	}
	it := query.Limit(100).Documents(ctx)
	defer it.Stop()
	out := []json.RawMessage{}
	for {
		d, e := it.Next()
		if e == iterator.Done {
			break
		}
		if e != nil {
			return nil, e
		}
		s, ok := d.Data()["json"].(string)
		if !ok {
			return nil, errors.New("corrupt record")
		}
		out = append(out, json.RawMessage(s))
	}
	return out, nil
}
func (c *Cloud) PutPDF(ctx context.Context, k string, b []byte) error {
	if !KeyOK(k) {
		return errors.New("invalid key")
	}
	w := c.objects.Bucket(c.bucket).Object(k + ".pdf").NewWriter(ctx)
	w.ContentType = "application/pdf"
	if _, e := w.Write(b); e != nil {
		w.Close()
		return e
	}
	return w.Close()
}
func (c *Cloud) PDF(ctx context.Context, k string) ([]byte, error) {
	if !KeyOK(k) {
		return nil, errors.New("invalid key")
	}
	r, e := c.objects.Bucket(c.bucket).Object(k + ".pdf").NewReader(ctx)
	if errors.Is(e, storage.ErrObjectNotExist) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	defer r.Close()
	b, e := io.ReadAll(io.LimitReader(r, 16<<20))
	if len(b) >= 16<<20 {
		return nil, errors.New("artifact too large")
	}
	return b, e
}
func (c *Cloud) Close() error { return errors.Join(c.db.Close(), c.objects.Close()) }
