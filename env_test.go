package flue

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalEnvRejectsPathEscape(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	root := filepath.Join(parent, "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "secret.txt"), []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}

	env, err := NewLocalEnv(root)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := env.ReadFile(context.Background(), "../secret.txt"); err == nil {
		t.Fatal("expected path escape to be rejected")
	}
	if err := env.WriteFile(context.Background(), "nested/file.txt", []byte("ok")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "nested", "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "ok" {
		t.Fatalf("written content = %q", got)
	}
}
