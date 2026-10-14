# LogHelp — 协作入口

本项目是 Go / EbitenUI 桌面 Android 日志工具。协作资料统一维护在 `.agent/`，规则仅围绕 LogHelp 的代码、构建、验收与发布维护。

## 按任务读取

| 文件 | 适用场景 |
|---|---|
| `.agent/README.md` | 了解协作目录或维护规则 |
| `.agent/project.md` | 定位模块、版本、构建及验收资料 |
| `.agent/rules/code-guardrails.md` | 修改或审查业务代码、UI、更新功能 |
| `.agent/skills/loghelp-desktop-features/SKILL.md` | 新增或修改 Go 桌面功能；跨平台、投屏、图片剪贴板及依赖分发 |
| `.agent/rules/build-and-validation.md` | 编译、运行与手动验收 |
| `.agent/rules/commit-message.md` | 生成 review、按功能分类拆分提交、准备推送或 PR |
| `.agent/rules/changelog.md` | 准备提交或发布 |
| `.agent/workflows/github-release.md` | GitHub Release、修订包、附件上传与校验 |

只读取当前任务匹配的资料；已读且未变更的文件不重复读取。新增规范同步补齐此表，文档与规则在 `.agent/` 中集中维护。

## 执行边界

- 不新增测试，所有项目采用手动验收。文档修改只做静态检查，不触发应用编译、启动或设备操作。
- 编译尽量最小化；检测到实际并发编译时等待，不停止其他会话或进程。完整桌面构建需人工确认；当前会话已经明确授权的同一更新/发布工作沿用授权，不重复询问。
- 成功构建后源码未修改，禁止因 review 重复构建。复用构建记录及产物指纹。
- 脚本、文本文件使用 UTF-8；Python 建议 `python -X utf8 ...`。
- 保留无关修改；可以使用 subagent，分工时明确文件范围并避免并发构建。
- 用户授权后才推送、发布或更换 Release 附件；已有会话授权继续有效。浏览器优先内置浏览器。
- 问题单评论保持简洁：`问题原因：...`、`解决方式：...`、`影响功能：...`。能从已可访问问题单获取时间时不再询问。
