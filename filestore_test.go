package flue

import (
	"testing"
	"time"
)

func TestFileStorePersistsSessionData(t *testing.T) {
	t.Parallel()

	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data := SessionData{
		Version:   1,
		Messages:  []Message{{Role: MessageRoleUser, Content: "hello"}},
		Metadata:  map[string]any{"case": "file"},
		CreatedAt: time.Now().UTC(),
	}
	if err := store.Save("agent/session", data); err != nil {
		t.Fatal(err)
	}

	loaded, ok := store.Load("agent/session")
	if !ok {
		t.Fatal("expected stored data")
	}
	if len(loaded.Messages) != 1 || loaded.Messages[0].Content != "hello" {
		t.Fatalf("unexpected loaded data: %+v", loaded)
	}
	if err := store.Delete("agent/session"); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Load("agent/session"); ok {
		t.Fatal("expected deleted data to be missing")
	}
}
