# LogHelp 工程概览

| 项目 | 位置 / 约定 |
|---|---|
| 语言与依赖 | Go；实际版本以 `go.mod` 为准；Ebiten / EbitenUI |
| 程序入口 | `cmd/loghelp/main.go` |
| 窗口、布局与交互 | `internal/ui/` |
| 采集、查询、录制、Shell、更新 | `internal/core/` |
| 内存诊断 | `internal/diagnostics/`、`docs/memory-profiling.md` |
| 本地运行日志、崩溃与反馈 | `internal/support/`、`internal/ui/feedback.go`；不自动上传，说明见根 README |
| 产品版本 | 根 `VERSION`，当前 `0.0.2` |
| 发布修订 | 根 `REVISION`，非负整数，同版本发布新包必须递增 |
| 版本嵌入 | 根 `version.go`，应用版本与修订均来自根文件 |
| 构建 | `scripts/build.py` / `scripts/build.ps1` |
| Windows 图标 | `assets/`、`scripts/generate-icon.py`、`cmd/loghelp/rsrc_windows_*.syso` |
| 构建结果 | Windows amd64 为 `bin/loghelp.exe`，其他为 `bin/<os>-<arch>/loghelp[.exe]`；构建指纹和哈希记录在 `artifacts/` |
| 发布包 | `dist/<版本>/`，通过 `.agent/scripts/package-release.py` 打包 |
| Windows 安装包 | `scripts/installer/loghelp.iss`；打包加 `--setup`，x64 复用 ZIP 自动更新、ARM64 手动升级；`--local` 输出本地验收包到 `artifacts/packages/<版本>/` |
| 手动验收 | `docs/manual-validation.md` |
| 发布说明 | `docs/releases/<版本>.md`、根 `CHANGELOG.md` |
| GitHub | `sunqing37/loghelp`，默认分支 `main`，版本标签 `v<版本>` |

`VERSION` 用于产品版本，`REVISION` 区分同版本的二进制修订。发布修订 `r3` 表示 `VERSION=0.0.1`、`REVISION=3`；修改任一根文件都会改变构建指纹。不要只改 ZIP 文件名、Release 文案或时间来冒充新的内嵌修订。

更新检查入口在设置页，结果通过独立弹窗展示。Windows x64 便携版和当前用户安装版下载匹配平台附件，校验 SHA-256，再交给独立进程替换 EXE 并重启。更新判断先比较产品版本，同版本再比较修订整数；配置与录制文件不随替换删除。

原始包没有修订后缀，视为 `r0`；历史 `r2` 没有内嵌修订比较能力，安装新的修订识别版本前需手动替换一次。新程序收到后续同版本更高修订时才可自动识别并提示替换。发布流程详见 `workflows/github-release.md`。
