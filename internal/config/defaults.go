package config

func Defaults() *Config {
	return &Config{
		Version:      1,
		DefaultModel: "deepseek/deepseek-v4-pro",
		Agent: AgentConfig{
			Temperature:            0.0,
			CompactRatio:           0.8,
			CompactForceRatio:      0.9,
			SoftCompactRatio:       0.5,
			MaxSubagentDepth:       2,
			MaxSubagentConcurrency: 6,
			MaxParallelWriters:     3,
			MaxSteps:               50,
		},
		Sandbox: SandboxConfig{
			Bash:    "enforce",
			Network: true,
		},
		Permissions: PermissionsConfig{
			Allow: AllowConfig{
				Tools: []string{
					// Registry uses snake_case tool names; NewGate also
					// normalizes legacy camelCase entries from older configs.
					"read_file", "glob", "grep", "web_search", "web_fetch",
					"todo_write", "remember", "code_index",
					"edit_file", "write_file",
					"task", "read_only_task", "fleet",
				},
				BashPattern: []string{
					// Unix-style (sh) commands
					"ls *", "cat *", "head *", "wc *", "pwd", "echo *", "cd *", "find *", "grep *",
					"git status *", "git diff *", "git log *", "git branch *",
					"git add *", "git commit *", "git checkout *", "git switch *",
					"git merge *", "git pull *", "git push *", "git stash *",
					"npm run *", "npm test *", "npm install *",
					"python *", "go build *", "go test *", "go vet *", "go mod *",
					"mkdir *", "touch *", "cp *", "mv *", "curl *", "curl.exe *",
					// Windows cmd-style commands
					"dir *", "type *", "where *", "set *", "ver", "tasklist *",
					"reg query *", "tree *", "whoami *", "hostname *", "ipconfig *",
					"ping *", "nslookup *",
				},
			},
			Deny: DenyConfig{
				BashPattern: []string{
					"rm -rf *", "rm *", "del *", "del /f *", "rd *", "rmdir *",
					"sudo *", "chmod 777 *", "chmod -R 777 *",
					"git push --force *", "git reset --hard *", "git clean *",
					"shutdown *", "reboot *", "format *", "taskkill *",
					"docker rm *", "docker rmi *", "reg delete *",
				},
				ForbidWrite: []string{
					"Windows/*", "Program Files/*", "Program Files (x86)/*",
					"System32/*", "/etc/*", "/boot/*", "~/.ssh/*",
				},
			},
		},
	}
}
