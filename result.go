package flue

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

const headlessPreamble = "You are running in headless mode with no human operator. Work autonomously; make your best judgment and proceed independently."

var resultBlockRE = regexp.MustCompile(`(?s)---RESULT_START---\s*\n(.*?)---RESULT_END---`)

// BuildPromptText adds the autonomous harness preamble and optional result
// instructions used by PromptInto.
func BuildPromptText(text string, wantResult bool) string {
	var b strings.Builder
	b.WriteString(headlessPreamble)
	b.WriteString("\n\n")
	b.WriteString(text)
	if wantResult {
		b.WriteString("\n\nWhen complete, output only your final result between these exact delimiters:\n")
		b.WriteString("---RESULT_START---\n")
		b.WriteString("{\"key\":\"value\"}\n")
		b.WriteString("---RESULT_END---")
	}
	return b.String()
}

// BuildSkillPrompt renders skill instructions with optional JSON arguments.
func BuildSkillPrompt(skill Skill, args map[string]any, wantResult bool) (string, error) {
	var b strings.Builder
	b.WriteString(headlessPreamble)
	b.WriteString("\n\n")
	b.WriteString(skill.Instructions)
	if len(args) > 0 {
		encoded, err := json.MarshalIndent(args, "", "  ")
		if err != nil {
			return "", err
		}
		b.WriteString("\n\nArguments:\n")
		b.Write(encoded)
	}
	if wantResult {
		b.WriteString("\n\nWhen complete, output only your final result between these exact delimiters:\n")
		b.WriteString("---RESULT_START---\n")
		b.WriteString("{\"key\":\"value\"}\n")
		b.WriteString("---RESULT_END---")
	}
	return b.String(), nil
}

// ExtractResult unmarshals the last delimited result block into out.
func ExtractResult(text string, out any) error {
	if out == nil {
		return errors.New("result target is nil")
	}
	matches := resultBlockRE.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return errors.New("no ---RESULT_START--- / ---RESULT_END--- block found")
	}
	raw := strings.TrimSpace(matches[len(matches)-1][1])
	switch target := out.(type) {
	case *string:
		*target = raw
		return nil
	case *[]byte:
		*target = []byte(raw)
		return nil
	default:
		return json.Unmarshal([]byte(raw), out)
	}
}
