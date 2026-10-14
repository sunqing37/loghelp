## ADDED Requirements

### Requirement: Field filters and query logic
系统 SHALL 支持 tag/package/process/message/line/level/age、pid/tid 及绝对时间范围过滤，支持引号短语、排除、正则、与/或及括号。level 表示不低于指定级别；同键非否定条件隐式 OR，其他条件隐式 AND，显式 AND 优先于 OR。纯文本按消息包含匹配，多词为 AND；默认不区分大小写并允许切换。绝对时间范围 MUST 起点包含、终点不包含。

#### Scenario: Combined field expression
- **WHEN** 用户输入 `package:com.example.app level:WARN -tag:Noise`
- **THEN** 只显示包名包含指定文本、WARN 及以上且 Tag 不包含 Noise 的条目

#### Scenario: Repeated tags and parentheses
- **WHEN** 用户输入 `tag:Audio tag:Bluetooth package:com.example.app`
- **THEN** 查询等同 `(tag:Audio | tag:Bluetooth) & package:com.example.app`，括号允许显式改变组合关系

### Requirement: Validation and query assistance
系统 SHALL 提供字段提示、示例、历史和命名收藏，采用 Go RE2 正则语义并说明兼容范围；第一版不支持 package:mine 及 is 特殊查询。无效语法或正则 MUST 标出错误位置，保留最后有效结果并注明新条件未生效。

#### Scenario: Invalid regular expression
- **WHEN** 用户输入不完整正则或不支持的环视
- **THEN** 显示错误原因和位置，原有效结果保持，不把错误解释为零条匹配

#### Scenario: Reuse saved filter
- **WHEN** 用户收藏并命名查询后重新启动应用
- **THEN** 收藏及查询内容仍可选择并应用

### Requirement: Search and highlight separately from filters
系统 SHALL 在当前过滤结果内提供独立搜索，支持字面量、大小写、正则、高亮、匹配数量和前后导航；搜索不改变过滤和录制。大范围搜索 MUST 可取消并显示进度，声明搜索范围为当前缓存或离线文件。

#### Scenario: Find next occurrence
- **WHEN** 用户在已过滤结果中搜索 timeout 并执行下一处
- **THEN** 定位并高亮下一个命中，显示匹配进度/数量，过滤条件及采集流保持

#### Scenario: Search includes Unicode
- **WHEN** 消息包含中文及多字节字符并产生匹配
- **THEN** 高亮准确对应字符，不能因字节偏移切坏字符

### Requirement: Context and time navigation
系统 SHALL 允许对命中条目查看前后未过滤上下文，并跳转指定时间；上下文数量可调整。目标超出可用来源或已经淘汰时 MUST 提示并提供打开录制文件入口。

#### Scenario: Filter hides cause
- **WHEN** 用户选中一条 ERROR 并查看上下文
- **THEN** 独立面板显示附近 INFO/DEBUG 等原始条目，而不永久改变当前过滤器

### Requirement: Explicit relative-time semantics
系统 SHALL 对实时 age 使用主机当前时间，对离线 age 使用最后一条可解析设备时间为固定锚点；界面显示时区和锚点。设备时钟异常时 MUST 提示，不能通过静默改写原始时间掩盖偏差。

#### Scenario: Open old session
- **WHEN** 用户打开过去的录制并输入 `age:5m`
- **THEN** 显示该会话末尾五分钟内的日志，不因当前日期已过而得到无意义的空结果
