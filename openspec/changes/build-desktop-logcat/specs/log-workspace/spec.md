## ADDED Requirements

### Requirement: Cross-platform desktop workspace
系统 SHALL 在 Windows、macOS、Linux 提供桌面界面，包含设备/应用选择、过滤栏、日志区域、搜索栏及采集/录制状态。系统 SHALL 支持深浅色、字体大小、自动换行、时间/PID/TID/Tag/包名等列配置和级别配色，并展示未知字段。

#### Scenario: Customize display
- **WHEN** 用户改变字体、主题、换行或列配置
- **THEN** 日志视图立即更新且偏好可保存，不改变原始采集和文件内容

### Requirement: Independent and manageable tabs
系统 SHALL 支持新建、复制、重命名、排序和关闭实时日志、离线日志及终端标签页；实时页各自保存设备、应用、过滤、搜索及滚动位置，复制页继承配置而非复制采集进程。

#### Scenario: Compare two queries
- **WHEN** 用户复制同设备标签页并设置另一过滤条件
- **THEN** 两页可独立切换和查询，共享采集且各自滚动状态保持

### Requirement: Background recording and exit
系统 SHALL 在最后一个日志标签页关闭时允许已有录制继续，并在全局录制入口展示。退出应用 MUST 结束本工具的采集、Shell 和录制，完成可写数据刷新及文件关闭；不是退出后常驻录制服务。

#### Scenario: Close last tab while recording
- **WHEN** 用户关闭设备最后一个日志标签页但录制尚未停止
- **THEN** 录制继续，可从全局入口查看和停止

#### Scenario: Exit with active sessions
- **WHEN** 用户退出应用
- **THEN** 采集和终端子进程释放，录制文件正常结束或报告刷新失败，不终止其他工具的 ADB 服务

### Requirement: Restore configuration without replaying actions
系统 SHALL 恢复标签页配置、过滤收藏及显示偏好；失效设备或文件显示占位和重新定位入口。启动 MUST 不自动执行 Shell 命令或开始录制，恢复实时日志订阅时显示连接状态。

#### Scenario: Restart with missing device
- **WHEN** 应用重启但上一会话设备未连接
- **THEN** 对应标签页和条件保留并显示等待设备；录制未开启，Shell 命令不重放

### Requirement: Keyboard navigation and focus
系统 SHALL 提供搜索、匹配导航、过滤聚焦、新建/关闭页、切换页、暂停显示和跳到末尾的快捷键及帮助入口；使用各平台惯用修饰键，允许调整有冲突的绑定。终端焦点 MUST 优先处理终端按键，不能用全局快捷键吞掉 Ctrl+C 等输入。

#### Scenario: Search with keyboard
- **WHEN** 用户在日志页按 Ctrl/Cmd+F，再按 F3 或 Shift+F3
- **THEN** 搜索栏获得焦点并前后导航命中，Esc 可关闭搜索或退出编辑焦点

#### Scenario: Terminal focused
- **WHEN** 终端运行命令且用户按 Ctrl+C
- **THEN** 按键传给当前终端，不触发日志复制或其他全局动作

### Requirement: Go-only desktop compilation
系统 SHALL 使用 Go 实现界面和核心，在 Windows、macOS、Linux 桌面目标支持 CGO_ENABLED=0 编译，不要求安装 C 编译器或 C 开发头文件。运行时 MAY 使用操作系统图形接口、共享库及外部 ADB；这些运行依赖 SHALL 在启动说明中明确。

#### Scenario: Build without a C toolchain
- **WHEN** 开发者使用 Go 1.25 或更新版本，关闭 CGO，未安装 C 编译器
- **THEN** 桌面代码能够编译，图形、剪贴板和 ADB 的运行依赖在文档中说明；编译通过不等同于平台实际运行验收通过
