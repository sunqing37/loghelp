package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ebitenui/ebitenui/widget"
	"github.com/hajimehoshi/ebiten/v2"
	"loghelp/internal/core"
)

const deviceFilesPanelWidth = 480

type deviceFileExplorer struct {
	client          *core.ADB
	device          core.Device
	directory       string
	entries         []core.DeviceFile
	selected        string
	clicked         time.Time
	filter          string
	offset          int
	children        map[string][]core.DeviceFile
	expanded        map[string]bool
	refresh         func()
	table           *deviceFileTable
	loaded, busy    bool
	status          string
	errorDetail     string
	directoryErrors map[string]error
	cancel          context.CancelFunc
	visible         bool
	body            *widget.Container
	pathText        string
	label           *compactText
	inputs          []*boundedInput
}

func (w *app) openDeviceFiles() {
	if w.explorer == nil {
		w.explorer = &deviceFileExplorer{directory: "/sdcard", pathText: "/sdcard", status: "选择在线设备后浏览文件。"}
	}
	s := w.explorer
	if s.visible {
		w.closeDeviceFiles()
		return
	}
	if !s.busy && s.client != w.client {
		s.client, s.device = w.client, core.Device{}
		s.entries, s.selected, s.loaded = nil, "", false
		s.children, s.expanded, s.directoryErrors = nil, nil, nil
	}
	s.visible = true
	w.terminalFocus = false
	w.cancelDeviceTouch()
	w.resizeDeviceDock()
	w.rebuildDeviceLayout()
	if s.device.Serial == "" && !s.busy {
		w.chooseFileDevice()
	}
}

func (w *app) deviceFilesVisible() bool {
	return w.explorer != nil && w.explorer.visible
}

func (w *app) resizeDeviceDock() {
	minimum := 1000
	if w.deviceFilesVisible() || w.preview != nil {
		minimum = 1440
	}
	ebiten.SetWindowSizeLimits(minimum, 720, -1, -1)
	width, height := ebiten.WindowSize()
	if width < minimum {
		ebiten.SetWindowSize(minimum, height)
	}
}

func (w *app) closeDeviceFiles() {
	if w.explorer == nil {
		return
	}
	w.explorer.visible = false
	w.resizeDeviceDock()
	w.rebuildDeviceLayout()
}

func (w *app) deviceFilesPanel() *widget.Container {
	s := w.explorer
	s.body = widget.NewContainer(widget.ContainerOpts.Layout(widget.NewGridLayout(
		widget.GridLayoutOpts.Columns(1), widget.GridLayoutOpts.Stretch([]bool{true}, []bool{true}))))
	w.renderDeviceFiles()
	panel := w.inset(s.body, 12, w.colors().sidebar)
	panel.GetWidget().MinWidth = w.px(deviceFilesPanelWidth)
	panel.GetWidget().LayoutData = widget.GridLayoutData{MaxWidth: w.px(deviceFilesPanelWidth)}
	return panel
}

func (w *app) chooseFileDevice() {
	s := w.explorer
	if s == nil || s.busy {
		return
	}
	client, owner := w.client, w.modalWindow
	if client == nil {
		w.message("选择文件浏览设备", "请先在设置中配置可用的 ADB。")
		return
	}
	ctx, cancel := context.WithTimeout(w.ctx, 5*time.Second)
	s.busy, s.cancel, s.status, s.errorDetail = true, cancel, "正在查找在线设备…", ""
	w.renderDeviceFiles()
	finished := make(chan struct{})
	w.triggerWorkers = append(w.triggerWorkers, finished)
	go func() {
		defer close(finished)
		defer cancel()
		devices, err := client.Devices(ctx)
		w.post(func() {
			s.busy, s.cancel = false, nil
			if w.closing.Load() || w.explorer != s {
				return
			}
			// A late enumeration must not reopen a hidden dock or replace another dialog.
			if !s.visible || w.client != client || w.modalWindow != owner {
				s.status = "点击切换选择在线设备。"
				w.renderDeviceFiles()
				return
			}
			if err != nil {
				s.errorDetail = err.Error()
				w.setDeviceFileStatus(s, "读取设备失败，请点击切换重试")
				w.renderDeviceFiles()
				return
			}
			var online []core.Device
			for _, device := range devices {
				if device.State == "device" {
					online = append(online, device)
				}
			}
			if len(online) == 1 {
				w.selectFileDevice(online[0])
				return
			}
			s.status = "请选择在线设备。"
			w.renderDeviceFiles()
			w.deviceChoices("选择文件浏览设备", "未发现在线设备，连接并授权后点击刷新。",
				func(d core.Device) bool { return d.State == "device" }, w.selectFileDevice)
		})
	}()
}

