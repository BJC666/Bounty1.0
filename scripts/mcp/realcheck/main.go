// Command realcheck 连接 3 个真实 MCP server（stdio x2 + SSE x1），
// 列出工具并各调用一个工具，输出可复现的证据转写（P7-4a 验收）。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"bounty/internal/mcp"
	"bounty/internal/tool"
)

type call struct {
	Tool string          `json:"tool"`
	Args json.RawMessage `json:"args"`
	Note string          `json:"note,omitempty"`
}

type config struct {
	Servers []mcp.Spec `json:"servers"`
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: realcheck <config.json> <out.txt>")
		os.Exit(2)
	}
	cfgPath, outPath := os.Args[1], os.Args[2]
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read config:", err)
		os.Exit(1)
	}
	var cfg config
	if err := json.Unmarshal(data, &cfg); err != nil {
		fmt.Fprintln(os.Stderr, "parse config:", err)
		os.Exit(1)
	}

	var out []string
	log := func(format string, a ...any) {
		line := fmt.Sprintf(format, a...)
		out = append(out, line)
		fmt.Println(line)
	}

	log("== P7-4a realcheck %s ==", time.Now().Format("2006-01-02 15:04:05"))
	host := mcp.NewHost()
	defer host.Close()

	reg := tool.NewRegistry()
	for _, spec := range cfg.Servers {
		start := time.Now()
		if err := host.Connect(spec); err != nil {
			log("FAIL connect %s: %v", spec.Name, err)
			os.Exit(1)
		}
		log("OK  connect %-12s kind=%s took=%s", spec.Name, kindOf(spec), time.Since(start).Round(time.Millisecond))
	}
	host.RegisterTools(reg)

	all := reg.All()
	names := make([]string, 0, len(all))
	for _, t := range all {
		names = append(names, t.Name())
	}
	sort.Strings(names)
	log("-- tools(%d): %v", len(names), names)

	for _, spec := range cfg.Servers {
		log("-- calls on %s", spec.Name)
		for _, c := range callsFor(spec.Name) {
			t, ok := reg.Get(c.Tool)
			if !ok {
				log("FAIL %s not found", c.Tool)
				os.Exit(1)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			res, err := t.Execute(ctx, c.Args)
			cancel()
			if err != nil {
				log("FAIL %s: %v", c.Tool, err)
				os.Exit(1)
			}
			note := c.Note
			if note != "" {
				note = " // " + note
			}
			log("OK   %s -> %s%s", c.Tool, trim(res, 200), note)
		}
	}
	log("== ALL PASS ==")

	if err := os.WriteFile(outPath, []byte(joinLines(out)), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write out:", err)
		os.Exit(1)
	}
	fmt.Println("transcript saved:", outPath)
}

func kindOf(s mcp.Spec) string {
	if s.Command != "" {
		return "stdio"
	}
	return "sse"
}

func callsFor(server string) []call {
	switch server {
	case "filesystem":
		return []call{
			{Tool: "mcp__filesystem__list_directory", Args: json.RawMessage(`{"path":"C:\\bounty-eval\\work"}`), Note: "官方 filesystem reference server"},
		}
	case "memory":
		return []call{
			{Tool: "mcp__memory__create_entities", Args: json.RawMessage(`{"entities":[{"name":"bounty","entityType":"agent","observations":["p7-4a real server check"]}]}`), Note: "官方 memory reference server"},
			{Tool: "mcp__memory__read_graph", Args: json.RawMessage(`{}`)},
		}
	case "math-sse":
		return []call{
			{Tool: "mcp__math-sse__add", Args: json.RawMessage(`{"a":21,"b":21}`), Note: "官方 Python SDK FastMCP SSE server"},
			{Tool: "mcp__math-sse__calc_fib", Args: json.RawMessage(`{"n":10}`)},
		}
	}
	return nil
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(" + fmt.Sprint(len(s)) + " chars)"
}

func joinLines(lines []string) string {
	return strings.Join(lines, "\n") + "\n"
}
