package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestUnchangedUpdateDoesNotReplaceRecord(t *testing.T) {
	dir := t.TempDir()
	db, err := NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	value := map[string]int{"count": 1}
	if err := db.Update(ctx, "record", func(json.RawMessage) (any, error) { return value, nil }); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(filepath.Join(dir, "record.json"))
	if err != nil {
		t.Fatal(err)
	}
	for range 20 {
		if err := db.Update(ctx, "record", func(raw json.RawMessage) (any, error) {
			var unchanged map[string]int
			err := json.Unmarshal(raw, &unchanged)
			return unchanged, err
		}); err != nil {
			t.Fatal(err)
		}
	}
	after, err := os.Stat(filepath.Join(dir, "record.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("unchanged update rewrote record")
	}
	value["count"] = 2
	if err := db.Update(ctx, "record", func(json.RawMessage) (any, error) { return value, nil }); err != nil {
		t.Fatal(err)
	}
	var got map[string]int
	if err := db.Get(ctx, "record", &got); err != nil || got["count"] != 2 {
		t.Fatal(got, err)
	}
}
