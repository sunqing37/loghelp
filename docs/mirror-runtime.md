# 设备投屏运行依赖

LogHelp 使用 scrcpy 3.3.4 的 Android 服务，Go 代码管理 ADB 转发、H.264 包、持续触摸控制、最新帧缓冲和 Ebitengine 绘制。截图另用 ADB PNG 输出获取原始分辨率，不从视频缩放帧生成。

截图兼容部分多屏设备在 PNG 前输出文本警告的行为：只允许剥离最多 16 KiB 的可打印 UTF-8 文本前缀，之后仍完整校验 PNG 头、尺寸和图像数据，保存/复制使用干净的原始 PNG。无图片而只返回文本错误时，界面显示有长度限制的命令提示；不把二进制杂质、超长前缀或损坏图片当作正常截图。

录屏直接复用投屏的 H.264 编码包，以纯 Go 的 [mp4ff v0.59.0](https://github.com/Eyevinn/mp4ff/tree/v0.59.0) 写入带时长和关键帧索引的分片 MP4，不新增外部程序、Android 服务或 CGO 依赖，Windows/macOS/Linux 共用实现。视频保留投屏分辨率（最长边上限 1280），不含音频，也不提供原分辨率录屏选项。编码器关键帧间隔设为 1 秒，开始后等待下一个关键帧；超过 15 秒未收到关键帧会报告失败。录屏不设置三分钟时限；实际持续时间取决于设备连接与本地磁盘空间。

折叠/展开、旋转及分辨率变化不中断录制：使用 avc3 描述，每个关键帧携带当前 SPS/PPS，编码配置更新后等待新关键帧，再继续写入同一个 MP4。过渡期间显示「切屏中」，上一帧延续到恢复后的时间点；跳过缺少新关键帧的画面，避免沿用旧参考帧。正常设备时间戳保留其间隔，配置更新后的编码器时间戳重置则重排到同一条递增时间线上，重置间隔参考实际接收时间及上一帧间隔。切屏后 15 秒仍没有关键帧会结束并保存此前内容。生成的视频分辨率随画面变化，播放器需支持 avc3 参数更新；FFmpeg 解码和跨切屏位置跳转的验收见手动记录，未保证所有系统播放器均已验收。

先选保存位置，录制中后台写入目标目录的 `.loghelp-recording-*.mp4`，正常结束并刷新成功后才替换目标文件；无有效视频或写入失败不会覆盖旧文件。暂停预览仅暂停显示，不暂停录屏；关闭、重连、切换和退出时结束并收尾。无效编码配置、没有配置边界的时间戳异常时保存此前片段并提示；连接断开、写入队列超过 240 包或 32 MiB 时结束并提示，避免阻塞投屏或丢掉帧后继续生成损坏视频。保存/改名失败会提示临时文件位置（有已写入帧时保留），不能保证磁盘故障后的临时文件完整；强制结束程序也不等同于正常停止保存。

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
- [mp4ff v0.59.0](https://github.com/Eyevinn/mp4ff/tree/v0.59.0)，MIT；完整许可见 [licenses/mp4ff.txt](licenses/mp4ff.txt)。
- [FFmpeg 7.1.1](https://github.com/FFmpeg/FFmpeg/tree/n7.1.1)，本次官方附件中的配置未启用 GPL，使用 LGPL 许可；运行库附 LICENSE.md、LGPLv2.1/LGPLv3 文本和来源信息。
- FFmpeg 官方包还包含 [dav1d](https://code.videolan.org/videolan/dav1d) 和 [zlib](https://github.com/madler/zlib) 的代码，随资源保留对应许可文本。

从其他来源分发 FFmpeg 程序时，应保留该构建实际适用的许可和源代码入口，不能沿用上述最小官方解码库的许可说明。

只读诊断（不改变手机界面或剪贴板）：`loghelp --check-mirror --serial <序列号> --duration 8s`；记录接收帧数、首帧耗时和分辨率，不当作目标平台 GUI、触摸或粘贴验收。`LOGHELP_MIRROR_DEBUG=1` 可保留 FFmpeg 后备路径的解码上下文用于定位失败。
