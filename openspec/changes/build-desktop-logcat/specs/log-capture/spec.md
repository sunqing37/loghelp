## ADDED Requirements

### Requirement: Structured buffer capture
系统 SHALL 持续采集可配置缓冲区，默认 main/system/crash，并探测设备支持情况；保留时间、PID/TID、级别、Tag、消息、多行边界和原始文本，以及可获取的包名、进程名。不支持的选项或解析失败 MUST 明确提示并保留可读原始记录，首次连接不得自动清空设备日志。

#### Scenario: Unsupported buffer
- **WHEN** 设备不支持用户请求的缓冲区
- **THEN** 系统说明具体选项并允许调整，不能把采集失败显示为成功的空日志

#### Scenario: Multiline or unparsed log
- **WHEN** 收到多行异常堆栈或不能解析的文本
- **THEN** 系统保留完整文本和顺序；未解析条目可查看、文本搜索和保存

### Requirement: Shared collection lifecycle
系统 SHALL 对同一设备及相同采集配置复用采集流，标签页和录制器独立订阅；只关闭当前订阅不影响其他订阅。停止整个共享会话 MUST 显示受影响视图和录制，提供只停止当前订阅的选择。

#### Scenario: Two tabs for one device
- **WHEN** 两个标签页使用相同设备和缓冲区
- **THEN** 共享一份采集流，各自过滤独立，关闭其中一页不停止另一页的采集

#### Scenario: Different capture configurations
- **WHEN** 同一设备的标签页选择不同缓冲区集合
- **THEN** 系统按采集配置区分会话，不能以复用为由忽略任一页的配置

### Requirement: Display pause and scroll
系统 SHALL 区分暂停显示、暂停录制和停止采集。暂停显示冻结有界视图快照，后台采集和录制继续；向上滚动后关闭跟随，提供回到最新日志入口。恢复显示时 MUST 提示当前缓存范围以及已淘汰数量。

#### Scenario: Inspect old log while recording
- **WHEN** 用户暂停显示或向上滚动，设备继续输出且录制开启
- **THEN** 当前查看位置稳定，文件继续新增；恢复后可以跳到最新，并看到缓存淘汰信息

### Requirement: Separate clear operations
系统 SHALL 将清空当前视图和清空设备 Logcat 缓冲区作为不同操作。清屏只改变当前视图起点；设备缓冲区清空 MUST 明确设备、影响范围及确认入口。

#### Scenario: Clear one tab
- **WHEN** 用户清空一个标签页的显示
- **THEN** 其他页、已有录制文件和设备缓冲区保留，当前页只显示起点后的条目

### Requirement: Bounded memory and observable overload
系统 SHALL 对显示缓存、暂停快照、事件队列和索引使用有界策略，默认缓存上限 200,000 条或估算 64 MiB，任一达到时淘汰较旧内容。界面 MUST 使用虚拟列表与批量更新，显示截断、淘汰或过载信息；显示淘汰不影响完整录制。

#### Scenario: Sustained high throughput
- **WHEN** 手动回放约 5,000 行/秒、平均 300 字节的日志并持续 10 分钟
- **THEN** 记录运行环境及实测表现，缓存遵守上限、内存趋稳且可操作；录制失败或丢失必须有解释和可见状态
