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
					"Read", "Glob", "Grep", "WebSearch", "WebFetch",
					"Skill", "TodoWrite",
					"AskUserQuestion",
					"Edit", "Write",
				},
				BashPattern: []string{
					"ls *", "cat *", "head *", "wc *", "pwd", "echo *", "cd *", "find *",
					"git status *", "git diff *", "git log *", "git branch *",
					"git add *", "git commit *", "git checkout *", "git switch *",
					"git merge *", "git pull *", "git push *", "git stash *",
					"npm run *", "npm test *", "npm install *",
					"python *", "go build *", "go test *",
					"mkdir *", "touch *", "cp *", "mv *", "curl *",
					"*",
				},
			},
			Deny: DenyConfig{
				BashPattern: []string{
					"rm -rf *", "rm *",
					"sudo *", "chmod 777 *",
					"git push --force *", "git reset --hard *", "git clean *",
					"shutdown *", "reboot *", "format *",
					"docker rm *", "docker rmi *",
				},
				ForbidWrite: []string{
					"Windows/*", "Program Files/*", "Program Files (x86)/*",
					"System32/*", "/etc/*", "/boot/*", "~/.ssh/*",
				},
			},
		},
	}
}
