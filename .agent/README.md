# `.agent/` 协作资料

所有协作工具先读根 `AGENTS.md`，再按任务读取本目录。正文围绕 LogHelp 的 Go 桌面项目维护。

```text
.agent/
├── README.md
├── project.md
├── rules/
│   ├── code-guardrails.md
│   ├── build-and-validation.md
│   ├── commit-message.md
│   └── changelog.md
├── workflows/
│   └── github-release.md
└── scripts/
    ├── git-commit.py
    └── package-release.py
```

`rules/` 放短而明确的约束，`workflows/` 放可执行步骤，`scripts/` 放本项目辅助工具。没有实际用途不创建空目录；需要专属工具入口时仅引用根 `AGENTS.md`。

维护时核对目录索引、入口路由及相关流程，避免路径失效或规则互相矛盾。静态检查文件引用、UTF-8 和 `git diff --check`；不要为文档维护触发应用构建。工具脚本不自动新增或运行测试。

常用命令：

```powershell
python -X utf8 .agent/scripts/git-commit.py --file artifacts/commit-message.txt
python -X utf8 .agent/scripts/package-release.py
```

提交脚本只提交已暂存内容，不自动暂存、同步分支或推送。打包脚本复用符合当前源码指纹的 EXE，不代替构建授权或发布授权。详见对应规则及工作流。
