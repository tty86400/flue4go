package flue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
)

const (
	maxReadLines       = 2000
	maxReadBytes       = 50 * 1024
	maxGrepMatches     = 100
	maxGlobResults     = 1000
	maxToolResultBytes = 64 * 1024
)

var builtinToolNames = map[string]struct{}{
	"read": {}, "write": {}, "edit": {}, "bash": {}, "grep": {}, "glob": {}, "task": {}, "handoff": {},
}

// createBuiltinTools builds the default tool belt.
//
// 中文说明：这些工具是上游 Flue 的核心体验：模型能读文件、写文件、
// 精确替换、执行命令、搜索文件、分派子任务。所有工具都通过 Env 执行，
// 因此安全边界仍然由沙箱控制。
func createBuiltinTools(env Env, runTask func(context.Context, string, PromptOptions) (PromptResponse, error), runHandoff func(context.Context, HandoffRequest, PromptOptions) (HandoffResult, error)) []Tool {
	return []Tool{
		readTool(env),
		writeTool(env),
		editTool(env),
		bashTool(env),
		grepTool(env),
		globTool(env),
		taskTool(runTask),
		handoffTool(runHandoff),
	}
}

func readTool(env Env) Tool {
	return Tool{
		Name:        "read",
		Description: "Read a file or list a directory. File output is truncated for safety.",
		Parameters:  map[string]any{"type": "object"},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			p := stringArg(args, "path")
			if p == "" {
				return "", errors.New("path is required")
			}
			if info, err := env.Stat(ctx, p); err == nil && info.IsDirectory {
				entries, err := env.ReadDir(ctx, p)
				if err != nil {
					return "", err
				}
				if len(entries) == 0 {
					return "(empty directory)", nil
				}
				return strings.Join(entries, "\n"), nil
			}
			content, err := env.ReadFile(ctx, p)
			if err != nil {
				return "", err
			}
			return truncateLinesAndBytes(string(content), intArg(args, "offset"), intArg(args, "limit")), nil
		},
	}
}

func writeTool(env Env) Tool {
	return Tool{
		Name:        "write",
		Description: "Write content to a file, creating parent directories inside the sandbox.",
		Parameters:  map[string]any{"type": "object"},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			p := stringArg(args, "path")
			content := stringArg(args, "content")
			if p == "" {
				return "", errors.New("path is required")
			}
			if err := env.WriteFile(ctx, p, []byte(content)); err != nil {
				return "", err
			}
			return fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), p), nil
		},
	}
}

func editTool(env Env) Tool {
	return Tool{
		Name:        "edit",
		Description: "Edit a file using exact text replacement. Non-replaceAll edits must be unique.",
		Parameters:  map[string]any{"type": "object"},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			p := stringArg(args, "path")
			oldText := stringArg(args, "oldText")
			newText := stringArg(args, "newText")
			if p == "" || oldText == "" {
				return "", errors.New("path and oldText are required")
			}
			contentBytes, err := env.ReadFile(ctx, p)
			if err != nil {
				return "", err
			}
			content := string(contentBytes)
			replaceAll := boolArg(args, "replaceAll")
			count := strings.Count(content, oldText)
			if count == 0 {
				return "", fmt.Errorf("could not find exact text in %s", p)
			}
			if !replaceAll && count > 1 {
				return "", fmt.Errorf("found %d occurrences in %s; provide more context or use replaceAll", count, p)
			}
			updated := strings.Replace(content, oldText, newText, map[bool]int{true: -1, false: 1}[replaceAll])
			if err := env.WriteFile(ctx, p, []byte(updated)); err != nil {
				return "", err
			}
			return fmt.Sprintf("Replaced %d occurrence(s) in %s", count, p), nil
		},
	}
}

func bashTool(env Env) Tool {
	return Tool{
		Name:        "bash",
		Description: "Execute a shell command in the sandbox and return stdout/stderr.",
		Parameters:  map[string]any{"type": "object"},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			command := stringArg(args, "command")
			if command == "" {
				return "", errors.New("command is required")
			}
			result, err := env.Exec(ctx, command, ExecOptions{})
			if err != nil {
				return "", err
			}
			encoded, _ := json.Marshal(result)
			return limitString(string(encoded), maxToolResultBytes), nil
		},
	}
}

