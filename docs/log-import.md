# 日志解压与解码

通过「打开日志文件」或拖入窗口打开日志、压缩包。ZIP、TAR、Gzip、Bzip2、Zstd（含 `.tar.gz`、`.tgz`、`.tar.bz2`、`.tbz2`、`.tar.zst`）内置解压；腾讯 Mars `.xlog` 内置 Go 解码，Windows、macOS、Linux 均无需安装 Python、OpenSSL 或解码脚本。

压缩包中的 `.txt`、`.log`、`.jsonl`、`.json`、`.xlog` 和已配置解码器的文件会合并到一个离线标签，支持原有分页、筛选、搜索、详情、书签和导出。ZIP 按条目名排序，TAR 按归档顺序读取，不按日志时间重排；LogHelp 会话包直接读取 JSONL 片段，不把 manifest 当作日志。常见 Mars 标准格式和 Android 紧凑格式可识别时间、级别、TAG；标准格式另识别 PID/TID，其余内容保留原文。

## JSONL 字段识别

普通业务 `.jsonl` 与 LogHelp 录制片段分别识别。LogHelp 片段保留原日志与录制序号；普通 JSONL 的详情、复制及文本导出保留整行 JSON，列表正文可以使用识别出的消息字段。未知结构、数组和标量仍按原文展示，不会因为 JSON 解析成功却字段不匹配而变成空日志。损坏行（包括尾部残片）保留原文，并记录解析警告。

默认按以下顺序选择第一个非 null 字段；字段名区分大小写：

| 内容 | 默认字段 |
|---|---|
| 正文 | `message`、`msg`、`log`、`text` |
| 级别 | `level`、`severity`、`log_level`；支持 INFO、WARN、ERROR 等文本及单字母形式 |
| 时间 | `time`、`timestamp`、`@timestamp`、`ts` |

例如 `{"msg":"请求失败","severity":"error","timestamp":"2026-10-29T10:00:00+08:00"}` 会识别正文、ERROR 级别与时间；其他业务字段仍完整保存在原文中。对象/数组正文和大整数不经过浮点数转换。

在「偏好设置 → 日志解压与解码」可为正文、级别、时间分别指定顶层字段名，留空使用默认列表。字段名按字面匹配，不解释点号为嵌套路径；指定字段缺失时不会继续猜测其他字段。保存设置后重新打开来源生效，不改变 LogHelp 原生录制格式。

时间支持 RFC3339（含时区）、`YYYY-MM-DD HH:MM:SS`（含可选小数秒）、日期，以及 Unix 数字时间。无时区文本按电脑本地时区解释；整数按绝对值推断秒（小于 10¹¹）、毫秒（小于 10¹⁴）、微秒（小于 10¹⁷）或纳秒，小数按秒且最多九位小数。无法识别的时间保持为空；原文不变。级别不猜测不同日志库的数字编码。

## Mars 私钥

在「偏好设置 → 日志解压与解码」选择一种方式，保存设置后打开或重新打开日志：

- **Mars 私钥文件**：本机文本文件，内容为 64 位十六进制私钥，可以有首尾空白。
- **Mars 私钥环境变量**：填写保存上述私钥的环境变量名称。应用从启动时继承的进程环境读取；修改系统环境变量后需重新启动应用。

不需要配置公钥。应用初始化 Mars 时使用的公钥无法替代解码私钥。设置只保存文件路径或变量名，不保存密钥内容；未加密日志不读取密钥。不同项目使用不同密钥时，切换引用后重新打开相应日志。

内置格式覆盖 Mars 的 `0x03–0x0d` 数据块：旧格式、同步明文、异步 Deflate，以及异步 Zstd；加密异步块使用 secp256k1 ECDH 与 Mars 的 16 轮 TEA。同步块遵循官方写入端，保持明文。解码失败会显示偏移和类型；缺失序号、跳过的损坏内容和尾部残片以离线警告提示，不冒充完整文件。自定义修改过的 Mars 格式可以改用外部工具。

