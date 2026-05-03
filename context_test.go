package flue

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverContextLoadsAgentsSkillsAndRoles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "AGENTS.md"), "# Agent Rules\nUse concise answers.")
	mustWrite(t, filepath.Join(root, ".agents", "skills", "triage", "SKILL.md"), "---\nname: triage\ndescription: issue triage\n---\nRead the issue and classify it.")
	mustWrite(t, filepath.Join(root, ".agents", "skills", "pack", "reproduce.md"), "---\nname: repro\ndescription: reproduce bug\n---\nRun the repro steps.")
	mustWrite(t, filepath.Join(root, "roles", "reviewer.md"), "---\ndescription: code reviewer\nmodel: openai/gpt-5.5\n---\nFind correctness risks.")

	env, err := NewLocalEnv(root)
	if err != nil {
		t.Fatal(err)
	}

	ctx, err := DiscoverContext(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}

	if ctx.SystemPrompt == "" || !contains(ctx.SystemPrompt, "Use concise answers.") {
		t.Fatalf("system prompt did not include AGENTS.md: %q", ctx.SystemPrompt)
	}
	if got := ctx.Skills["triage"].Description; got != "issue triage" {
		t.Fatalf("skill description = %q", got)
	}
	loaded, err := LoadSkillByPath(context.Background(), env, "pack/reproduce.md")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name != "repro" {
		t.Fatalf("path skill name = %q", loaded.Name)
	}
	if got := ctx.Roles["reviewer"].Model; got != "openai/gpt-5.5" {
		t.Fatalf("role model = %q", got)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
