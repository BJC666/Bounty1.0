package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"bounty/internal/tool"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

type BashTool struct {
	Timeout      time.Duration
	Sandbox      func(*exec.Cmd) *exec.Cmd
	DockerRunner func(ctx context.Context, command string) (string, error)
}

func (b *BashTool) Name() string      { return "bash" }
func (b *BashTool) ReadOnly() bool    { return false }
func (b *BashTool) Owner() tool.Owner { return tool.Owner{Kind: "core", ID: "builtin"} }
func (b *BashTool) Description() string {
	return "Execute a shell command. Use for running tests, building, file operations, git commands, and other terminal tasks."
}
func (b *BashTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","maxLength":4000,"description":"The shell command to execute"},"description":{"type":"string","maxLength":200,"description":"Clear, concise description of what this command does"},"timeout":{"type":"number","minimum":1,"maximum":600000,"description":"Optional timeout in milliseconds (max 600000)"}},"required":["command","description"],"additionalProperties":false}`)
}
func (b *BashTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Command     string  `json:"command"`
		Description string  `json:"description"`
		Timeout     float64 `json:"timeout"`
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
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" && shell == "cmd" {
		// Windows 上 os/exec 会把含引号的参数转义成 \" 形式，而 cmd.exe
		// 不识别 \" 转义，导致带引号命令（如 python -c "..."）被截断。
		// 用 SysProcAttr.CmdLine 原样传递命令行，绕过 os/exec 的转义。
		cmd = exec.CommandContext(execCtx, shell)
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: shell + " " + shellFlag + " " + command}
	} else {
		cmd = exec.CommandContext(execCtx, shell, shellFlag, command)
	}
	if b.Sandbox != nil {
		cmd = b.Sandbox(cmd)
	}
	output, err := cmd.CombinedOutput()
	if execCtx.Err() == context.DeadlineExceeded {
		return "", &TimeoutError{Command: params.Command, Timeout: timeout}
	}
	out := decodeOutput(output)
	out = trimHeadTail(out, bashHeadRunes, bashTailRunes)
	if err != nil {
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
