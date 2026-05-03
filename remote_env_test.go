package flue

import (
	"context"
	"testing"
)

type fakeSandboxAPI struct {
	readPath string
	files    map[string][]byte
}

func (f *fakeSandboxAPI) Exec(context.Context, string, ExecOptions) (ShellResult, error) {
	return ShellResult{Stdout: "ok"}, nil
}

func (f *fakeSandboxAPI) ReadFile(_ context.Context, path string) ([]byte, error) {
	f.readPath = path
	return f.files[path], nil
}

func (f *fakeSandboxAPI) WriteFile(_ context.Context, path string, content []byte) error {
	f.files[path] = content
	return nil
}

func (f *fakeSandboxAPI) Stat(context.Context, string) (FileInfo, error) {
	return FileInfo{IsFile: true}, nil
}
func (f *fakeSandboxAPI) ReadDir(context.Context, string) ([]string, error)   { return []string{}, nil }
func (f *fakeSandboxAPI) Exists(context.Context, string) (bool, error)        { return true, nil }
func (f *fakeSandboxAPI) Mkdir(context.Context, string) error                 { return nil }
func (f *fakeSandboxAPI) Remove(context.Context, string, RemoveOptions) error { return nil }

func TestRemoteEnvResolvesRelativePathsAgainstCWD(t *testing.T) {
	t.Parallel()

	api := &fakeSandboxAPI{files: map[string][]byte{"/workspace/project/README.md": []byte("hello")}}
	env := NewRemoteEnv(api, "/workspace/project", func(context.Context) error { return nil })
	got, err := env.ReadFile(context.Background(), "README.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("content = %q", got)
	}
	if api.readPath != "/workspace/project/README.md" {
		t.Fatalf("read path = %q", api.readPath)
	}
}
