---
name: git-workflow
description: 规范的 Git 分支、提交与合并流程（避免破坏性历史操作）
triggers: [git, 提交, 合并, 分支, commit, merge, 推送]
read_only: false
---
# Git 工作流规范

- 提交前先 `git status` + `git diff` 自查，diff 中只保留与任务相关的改动。
- 提交信息用「类型(范围): 中文摘要——要点1；要点2」结构，feat/fix/test/docs/style 分类。
- 分支：主线 main/master 保持可发布；功能改动开 feature/ 分支，修 bug 用 fix/ 分支。
- 合并优先 squash/rebase 保持线性历史；多人协作一律走 Pull Request 评审。
- 禁止改写已推送的公共历史；确需强推前必须与协作者确认（强推属破坏性操作）。
- 大文件、构建产物、密钥一律进 .gitignore，不提交凭据。
- 冲突解决后必须重新编译/测试再合并。
