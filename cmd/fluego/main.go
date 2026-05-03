package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	flue "github.com/xwlv/flue4go"
)

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "init":
		runInit(os.Args[2:])
	case "inspect":
		runInspect(os.Args[2:])
	case "list":
		runList(os.Args[2:])
	case "run":
		runRun(os.Args[2:])
	case "build":
		runBuild(os.Args[2:])
	case "serve-example":
		runServeExample(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage:
  fluego init --workspace <path>
  fluego inspect --workspace <path>
  fluego list --url http://localhost:3000
  fluego run <agent> --id <id> --url http://localhost:3000 --payload '{}'
  fluego build --package ./cmd/my-agent --output ./dist/my-agent
  fluego serve-example --addr :3000

Commands:
  init           Create a Go-friendly Flue workspace layout.
  inspect        Discover AGENTS.md, roles, and skills, then print JSON metadata.
  list           Fetch /agents from a running Flue4Go server.
  run            Invoke a running HTTP agent.
  build          Build a Go agent binary with go build.
  serve-example  Start a tiny HTTP registry example for smoke testing.`)
}

func runInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	workspace := fs.String("workspace", ".", "workspace path")
	_ = fs.Parse(args)

	root, err := filepath.Abs(*workspace)
	must(err)
	must(os.MkdirAll(filepath.Join(root, ".agents", "skills", "example"), 0o755))
	must(os.MkdirAll(filepath.Join(root, "roles"), 0o755))
	writeIfMissing(filepath.Join(root, "AGENTS.md"), "# Agent Instructions\n\n- Work autonomously.\n- Keep changes small and verified.\n")
	writeIfMissing(filepath.Join(root, ".agents", "skills", "example", "SKILL.md"), "---\nname: example\ndescription: example skill\n---\nSummarize the task and return the smallest useful next action.\n")
	writeIfMissing(filepath.Join(root, "roles", "reviewer.md"), "---\ndescription: careful code reviewer\n---\nPrioritize correctness, security, and maintainability risks.\n")
	fmt.Println("initialized", root)
}

func runInspect(args []string) {
	fs := flag.NewFlagSet("inspect", flag.ExitOnError)
	workspace := fs.String("workspace", ".", "workspace path")
	_ = fs.Parse(args)

	env, err := flue.NewLocalEnv(*workspace)
	must(err)
	ctx, err := flue.DiscoverContext(context.Background(), env)
	must(err)
	skills := make([]string, 0, len(ctx.Skills))
	for name := range ctx.Skills {
		skills = append(skills, name)
	}
	roles := make([]string, 0, len(ctx.Roles))
	for name := range ctx.Roles {
		roles = append(roles, name)
	}
	encoded, err := json.MarshalIndent(map[string]any{
		"workspace":         env.CWD(),
		"has_system_prompt": ctx.SystemPrompt != "",
		"skills":            skills,
		"roles":             roles,
	}, "", "  ")
	must(err)
	fmt.Println(string(encoded))
}

func runList(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	baseURL := fs.String("url", "http://localhost:3000", "server base URL")
	_ = fs.Parse(args)

	resp, err := http.Get(strings.TrimRight(*baseURL, "/") + "/agents")
	must(err)
	defer resp.Body.Close()
	copyResponse(resp)
}

func runRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	id := fs.String("id", "default", "agent session/request id")
	baseURL := fs.String("url", "http://localhost:3000", "server base URL")
	payload := fs.String("payload", "{}", "JSON payload")
	webhook := fs.Bool("webhook", false, "send X-Webhook: true")
	sse := fs.Bool("sse", false, "request text/event-stream")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		log.Fatal("run requires exactly one agent name")
	}
	agent := fs.Arg(0)
	endpoint := fmt.Sprintf("%s/agents/%s/%s", strings.TrimRight(*baseURL, "/"), agent, *id)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader([]byte(*payload)))
	must(err)
	req.Header.Set("Content-Type", "application/json")
	if *webhook {
		req.Header.Set("X-Webhook", "true")
	}
	if *sse {
		req.Header.Set("Accept", "text/event-stream")
	}
	resp, err := http.DefaultClient.Do(req)
	must(err)
	defer resp.Body.Close()
	copyResponse(resp)
}

func runBuild(args []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	pkg := fs.String("package", ".", "Go package to build")
	output := fs.String("output", filepath.Join("dist", binaryName("agent")), "output binary")
	_ = fs.Parse(args)
	must(os.MkdirAll(filepath.Dir(*output), 0o755))
	cmd := exec.Command("go", "build", "-o", *output, *pkg)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	must(cmd.Run())
	fmt.Println("built", *output)
}

func runServeExample(args []string) {
	fs := flag.NewFlagSet("serve-example", flag.ExitOnError)
	addr := fs.String("addr", ":3000", "listen address")
	_ = fs.Parse(args)

	registry := flue.NewRegistry()
	registry.Handle("echo", flue.Triggers{Webhook: true}, func(ctx context.Context, req flue.RequestContext) (any, error) {
		return map[string]any{
			"id":      req.ID,
			"payload": req.Payload,
		}, nil
	})
	log.Printf("fluego example listening on %s", *addr)
	must(http.ListenAndServe(*addr, flue.NewHTTPServer(registry, flue.HTTPServerOptions{})))
}

func writeIfMissing(path, content string) {
	if _, err := os.Stat(path); err == nil {
		return
	}
	must(os.WriteFile(path, []byte(content), 0o600))
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func copyResponse(resp *http.Response) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("HTTP %d", resp.StatusCode)
	}
	_, _ = io.Copy(os.Stdout, resp.Body)
}

func binaryName(base string) string {
	if filepath.Ext(base) != "" {
		return base
	}
	if strings.EqualFold(os.Getenv("GOOS"), "windows") || filepath.Separator == '\\' {
		return base + ".exe"
	}
	return base
}
