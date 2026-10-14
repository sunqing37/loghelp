# 设备投屏运行依赖

LogHelp 使用 scrcpy 3.3.4 的 Android 服务，Go 代码管理 ADB 转发、H.264 包、持续触摸控制、最新帧缓冲和 Ebitengine 绘制。截图另用 ADB PNG 输出获取原始分辨率，不从视频缩放帧生成。

| 平台 | 解码 | 图片剪贴板 |
| --- | --- | --- |
| Windows amd64 | 默认内嵌 FFmpeg 7.1.1 动态库；也可指定 FFmpeg 可执行程序 | Windows PNG 和 CF_DIB |
| Windows arm64 | 匹配架构的 FFmpeg 7 动态库或 FFmpeg 可执行程序 | Windows PNG 和 CF_DIB |
| macOS amd64/arm64 | 匹配架构的 FFmpeg 7 动态库，或 FFmpeg 5+ 可执行程序；识别 Homebrew 路径 | AppKit NSPasteboard/NSImage |
| Linux amd64/arm64 | FFmpeg 7 动态库，或 FFmpeg 5+ 可执行程序 | Wayland：wl-clipboard；X11：xclip |

原生绑定通过 purego 调用，检查 avcodec 61 / avutil 59 的公共 ABI；可执行程序后备路径通过标准输入接收 H.264，通过 PAM 帧头和 RGBA 像素输出处理尺寸变化，不依赖 FFmpeg 内部结构。两条路径均不需要项目启用 CGO。FFmpeg 帧率受设备内容、USB/无线连接、编码器和解码性能影响；配置上限不是实际运行帧率保证。

默认运行库释放到用户缓存的 `loghelp/mirror-3.3.4`，每次检查内嵌文件哈希。替代库可以放入 `LOGHELP_MIRROR_RUNTIME` 指定目录，该目录必须有对应版本 `scrcpy-server`；不会以内嵌文件覆盖用户指定目录，支持替换/重新构建兼容库。可执行程序查找顺序：`LOGHELP_FFMPEG`、程序旁 `runtime/ffmpeg`、macOS Homebrew 常见位置、PATH。发行包可以附带适合该平台/架构的 `runtime/ffmpeg` 和许可文件；本次只生成了自带解码库的 Windows amd64 EXE，不宣称其他平台已生成独立发行包。

`scripts/prepare-mirror.py` 使用官方 [scrcpy 3.3.4 附件](https://github.com/Genymobile/scrcpy/releases/tag/v3.3.4)，固定 Windows ZIP 的 SHA-256 为 `d8a155b7c180b7ca4cdadd40712b8750b63f3aab48cb5b8a2a39ac2d0d4c5d38`，仅提取 Android 服务、avcodec/avutil/swresample 和相应许可；不捆绑另一份 ADB 或 SDL。Android 服务归入所有架构共用资源，Windows 动态库只进入 Windows amd64 构建。生成的 ZIP 可由脚本重建，不提交到版本库；Python/PowerShell 构建入口都准备资源并把资源内容纳入构建指纹。

第三方源代码与许可证：

- [scrcpy 3.3.4](https://github.com/Genymobile/scrcpy/tree/v3.3.4)，Apache-2.0。
- [FFmpeg 7.1.1](https://github.com/FFmpeg/FFmpeg/tree/n7.1.1)，本次官方附件中的配置未启用 GPL，使用 LGPL 许可；运行库附 LICENSE.md、LGPLv2.1/LGPLv3 文本和来源信息。
- FFmpeg 官方包还包含 [dav1d](https://code.videolan.org/videolan/dav1d) 和 [zlib](https://github.com/madler/zlib) 的代码，随资源保留对应许可文本。

从其他来源分发 FFmpeg 程序时，应保留该构建实际适用的许可和源代码入口，不能沿用上述最小官方解码库的许可说明。

只读诊断（不改变手机界面或剪贴板）：`loghelp --check-mirror --serial <序列号> --duration 8s`；记录接收帧数、首帧耗时和分辨率，不当作目标平台 GUI、触摸或粘贴验收。`LOGHELP_MIRROR_DEBUG=1` 可保留 FFmpeg 后备路径的解码上下文用于定位失败。
