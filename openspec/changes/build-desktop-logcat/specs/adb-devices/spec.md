## ADDED Requirements

### Requirement: ADB discovery and configuration
系统 SHALL 优先使用已配置且有效的 ADB，否则在未配置时从当前进程 PATH 查找；允许选择可执行文件或其目录，校验版本，保存绝对路径并显示实际路径和版本。缺失、无执行权限或已配置路径失效时 MUST 提供修复入口，不修改系统 PATH 或静默切换已配置版本。

#### Scenario: ADB available in PATH
- **WHEN** 用户未配置 ADB 且 PATH 中存在可正常执行的 ADB
- **THEN** 系统自动使用它并显示路径及版本，无需用户配置

#### Scenario: ADB unavailable or invalid
- **WHEN** 没有可用 ADB 或已保存路径失效
- **THEN** 系统提示选择路径并说明原因，允许重新选择或显式恢复 PATH 查找，校验成功后才启用设备操作

### Requirement: Device enumeration and explicit targeting
系统 SHALL 自动更新设备名称、序列号、USB/网络连接信息及授权/在线状态；所有日志和 Shell 操作 MUST 显式绑定序列号。系统 MUST 不自动执行影响其他工具的 adb kill-server。

#### Scenario: Two connected devices
- **WHEN** 两台设备同时在线且用户选择其中一台
- **THEN** 日志和新建终端明确显示所选序列号，操作不落到另一台设备

#### Scenario: Device unauthorized
- **WHEN** 设备未授权或离线
- **THEN** 列表显示相应状态及授权/连接指引，不将其显示为正常采集中

### Requirement: Connection recovery
系统 SHALL 保留断线前日志，对曾在线且有订阅或录制的设备按序列号退避重连；记录断线与恢复事件。重连不能确定连续性时 MUST 显示缺口或重复风险。

#### Scenario: Device disconnected and returned
- **WHEN** 已采集设备断开后重新在线
- **THEN** 原标签页保留旧数据并恢复采集，新增连接事件；缓冲区被覆盖时不宣称已补齐全部日志

### Requirement: Package and process tracking
系统 SHALL 提供应用包名选择及可获取的进程信息；应用选择按完整包名匹配并跟踪重启后的新 PID 和多个所属进程。映射 MUST 绑定会话、连接代次及有效时间，无法获取时显示未知。

#### Scenario: Application restarted
- **WHEN** 用户选择的应用退出后以新 PID 启动
- **THEN** 日志视图继续匹配新进程，保留此前日志并显示进程变化事件

#### Scenario: PID reused or metadata unavailable
- **WHEN** PID 被其他应用复用或进程查询失败
- **THEN** 系统不使用失效映射给新日志标注旧包名，无法确认的字段显示未知
