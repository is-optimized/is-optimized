// Package store provides transactional JSON records and immutable artifacts.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

var ErrNotFound = errors.New("not found")
var validKey = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,180}$`)

func KeyOK(key string) bool { return validKey.MatchString(key) }

type Store interface {
	Get(context.Context, string, any) error
	Update(context.Context, string, func(json.RawMessage) (any, error)) error
	List(context.Context, string) ([]json.RawMessage, error)
	PutPDF(context.Context, string, []byte) error
	PDF(context.Context, string) ([]byte, error)
	Close() error
}

// Local is for single-process development only. Atomic rename prevents partial records.
type Local struct {
	mu  sync.Mutex
	dir string
}

func NewLocal(dir string) (*Local, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &Local{dir: dir}, nil
}
func (l *Local) path(k string) (string, error) {
	if !KeyOK(k) {
		return "", errors.New("invalid key")
	}
	return filepath.Join(l.dir, k), nil
}
func (l *Local) Get(ctx context.Context, k string, out any) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	p, e := l.path(k)
	if e != nil {
		return e
	}
	b, e := os.ReadFile(p + ".json")
	if os.IsNotExist(e) {
		return ErrNotFound
	}
	if e != nil {
		return e
	}
	return json.Unmarshal(b, out)
}
func (l *Local) Update(ctx context.Context, k string, fn func(json.RawMessage) (any, error)) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	p, e := l.path(k)
	if e != nil {
		return e
	}
	b, e := os.ReadFile(p + ".json")
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	v, e := fn(b)
	if e != nil {
		return e
	}
	b, e = json.Marshal(v)
	if e != nil {
		return e
	}
	return atomicWrite(p+".json", b)
}
func atomicWrite(p string, b []byte) error {
	f, e := os.CreateTemp(filepath.Dir(p), ".write-")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if _, e = f.Write(b); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(name, p)
}
func (l *Local) List(ctx context.Context, prefix string) ([]json.RawMessage, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	files, e := filepath.Glob(filepath.Join(l.dir, prefix+"*.json"))
	if e != nil {
		return nil, e
	}
	out := []json.RawMessage{}
	for _, p := range files {
		b, e := os.ReadFile(p)
		if e != nil {
			return nil, e
		}
		out = append(out, b)
	}
	return out, nil
}
func (l *Local) PutPDF(ctx context.Context, k string, b []byte) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	p, e := l.path(k)
	if e != nil {
		return e
	}
	return atomicWrite(p+".pdf", b)
}
func (l *Local) PDF(ctx context.Context, k string) ([]byte, error) {
	p, e := l.path(k)
	if e != nil {
		return nil, e
	}
	b, e := os.ReadFile(p + ".pdf")
	if os.IsNotExist(e) {
		return nil, ErrNotFound
	}
	return b, e
}
func (l *Local) Close() error { return nil }
