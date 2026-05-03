package flue

import (
	"context"
)

// SandboxAPI is the minimal provider contract for remote/container sandboxes.
// Implement it for Daytona, Docker, SSH, Kubernetes, or an internal executor.
//
// 中文说明：远程沙箱供应商只需要实现这组方法，核心 Session 不关心底层
// 是 Docker、SSH、K8s 还是云沙箱。
type SandboxAPI interface {
	Exec(context.Context, string, ExecOptions) (ShellResult, error)
	ReadFile(context.Context, string) ([]byte, error)
	WriteFile(context.Context, string, []byte) error
	Stat(context.Context, string) (FileInfo, error)
	ReadDir(context.Context, string) ([]string, error)
	Exists(context.Context, string) (bool, error)
	Mkdir(context.Context, string) error
	Remove(context.Context, string, RemoveOptions) error
}

// RemoteEnv adapts a SandboxAPI to Env.
type RemoteEnv struct {
	api     SandboxAPI
	cwd     string
	cleanup func(context.Context) error
}

// NewRemoteEnv wraps a remote/container sandbox API.
func NewRemoteEnv(api SandboxAPI, cwd string, cleanup func(context.Context) error) *RemoteEnv {
	if cwd == "" {
		cwd = "/"
	}
	return &RemoteEnv{api: api, cwd: pathClean(cwd), cleanup: cleanup}
}

func (e *RemoteEnv) CWD() string { return e.cwd }

func (e *RemoteEnv) ResolvePath(p string) (string, error) {
	if p == "" {
		return e.cwd, nil
	}
	if p[0] == '/' {
		return pathClean(p), nil
	}
	return pathJoin(e.cwd, p), nil
}

func (e *RemoteEnv) Exec(ctx context.Context, command string, opts ExecOptions) (ShellResult, error) {
	if opts.CWD == "" {
		opts.CWD = e.cwd
	}
	return e.api.Exec(ctx, command, opts)
}

func (e *RemoteEnv) ReadFile(ctx context.Context, p string) ([]byte, error) {
	resolved, err := e.ResolvePath(p)
	if err != nil {
		return nil, err
	}
	return e.api.ReadFile(ctx, resolved)
}

func (e *RemoteEnv) WriteFile(ctx context.Context, p string, content []byte) error {
	resolved, err := e.ResolvePath(p)
	if err != nil {
		return err
	}
	return e.api.WriteFile(ctx, resolved, content)
}

func (e *RemoteEnv) Stat(ctx context.Context, p string) (FileInfo, error) {
	resolved, err := e.ResolvePath(p)
	if err != nil {
		return FileInfo{}, err
	}
	return e.api.Stat(ctx, resolved)
}

func (e *RemoteEnv) ReadDir(ctx context.Context, p string) ([]string, error) {
	resolved, err := e.ResolvePath(p)
	if err != nil {
		return nil, err
	}
	return e.api.ReadDir(ctx, resolved)
}

func (e *RemoteEnv) Exists(ctx context.Context, p string) (bool, error) {
	resolved, err := e.ResolvePath(p)
	if err != nil {
		return false, err
	}
	return e.api.Exists(ctx, resolved)
}

func (e *RemoteEnv) Mkdir(ctx context.Context, p string) error {
	resolved, err := e.ResolvePath(p)
	if err != nil {
		return err
	}
	return e.api.Mkdir(ctx, resolved)
}

func (e *RemoteEnv) Remove(ctx context.Context, p string, opts RemoveOptions) error {
	resolved, err := e.ResolvePath(p)
	if err != nil {
		return err
	}
	return e.api.Remove(ctx, resolved, opts)
}

func (e *RemoteEnv) Scope(ctx context.Context, _ ScopeOptions) (Env, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return e, nil
}

func (e *RemoteEnv) Cleanup(ctx context.Context) error {
	if e.cleanup == nil {
		return nil
	}
	return e.cleanup(ctx)
}