## 外部解码器

在同一设置区点击「添加外部解码器」，填写名称、逗号分隔的扩展名、程序、参数 JSON 数组及输出方式。扩展名使用 `.customlog` 格式；同一扩展名只能配置一次。程序可使用 PATH 中名称，建议程序、脚本和密钥文件使用绝对路径。显式配置 `.xlog` 会优先使用外部工具。

例如，已有脚本接受输入和输出路径时，程序填写 Python 可执行文件路径，参数填写：

```json
["D:/tools/decode.py", "{input}", "{output}"]
```

macOS/Linux 对应填写当地 Python 路径及 `/absolute/path/decode.py`。本功能不分发外部工具；需要在各系统自行安装匹配架构的工具和依赖。内置 Mars 解码不需要这项配置。

| 配置 | 说明 |
|---|---|
| `{input}` | 当前输入文件的绝对路径，必填 |
| `{output}` | 工具应写入的临时文本文件路径；`file` 模式必填 |
| `{keyfile}` | 「工具密钥文件」路径，按工具的参数约定使用 |
| `file` | 工具写入 `{output}`，标准输出忽略 |
| `stdout` | 工具的标准输出直接作为文本日志，参数不能含 `{output}` |
| 工具密钥环境变量 | 填变量名，其值通过 `LOGHELP_LOG_KEY` 传给工具 |

输出应为 UTF-8 文本，诊断信息不要写到标准输出。参数按数组直接传递，不经过 Shell；包含空格的路径作为一个完整字符串填写，不再包嵌额外引号。工具工作目录为本次导入的临时目录。每次执行最长两分钟；外部工具错误输出不记录，避免带出密钥或日志内容。RAR、7z 和自定义二进制日志不提供内置解码，可接入能够输出文本的本机工具。

## 资源与交互

导入在后台执行，可在「日志操作 → 取消当前查询」中取消，也可关闭标签。每次导入最多四层压缩包嵌套、4096 个文件条目及临时文件、累计 4 GiB 解压/解码内容；Mars 单个输入块最多 16 MiB、解压后最多 64 MiB。遇到越界路径、链接、损坏压缩包或资源超限时失败并提示。原文件保持不变，临时文本保留到标签关闭、切换来源或正常退出。异常退出可能遗留系统临时目录中的 `loghelp-import-*`，确认应用退出后可清理。

最近文件、标签恢复和书签保存原压缩包或 xlog 路径；重新打开时重新解码。导出不能覆盖原压缩包或源日志。

## 实现与第三方依赖

格式依据 [Mars 解码脚本](https://github.com/Tencent/mars/blob/master/mars/xlog/crypt/decode_mars_crypt_log_file.py)、[Mars 写入与加密实现](https://github.com/Tencent/mars/blob/master/mars/xlog/crypt/log_crypt.cc)、[Mars Zstd 写入](https://github.com/Tencent/mars/blob/master/mars/xlog/src/log_zstd_buffer.cc)、[Mars 日志格式](https://github.com/Tencent/mars/blob/master/mars/xlog/src/formater.cc)。xlog 的 Zstd 流缺少帧结束标记时，仅在压缩块边界完整且无待校验的 checksum 时补空结束块，不容忍块内截断；普通 `.zst` 文件仍严格校验。不内置任何示例或业务私钥。

新增依赖固定为 `github.com/decred/dcrd/dcrec/secp256k1/v4 v4.4.1`（ISC）及 `github.com/klauspost/compress v1.20.3`（BSD-3-Clause，部分源文件含其上游许可证说明），完整性记录在 `go.sum`。这两项编译进程序，不需要额外分发动态库，继续支持 `CGO_ENABLED=0`。

Mars 兼容解码中的协议及 TEA 逻辑参考腾讯 MIT 实现，保留其许可声明：

> Copyright (C) 2016 THL A29 Limited, a Tencent company. All rights reserved.
>
> Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:
>
> The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.
>
> THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
