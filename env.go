package flue

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

var errPathEscape = errors.New("path escapes sandbox root")

// LocalEnv exposes a real directory as a sandbox. All file paths are confined
// beneath root; attempts to reach outside root fail before touching the OS.
//
// 中文说明：LocalEnv 是“把本地某个目录挂给 Agent”。它会检查路径是否还
// 在 root 内，像 ../secret.txt 这样的逃逸路径会被拒绝。
type LocalEnv struct {
	root     string
	cwd      string
	commands map[string]Command
}

// NewLocalEnv mounts root as the sandbox workspace.
func NewLocalEnv(root string) (*LocalEnv, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, err
	}
	return &LocalEnv{root: abs, cwd: "/", commands: map[string]Command{}}, nil
}

func (e *LocalEnv) CWD() string { return e.cwd }

func (e *LocalEnv) ResolvePath(p string) (string, error) {
	if p == "" {
		p = "."
	}
	workspacePath := filepath.ToSlash(p)
	if !strings.HasPrefix(workspacePath, "/") {
		workspacePath = pathJoin(e.cwd, workspacePath)
	}
	cleanWorkspace := pathClean(workspacePath)
	rel := strings.TrimPrefix(cleanWorkspace, "/")
	host := filepath.Join(e.root, filepath.FromSlash(rel))
	cleanHost, err := filepath.Abs(host)
	if err != nil {
		return "", err
	}
	if cleanHost != e.root && !strings.HasPrefix(cleanHost, e.root+string(os.PathSeparator)) {
		// 中文导读：这里是 LocalEnv 最关键的安全检查。先把路径转成绝对
		// host path，再确认它仍然以 sandbox root 开头。
		return "", fmt.Errorf("%w: %s", errPathEscape, p)
	}
	return cleanHost, nil
}

func (e *LocalEnv) Exec(ctx context.Context, command string, opts ExecOptions) (ShellResult, error) {
	if cmd, args, ok := splitCommand(command); ok {
		if registered, exists := e.commands[cmd]; exists {
			return registered.Execute(ctx, args)
		}
	}
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}
	cwd := e.root
	if opts.CWD != "" {
		resolved, err := e.ResolvePath(opts.CWD)
		if err != nil {
			return ShellResult{}, err
		}
		cwd = resolved
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	for k, v := range opts.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := ShellResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: 0}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			return result, nil
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result.ExitCode = -1
			result.Stderr += fmt.Sprintf("[flue4go] command timed out after %s", opts.Timeout)
			return result, nil
		}
		return result, err
	}
	return result, nil
}

func (e *LocalEnv) ReadFile(ctx context.Context, p string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resolved, err := e.ResolvePath(p)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(resolved)
}

func (e *LocalEnv) WriteFile(ctx context.Context, p string, content []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	resolved, err := e.ResolvePath(p)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return err
	}
	return os.WriteFile(resolved, content, 0o600)
}

func (e *LocalEnv) Stat(ctx context.Context, p string) (FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return FileInfo{}, err
	}
	resolved, err := e.ResolvePath(p)
	if err != nil {
		return FileInfo{}, err
	}
	info, err := os.Lstat(resolved)
	if err != nil {
		return FileInfo{}, err
	}
	return fileInfoFromOS(info, p), nil
}

