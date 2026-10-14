# 提交规范

按 [GitHub 官方贡献指南的提交建议](https://docs.github.com/en/get-started/exploring-projects-on-github/contributing-to-open-source#committing-and-pushing-your-changes)生成提交信息：标题清晰简洁、少于 50 个字符，概括这笔提交的实际改动；正文每行尽量少于 72 个字符。相关改动放在同一笔提交，无关改动分别提交。

采用“摘要标题 + 空行 + 可选正文”的格式，不再沿用安卓项目的固定模板，不强制类型前缀、模块括号、问题单号或四段正文。上述 GitHub 指南没有要求 `feat(scope): ...` 等 Conventional Commits 格式，本项目也不强制采用。

```text
添加日志列宽拖动调整

固定列宽不便于查看较长的日志字段。
允许拖动列边界调整宽度，并在重启后恢复设置。
```

生成时遵循以下规则：

- 标题用一句话说明改动，少于 50 个字符；中文或英文均可，字符数按 Unicode 字符计数。
- 简单改动仅写标题即可；需要补充背景时，标题后空一行，正文说明修改原因和行为变化，必要时列实际验证结果或问题单引用。
- 正文按段落或简短列表组织，每行尽量少于 72 个字符；链接、路径和 trailer 等不可拆分内容可保留完整。
- 避免“修改代码”“更新”等宽泛标题，不用固定四段模板填充正文，不虚构验证结果。
- GitHub / Git 自动生成的 merge / revert 信息可沿用默认格式，不为满足长度建议改写历史。

## Review 与提交拆分

- 生成 review 时，先按功能、修复目标或维护目的分类说明修改内容，并列出每组涉及的文件或 diff 范围及建议提交标题。
- 不同功能或独立目标必须拆成多笔提交，不把所有工作区修改合成一笔提交；只有单一目标时可用一笔提交。
- 同一功能的紧密相关代码、配置、文档及发布元数据放在同一笔提交，保持每笔提交完整且可独立审查；公共前置改动可单独提交，并说明依赖顺序。
- 一个文件包含多个独立目标时按 diff hunk 暂存，避免仅按文件分组导致混入无关改动。无法安全拆分的耦合改动在 review 中说明原因。
- review 分类本身不授权提交或推送；实际提交时按分类逐组核对暂存范围，保留他人或其他会话的修改。

## 提交前

1. 检查当前分支、工作区、暂存区及 upstream，保护无关修改。
2. 在已有同步授权范围内 fetch，按当前分支与 upstream 核对远端差异。只在确有更新时选择安全同步方式，不自动强推、清理或 stash 他人修改。
3. 基于本次实际提交范围更新 `CHANGELOG.md`，参见 `changelog.md`。
4. 按 review 分类逐组暂存，执行 `git diff --check` 并核对 staged diff；不要无差别 `git add .` 纳入日志、临时文件、本地配置或其他目标的改动。
5. 按上述格式生成本组提交信息，核对标题与 staged diff 一致。不自动追加 AI 工具的 `Co-authored-by`。用户明确要求的真人共同作者署名可以保留。

## 安全提交

AI 统一用脚本传递 Git 参数和 UTF-8 正文文件，不拼接 shell 命令：

```powershell
python -X utf8 .agent/scripts/git-commit.py --file artifacts/commit-message.txt
python -X utf8 .agent/scripts/git-commit.py --check
```

脚本通过 Python 参数数组调用标准 Git 提交，保留 Git hooks、签名及正常提交语义。脚本检查非空标题、可选正文的空行分隔和自动 AI trailer；标题或正文行超过 GitHub 建议长度时仅提示，不阻止提交或要求改写历史。自动生成的 merge / revert 信息不作长度提示。脚本只提交已经暂存的文件，不自动 stage、同步或 push。提交分类与拆分由 review 和暂存范围核对保证，脚本不推断功能边界。它不会自动改写已发布提交。

如果需要更正本会话新建且未推送的最新提交，可使用 `--amend-unpushed`；脚本在 HEAD 被远端分支或标签引用时拒绝执行。使用前确认该提交属于本次任务，没有其他人的改动。

提交后核对新 commit 的信息和范围，再按用户授权推送。已推送历史禁止为调整提交信息或拆分提交自动 amend / rebase / force-push。发布标签规则参见 `../workflows/github-release.md`。