func (w *app) selectFileDevice(device core.Device) {
	s := w.explorer
	if s == nil || s.busy {
		return
	}
	s.client, s.device = w.client, device
	s.entries, s.selected, s.loaded, s.offset = nil, "", false, 0
	s.children, s.expanded, s.directoryErrors = nil, nil, nil
	s.directory, s.pathText, s.filter = "/sdcard", "/sdcard", ""
	w.loadDeviceDirectory(s.directory)
}

func (w *app) selectedDeviceFile() (core.DeviceFile, bool) {
	s := w.explorer
	if s == nil || s.selected == "" {
		return core.DeviceFile{}, false
	}
	entries := s.entries
	if parent := path.Dir(s.selected); parent != s.directory {
		entries = s.children[parent]
	}
	for _, entry := range entries {
		if entry.Name == path.Base(s.selected) {
			return entry, true
		}
	}
	return core.DeviceFile{}, false
}

func (w *app) renderDeviceFiles() {
	s := w.explorer
	if s == nil || !s.visible || s.body == nil {
		return
	}
	tableFocused := s.table != nil && s.table.IsFocused()
	focusedInput := -1
	previousInputs := s.inputs
	for i, input := range previousInputs {
		if input.IsFocused() {
			focusedInput = i
		}
	}
	if !w.modal && (tableFocused || focusedInput >= 0) {
		w.ui.ClearFocus()
	}
	for _, input := range s.inputs {
		input.disposeBuffer()
	}
	s.inputs = nil
	unavailable := s.busy || s.device.Serial == "" || s.client == nil || s.client != w.client
	button := func(label string, disabled bool, action func()) *widget.Button {
		b := w.action(label, "quiet", action)
		b.GetWidget().Disabled = disabled
		return b
	}
	device := "选择在线设备"
	if s.device.Serial != "" {
		device = s.device.Name + "  ·  " + s.device.Serial
	}
	box := widget.NewContainer(widget.ContainerOpts.Layout(widget.NewGridLayout(
		widget.GridLayoutOpts.Columns(1),
		widget.GridLayoutOpts.Stretch([]bool{true}, []bool{false, false, false, false, false, false, true}),
		widget.GridLayoutOpts.Spacing(0, w.px(8)))))
	box.AddChild(w.stretchRow(w.row(w.action("设备文件", "active", func() {}), w.action("设备预览", "quiet", w.openDevicePreview)), w.hintAction("×", "收起文件管理，传输继续在后台运行", "quiet", w.closeDeviceFiles)))
	box.AddChild(w.stretchRow(w.oneLine(device, false, 300), button("切换…", s.busy, w.chooseFileDevice)))
	download := button("下载…", true, w.downloadDeviceFile)
	upload := button("上传…", unavailable || !s.loaded, w.uploadDeviceFile)
	box.AddChild(w.row(
		button("上级", unavailable || s.directory == "/", func() { w.loadDeviceDirectory(path.Dir(s.directory)) }),
		button("根目录", unavailable, func() { w.loadDeviceDirectory("/") }),
		button("共享存储", unavailable, func() { w.loadDeviceDirectory("/sdcard") }),
		button("刷新", unavailable, func() { w.loadDeviceDirectory(s.directory) }),
		button("折叠", unavailable, func() { s.expanded = map[string]bool{}; s.offset = 0; s.refresh() })))
	var location *boundedInput
	goTo := func() { w.loadDeviceDirectory(location.GetText()) }
	location = w.input(s.pathText, "输入设备路径，按 Enter 跳转", func(value string) { s.pathText = value }, goTo)
	location.GetWidget().Disabled = unavailable
	box.AddChild(w.stretchRow(location, button("前往", unavailable, goTo)))
	count := w.oneLine("", true, 70)
	table := w.newDeviceFileTable(s)
	s.table = table
	selected := w.oneLine("选择文件查看详情", true, 450)
	metadata := w.oneLine("点击箭头展开或收起目录", true, 450)
	s.refresh = func() {
		table.rows = s.fileRows()
		table.clampScroll()
		countItems := 0
		for _, row := range table.rows {
			if row.notice == "" {
				countItems++
			}
		}
		count.Label = fmt.Sprintf("%d 项", countItems)
		entry, ok := w.selectedDeviceFile()
		download.GetWidget().Disabled = unavailable || !ok || !entry.IsRegular()
		selected.Label = "选择文件查看详情"
		metadata.Label = "点击箭头展开或收起目录"
		if ok {
			selected.Label = s.selected
			metadata.Label = deviceFilePermissions(entry) + " · " + entry.Modified.Format("2006-01-02 15:04")
			if entry.IsRegular() {
				metadata.Label += " · " + deviceFileSize(entry.Size)
			}
		}
		w.renderDirty = true
	}
	filter := w.input(s.filter, "筛选已加载的文件…", func(value string) {
		s.filter, s.offset = value, 0
		if s.refresh != nil {
			s.refresh()
		}
	}, nil)
	filter.GetWidget().Disabled = s.busy
	box.AddChild(w.stretchRow(filter, count))
	transfers := w.row(upload, download)
	if s.busy {
		transfers.AddChild(w.action("取消任务", "danger", func() {
			if s.cancel != nil {
				s.cancel()
				w.setDeviceFileStatus(s, "正在取消并清理临时文件…")
			}
		}))
	}
	box.AddChild(transfers, table)
	s.label = w.oneLine(s.status, false, 450)
	statusRow := w.stretchRow(s.label, button("详情", s.errorDetail == "", func() { w.message("文件操作详情", s.errorDetail) }))
	box.AddChild(statusRow, selected, metadata)
	s.inputs = []*boundedInput{location, filter}
	s.refresh()
	s.body.RemoveChildren()
	s.body.AddChild(box)
	s.body.RequestRelayout()
	w.ui.Container.RequestRelayout()
	w.renderDirty = true
	if !w.modal {
		if tableFocused {
			w.ui.SetFocusedWidget(table)
		}
		if focusedInput >= 0 && !s.inputs[focusedInput].GetWidget().Disabled {
			input, old := s.inputs[focusedInput], previousInputs[focusedInput]
			input.cursor, input.anchor, input.scrollX = old.cursor, old.anchor, old.scrollX
			w.ui.SetFocusedWidget(input)
		}
	}
}

