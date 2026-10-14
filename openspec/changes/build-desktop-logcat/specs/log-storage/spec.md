## ADDED Requirements

### Requirement: Full-stream local recording
系统 SHALL 允许选择目录，校验可写性后录制从启动边界起的所选缓冲区完整采集流，与显示过滤、搜索、暂停和清屏无关。格式 MUST 为带版本的 UTF-8 JSONL 及会话元数据，保存设备身份、时区、原始文本、结构化字段和可用进程映射，避免同名文件覆盖。

#### Scenario: Change filter while recording
- **WHEN** 用户录制时将显示过滤为 ERROR 并暂停显示
- **THEN** 文件仍保存包括 INFO 等在内的完整采集流，原有文件不被覆盖

#### Scenario: Start in invalid directory
- **WHEN** 用户选择不可写或不存在且无法创建的目录
- **THEN** 系统解释原因并保持未录制状态，允许重新选择

### Requirement: Rotation and retention
系统 SHALL 按大小或时间轮转，默认约 100 MiB 一段且不拆开单条日志；默认目录额度 2 GiB，达到后停止该目录新增录制并提示，默认不自动删除。用户启用容量/天数淘汰后 MUST 只清理本工具管理、非活动且未标记保留的会话。

#### Scenario: Segment threshold reached
- **WHEN** 当前片段达到轮转阈值
- **THEN** 后续完整条目写入下一片段，文件顺序及会话元数据连续

#### Scenario: Directory quota reached
- **WHEN** 未启用自动清理且录制目录达到额度
- **THEN** 录制停止并可见提示，已有文件和实时查看保留，用户可调整额度或目录恢复

#### Scenario: Retention cleanup enabled
- **WHEN** 用户启用七天保留策略且目录包含过期会话、活动会话、标记保留会话及其他文件
- **THEN** 仅清理符合条件的过期受管会话，不删除其他三类内容

### Requirement: Recording controls and failure recovery
系统 SHALL 提供录制状态、已写字节、目录和暂停/继续/停止入口，后台录制可在无标签页时管理。写入失败或队列持续过载 MUST 停止录制，说明最后成功位置和未写入数量，实时查看继续；恢复到新目录创建新片段并标明间隔。

#### Scenario: Disk full or permission removed
- **WHEN** 写盘过程中磁盘满或权限被撤销
- **THEN** 系统显示录制故障及未写入范围，不保持虚假的录制中状态，用户可选新目录恢复

#### Scenario: Pause recording
- **WHEN** 用户暂停录制后继续
- **THEN** 采集和显示不中断，文件元数据记录录制暂停造成的缺口

### Requirement: Offline loading and large-file queries
系统 SHALL 无需 ADB 即可打开本工具会话及标准 threadtime 文本，保留过滤、搜索、高亮、时间跳转和上下文功能；读取 MUST 分页并提供可取消的后台查询。无法解析的字段显示未知；损坏文件保留可读内容并说明损坏位置，不改写源文件。

#### Scenario: Review without connected device
- **WHEN** 无 ADB 或设备时打开多片段会话
- **THEN** 日志按会话顺序查看并可查询，已保存进程映射支持包名过滤

#### Scenario: Truncated final record
- **WHEN** 文件因异常退出留下不完整尾行
- **THEN** 已完成条目仍可查看，界面报告尾行损坏而不丢弃整个会话

### Requirement: Export selected or filtered logs
系统 SHALL 独立导出选中内容或当前过滤结果为 UTF-8 文本或结构化文件，并明确来源范围和条目数量。导出 MUST 不覆盖原始录制文件；目标其他文件已存在时提供覆盖确认。

#### Scenario: Export ERROR results
- **WHEN** 用户导出当前 ERROR 过滤结果
- **THEN** 输出只包含该来源范围内的匹配条目，完整原始录制保持
