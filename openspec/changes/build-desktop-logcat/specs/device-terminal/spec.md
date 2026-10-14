## ADDED Requirements

### Requirement: Device-bound interactive shell
系统 SHALL 提供真正交互式的设备 Shell，终端标题持续显示名称和序列号，支持输入、ANSI 显示、复制粘贴、Ctrl+C 及终端尺寸调整；无需用户在宿主终端额外操作。切换日志页 MUST 不改变已打开终端的设备绑定。

#### Scenario: Switch log device during shell session
- **WHEN** 设备 A 的终端已打开且用户切换日志页到设备 B
- **THEN** 原终端仍绑定 A，新建 B 终端单独显示 B，命令不会发送到错误设备

#### Scenario: Interrupt and resize
- **WHEN** 用户运行长时间命令，调整窗口后按 Ctrl+C
- **THEN** 远端终端尺寸随之调整且命令可被中断，Shell 返回可输入状态

### Requirement: Shell lifecycle separate from logs
系统 SHALL 在关闭终端或设备断线时结束对应 Shell 并显示退出原因，释放本工具创建的资源，日志采集及其他终端不受影响。重连 MUST 由用户显式创建或恢复 Shell，不自动重放任何历史输入。

#### Scenario: Disconnect during command
- **WHEN** Shell 执行过程中设备断开后重新在线
- **THEN** 原终端显示断线结束，日志可自动重连，但命令不自动重新执行

### Requirement: Local command history control
系统 SHALL 支持本地命令历史、关闭保存和清除历史；历史仅包含用户明确提交的命令，不记录逐键输入或上传内容。

#### Scenario: Disable history
- **WHEN** 用户关闭历史保存后提交命令
- **THEN** 命令正常执行且不新增持久化历史，既有历史可单独清除