func (w *app) toggleDeviceDirectory(remote string) {
	s := w.explorer
	if s.busy {
		return
	}
	if s.expanded[remote] {
		s.expanded[remote] = false
		s.refresh()
		return
	}
	if _, ok := s.children[remote]; ok {
		s.expanded[remote] = true
		s.refresh()
		return
	}
	if len(s.children)+len(s.directoryErrors) >= 64 && s.directoryErrors[remote] == nil {
		w.setDeviceFileStatus(s, "已加载 64 个目录；请使用路径栏进入目标目录，或刷新后继续。")
		return
	}
	client, serial := s.client, s.device.Serial
	cached := len(s.entries)
	for _, entries := range s.children {
		cached += len(entries)
	}
	var entries []core.DeviceFile
	w.runDeviceFileTask("正在展开："+remote, 45*time.Second, func(ctx context.Context) error {
		var err error
		entries, err = client.ListDeviceFiles(ctx, serial, remote)
		if err == nil && cached+len(entries) > 50000 {
			return fmt.Errorf("已加载文件数量过多，请通过路径栏进入目标目录后浏览")
		}
		return err
	}, func() {
		if s.children == nil {
			s.children = map[string][]core.DeviceFile{}
		}
		if s.expanded == nil {
			s.expanded = map[string]bool{}
		}
		s.children[remote], s.expanded[remote] = entries, true
		delete(s.directoryErrors, remote)
		s.status = fmt.Sprintf("%s · %d 项", remote, len(entries))
	}, func(err error) {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		if s.directoryErrors == nil {
			s.directoryErrors = map[string]error{}
		}
		if s.expanded == nil {
			s.expanded = map[string]bool{}
		}
		s.directoryErrors[remote], s.expanded[remote] = err, true
	})
}