func grepTool(env Env) Tool {
	return Tool{
		Name:        "grep",
		Description: "Search files in the sandbox by substring pattern.",
		Parameters:  map[string]any{"type": "object"},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			pattern := stringArg(args, "pattern")
			root := stringArg(args, "path")
			if root == "" {
				root = "."
			}
			if pattern == "" {
				return "", errors.New("pattern is required")
			}
			files, err := globFiles(ctx, env, root, "*")
			if err != nil {
				return "", err
			}
			var matches []string
			for _, file := range files {
				content, err := env.ReadFile(ctx, file)
				if err != nil {
					continue
				}
				for i, line := range strings.Split(string(content), "\n") {
					if strings.Contains(line, pattern) {
						matches = append(matches, fmt.Sprintf("%s:%d:%s", file, i+1, limitString(line, 500)))
						if len(matches) >= maxGrepMatches {
							return strings.Join(matches, "\n"), nil
						}
					}
				}
			}
			if len(matches) == 0 {
				return "(no matches)", nil
			}
			return strings.Join(matches, "\n"), nil
		},
	}
}

func globTool(env Env) Tool {
	return Tool{
		Name:        "glob",
		Description: "List files matching a shell-style pattern inside the sandbox.",
		Parameters:  map[string]any{"type": "object"},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			root := stringArg(args, "path")
			if root == "" {
				root = "."
			}
			pattern := stringArg(args, "pattern")
			if pattern == "" {
				pattern = "*"
			}
			files, err := globFiles(ctx, env, root, pattern)
			if err != nil {
				return "", err
			}
			if len(files) == 0 {
				return "(no matches)", nil
			}
			return strings.Join(files, "\n"), nil
		},
	}
}

func taskTool(runTask func(context.Context, string, PromptOptions) (PromptResponse, error)) Tool {
	return Tool{
		Name:        "task",
		Description: "Delegate a focused task to a detached child session.",
		Parameters:  map[string]any{"type": "object"},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			prompt := stringArg(args, "prompt")
			if prompt == "" {
				return "", errors.New("prompt is required")
			}
			resp, err := runTask(ctx, prompt, PromptOptions{Role: stringArg(args, "role")})
			if err != nil {
				return "", err
			}
			return resp.Text, nil
		},
	}
}

func handoffTool(runHandoff func(context.Context, HandoffRequest, PromptOptions) (HandoffResult, error)) Tool {
	return Tool{
		Name:        "handoff",
		Description: "Transfer work to a named agent and return its result.",
		Parameters:  map[string]any{"type": "object"},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			target := stringArg(args, "target")
			prompt := stringArg(args, "prompt")
			if target == "" || prompt == "" {
				return "", errors.New("target and prompt are required")
			}
			result, err := runHandoff(ctx, HandoffRequest{
				ToAgentID: target,
				Prompt:    prompt,
				Summary:   stringArg(args, "summary"),
			}, PromptOptions{Role: stringArg(args, "role")})
			if err != nil {
				return "", err
			}
			return result.Text, nil
		},
	}
}

func validateTools(custom []Tool) error {
	// 中文导读：自定义工具不能覆盖内置工具，也不能重名。这样可以避免
	// provider 或业务代码误把 read/bash 等关键能力替换成不受控实现。
	seen := map[string]struct{}{}
	for _, tool := range custom {
		if tool.Name == "" || tool.Execute == nil {
			return errors.New("tool name and execute are required")
		}
		if _, ok := builtinToolNames[tool.Name]; ok {
			return fmt.Errorf("custom tool %q conflicts with a built-in tool", tool.Name)
		}
		if _, ok := seen[tool.Name]; ok {
			return fmt.Errorf("duplicate custom tool %q", tool.Name)
		}
		seen[tool.Name] = struct{}{}
	}
	return nil
}

func globFiles(ctx context.Context, env Env, root, pattern string) ([]string, error) {
	var out []string
	var walk func(string) error
	walk = func(dir string) error {
		if len(out) >= maxGlobResults {
			return nil
		}
		entries, err := env.ReadDir(ctx, dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			p := joinWorkspace(dir, entry)
			info, err := env.Stat(ctx, p)
			if err != nil {
				continue
			}
			if info.IsDirectory {
				if err := walk(p); err != nil {
					return err
				}
				continue
			}
			ok, _ := path.Match(pattern, entry)
			if ok || pattern == "*" {
				out = append(out, p)
			}
			if len(out) >= maxGlobResults {
				return nil
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

func truncateLinesAndBytes(content string, offset, limit int) string {
	lines := strings.Split(content, "\n")
	start := 0
	if offset > 0 {
		start = offset - 1
	}
	if start >= len(lines) {
		return ""
	}
	end := len(lines)
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	if end-start > maxReadLines {
		end = start + maxReadLines
	}
	return limitString(strings.Join(lines[start:end], "\n"), maxReadBytes)
}

func limitString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n[truncated]"
}

func stringArg(args map[string]any, key string) string {
	value, ok := args[key]
	if !ok || value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}

func intArg(args map[string]any, key string) int {
	value, ok := args[key]
	if !ok {
		return 0
	}
	switch v := value.(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 0
	}
}

func boolArg(args map[string]any, key string) bool {
	value, ok := args[key]
	if !ok {
		return false
	}
	v, _ := value.(bool)
	return v
}
