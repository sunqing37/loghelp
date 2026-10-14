# LogHelp

独立的 Android Logcat 桌面工具，界面和核心均使用 Go，采用 Ebitengine + EbitenUI。Windows、macOS、Linux 编译均可关闭 CGO，仅需 Go 编译器。无需 Node.js、TypeScript、浏览器或 Android Studio。

## 下载

当前版本 **0.0.1 r3**（产品版本记录在 `VERSION`，修订号记录在 `REVISION`）。在 [GitHub Releases](https://github.com/sunqing37/loghelp/releases/tag/v0.0.1) 下载 Windows 64 位便携包，优先选择 `loghelp-0.0.1-windows-amd64-r3.zip`，解压后运行 `loghelp.exe`。ADB 需另行准备，可在设置中选择其位置。发布附件提供 SHA-256 校验文件。

见 [0.0.1 发布说明](docs/releases/0.0.1.md) 和 [更新记录](CHANGELOG.md)。

源码构建时会将 `VERSION` 和 `REVISION` 嵌入程序，版本及修订显示在窗口标题、主界面顶部和设置页。修改任一文件后，构建脚本会识别变更并重新编译。检查更新先比较产品版本，同版本再按整数比较修订号，选择本平台最高修订附件；历史原始包没有修订后缀，视为 `r0`。

已经下载的旧 `r2` 只比较产品版本，无法识别同为 `0.0.1` 的 `r3`，需要手动下载并替换一次。使用 `r3` 后，后续 `r4` 等同版本修订可以通过应用内更新替换。

设置页提供“检查更新”，打开独立弹窗，在后台检查 GitHub 最新正式 Release，显示版本和发布说明摘要；关闭后返回原设置页，保留未保存内容和滚动位置。Windows 便携版发现新版后可点击“下载更新并重启”：确认后下载并校验 SHA-256，结束采集、录制和终端，由独立更新进程替换 EXE 并重启，替换或启动失败时恢复旧程序。需要程序目录可写，旧 EXE 保留为备份；其他平台或缺少可校验附件时可手动下载。检查仅在点击按钮时进行，超时或网络错误会显示提示，重启前需先保存设置。

Windows EXE 已嵌入与窗口一致的蓝色 `L` 图标。图标源文件为 `assets/loghelp.ico`，多尺寸 PNG/ICO 由 `python scripts/generate-icon.py` 根据现有绘图规则生成；`cmd/loghelp/rsrc_windows_amd64.syso` 和 `rsrc_windows_arm64.syso` 是可直接参与构建的资源文件。

协作规则见 [AGENTS.md](AGENTS.md) 和 [.agent 目录](.agent/README.md)，其中包含 [提交规范](.agent/rules/commit-message.md) 及 [GitHub 发布流程](.agent/workflows/github-release.md)。发布包由 `python -X utf8 .agent/scripts/package-release.py` 基于成功构建记录生成，不重复编译。

## 当前功能

- 从 PATH 自动查找 ADB，支持选择 executable 或 platform-tools 目录、版本校验和设置保存。
- 设备状态与热插拔、指定序列号采集、应用包名选择、进程变化事件、断线重试及中断标记。
- 顶部「设备预览」打开右侧 H.264 视频投屏面板，支持独立切换设备、暂停显示/重连、原分辨率截图保存与复制，以及实时触摸、拖动、返回、主页、最近任务和唤醒。流配置上限 60 FPS，界面显示实际接收/解码帧率；不再使用定时 PNG 抓屏刷新。
- 多标签页独立过滤，同设备相同配置共享采集；复制、重命名、排序和配置恢复。每个标签右侧可直接关闭，Ctrl+W（macOS：Cmd+W）关闭当前标签；关闭时取消查询、释放视图缓存和终端连接，正在录制的会话保留在录制管理中。
- 日志/ADB 文件、录制目录和导出位置使用系统文件对话框；导出直接“另存为”，设置选路径时保留其他未保存的字段。
- 鼠标按组件切换系统光标：输入框/已连接终端为文本光标，可点击按钮/标签/日志行为手型，空白和禁用控件为箭头；光标更新不依赖界面缓存重绘。
- 日志详情默认自动换行，可用“换行”按钮切换；拖动详情上沿的分隔条调整高度并记住，长内容可纵向滚动，换行边界上的搜索词仍完整高亮。复制保留原文，日志列表仍为单行表格。
- 详情支持鼠标拖选、Shift+点击扩展选区；聚焦详情后 Ctrl/Cmd+C 复制选中、Ctrl/Cmd+A 全选，Esc 清除选区。“复制选中”和“复制全部”分别复制所选文字及完整原始日志，自动换行不影响复制内容。
- 工具栏“合并多行”按标签开启/关闭并记住。连续且毫秒时间、设备、进程/线程、TAG、级别等一致的记录合成一项，显示 `[N 行]`；详情显示完整块，复制与选中项导出包含该组全部原始记录。
- 字段过滤、正则、排除、AND/OR 和括号；过滤历史、命名收藏和字段建议列表。
- 独立搜索与高亮、匹配导航、未过滤上下文、时间跳转、暂停显示、清屏及跟随控制。
- 完整采集流的本地录制、按大小/时间轮转、目录额度、可选受管会话清理、后台录制管理。
- 离线打开会话 manifest.json 或 threadtime 文本；分页查询、可取消的扫描、导出选中/过滤结果。
- 固定设备绑定的交互 Shell，ANSI 显示、Ctrl+C、窗口尺寸、复制粘贴及可选命令历史。
- 设备侧栏与日志分析工作区、固定列头与单行等宽日志、独立详情区及清晰的采集状态。
- 深浅色、字体大小、可见列切换、详情换行、可配置快捷键、有界缓存和可见错误状态。
- 分组偏好设置、固定保存栏、统一圆角/间距/焦点反馈，低频与设备清理操作收纳到菜单。

Windows amd64 EXE 已生成（`bin/loghelp.exe`），完成启动、真实设备采集、过滤/搜索、双标签共享、录制/离线分页及 Shell 连接的基础手动检查。完整场景及其他平台实际运行状态见 [手动验收记录](docs/manual-validation.md)。尚未生成安装包；macOS/Linux 目前只验证模块编译。

投屏的 ADB/scrcpy 协议、会话、视频缓冲和渲染共用一套 Go 实现，Windows/macOS/Linux 均有解码与图片剪贴板路径，仍以 `CGO_ENABLED=0` 编译。Windows amd64 EXE 内嵌官方 scrcpy 3.3.4 的服务和 FFmpeg 7.1.1 解码库，首次投屏解压至用户缓存。macOS/Linux（以及未捆绑解码库的架构）优先加载兼容 FFmpeg 7 的动态库，找不到时使用 FFmpeg 5 或更高版本的可执行程序；只嵌入 Android 服务不等于已捆绑当地平台的 FFmpeg。

FFmpeg 可安装到 PATH、放在程序旁 `runtime/ffmpeg`（Windows 为 `runtime/ffmpeg.exe`），或用 `LOGHELP_FFMPEG` 指定路径；macOS 也识别 Homebrew 常见位置。可通过 `LOGHELP_MIRROR_RUNTIME` 指定包含匹配架构动态库与 `scrcpy-server` 的替代运行库目录。Windows 图片剪贴板发布原 PNG 与标准位图，macOS 使用 AppKit 图片对象，Linux Wayland 需要 `wl-clipboard`、X11 需要 `xclip`。缺依赖时界面显示具体原因和安装/指定路径方式。

面板固定绑定设备，切换日志标签不改变目标；打开面板时最小窗口宽度为 1440。视频最长边上限 1280，屏幕横竖尺寸改变时重建纹理；GPU 转换颜色，缓冲只保留最新帧，视频更新不重绘全部日志。暂停只冻结显示，恢复立即取最新帧。失焦、弹窗、关闭和旋转会结束触摸；关闭投屏取消会话，退出等待本会话转发与临时服务清理。截图按钮单独抓取原分辨率 PNG，不使用缩小的视频帧；「保存截图」打开另存为，「复制截图」直接写图片剪贴板。设备画面本身静止时实际帧率可能降低，不代表配置保证持续 60 FPS。

新版 Windows EXE 位于 `bin/loghelp.exe`。Windows 已验证真实设备视频接收、应用自有画面和原分辨率图片剪贴板格式；macOS/Linux 的实际桌面、粘贴与触摸交互仍需在目标系统验收，具体编译/运行记录见 [手动验收记录](docs/manual-validation.md)。

## 工程结构

```text
cmd/loghelp/       桌面入口
internal/core/     ADB、Shell 协议、采集、缓存、查询、录制、离线及配置
internal/ui/       EbitenUI 工作区、虚拟日志视图、VT 终端、设置与快捷键
scripts/build.ps1  Windows 分模块检查 / 显式桌面构建，跳过未修改源码的重复构建
scripts/build.py   Python 3.9+ 跨平台编译脚本，仅使用标准库
openspec/changes/build-desktop-logcat/  需求、设计和任务进度
```

固定 Ebitengine v2.10.5、EbitenUI v0.7.3、vt10x 提交 5011da428d02，终端通过 Go ADB Shell-v2 协议连接。ADB 使用外部 SDK 的安装，不修改 PATH，不自动 kill-server。配置位于操作系统用户配置目录下 `loghelp/settings.json`，例如 Windows `%APPDATA%\loghelp\settings.json`。

## 编译与启动

需要 **Go 1.25 或更高版本**，构建脚本始终设置 `CGO_ENABLED=0`，无需安装 GCC、Clang、MinGW 或 Xcode 编译工具。Ebitengine 从 2.10 起支持所有桌面平台的纯 Go 构建，见 [官方发布说明](https://ebitengine.org/en/documents/2.10.html)。

构建脚本先调用 `scripts/prepare-mirror.py`，从固定的官方附件下载并核验 SHA-256，生成架构无关的 Android 服务资源和 Windows amd64 解码库资源，并纳入指纹。首次构建需要联网及 Python 3；后续复用缓存。直接调用 `go build` 前也需运行该准备脚本。第三方版本、许可证及源代码入口见 [投屏依赖说明](docs/mirror-runtime.md)。

Linux 运行仍需 X11/XWayland、系统图形库和可用显卡驱动；不需要 C 开发头文件。剪贴板需要 wl-clipboard、xclip 或 xsel。Windows 使用系统剪贴板，macOS 使用系统 pbcopy/pbpaste。平台运行条件见 [官方安装说明](https://ebitengine.org/en/documents/install.html)。

文件对话框使用 [ncruces/zenity](https://github.com/ncruces/zenity)，同样无需 CGO：Windows 调用系统 Common Item Dialog，无需安装额外组件；macOS 使用系统 `osascript`；Linux 需提供 `zenity`、`matedialog` 或 `qarma` 之一。应用不会自动安装这些工具，缺失时显示提示。打开日志可选 TXT/LOG/JSONL 或录制会话的 `manifest.json`，导出格式按最终扩展名识别（`.jsonl` 为结构化，其余为 UTF-8 文本）。

仅检查 Go 模块，不生成桌面程序：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\build.ps1
# 可选：只检查其他平台模块，不链接桌面程序
powershell -ExecutionPolicy Bypass -File .\scripts\build.ps1 -TargetOS linux
powershell -ExecutionPolicy Bypass -File .\scripts\build.ps1 -TargetOS darwin
```

**项目约定要求全量构建先人工确认。** 确认后执行完整 Windows 桌面构建：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\build.ps1 -Desktop
.\bin\loghelp.exe
```

macOS/Linux 在对应系统确认完整构建后：

```sh
CGO_ENABLED=0 go build -o bin/loghelp ./cmd/loghelp
./bin/loghelp
```

构建前检查其他编译进程，避免并发构建；源码未修改时不重复执行已成功的检查。不新增、也不执行自动化测试。Windows 脚本保存源码哈希，重复调用时跳过成功过的相同检查。

也可以使用 Python 3.9 或更高版本，无需安装额外 Python 包，在项目根目录执行：

```sh
python scripts/build.py --dry-run                 # 查看默认模块检查计划，不编译
python scripts/build.py                           # 仅编译检查模块
python scripts/build.py --target-os linux          # 其他平台模块检查
python scripts/build.py --desktop                 # 人工确认后的完整桌面构建
python scripts/build.py --desktop --arch arm64     # ARM64 桌面程序
python scripts/build.py --desktop --run            # 构建后打开程序
```

默认目标为当前 Go 主机系统、amd64，完整桌面构建需在对应系统执行。输出为 Windows 的 `bin/loghelp.exe` 或 macOS/Linux 的 `bin/loghelp`。Python 脚本始终关闭 CGO，使用 UTF-8 输出与参数列表调用编译器；在 Windows 使用系统 PowerShell/CIM 检查进程，在 Linux/macOS 使用 `/proc` 或 `ps`。发现其他编译时默认等待最多 600 秒，可通过 `--wait-timeout` 调整。同一源码的成功构建会跳过，在 Windows 可复用现有 PowerShell 脚本的记录；Python 脚本之间另有系统文件锁，退出时自动释放。`--dry-run` 不编译、不启动程序、不写构建记录。

Windows 界面优先使用微软雅黑，日志优先使用 Consolas，缺失时分别回退到 SimHei 与内置 Go Mono。macOS 尝试 PingFang，Linux 尝试 Noto Sans CJK；可设置 `LOGHELP_FONT` 为支持中文的 TTF/TTC 路径。未找到时回退到内置 Go 字体，中文可能无法显示，字体未捆绑分发。

## 常用查询

```text
package:com.example.app level:WARN -tag:Noise
(tag:Audio | tag:Bluetooth) & level:INFO
message~:"timeout|failed"
tag:Audio tag:Bluetooth package:com.example.app
pid:123 tid:456 age:5m
after:"2026-10-12 10:00:00" before:"2026-10-12 11:00:00"
"D:\logs" "网络错误"
```

支持 `tag/package/process/message/line/level/age/pid/tid/after/before`。文本包含匹配，默认不区分大小写；应用选择器按完整包名匹配。`-tag:` 排除，`tag~:` 正则。同键非否定条件隐式 OR，其他隐式条件 AND，显式 `&` 优先于 `|`。带逻辑字符、括号或空格的正则及短语使用引号。Go RE2 不支持环视/反向引用，不支持 AS 的 `package:mine`、`is:` 特殊条件。

`age` 支持秒、分钟、小时、天，例如 `30s/5m/2h/7d`；实时使用主机当前时间，离线使用打开文件时的最后一条设备时间。时间范围包含 `after` 起点，不包含 `before` 终点，支持 RFC3339 时区。

## 采集、录制与历史范围

- 默认采集 main/system/crash；缓冲区不支持或设备未授权时显示错误，不悄悄跳过。
- 暂停显示继续采集和录制；停止当前页只移除该页订阅。停止共享流先显示受影响的标签页及录制。
- “合并多行”默认关闭，只改变当前过滤结果的展示，离线文件在当前页内合并。不会跨线程、设备、采集代次或原始序号缺口；每组最多 512 行或约 256 KiB，超出后分组展示。相同毫秒内的不同日志调用也可能被归到一起，关闭即可逐条查看。搜索计数仍按原始记录统计，F3 按命中组跳转；合并详情仅在首行保留日志头，复制/导出保留每条原始日志头。录制、完整来源导出及原始过滤规则保持原有语义。
- 默认缓存最多 200,000 条或约 64 MiB 字段/文本。实时搜索与导出范围为可用缓存；离线过滤与统计扫描完整文件，视图按 2,000 条分页。
- 列表采用带固定列头的单行表格，长字段显示省略号，选中后查看完整原文和高亮；Shift+滚轮可横向浏览列，详情换行可配置。每条最多展示/统计 1,000 处搜索匹配，异常超过 16 MiB 的文件单条显示受限并提示，原始文件保留。
- 录制从点击开始的边界起保存完整采集流，过滤和清屏不影响保存。默认约 100 MiB 一段、目录额度 2 GiB，达到后停止录制；额度在同目录多设备录制之间共享，并为元数据预留小量空间。
- 自动清理默认关闭。开启后仅删除已关闭、未标记保留、包含已知会话文件的受管目录，不触碰其他文件。崩溃残留的活动会话默认保留。
- 已有记录写入错误、队列过载和录制暂停都会有状态或事件；重连不保证补回设备已覆盖的日志，时间边界可能重复。
- 进程元数据从观察时起生效；初始缓冲区较早记录、权限不足或无法确认的包名显示未知。文本格式不能可靠区分每条日志的缓冲区，记录的是采集缓冲区集合。
- 离线阅读活动会话只读取打开时发现的片段；完整分析建议先停止录制。退出应用结束本工具的录制、采集和终端。

## 快捷键与终端

`Ctrl/Cmd+F` 搜索，`Ctrl/Cmd+L` 过滤，`F3/Shift+F3` 前后匹配，`Ctrl/Cmd+T/W` 新建/关闭页，`Ctrl+Tab/Ctrl+Shift+Tab` 切换，`Ctrl/Cmd+Shift+P` 暂停显示，`Ctrl/Cmd+End` 到末尾。设置中可调整绑定；终端焦点优先，不抢占 Ctrl+C。

终端使用配置的 ADB 启动本机服务，再通过 ADB Shell-v2 连接指定序列号；要求设备支持 Shell-v2。支持默认 TCP ADB 服务地址及相应环境变量，非 TCP 的自定义 ADB 服务套接字会明确提示暂不支持。

终端断线后需手动重连，不重放命令。终端直接键入不逐键持久化；可选历史只记录命令输入栏明确提交的命令，默认关闭。Windows/Linux 终端复制粘贴用 `Ctrl+Shift+C/V`，macOS 用 `Command+C/V`。

## 本地内存剖析

默认不启用诊断。需要分析时，在启动前设置 `LOGHELP_PROFILE_DIR` 为独立的本地目录：

```powershell
$env:LOGHELP_PROFILE_DIR = 'D:\logs\loghelp-memory'
./bin/loghelp.exe
Remove-Item Env:LOGHELP_PROFILE_DIR
```

目录内每 5 秒记录 Go 内存、图形后端和 GPU 纹理估算 `memory.jsonl`，前 10 分钟每 30 秒生成 `heap-*.pprof`，正常退出时再生成一份。使用 `go tool pprof -top -sample_index=inuse_space <文件>` 查看保留对象，`-sample_index=alloc_space` 查看累计分配。默认在堆采样前主动 GC；设置 `LOGHELP_PROFILE_GC=0` 可保留正常 GC 行为来核对实际占用，结束后清除该环境变量。Go 堆统计不包含全部图形驱动资源，GPU 纹理估算也不代表完整的驱动占用。诊断不开放 HTTP 或其他网络端口。

Windows 可以直接后台采样，复用已构建的 EXE，不触发编译：

```powershell
./scripts/profile-memory.ps1 -DurationSeconds 600 -NaturalGC
./scripts/profile-memory.ps1 -DurationSeconds 120 -NaturalGC -Scene settings
```

脚本使用独立配置副本、后台窗口与正常 GC，记录实际工作集及私有内存，并在结束后自动退出。`workspace.png` 来自应用自身绘图缓冲，不读取其他窗口或桌面。`-LegacyInputs` 仅用于诊断，复现原有整窗输入框缓冲以做对照；正常启动始终使用控件大小的缓冲。采样目录须为空，原偏好及录制文件不会被采样脚本改写。