func deviceDirectoryNotice(err error) string {
	if errors.Is(err, os.ErrPermission) {
		return "无访问权限 · 当前 ADB 用户无法读取此目录"
	}
	return "读取失败 · 收起后重新展开可重试"
}

func deviceFileSize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	value := float64(size)
	for _, unit := range []string{"KiB", "MiB", "GiB", "TiB"} {
		value /= 1024
		if value < 1024 || unit == "TiB" {
			return fmt.Sprintf("%.1f %s", value, unit)
		}
	}
	return ""
}

func (w *app) setDeviceFileStatus(s *deviceFileExplorer, status string) {
	if r := []rune(status); len(r) > 800 {
		status = string(r[:800]) + "…"
	}
	s.status = status
	if s.visible && s.body != nil && s.label != nil {
		s.label.Label = status
		s.body.RequestRelayout()
		w.renderDirty = true
	}
}

func (w *app) runDeviceFileTask(status string, timeout time.Duration, work func(context.Context) error, done func(), failed ...func(error)) {
	s := w.explorer
	if s == nil || s.busy || s.client == nil || s.client != w.client || s.device.Serial == "" {
		return
	}
	ctx, cancel := context.WithTimeout(w.ctx, timeout)
	s.busy, s.cancel, s.status, s.errorDetail = true, cancel, status, ""
	w.renderDeviceFiles()
	finished := make(chan struct{})
	w.triggerWorkers = append(w.triggerWorkers, finished)
	go func() {
		defer close(finished)
		defer cancel()
		err := work(ctx)
		ctxErr := ctx.Err()
		w.post(func() {
			s.busy, s.cancel = false, nil
			if err != nil {
				failure := errors.Join(ctxErr, err)
				s.errorDetail = failure.Error()
				message := "操作失败，可查看详情"
				if errors.Is(failure, context.Canceled) {
					message = "任务已取消，可查看详情"
				} else if errors.Is(failure, context.DeadlineExceeded) {
					message = "任务超时，请检查设备连接"
				} else if errors.Is(failure, os.ErrPermission) {
					message = "无访问权限，可查看列表中的提示"
				}
				w.setDeviceFileStatus(s, message)
				for _, onFailure := range failed {
					onFailure(failure)
				}
			} else {
				done()
			}
			if s.visible {
				w.renderDeviceFiles()
			}
		})
	}()
}

