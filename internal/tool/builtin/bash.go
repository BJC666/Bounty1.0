package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"bounty/internal/sandbox"
	"bounty/internal/tool"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

type BashTool struct {
	Timeout      time.Duration
	Sandbox      func(*exec.Cmd) *exec.Cmd
	DockerRunner func(ctx context.Context, command string) (string, error)
	// PolicyCheck pre-validates the command (path policy / network policy).
	PolicyCheck func(command string) error
	// SandboxStart starts the command inside a Job Object container when
	// configured; nil falls back to a plain start.
	SandboxStart func(cmd *exec.Cmd) (*sandbox.Container, error)
	// Background enables background jobs (run_in_background or requested
	// timeout > 60s); nil falls back to synchronous execution.
	Background *BackgroundStore
}

func (b *BashTool) Name() string      { return "bash" }
func (b *BashTool) ReadOnly() bool    { return false }
func (b *BashTool) Owner() tool.Owner { return tool.Owner{Kind: "core", ID: "builtin"} }
func (b *BashTool) Description() string {
	return "Execute a shell command (tests, build, git, file ops). timeout>60000ms or run_in_background=true starts detached; poll with bash_output, stop with bash_kill."
}
func (b *BashTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","maxLength":4000,"description":"The shell command to execute"},"description":{"type":"string","maxLength":200},"timeout":{"type":"number","minimum":1,"maximum":600000,"description":"Timeout ms (max 600000; >60000 = background)"},"run_in_background":{"type":"boolean","description":"Run detached and return a job id immediately; poll with bash_output"}},"required":["command","description"],"additionalProperties":false}`)
}
func (b *BashTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Command         string  `json:"command"`
		Description     string  `json:"description"`
		Timeout         float64 `json:"timeout"`
		RunInBackground bool    `json:"run_in_background"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
	timeout := b.Timeout
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	if params.Timeout > 0 {
		timeout = time.Duration(params.Timeout) * time.Millisecond
		// Clamp to [1s, 600s] regardless of what the model supplies.
		if timeout < time.Second {
			timeout = time.Second
		}
		if timeout > 600*time.Second {
			timeout = 600 * time.Second
		}
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// P3-2: sandbox policy pre-check (network/path) before anything runs.
	if b.PolicyCheck != nil {
		if err := b.PolicyCheck(params.Command); err != nil {
			return "", err
		}
	}

	// If Docker runner is configured, use it for isolated execution.
	if b.DockerRunner != nil {
		output, err := b.DockerRunner(execCtx, params.Command)
		if err != nil {
			return output, &ExecError{Command: params.Command, Output: output, Err: err}
		}
		return output, nil
	}

	shell, shellFlag := "sh", "-c"
	if _, err := exec.LookPath("sh"); err != nil {
		shell, shellFlag = "cmd", "/c" // Windows fallback
	}
	command := params.Command
	if shell == "sh" {
		command = prepareCommand(command)
	}
	// P2-4: under cmd.exe, fail fast with a Windows-equivalent suggestion
	// when the command is a known Unix-only tool (ls/pwd/cat/...).
	if shell == "cmd" {
		if hint, blocked := precheckWindowsCommand(command); blocked {
			return "", fmt.Errorf("cmd.exe 不识别 %s，请改用 Windows 等价命令：%s（命令未执行，可直接重试）", firstCommandToken(command), hint)
		}
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" && shell == "cmd" {
		// Windows 上 os/exec 会把含引号的参数转义成 \" 形式，而 cmd.exe
		// 不识别 \" 转义，导致带引号命令（如 python -c "..."）被截断。
		// 用 SysProcAttr.CmdLine 原样传递命令行，绕过 os/exec 的转义
		// （平台差异封装在 sysproc_windows.go / sysproc_other.go）。
		cmd = exec.Command(shell)
		applyWindowsCmdLine(cmd, shell, shellFlag, command)
	} else {
		cmd = exec.Command(shell, shellFlag, command)
	}
	if b.Sandbox != nil {
		cmd = b.Sandbox(cmd)
	}
	// P4-3: explicitly requested long commands run detached and are polled
	// via bash_output. Default timeouts stay synchronous for compatibility.
	if (params.Timeout > 60000 || params.RunInBackground) && b.Background != nil {
		var container *sandbox.Container
		var startErr error
		if b.SandboxStart != nil {
			container, startErr = b.SandboxStart(cmd)
		}
		id, err := b.Background.Start(cmd, container, startErr, params.Command)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("已启动后台任务 %s，命令：%s\n轮询：bash_output {\"job_id\":\"%s\"}；终止：bash_kill {\"job_id\":\"%s\"}（任务随 bounty 进程退出而结束）",
			id, params.Command, id, id), nil
	}
	output, err := runWithTreeKill(cmd, execCtx, b.SandboxStart)
	if execCtx.Err() == context.DeadlineExceeded {
		return "", &TimeoutError{Command: params.Command, Timeout: timeout}
	}
	out := decodeOutput(output)
	out = trimHeadTail(out, bashHeadRunes, bashTailRunes)
	if err != nil {
		out = maybeAppendWindowsHint(shell, params.Command, out)
		return out, &ExecError{Command: params.Command, Output: out, Err: err}
	}
	return out, nil
}

// bashHeadRunes / bashTailRunes: bash output keeps the first and last 15000
// runes each so the agent sees startup and failure context while the middle
// (bulk logs) is elided.
const (
	bashHeadRunes = 15000
	bashTailRunes = 15000
)

// trimHeadTail keeps headRunes from the start and tailRunes from the end of
// s, inserting an explanatory notice in the middle. Unchanged when the input
// fits within the budget.
func trimHeadTail(s string, headRunes, tailRunes int) string {
	runes := []rune(s)
	if len(runes) <= headRunes+tailRunes {
		return s
	}
	head := string(runes[:headRunes])
	tail := string(runes[len(runes)-tailRunes:])
	return head + fmt.Sprintf("\n...[bash output truncated: 共 %d 字符，保留头尾各 %d 字符。可把输出重定向到文件后分段读取。]\n", len(runes), headRunes) + tail
}

// prepareCommand rewrites unquoted Windows drive paths (D:\文件夹\a.png) into
// forward-slash form (D:/文件夹/a.png). The POSIX shell used on Windows (e.g.
// Git Bash sh) strips backslashes from unquoted tokens, so such paths would
// otherwise arrive mangled ("The filename, directory name, or volume label
// syntax is incorrect." / 文件名、目录名或卷标语法不正确). Quoted paths are
// left untouched: the shell's own path conversion already handles them.
func prepareCommand(command string) string {
	if runtime.GOOS != "windows" {
		return command
	}
	var sb strings.Builder
	sb.Grow(len(command))
	inSingle, inDouble, escaped := false, false, false
	for i := 0; i < len(command); {
		c := command[i]
		if escaped {
			sb.WriteByte(c)
			escaped = false
			i++
			continue
		}
		switch c {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '\\':
			if inDouble {
				escaped = true
			}
		}
		if !inSingle && !inDouble && !escaped && isDrivePathStart(command, i) {
			sb.WriteByte(c)
			sb.WriteByte(command[i+1])
			i += 2
			for i < len(command) {
				cc := command[i]
				if cc == '\\' {
					sb.WriteByte('/')
					i++
					continue
				}
				if isPathTokenEnd(cc) {
					break
				}
				sb.WriteByte(cc)
				i++
			}
			continue
		}
		sb.WriteByte(c)
		i++
	}
	return sb.String()
}

func isDrivePathStart(command string, i int) bool {
	return i+2 < len(command) && isASCIILetter(command[i]) && command[i+1] == ':' && command[i+2] == '\\'
}

func isASCIILetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// isPathTokenEnd reports whether c terminates an unquoted Windows path token.
func isPathTokenEnd(c byte) bool {
	switch c {
	case ' ', '\t', '\r', '\n', '|', '&', ';', '<', '>', '(', ')', '[', ']', '{', '}', '*', '?', '`', '\'', '"':
		return true
	}
	return false
}