func (e *LocalEnv) ReadDir(ctx context.Context, p string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resolved, err := e.ResolvePath(p)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

func (e *LocalEnv) Exists(ctx context.Context, p string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	resolved, err := e.ResolvePath(p)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(resolved)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func (e *LocalEnv) Mkdir(ctx context.Context, p string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	resolved, err := e.ResolvePath(p)
	if err != nil {
		return err
	}
	return os.MkdirAll(resolved, 0o755)
}

func (e *LocalEnv) Remove(ctx context.Context, p string, opts RemoveOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	resolved, err := e.ResolvePath(p)
	if err != nil {
		return err
	}
	if opts.Recursive {
		return os.RemoveAll(resolved)
	}
	err = os.Remove(resolved)
	if opts.Force && errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func (e *LocalEnv) Scope(ctx context.Context, opts ScopeOptions) (Env, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	commands := make(map[string]Command, len(e.commands)+len(opts.Commands))
	for k, v := range e.commands {
		commands[k] = v
	}
	for _, command := range opts.Commands {
		if command.Name != "" && command.Execute != nil {
			commands[command.Name] = command
		}
	}
	return &LocalEnv{root: e.root, cwd: e.cwd, commands: commands}, nil
}

func (e *LocalEnv) Cleanup(context.Context) error { return nil }

// MemoryEnv is a deterministic in-process filesystem for fast tests and
// high-scale virtual agents.
//
// 中文说明：MemoryEnv 不碰真实磁盘，适合单元测试、翻译/总结这类不需要
// 项目文件的高并发 Agent。
type MemoryEnv struct {
	mu    sync.RWMutex
	files map[string][]byte
	dirs  map[string]struct{}
	cwd   string
}

// NewMemoryEnv creates an empty virtual sandbox.
func NewMemoryEnv() *MemoryEnv {
	return &MemoryEnv{
		files: map[string][]byte{},
		dirs:  map[string]struct{}{"/": {}},
		cwd:   "/",
	}
}

func (e *MemoryEnv) CWD() string { return e.cwd }

func (e *MemoryEnv) ResolvePath(p string) (string, error) {
	if p == "" || p == "." {
		return e.cwd, nil
	}
	if !strings.HasPrefix(p, "/") {
		p = pathJoin(e.cwd, p)
	}
	clean := pathClean(p)
	if strings.HasPrefix(clean, "/..") || clean == ".." {
		return "", fmt.Errorf("%w: %s", errPathEscape, p)
	}
	return clean, nil
}

func (e *MemoryEnv) Exec(ctx context.Context, command string, opts ExecOptions) (ShellResult, error) {
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}
	if err := ctx.Err(); err != nil {
		return ShellResult{}, err
	}
	return ShellResult{Stderr: "memory sandbox does not execute host commands: " + command, ExitCode: 127}, nil
}

func (e *MemoryEnv) ReadFile(ctx context.Context, p string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resolved, err := e.ResolvePath(p)
	if err != nil {
		return nil, err
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	content, ok := e.files[resolved]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return append([]byte(nil), content...), nil
}

func (e *MemoryEnv) WriteFile(ctx context.Context, p string, content []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	resolved, err := e.ResolvePath(p)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ensureDirLocked(pathDir(resolved))
	e.files[resolved] = append([]byte(nil), content...)
	return nil
}

func (e *MemoryEnv) Stat(ctx context.Context, p string) (FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return FileInfo{}, err
	}
	resolved, err := e.ResolvePath(p)
	if err != nil {
		return FileInfo{}, err
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if content, ok := e.files[resolved]; ok {
		return FileInfo{IsFile: true, Size: int64(len(content)), WorkspacePath: resolved, ModifiedTime: time.Now()}, nil
	}
	if _, ok := e.dirs[resolved]; ok {
		return FileInfo{IsDirectory: true, WorkspacePath: resolved, ModifiedTime: time.Now()}, nil
	}
	return FileInfo{}, fs.ErrNotExist
}

func (e *MemoryEnv) ReadDir(ctx context.Context, p string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resolved, err := e.ResolvePath(p)
	if err != nil {
		return nil, err
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if _, ok := e.dirs[resolved]; !ok {
		return nil, fs.ErrNotExist
	}
	prefix := strings.TrimSuffix(resolved, "/") + "/"
	seen := map[string]struct{}{}
	for dir := range e.dirs {
		if strings.HasPrefix(dir, prefix) {
			rest := strings.TrimPrefix(dir, prefix)
			if rest != "" && !strings.Contains(rest, "/") {
				seen[rest] = struct{}{}
			}
		}
	}
	for file := range e.files {
		if strings.HasPrefix(file, prefix) {
			rest := strings.TrimPrefix(file, prefix)
			if rest != "" {
				seen[strings.Split(rest, "/")[0]] = struct{}{}
			}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func (e *MemoryEnv) Exists(ctx context.Context, p string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	resolved, err := e.ResolvePath(p)
	if err != nil {
		return false, err
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	_, fileOK := e.files[resolved]
	_, dirOK := e.dirs[resolved]
	return fileOK || dirOK, nil
}

func (e *MemoryEnv) Mkdir(ctx context.Context, p string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	resolved, err := e.ResolvePath(p)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ensureDirLocked(resolved)
	return nil
}

func (e *MemoryEnv) Remove(ctx context.Context, p string, opts RemoveOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	resolved, err := e.ResolvePath(p)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.files[resolved]; ok {
		delete(e.files, resolved)
		return nil
	}
	if _, ok := e.dirs[resolved]; ok {
		if opts.Recursive {
			prefix := strings.TrimSuffix(resolved, "/") + "/"
			for file := range e.files {
				if strings.HasPrefix(file, prefix) {
					delete(e.files, file)
				}
			}
			for dir := range e.dirs {
				if dir == resolved || strings.HasPrefix(dir, prefix) {
					delete(e.dirs, dir)
				}
			}
			return nil
		}
		return errors.New("directory removal requires Recursive")
	}
	if opts.Force {
		return nil
	}
	return fs.ErrNotExist
}

func (e *MemoryEnv) Scope(ctx context.Context, _ ScopeOptions) (Env, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return e, nil
}

func (e *MemoryEnv) Cleanup(context.Context) error { return nil }

func (e *MemoryEnv) ensureDirLocked(dir string) {
	if dir == "" {
		dir = "/"
	}
	current := "/"
	e.dirs[current] = struct{}{}
	for _, part := range strings.Split(strings.Trim(dir, "/"), "/") {
		if part == "" {
			continue
		}
		current = pathJoin(current, part)
		e.dirs[current] = struct{}{}
	}
}

func fileInfoFromOS(info os.FileInfo, workspacePath string) FileInfo {
	mode := info.Mode()
	return FileInfo{
		IsFile:        mode.IsRegular(),
		IsDirectory:   mode.IsDir(),
		IsSymlink:     mode&os.ModeSymlink != 0,
		Size:          info.Size(),
		ModifiedTime:  info.ModTime(),
		WorkspacePath: filepath.ToSlash(workspacePath),
	}
}

func splitCommand(command string) (string, []string, bool) {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return "", nil, false
	}
	return fields[0], fields[1:], true
}

func pathClean(p string) string {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(p)))
	if clean == "." {
		return "/"
	}
	if !strings.HasPrefix(clean, "/") {
		clean = "/" + clean
	}
	return clean
}

func pathJoin(parts ...string) string {
	return pathClean(strings.Join(parts, "/"))
}

func pathDir(p string) string {
	dir := filepath.ToSlash(filepath.Dir(filepath.FromSlash(p)))
	return pathClean(dir)
}
