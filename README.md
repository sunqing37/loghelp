# LogHelp

独立的 Android Logcat 桌面工具，界面和核心均使用 Go，采用 Ebitengine + EbitenUI。Windows、macOS、Linux 编译均可关闭 CGO，仅需 Go 编译器。无需 Node.js、TypeScript、浏览器或 Android Studio。

## 下载

当前版本 **0.0.2**（`VERSION=0.0.2`、`REVISION=0`）。在 [GitHub Releases](https://github.com/sunqing37/loghelp/releases/tag/v0.0.2) 下载与系统/CPU 匹配的便携包：Windows、macOS（文件名使用 `darwin`）、Linux 各提供 amd64 和 arm64。Windows 解压运行 `loghelp.exe`，macOS/Linux 解压运行 `./loghelp`；若解压工具未保留权限，先执行 `chmod +x loghelp`。ADB 需另行准备，可在设置中选择其位置。附件 `SHA256SUMS.txt` 提供六个 ZIP 的 SHA-256。

见 [0.0.2 发布说明](docs/releases/0.0.2.md)、[投屏运行依赖](docs/mirror-runtime.md) 和 [更新记录](CHANGELOG.md)。Windows amd64 内嵌投屏解码库；其余平台投屏需安装匹配架构的 FFmpeg 或配置 `LOGHELP_FFMPEG`。Linux 图片复制另需 wl-clipboard 或 xclip，系统文件对话框另需 zenity/matedialog/qarma。

源码构建时会将 `VERSION` 和 `REVISION` 嵌入程序，版本及修订显示在窗口标题、主界面顶部和设置页。修改任一文件后，构建脚本会识别变更并重新编译。检查更新先比较产品版本，同版本再按整数比较修订号，选择本平台最高修订附件；历史原始包没有修订后缀，视为 `r0`。

已经下载的旧 `r2` 只比较产品版本，无法识别同为 `0.0.1` 的 `r3`，需要手动下载并替换一次。使用 `r3` 后，后续 `r4` 等同版本修订可以通过应用内更新替换。

设置页提供“检查更新”，打开独立弹窗，在后台检查 GitHub 最新正式 Release，显示版本和发布说明摘要；关闭后返回原设置页，保留未保存内容和滚动位置。Windows 便携版发现新版后可点击“下载更新并重启”：确认后下载并校验 SHA-256，结束采集、录制和终端，由独立更新进程替换 EXE 并重启，替换或启动失败时恢复旧程序。需要程序目录可写，旧 EXE 保留为备份；其他平台或缺少可校验附件时可手动下载。检查仅在点击按钮时进行，超时或网络错误会显示提示，重启前需先保存设置。

设置页的“更新网络”可选择环境变量、直连或自定义代理。自定义地址支持 `http://`、`https://`、`socks5://`、`socks5h://`，例如 `http://127.0.0.1:7890`（端口以代理软件实际配置为准）；检查更新会立即使用当前输入，点击“保存设置”后长期生效，下载更新包也使用同一代理。环境变量模式读取 `HTTPS_PROXY` / `HTTP_PROXY` / `NO_PROXY`，不主动读取操作系统的系统代理设置。临时连接、读取失败或服务器暂时故障最多尝试 3 次，每次超时 15 秒，两次重试分别等待 1 秒、2 秒；限流、代理认证和证书错误不自动重试，并显示具体中文原因及可获取的限流恢复时间。检查失败时仍可点击“打开发布页手动下载”，关闭更新弹窗后保留设置草稿；关闭或切换弹窗会取消正在进行的检查。

界面主题可选择“跟随系统”“深色”“浅色”，新配置默认跟随系统，已有明确的深色/浅色选择保持不变。跟随系统通过 Ebitengine 读取 Windows 应用主题、macOS 外观、Linux GSettings/GTK 偏好，系统主题变化后自动更新主界面、日志颜色和详情选区；有弹窗或正在编辑/拖动时延后应用并保留输入。无法识别系统偏好时使用浅色。

Windows EXE 已嵌入与窗口一致的蓝色 `L` 图标。图标源文件为 `assets/loghelp.ico`，多尺寸 PNG/ICO 由 `python scripts/generate-icon.py` 根据现有绘图规则生成；`cmd/loghelp/rsrc_windows_amd64.syso` 和 `rsrc_windows_arm64.syso` 是可直接参与构建的资源文件。

协作规则见 [AGENTS.md](AGENTS.md) 和 [.agent 目录](.agent/README.md)，其中包含 [提交规范](.agent/rules/commit-message.md) 及 [GitHub 发布流程](.agent/workflows/github-release.md)。发布包由 `python -X utf8 .agent/scripts/package-release.py --all` 基于六种完整桌面构建记录生成，不重复编译；不带 `--all` 默认打包 Windows amd64。

## 当前功能

- 从 PATH 自动查找 ADB，支持选择 executable 或 platform-tools 目录、版本校验和设置保存。
- 设备状态与热插拔、指定序列号采集、应用包名选择、进程变化事件、断线重试及中断标记。
- 设备侧栏「Wi-Fi」支持地址直连、Android 11+ 扫码/配对码配对、USB 切换 TCP 调试和断开指定无线设备；连接后复用日志、终端与投屏功能。
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

## Wi-Fi 连接设备

日志设备侧栏点击「Wi-Fi」，手机和电脑需处于同一可互访网络，使用支持无线配对的新版 Android SDK Platform-Tools。连接流程参考 [Android 官方 ADB 说明](https://developer.android.com/tools/adb)：

- **Android 11+ 扫码**：工具选择「Android 11+ 扫码配对」，手机打开「开发者选项 → 无线调试 → 使用二维码配对」扫描电脑上的二维码。工具通过 ADB mDNS 只匹配本次二维码对应的设备并完成配对，ADB 自动尝试连接；若未出现在线设备，可使用「地址直连」。二维码本地生成、凭据不保存，有效期 2 分钟，可刷新；关闭窗口停止等待，已完成的配对不会撤销。mDNS 不可用时提示更新 Platform-Tools、检查发现能力或改用配对码；网络隔离/防火墙阻止发现时可能等待超时。
- **Android 11+ 配对码**：手机打开「开发者选项 → 无线调试 → 使用配对码配对」，在工具中输入该页面的 `IP:配对端口` 和 6 位配对码。成功后返回手机无线调试主页，再用「地址直连」输入主页显示的 `IP:连接端口`；配对端口不能当作连接端口使用。
- **已配对 / 已开启 TCP 调试**：选择「地址直连 / 重新连接」，输入 `IP:端口`。IPv6 地址使用 `[地址]:端口`。Android 11+ 无线调试端口可能变化，重新连接时以手机当前页面为准。
- **通过 USB 开启**：先用数据线连接并允许 USB 调试，选择「USB 切换 Wi-Fi」和目标真机，输入手机的 Wi-Fi `IP:端口`（通常为 `5555`）。此步骤重启设备 adbd，可能中断该设备现有会话；连接成功后再拔线，仅在可信网络使用。

成功后关闭连接窗口，在「选择设备」中选中标记为 `Wi-Fi` 的在线设备，即可采集日志；终端和投屏也能选择该设备。连接操作在后台执行，普通操作最长 30 秒，扫码等待最长 2 分钟；关闭/切换窗口会取消当前请求，已经完成的配对或 TCP 模式切换不会自动撤销。失败时核对地址、调试授权、防火墙和网络是否隔离。

「断开指定无线设备」仅断开选中的 ADB 网络连接，会中断该连接上的采集、录制、终端和投屏，不关闭手机调试开关或清除配对记录。Android 无线调试的自动发现可能再次建立连接；要彻底禁用，请在手机关闭无线调试，传统 TCP 模式可在 USB 连接后执行 `adb -s <USB序列号> usb` 或重启手机。

扫码协议参照 [AOSP ADB Wi-Fi 架构](https://android.googlesource.com/platform/packages/modules/adb/+/refs/heads/main/docs/dev/adb_wifi.md)。二维码编码使用纯 Go 的 [go-qrcode](https://github.com/skip2/go-qrcode)，固定提交 `da1b6568686e`，由 `go.sum` 校验，无新增运行时外部程序；许可证见 [go-qrcode MIT 许可](docs/licenses/go-qrcode.txt)。

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
python scripts/build.py --desktop --run            # 构建后打开本机程序
python scripts/build.py --desktop --cross-desktop --target-os linux --arch arm64 # 明确授权的交叉链接
```

默认目标为当前 Go 主机系统、amd64。跨系统完整链接需显式传入 `--cross-desktop`（PowerShell 为 `-CrossDesktop`），交叉构建不代替目标平台运行验收。Windows amd64 输出 `bin/loghelp.exe`，其余输出 `bin/<os>-<arch>/loghelp`（Windows 为 `loghelp.exe`），避免架构间覆盖。Python 脚本始终关闭 CGO，使用 UTF-8 输出与参数列表调用编译器；在 Windows 使用系统 PowerShell/CIM 检查进程，在 Linux/macOS 使用 `/proc` 或 `ps`。发现其他编译时默认等待最多 600 秒，可通过 `--wait-timeout` 调整。同一源码的成功构建会跳过，PowerShell 入口也委托相同 Python 实现；构建脚本之间另有系统文件锁，退出时自动释放。`--dry-run` 不编译、不启动程序、不写构建记录。

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

## 启动与单实例

同一用户缓存目录只运行一个 LogHelp 主实例，不受 EXE 所在目录影响。重复打开会通知现有窗口恢复（若已最小化）并提醒用户，新进程随后退出。系统锁在正常退出或崩溃时自动释放，残留的 `instance.lock` 文件无需手动删除。通知失败时也不创建第二个主实例。

实例锁位于用户缓存目录的 `loghelp/instance.lock`，通知使用本机回环连接和随机凭据。`--version`、`--check-update`、设备诊断命令和自动更新辅助进程保留独立入口，不占用 GUI 实例锁。升级到支持单实例的新版本后，请先退出此前的旧程序再启动；旧版不会参与新的实例锁协议。

## 发布说明排版

版本更新弹窗支持 CommonMark/GFM、表格、脚注、提示块、代码高亮、KaTeX 数学公式及 Mermaid 图表。首选系统 Edge/Chrome/Chromium，渲染库和字体离线内嵌；浏览器不可用或排版失败时自动使用原生排版，保留表格、文字和链接，公式/图表显示源码。详细语法、运行依赖及边界见 [Markdown 说明](docs/markdown.md)。

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