// decodeOutput converts raw command output into UTF-8 text. Native Windows
// tools (cmd.exe, PowerShell 5.x) emit GBK bytes on Chinese systems; decoding
// them keeps error messages readable instead of garbled.
func decodeOutput(out []byte) string {
	if utf8.Valid(out) {
		return string(out)
	}
	if runtime.GOOS == "windows" {
		if s, _, err := transform.String(simplifiedchinese.GBK.NewDecoder(), string(out)); err == nil {
			return s
		}
	}
	return string(out)
}

type TimeoutError struct {
	Command string
	Timeout time.Duration
}

func (e *TimeoutError) Error() string {
	return "command timed out after " + e.Timeout.String() + ": " + e.Command
}

type ExecError struct {
	Command string
	Output  string
	Err     error
}

func (e *ExecError) Error() string { return e.Output + "\n" + e.Err.Error() }

// windowsAlias maps Unix-only commands to their cmd.exe equivalents. The
// suggestion strings keep the actionable form the model can re-emit directly.
var windowsAlias = map[string]string{
	"ls":     "dir（ls -la → dir /a）",
	"pwd":    "cd",
	"cat":    "type",
	"cp":     "copy",
	"mv":     "move",
	"rm":     "del（危险删除会先过权限门）",
	"touch":  "type nul > 文件名",
	"grep":   "findstr",
	"clear":  "cls",
	"which":  "where",
	"uname":  "ver",
	"sleep":  "timeout /t 秒数",
	"head":   `powershell -Command "Get-Content 文件 -TotalCount N"`,
	"tail":   `powershell -Command "Get-Content 文件 -Tail N"`,
	"wc":     `find /c /v ""`,
	"chmod":  "icacls",
	"export": "set",
	"env":    "set",
	"man":    "help",
	"diff":   "fc",
	"ln":     "mklink",
	"kill":   "taskkill /PID 进程号 /F",
	"ps":     "tasklist",
	"open":   "start",
}

