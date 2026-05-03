package flue

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

var frontmatterRE = regexp.MustCompile(`(?s)^---\s*\n(.*?)\n---\s*\n(.*)$`)

// DiscoverContext loads AGENTS.md, CLAUDE.md, roles, and skills from env.CWD().
func DiscoverContext(ctx context.Context, env Env) (DiscoveredContext, error) {
	agentsText, err := readAgentsFiles(ctx, env)
	if err != nil {
		return DiscoveredContext{}, err
	}
	skills, err := DiscoverSkills(ctx, env)
	if err != nil {
		return DiscoveredContext{}, err
	}
	roles, err := DiscoverRoles(ctx, env)
	if err != nil {
		return DiscoveredContext{}, err
	}
	return DiscoveredContext{
		SystemPrompt: composeSystemPrompt(agentsText, skills, env),
		Skills:       skills,
		Roles:        roles,
	}, nil
}

// DiscoverSkills reads .agents/skills/<name>/SKILL.md.
func DiscoverSkills(ctx context.Context, env Env) (map[string]Skill, error) {
	base := joinWorkspace(env.CWD(), ".agents/skills")
	exists, err := env.Exists(ctx, base)
	if err != nil || !exists {
		return map[string]Skill{}, err
	}
	entries, err := env.ReadDir(ctx, base)
	if err != nil {
		return nil, err
	}
	out := map[string]Skill{}
	for _, entry := range entries {
		skillPath := joinWorkspace(base, entry, "SKILL.md")
		exists, err := env.Exists(ctx, skillPath)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		content, err := env.ReadFile(ctx, skillPath)
		if err != nil {
			return nil, err
		}
		parsed := parseFrontmatter(string(content), entry)
		out[parsed.Name] = Skill(parsed)
	}
	return out, nil
}

// LoadSkillByPath loads a markdown file relative to .agents/skills.
func LoadSkillByPath(ctx context.Context, env Env, relPath string) (Skill, error) {
	clean := path.Clean(strings.TrimPrefix(relPath, "/"))
	if clean == "." || strings.HasPrefix(clean, "../") {
		return Skill{}, fmt.Errorf("%w: %s", errPathEscape, relPath)
	}
	skillPath := joinWorkspace(env.CWD(), ".agents/skills", clean)
	content, err := env.ReadFile(ctx, skillPath)
	if err != nil {
		return Skill{}, err
	}
	defaultName := strings.TrimSuffix(strings.TrimSuffix(clean, ".markdown"), ".md")
	parsed := parseFrontmatter(string(content), defaultName)
	return Skill(parsed), nil
}

// DiscoverRoles reads roles/*.md.
func DiscoverRoles(ctx context.Context, env Env) (map[string]Role, error) {
	base := joinWorkspace(env.CWD(), "roles")
	exists, err := env.Exists(ctx, base)
	if err != nil || !exists {
		return map[string]Role{}, err
	}
	entries, err := env.ReadDir(ctx, base)
	if err != nil {
		return nil, err
	}
	out := map[string]Role{}
	for _, entry := range entries {
		if !strings.HasSuffix(strings.ToLower(entry), ".md") && !strings.HasSuffix(strings.ToLower(entry), ".markdown") {
			continue
		}
		content, err := env.ReadFile(ctx, joinWorkspace(base, entry))
		if err != nil {
			return nil, err
		}
		name := strings.TrimSuffix(strings.TrimSuffix(entry, ".markdown"), ".md")
		parsed := parseFrontmatter(string(content), name)
		out[name] = Role{
			Name:         name,
			Description:  parsed.Description,
			Instructions: parsed.Instructions,
			Model:        parsed.Frontmatter["model"],
			Frontmatter:  parsed.Frontmatter,
		}
	}
	return out, nil
}

type frontmatterDoc struct {
	Name         string
	Description  string
	Instructions string
	Frontmatter  map[string]string
}

func parseFrontmatter(content, defaultName string) frontmatterDoc {
	doc := frontmatterDoc{Name: defaultName, Instructions: strings.TrimSpace(content), Frontmatter: map[string]string{}}
	match := frontmatterRE.FindStringSubmatch(content)
	if len(match) == 3 {
		doc.Instructions = strings.TrimSpace(match[2])
		for _, line := range strings.Split(match[1], "\n") {
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			key = strings.TrimSpace(key)
			value = strings.Trim(strings.TrimSpace(value), `"'`)
			if key != "" {
				doc.Frontmatter[key] = value
			}
		}
	}
	if name := doc.Frontmatter["name"]; name != "" {
		doc.Name = name
	}
	doc.Description = doc.Frontmatter["description"]
	return doc
}

func readAgentsFiles(ctx context.Context, env Env) (string, error) {
	var parts []string
	for _, filename := range []string{"AGENTS.md", "CLAUDE.md"} {
		p := joinWorkspace(env.CWD(), filename)
		exists, err := env.Exists(ctx, p)
		if err != nil {
			return "", err
		}
		if !exists {
			continue
		}
		content, err := env.ReadFile(ctx, p)
		if err != nil {
			return "", err
		}
		parts = append(parts, strings.TrimSpace(string(content)))
	}
	return strings.Join(parts, "\n\n"), nil
}

func composeSystemPrompt(agentsText string, skills map[string]Skill, env Env) string {
	var parts []string
	if agentsText != "" {
		parts = append(parts, agentsText)
	}
	if len(skills) > 0 {
		names := make([]string, 0, len(skills))
		for name := range skills {
			names = append(names, name)
		}
		sort.Strings(names)
		parts = append(parts, "## Available Skills")
		for _, name := range names {
			skill := skills[name]
			line := "- **" + skill.Name + "**"
			if skill.Description != "" {
				line += " - " + skill.Description
			}
			parts = append(parts, line)
		}
	}
	parts = append(parts, "Date: "+time.Now().Format("Mon, Jan 2, 2006"))
	parts = append(parts, "Working directory: "+env.CWD())
	return strings.Join(parts, "\n\n")
}

func joinWorkspace(parts ...string) string {
	return pathClean(strings.Join(parts, "/"))
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