func (w *app) loadDeviceDirectory(directory string) {
	s := w.explorer
	if s == nil || s.busy {
		return
	}
	client, serial := s.client, s.device.Serial
	var entries []core.DeviceFile
	w.runDeviceFileTask("正在读取目录…", 45*time.Second, func(ctx context.Context) error {
		var err error
		entries, err = client.ListDeviceFiles(ctx, serial, directory)
		return err
	}, func() {
		s.directory, s.entries, s.loaded = path.Clean(directory), entries, true
		s.pathText = s.directory
		s.selected, s.filter, s.offset = "", "", 0
		s.children, s.expanded, s.directoryErrors = map[string][]core.DeviceFile{}, map[string]bool{}, nil
		s.status = fmt.Sprintf("已加载 %d 项 · 双击目录进入，或点击箭头展开", len(entries))
	}, func(err error) {
		if !errors.Is(err, os.ErrPermission) {
			return
		}
		s.directory, s.pathText = path.Clean(directory), path.Clean(directory)
		s.entries, s.selected, s.filter, s.offset, s.loaded = nil, "", "", 0, false
		s.children, s.expanded = nil, nil
		s.directoryErrors = map[string]error{s.directory: err}
	})
}

func (w *app) downloadDeviceFile() {
	s := w.explorer
	entry, ok := w.selectedDeviceFile()
	if s.busy || !ok || !entry.IsRegular() {
		return
	}
	client, serial, directory := s.client, s.device.Serial, s.directory
	remote := s.selected
	// Device names may be illegal host filenames; keep the save dialog inside
	// its initial directory and let the user select a portable local name.
	name := entry.Name
	if strings.ContainsAny(name, "\\/:*?\"<>|\r\n\t") || filepath.Base(name) != name {
		name = "download.bin"
	}
	w.fileDialog("save", "下载设备文件", filepath.Join(w.dialogDirectory(""), name), nil, func(local string) {
		if s.busy || s.client != client || s.device.Serial != serial || s.directory != directory {
			return
		}
		w.runDeviceFileTask("正在下载 "+entry.Name+"（"+deviceFileSize(entry.Size)+"）…", 30*time.Minute,
			func(ctx context.Context) error { return client.DownloadDeviceFile(ctx, serial, remote, local) },
			func() { w.setDeviceFileStatus(s, "已下载到："+local) })
	})
}

func (w *app) uploadDeviceFile() {
	s := w.explorer
	if s.busy || !s.loaded {
		return
	}
	client, serial, root := s.client, s.device.Serial, s.directory
	directory := root
	if entry, ok := w.selectedDeviceFile(); ok {
		directory = path.Dir(s.selected)
		if entry.IsDir() {
			directory = s.selected
		}
	}
	w.fileDialog("open", "选择上传文件", w.dialogDirectory(""), nil, func(local string) {
		if s.busy || s.client != client || s.device.Serial != serial || s.directory != root {
			return
		}
		remote := path.Join(directory, filepath.Base(local))
		w.confirm("确认上传文件", "设备："+serial+"\n目标："+remote+"\n目标同名文件会被替换，是否继续？", func() {
			if s.busy || s.client != client || s.device.Serial != serial || s.directory != root {
				return
			}
			var refreshed []core.DeviceFile
			var refreshErr error
			w.runDeviceFileTask("正在上传 "+filepath.Base(local)+"…", 30*time.Minute,
				func(ctx context.Context) error {
					err := client.UploadDeviceFile(ctx, serial, local, remote, func(sent, total int64) {
						w.post(func() {
							w.setDeviceFileStatus(s, "正在上传："+deviceFileSize(sent)+" / "+deviceFileSize(total)+"；等待设备确认后完成。")
						})
					})
					if err != nil {
						return err
					}
					refreshCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
					defer cancel()
					refreshed, refreshErr = client.ListDeviceFiles(refreshCtx, serial, directory)
					return nil // A refresh failure must not hide a completed upload.
				}, func() {
					status := "已上传到：" + remote
					if refreshErr == nil {
						if directory == s.directory {
							s.entries = refreshed
						} else {
							if s.children == nil {
								s.children = map[string][]core.DeviceFile{}
							}
							if s.expanded == nil {
								s.expanded = map[string]bool{}
							}
							s.children[directory], s.expanded[directory] = refreshed, true
						}
						s.selected, s.filter = remote, ""
					} else {
						status += "；目录刷新失败，请手动刷新：" + refreshErr.Error()
					}
					w.setDeviceFileStatus(s, status)
				})
		})
	})
}
