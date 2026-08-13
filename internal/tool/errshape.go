package tool

import (
	"fmt"
	"strings"
)

// ShapeError renders a tool execution error in the three-line format the
// model sees in the next turn: 错误类型 / 原因 / 建议重试（含可操作参数）。
// The classification reuses the provider's 8-category naming where it applies
// (NetworkError/ContentFilter/...) and adds tool-specific categories.
func ShapeError(name string, err error) string {
	raw := err.Error()
	cat := classifyToolError(raw)
	reason := raw
	if r := []rune(reason); len(r) > 500 {
		reason = string(r[:500]) + "...(截断)"
	}
	return fmt.Sprintf("【错误类型】%s\n【原因】%s\n【建议重试】%s", cat, reason, hintFor(cat, name))
}

type errorRule struct {
	cat    string
	substr []string
}

// Ordered rule list: more specific categories are checked first.
var errorRules = []errorRule{
	{"未知工具", []string{"unknown tool"}},
	{"权限拒绝", []string{"denied", "permission", "approval", "拒绝", "not permitted", "requires user approval"}},
	{"文件不存在", []string{"no such file or directory", "cannot find the file", "找不到文件", "系统找不到", "The system cannot find", "文件不存在", "path does not exist", "ENOENT"}},
	{"匹配不唯一", []string{"不唯一", "not unique", "multiple occurrences", "出现 2 次", "出现 3 次", "出现 4 次", "出现 5 次", "出现 6 次", "出现 7 次", "出现 8 次", "出现 9 次"}},
	{"匹配未命中", []string{"未找到", "未命中", "old_string"}},
	{"路径/编码错误", []string{"文件名、目录名或卷标语法不正确", "语法不正确", "FINDSTR", "不是内部或外部命令", "找不到文件"}},
	{"超时", []string{"timeout", "超时", "deadline", "timed out"}},
	{"网络错误", []string{"connection refused", "refusing to fetch", "no such host", "network", "unreachable", "dial tcp", "DNS", "EOF"}},
	{"内容过滤", []string{"content_filter", "内容过滤", "敏感"}},
	{"参数错误", []string{"json", "unmarshal", "invalid character", "must not be empty", "cannot be empty", "expected", "invalid arguments", "required", "null"}},
}

func classifyToolError(raw string) string {
	low := strings.ToLower(raw)
	for _, r := range errorRules {
		for _, s := range r.substr {
			if strings.Contains(low, strings.ToLower(s)) {
				return r.cat
			}
		}
	}
	return "其他错误"
}

func hintFor(cat, name string) string {
	switch cat {
	case "参数错误":
		return fmt.Sprintf("对照 %s 的 Schema 检查必填字段与类型（read_file 需 file_path；bash 需 command；edit_file 需 file_path/old_string/new_string），修正后重试", name)
	case "文件不存在":
		return "先用 glob 或 read_file 确认真实路径（注意 Windows 盘符与中文路径），再重试"
	case "权限拒绝":
		return "换用只读工具完成同一目标，或请求用户批准/切换更宽松的权限姿态后重试"
	case "匹配不唯一":
		return "提供更长的 old_string 以唯一定位，或显式 replace_all:true"
	case "匹配未命中":
		return "按错误信息中的附近行上下文修正 old_string（或先 read_file 读取文件最新内容），再重试"
	case "路径/编码错误":
		return "Windows 下改用 8.3 短路径或通配符枚举，避免在 cmd 中直接传中文路径"
	case "超时":
		return "增大 timeout 参数（bash 最大 600000ms），或收窄命令范围后重试"
	case "网络错误":
		return "确认 URL 为可达公网地址，必要时切换国内镜像源后重试"
	case "内容过滤":
		return "改写请求内容避开敏感表述后重试"
	case "未知工具":
		return "从可用工具列表中选择正确的工具名重试"
	default:
		return "保留完整错误信息，缩小操作范围后重试"
	}
}