// windowsNativeCommands is the pre-check whitelist: commands known to exist
// in cmd.exe. Unix aliases are blocked with a suggestion; everything else
// (python/go/git/npm installed on PATH) passes through untouched.
var windowsNativeCommands = map[string]bool{
	"dir": true, "cd": true, "type": true, "copy": true, "move": true,
	"del": true, "ren": true, "md": true, "rd": true, "echo": true,
	"set": true, "cls": true, "findstr": true, "where": true, "ver": true,
	"for": true, "if": true, "goto": true, "call": true, "start": true,
	"tasklist": true, "taskkill": true, "fc": true, "mklink": true,
	"icacls": true, "timeout": true, "chcp": true, "title": true,
	"assoc": true, "ftype": true, "more": true, "sort": true, "find": true,
}

// precheckWindowsCommand returns a suggestion and true when the command's
// first token is a known Unix-only tool that cmd.exe cannot run.
func precheckWindowsCommand(command string) (string, bool) {
	token := strings.ToLower(firstCommandToken(command))
	if token == "" || windowsNativeCommands[token] {
		return "", false
	}
	hint, ok := windowsAlias[token]
	if !ok {
		return "", false
	}
	return hint, true
}

// firstCommandToken extracts the first whitespace-delimited token of a
// command, stripping surrounding quotes (e.g. `"ls" -la` → ls).
func firstCommandToken(command string) string {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return ""
	}
	end := strings.IndexAny(trimmed, " \t\r\n")
	if end < 0 {
		end = len(trimmed)
	}
	return strings.Trim(trimmed[:end], `"'`)
}

// maybeAppendWindowsHint appends a candidate suggestion when a cmd.exe run
// failed with a not-recognized error or used a known Unix-only command.
func maybeAppendWindowsHint(shell, command, output string) string {
	if shell != "cmd" {
		return output
	}
	low := strings.ToLower(output)
	notRecognized := strings.Contains(low, "不是内部或外部命令") ||
		strings.Contains(low, "is not recognized as an internal or external command")
	if !notRecognized {
		return output
	}
	token := strings.ToLower(firstCommandToken(command))
	if hint, ok := windowsAlias[token]; ok {
		return output + "\n提示：cmd.exe 不识别 " + token + "，建议改用 " + hint
	}
	if token != "" {
		return output + "\n提示：命令 " + token + " 不存在或不在 PATH 中，可用 where " + token + " 检查；如需 Unix 等价命令可对照 findstr/type/dir 等内置命令"
	}
	return output
}

// runWithTreeKill starts cmd, waits for completion or the context deadline,
// and on deadline kills the whole process tree on Windows (cmd.exe spawns
// children such as ping that outlive the root process and keep output pipes
// open; taskkill /T /F reaps them so the call returns promptly).
func runWithTreeKill(cmd *exec.Cmd, ctx context.Context, start func(*exec.Cmd) (*sandbox.Container, error)) ([]byte, error) {
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	var container *sandbox.Container
	var err error
	if start != nil {
		container, err = start(cmd)
	} else {
		err = cmd.Start()
	}
	if err != nil {
		return nil, err
	}
	if container != nil {
		defer container.Close()
	}
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	select {
	case err := <-waitCh:
		return buf.Bytes(), err
	case <-ctx.Done():
		if cmd.Process != nil {
			killProcessTree(cmd.Process.Pid)
			if container != nil {
				container.Kill()
			}
		}
		err := <-waitCh
		return buf.Bytes(), err
	}
}

// killProcessTree terminates pid and all its descendants. No-op off Windows.
func killProcessTree(pid int) {
	if runtime.GOOS != "windows" {
		return
	}
	// Ignore errors: the root may already be dead; taskkill reaps the rest.
	_ = exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F").Run()
}
