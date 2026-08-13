package skill

import (
	"strings"
)

// AuditFinding is one detected dangerous pattern in a skill body.
type AuditFinding struct {
	Rule   string // rule name, e.g. pipe-to-shell
	Detail string // offending snippet (truncated)
}

// AuditResult is the outcome of scanning one skill.
type AuditResult struct {
	Passed   bool
	Findings []AuditFinding
}

// RejectedSkill records a skill file refused at discovery time.
type RejectedSkill struct {
	Name       string
	SourcePath string
	Findings   []AuditFinding
}

// auditRule couples a rule name with the patterns it blocks. Matches are
// case-insensitive; prefix "\b" is avoided on purpose because shell payloads
// are usually unquoted in prose.
type auditRule struct {
	name     string
	patterns []string
}

var auditRules = []auditRule{
	{
		name: "recursive-delete",
		patterns: []string{
			"rm -rf", "rm -fr", "rm -r -f", "rm -f -r",
			"Remove-Item -Recurse -Force", "del /s /q", "rd /s /q",
		},
	},
	{
		name: "pipe-to-shell",
		patterns: []string{
			"curl ", "wget ", "| sh", "| bash", "| sudo bash",
			"Invoke-Expression", "iex ", "eval(",
		},
	},
	{
		name:     "privilege-escalation",
		patterns: []string{"sudo ", "chmod 777", "chmod -R", "chmod +s"},
	},
	{
		name:     "history-rewrite",
		patterns: []string{"git push --force", "git push -f", "git reset --hard", "git clean -f"},
	},
	{
		name:     "fork-bomb",
		patterns: []string{":(){", "%0|%0"},
	},
	{
		name:     "base64-decode-exec",
		patterns: []string{"base64 -d |", "base64 --decode |", "certutil -decode"},
	},
	{
		name: "credential-exfil",
		patterns: []string{
			"cat ~/.ssh/", "cat .env", "type .env", "Get-Content .env",
			"api_key=", "api-key", "Authorization: Bearer",
		},
	},
	{
		name:     "network-download-exec",
		patterns: []string{"& curl ", "& wget ", "powershell -enc", "-enc "},
	},
}

const findingSnippetMax = 120

// AuditSkill scans a skill body and reports every dangerous pattern found.
func AuditSkill(sk *Skill) AuditResult {
	res := AuditResult{Passed: true}
	lower := strings.ToLower(sk.Body)
	for _, rule := range auditRules {
		for _, pat := range rule.patterns {
			idx := strings.Index(lower, strings.ToLower(pat))
			if idx < 0 {
				continue
			}
			start := idx
			if start > 40 {
				start -= 40
			}
			end := idx + len(pat) + 40
			if end > len(sk.Body) {
				end = len(sk.Body)
			}
			snippet := sk.Body[start:end]
			if len(snippet) > findingSnippetMax {
				snippet = snippet[:findingSnippetMax] + "..."
			}
			res.Passed = false
			res.Findings = append(res.Findings, AuditFinding{Rule: rule.name, Detail: snippet})
			break // one finding per rule keeps the report readable
		}
	}
	return res
}

// auditRulesDoc exposes the rule list for the boot-time warning and docs.
func auditRuleNames() []string {
	names := make([]string, 0, len(auditRules))
	for _, r := range auditRules {
		names = append(names, r.name)
	}
	return names
}
