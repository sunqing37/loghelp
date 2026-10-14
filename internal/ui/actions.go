package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ebitenui/ebitenui/widget"
	"loghelp/internal/core"
)

func (w *app) duplicate() {
	p := w.active()
	if p == nil || p.shell != nil {
		return
	}
	w.syncInputs()
	state := p.state
	state.Name += " 副本"
	state.Filter = p.query.Text
	state.Search = p.search.Text
	copy := w.newPage(state)
	w.rebuild()
	if state.File != "" {
		w.openOffline(copy, state.File)
	} else {
		w.start(copy)
	}
	w.save()
}
func (w *app) rename() {
	p := w.active()
	if p != nil {
		w.prompt("重命名", p.state.Name, func(name string) {
			if strings.TrimSpace(name) != "" {
				p.state.Name = strings.TrimSpace(name)
				w.rebuild()
				w.save()
			}
		})
	}
}
func (w *app) move(delta int) {
	w.moveTab(w.active(), w.selected+delta)
}
func (w *app) clearView() {
	p := w.active()
	if p == nil || p.shell != nil || p.offline != nil {
		return
	}
	if p.collector != nil {
		p.clearSeq = p.collector.Ring.Version()
	} else {
		for _, entry := range p.stopped {
			p.clearSeq = max(p.clearSeq, entry.Seq)
		}
	}
	if p.cancel != nil {
		p.cancel()
	}
	p.job++
	p.busy = false
	p.result = core.Page{}
	p.rawResult = core.Page{}
	p.groups, p.groupDetail = nil, nil
	p.groupScratch, p.groupRowsScratch, p.groupDetailSource = nil, nil, nil
	p.detailCache = detailTextCache{}
	p.detailSelection = detailSelection{}
	p.selectRow(-1)
	p.viewportSeq = 0
	p.selectFirst = false
	p.scroll, p.detailScroll, p.detailX = 0, 0, 0
	w.renderDirty = true
	w.updateLiveStatus(p)
	w.refresh(p, true)
}

func (w *app) pause() {
	p := w.active()
	if p == nil || p.collector == nil {
		return
	}
	p.paused = !p.paused
	if p.paused {
		p.frozen, _, _ = p.collector.Ring.Snapshot()
		p.follow = false
	} else {
		p.frozen = nil
	}
	w.refresh(p, true)
	w.rebuild()
}
func (w *app) filterHistory() {
	p := w.active()
	if p == nil {
		return
	}
	var favorites []string
	for name := range w.config.Favorites {
		favorites = append(favorites, name)
	}
	sort.Strings(favorites)
	box := column()
	for _, name := range favorites {
		name := name
		box.AddChild(w.button("★ "+name, func() { w.closeModal(); w.filterInput.SetText(w.config.Favorites[name]); w.apply(p) }))
	}
	for _, query := range w.config.History {
		query := query
		box.AddChild(w.button(query, func() { w.closeModal(); w.filterInput.SetText(query); w.apply(p) }))
	}
	box.AddChild(w.button("收藏当前有效过滤", func() {
		w.prompt("收藏名称", "", func(name string) {
			if name != "" {
				w.config.Favorites[name] = p.query.Text
				w.save()
			}
		})
	}))
	w.show("过滤历史与收藏", box)
}
func (w *app) record() {
	p := w.active()
	if p == nil || p.collector == nil {
		w.message("录制", "先开始采集。")
		return
	}
	if p.collector.IsRecording() {
		w.recordings()
		return
	}
	start := func() {
		collector, hub, options := p.collector, p.hub, core.OptionsFromConfig(w.config)
		w.async(func() error { return hub.StartRecording(collector, options) }, func() { p.status = "已开始录制完整采集流" })
	}
	if w.config.Directory == "" {
		w.browse("directory", func(path string) { w.config.Directory = path; w.save(); start() })
	} else {
		start()
	}
}
func (w *app) recordings() {
	box := column()
	for _, collector := range w.hub.Items() {
		collector := collector
		status := collector.RecordingStatus()
		if status.Directory == "" {
			continue
		}
		label := w.label(fmt.Sprintf("%s\n%s\n%d MiB · 持久化序号 %d · 未写入 %d · %s", collector.Serial, status.Directory, status.Bytes>>20, status.LastSeq, status.Unwritten, status.Error))
		label.MaxWidth = 850
		box.AddChild(label, row(w.button("暂停/继续", func() { collector.PauseRecording(!collector.RecordingStatus().Paused) }), w.button("停止录制", func() { w.async(func() error { return w.hub.StopRecording(collector) }, func() { w.recordings() }) }), w.button("离线打开", func() {
			w.closeModal()
			w.syncInputs()
			p := w.newPage(core.TabState{Name: "录制", File: status.Directory})
			w.rebuild()
			w.openOffline(p, status.Directory)
		}), w.button("标记保留", func() { w.async(func() error { return core.ProtectSession(status.Directory, true) }, nil) })))
	}
	box.AddChild(w.button("刷新", w.recordings))
	w.show("后台录制 · 关闭日志页仍继续", box)
}
func (w *app) stopShared() {
	p := w.active()
	if p == nil || p.collector == nil {
		return
	}
	collector, hub := p.collector, p.hub
	var names []string
	for _, tab := range w.pages {
		if tab.collector == collector {
			names = append(names, tab.state.Name)
		}
	}
	w.confirm("停止共享流", fmt.Sprintf("设备 %s\n影响：%s\n所有这些页及该流的录制将停止；只停当前页可使用设备栏「停止」。", collector.Serial, strings.Join(names, ", ")), func() {
		for _, tab := range w.pages {
			if tab.collector == collector {
				w.stop(tab)
			}
		}
		w.async(func() error { return hub.StopRecording(collector) }, nil)
	})
}
func (w *app) clearDevice() {
	p := w.active()
	if p == nil || w.client == nil {
		return
	}
	w.syncInputs()
	buffers, err := core.ParseBuffers(p.state.Buffers)
	if err != nil {
		w.message("缓冲区", err.Error())
		return
	}
	serial, client := p.state.Serial, w.client
	w.confirm("清空设备缓冲区", fmt.Sprintf("将清空设备 %s 的 %s；影响其他工具，已有录制文件不删除。", serial, p.state.Buffers), func() {
		w.async(func() error {
			ctx, cancel := context.WithTimeout(w.ctx, 10*time.Second)
			defer cancel()
			return client.Clear(ctx, serial, buffers)
		}, nil)
	})
}
func (w *app) nextMatch(reverse bool) {
	p := w.active()
	if p == nil || p.shell != nil || p.search.Text == "" {
		return
	}
	if reverse {
		for i := len(p.result.Hits) - 1; i >= 0; i-- {
			if index := p.result.Hits[i]; index < p.selected {
				w.selectLog(p, index)
				return
			}
		}
	} else {
		for _, index := range p.result.Hits {
			if index > p.selected {
				w.selectLog(p, index)
				return
			}
		}
	}
	if p.offline == nil {
		if len(p.result.Hits) > 0 {
			index := p.result.Hits[0]
			if reverse {
				index = p.result.Hits[len(p.result.Hits)-1]
			}
			w.selectLog(p, index)
		}
		return
	}
	after := p.offset - 1
	if p.selected >= 0 {
		start, end := p.rowRange(p.selected)
		if reverse {
			after = p.offset + start
		} else {
			after = p.offset + end - 1
		}
	}
	source, q, search, pkg := p.offline, p.query, p.search, p.state.Package
	if reverse && p.selected < 0 {
		after = p.offset
	}
	if p.cancel != nil {
		p.cancel()
	}
	ctx, cancel := context.WithCancel(w.ctx)
	p.cancel = cancel
	p.job++
	job := p.job
	go func() {
		index, err := source.Find(ctx, q, search, pkg, after, reverse)
		w.post(func() {
			if p.dead || p.job != job {
				return
			}
			if err != nil {
				p.status = err.Error()
				return
			}
			if index < 0 {
				p.status = "已到搜索边界"
				return
			}
			p.offset = index
			p.selectFirst = true
			p.scroll = 0
			w.refresh(p, true)
		})
	}()
}
func (w *app) selectLog(p *page, index int) {
	p.detailSelection = detailSelection{}
	p.selectRow(index)
	p.scroll = max(index-w.visibleRows()/2, 0)
	p.follow = false
	p.detailScroll = 0
	p.detailX = 0
}
func (w *app) context() {
	p := w.active()
	if p == nil || p.selected < 0 || p.selected >= len(p.result.Rows) {
		return
	}
	seq := p.result.Rows[p.selected].Seq
	lastSeq := p.rowLastSeq(p.selected)
	source, collector := p.offline, p.collector
	fallback := p.stopped
	go func() {
		var rows []*core.Entry
		var err error
		if source != nil {
			rows, err = source.Context(w.ctx, seq, 20, int(lastSeq-seq)+20)
		} else {
			if collector != nil {
				fallback, _, _ = collector.Ring.Snapshot()
			}
			for _, e := range fallback {
				delta := int64(e.Seq) - int64(seq)
				if delta >= -20 && delta <= int64(lastSeq-seq)+20 {
					rows = append(rows, e)
				}
			}
		}
		w.post(func() {
			if p.dead {
				return
			}
			if err != nil {
				w.message("上下文", err.Error())
				return
			}
			content := column()
			for _, e := range rows {
				label := w.label(e.Raw)
				label.MaxWidth = 850
				content.AddChild(label)
			}
			w.show("未过滤上下文 · 前后 20 条", content)
		})
	}()
}
func (w *app) jumpTime() {
	p := w.active()
	if p == nil {
		return
	}
	w.prompt("跳到时间（RFC3339 或 YYYY-MM-DD HH:MM:SS）", "", func(value string) {
		date, err := core.ParseTime(value)
		if err != nil {
			w.message("时间错误", err.Error())
			return
		}
		if p.offline == nil {
			for index, e := range p.result.Rows {
				members := p.rowMembers(index)
				if !e.Time.IsZero() && !members[len(members)-1].Time.Before(date) {
					w.selectLog(p, index)
					return
				}
			}
			w.message("时间范围", "当前过滤结果没有该时间，检查条件或打开录制文件。 ")
			return
		}
		source, q, pkg := p.offline, p.query, p.state.Package
		go func() {
			index, found := 0, -1
			err := source.Scan(w.ctx, func(e *core.Entry) bool {
				if !q.Match(e, source.Anchor) || pkg != "" && e.Package != pkg {
					return true
				}
				if !e.Time.IsZero() && !e.Time.Before(date) {
					found = index
					return false
				}
				index++
				return true
			}, nil)
			w.post(func() {
				if err != nil {
					w.message("定位失败", err.Error())
				} else if found < 0 {
					w.message("时间范围", "文件中没有符合条件的时间")
				} else {
					p.offset = found
					p.selectFirst = true
					p.scroll = 0
					w.refresh(p, true)
				}
			})
		}()
	})
}
func samePath(a, b string) bool {
	a, _ = filepath.Abs(a)
	b, _ = filepath.Abs(b)
	if resolved, err := filepath.EvalSymlinks(a); err == nil {
		a = resolved
	}
	if resolved, err := filepath.EvalSymlinks(b); err == nil {
		b = resolved
	}
	return strings.EqualFold(a, b)
}
func (w *app) validateExport(p *page, path string) error {
	if p.offline != nil {
		for _, source := range p.offline.Files {
			if samePath(source, path) {
				return fmt.Errorf("不能覆盖原始日志")
			}
		}
		if samePath(path, filepath.Join(filepath.Dir(p.offline.Files[0]), "manifest.json")) {
			return fmt.Errorf("不能覆盖会话元数据")
		}
	}
	for _, c := range w.hub.Items() {
		dir := c.RecordingStatus().Directory
		if dir != "" {
			abs, _ := filepath.Abs(path)
			rel, err := filepath.Rel(dir, abs)
			if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return fmt.Errorf("不能导出到原始录制会话目录")
			}
		}
	}
	return nil
}
func (w *app) export() {
	p := w.active()
	if p == nil || p.shell != nil {
		return
	}
	w.choices("导出范围", []string{"当前过滤结果（完整来源）", "选中条目"}, func(choice string) {
		selected := choice == "选中条目"
		if selected && (p.selected < 0 || p.selected >= len(p.result.Rows)) {
			w.message("导出", "先选中日志")
			return
		}
		w.saveLogFile(func(path string) {
			if p.dead {
				return
			}
			if err := w.validateExport(p, path); err != nil {
				w.message("导出", err.Error())
				return
			}
			w.exportTo(p, path, strings.EqualFold(filepath.Ext(path), ".jsonl"), selected)
		})
	})
}
func (w *app) exportTo(p *page, path string, jsonLines, selected bool) {
	q, source, pkg, clear := p.query, p.offline, p.state.Package, p.clearSeq
	var rows []*core.Entry
	if selected {
		if p.selected < 0 || p.selected >= len(p.result.Rows) {
			w.message("导出", "选中条目已变化，请重新选择")
			return
		}
		rows = append([]*core.Entry(nil), p.rowMembers(p.selected)...)
	} else if source == nil {
		if p.paused {
			rows = p.frozen
		} else if p.collector != nil {
			rows, _, _ = p.collector.Ring.Snapshot()
		} else {
			rows = p.stopped
		}
	}
	ctx, cancel := context.WithCancel(w.ctx)
	w.show("导出中", column(w.label("UTF-8 · 不修改原始录制"), w.button("取消", cancel)))
	go func() {
		temp, err := os.CreateTemp(filepath.Dir(path), ".loghelp-export-*")
		count := 0
		tmp := ""
		if err == nil {
			tmp = temp.Name()
			temp.Close()
			os.Remove(tmp)
			count, err = core.Export(ctx, tmp, jsonLines, func(visit func(*core.Entry) bool) error {
				if source != nil && !selected {
					return source.Scan(ctx, func(e *core.Entry) bool {
						if q.Match(e, source.Anchor) && (pkg == "" || pkg == e.Package) {
							return visit(e)
						}
						return true
					}, nil)
				}
				anchor := time.Now()
				for _, e := range rows {
					if selected || e.Seq > clear && q.Match(e, anchor) && (pkg == "" || pkg == e.Package) {
						if !visit(e) {
							break
						}
					}
				}
				return nil
			})
			if err == nil {
				err = os.Rename(tmp, path)
			}
			os.Remove(tmp)
		}
		cancel()
		w.post(func() {
			if err != nil {
				w.message("导出失败", err.Error())
			} else {
				w.message("导出完成", fmt.Sprintf("%d 条 · %s", count, path))
			}
		})
	}()
}

func (w *app) settings() { w.settingsFor(w.config) }
func (w *app) settingsFor(initial core.Config) {
	inputs := map[string]*boundedInput{}
	box := column(stretchRow(w.label("LogHelp "+appVersionLabel), w.action("检查更新", "quiet", w.showUpdates)), w.heading("连接与工作区"), w.muted("设置只保存在当前电脑"))
	add := func(key, label, value string) {
		entry := w.input(value, "", nil, nil)
		inputs[key] = entry
		box.AddChild(w.inputRow(label, entry))
	}
	addBool := func(key, label string, value bool) {
		entry := w.input(fmt.Sprint(value), "", nil, nil)
		inputs[key] = entry
		var toggle *widget.Button
		toggle = w.button("关闭", func() {
			value = !value
			entry.SetText(fmt.Sprint(value))
			if value {
				toggle.SetText("已开启")
			} else {
				toggle.SetText("已关闭")
			}
		})
		if value {
			toggle.SetText("已开启")
		} else {
			toggle.SetText("已关闭")
		}
		box.AddChild(stretchRow(w.label(label), toggle))
	}
	add("adb", "ADB（空=PATH）", initial.ADB)
	add("directory", "录制目录", initial.Directory)
	box.AddChild(row(w.button("选择 ADB 文件", func() {
		w.selectADBFile(inputs["adb"].GetText(), func(path string) { inputs["adb"].SetText(path) })
	}), w.button("选择录制目录", func() {
		w.selectDirectory(inputs["directory"].GetText(), func(path string) { inputs["directory"].SetText(path) })
	})))
	box.AddChild(w.heading("外观与阅读"))
	theme := w.input(initial.Theme, "", nil, nil)
	inputs["theme"] = theme
	var dark, light *widget.Button
	dark = w.action("深色", "", func() { theme.SetText("dark"); dark.SetText("● 深色"); light.SetText("浅色") })
	light = w.action("浅色", "", func() { theme.SetText("light"); light.SetText("● 浅色"); dark.SetText("深色") })
	if initial.Theme == "light" {
		light.SetText("● 浅色")
	} else {
		dark.SetText("● 深色")
	}
	box.AddChild(stretchRow(w.label("界面主题"), row(dark, light)))
	add("font", "字体大小", fmt.Sprint(initial.FontSize))
	columns := w.input(strings.Join(initial.Columns, ","), "", nil, nil)
	inputs["columns"] = columns
	selectedColumns := map[string]bool{}
	for _, key := range initial.Columns {
		selectedColumns[key] = true
	}
	columnButtons := row()
	for _, item := range []struct{ key, title string }{{"time", "时间"}, {"pid", "PID"}, {"tid", "TID"}, {"tag", "Tag"}, {"package", "应用"}, {"process", "进程"}} {
		item := item
		var button *widget.Button
		name := item.title
		if selectedColumns[item.key] {
			name = "● " + name
		}
		button = w.button(name, func() {
			selectedColumns[item.key] = !selectedColumns[item.key]
			name := item.title
			if selectedColumns[item.key] {
				name = "● " + name
			}
			button.SetText(name)
			var keys []string
			for _, key := range []string{"time", "pid", "tid", "tag", "package", "process"} {
				if selectedColumns[key] {
					keys = append(keys, key)
				}
			}
			columns.SetText(strings.Join(keys, ","))
		})
		columnButtons.AddChild(button)
	}
	box.AddChild(w.label("可见日志列"), columnButtons)
	addBool("wrap", "详情自动换行", initial.Wrap)
	box.AddChild(w.heading("录制与存储"))
	add("segment", "片段 MiB", fmt.Sprint(initial.SegmentBytes>>20))
	add("minutes", "轮转分钟 0=关", fmt.Sprint(initial.SegmentMinutes))
	add("quota", "目录额度 MiB", fmt.Sprint(initial.QuotaBytes>>20))
	add("days", "保留天数 0=关", fmt.Sprint(initial.RetentionDays))
	addBool("cleanup", "自动清理未保护的旧会话", initial.Cleanup)
	box.AddChild(w.heading("缓存与终端"))
	add("count", "缓存条目上限", fmt.Sprint(initial.CacheCount))
	add("bytes", "缓存 MiB", fmt.Sprint(initial.CacheBytes>>20))
	addBool("history", "保存明确提交的终端命令历史", initial.SaveShellHistory)
	box.AddChild(w.heading("快捷键"))
	var keys []string
	for name := range defaultShortcuts {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	var shortcuts []string
	for _, name := range keys {
		value := defaultShortcuts[name]
		if custom := initial.Shortcuts[name]; custom != "" {
			value = custom
		}
		shortcuts = append(shortcuts, name+"="+value)
	}
	add("shortcuts", "快捷键 ; 分隔", strings.Join(shortcuts, ";"))
	box.AddChild(w.button("清除 Shell 历史", func() { w.config.ShellHistory = nil; w.save() }))
	saveButton := w.action("保存设置", "primary", func() {
		candidate := w.config
		get := func(key string) string { return strings.TrimSpace(inputs[key].GetText()) }
		integer := func(key string, min, max int64) (int64, error) {
			v, err := strconv.ParseInt(get(key), 10, 64)
			if err != nil || v < min || v > max {
				return 0, fmt.Errorf("%s 需要 %d–%d", key, min, max)
			}
			return v, nil
		}
		values := map[string]int64{}
		for _, spec := range []struct {
			key      string
			min, max int64
		}{{"font", 9, 32}, {"segment", 1, 4096}, {"minutes", 0, 1440}, {"quota", 1, 1048576}, {"days", 0, 36500}, {"count", 100, 1000000}, {"bytes", 1, 1024}} {
			v, err := integer(spec.key, spec.min, spec.max)
			if err != nil {
				w.message("设置错误", err.Error())
				return
			}
			values[spec.key] = v
		}
		bools := map[string]bool{}
		for _, key := range []string{"wrap", "cleanup", "history"} {
			v, err := strconv.ParseBool(get(key))
			if err != nil {
				w.message("设置错误", key+" 使用 true/false")
				return
			}
			bools[key] = v
		}
		if get("theme") != "dark" && get("theme") != "light" {
			w.message("设置错误", "主题使用 dark/light")
			return
		}
		candidate.ADB = get("adb")
		if candidate.ADB != w.config.ADB && (len(w.hub.Items()) > 0 || w.anyShell()) {
			w.message("ADB 配置", "更换 ADB 前停止所有采集、录制并关闭终端页。")
			return
		}
		candidate.Directory = get("directory")
		candidate.Theme = get("theme")
		candidate.FontSize = float32(values["font"])
		candidate.Columns = nil
		for _, name := range strings.Split(get("columns"), ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			switch name {
			case "time", "pid", "tid", "tag", "package", "process":
				candidate.Columns = append(candidate.Columns, name)
			default:
				w.message("设置错误", "未知列 "+name)
				return
			}
		}
		candidate.Wrap = bools["wrap"]
		candidate.Cleanup = bools["cleanup"]
		candidate.SaveShellHistory = bools["history"]
		candidate.SegmentBytes = values["segment"] << 20
		candidate.QuotaBytes = values["quota"] << 20
		candidate.SegmentMinutes = int(values["minutes"])
		candidate.RetentionDays = int(values["days"])
		candidate.CacheCount = int(values["count"])
		candidate.CacheBytes = values["bytes"] << 20
		custom, err := w.parseShortcuts(get("shortcuts"))
		if err != nil {
			w.message("快捷键", err.Error())
			return
		}
		candidate.Shortcuts = custom
		reset := !w.configWritable
		w.closeModal()
		w.async(func() error {
			client, err := core.DiscoverADB(candidate.ADB)
			if err != nil && candidate.ADB != "" {
				return err
			}
			if reset {
				if err := core.BackupConfig(); err != nil {
					return err
				}
			}
			w.post(func() {
				if candidate.ADB != w.config.ADB || w.client == nil {
					w.client = client
					w.adb.Store(client)
					w.hub = core.NewHub(client, candidate)
				}
				w.config = candidate
				w.configWritable = true
				w.hub.UpdateConfig(candidate)
				w.loadFace()
				w.rebuild()
				w.save()
			})
			return nil
		}, nil)
	})
	w.showDialog("偏好设置", box, saveButton)
}
func (w *app) help() {
	w.message("使用说明", `日志分析与快捷键
过滤：package:com.example level:WARN -tag:Noise
正则：message~:"timeout|failed"；带逻辑字符用引号。
同键多个正条件 OR，其他 AND；括号、&、|；Go RE2。
pid/tid 精确数值；age:5m 支持 s/m/h/d。
after:"2026-10-12 10:00:00" before:"2026-10-12 11:00:00"
时间起点包含、终点排除；离线 age 相对最后设备时间。
过滤与搜索分开；字段建议、历史与收藏通过对应按钮。
暂停显示继续采集写盘；停止单页不停止后台录制。
默认 100 MiB 轮转、目录 2 GiB；自动清理默认关闭。
离线打开 manifest.json 或 threadtime 文本，2000 条分页。
Ctrl/Cmd+F/L 搜索/过滤，F3/Shift+F3 匹配导航。
选中日志后 ↑/↓ 移动到相邻行，长按连续移动；自动滚动并更新详情。
Ctrl/Cmd+T/W 新建/关闭，Ctrl+Tab/Shift+Ctrl+Tab 切换。
按住标签名称左右拖动调整顺序，松开保存；Esc 或移出标签栏松开取消。
Ctrl/Cmd+Shift+P 暂停，Ctrl/Cmd+End 到末尾。
终端 Ctrl+C 中断；Ctrl+Shift+C/V 复制/粘贴。
终端固定设备，断线不重放命令；历史默认关闭。
详情可拖选文字，Ctrl/Cmd+C 复制选中、Ctrl/Cmd+A 全选。
Shift+点击扩展选区，Esc 清除；“复制全部”保留完整原文。
详情 Shift+滚轮水平浏览，支持换行和拖动分隔条调高。
Linux 剪贴板需系统的 xclip/xsel 之一。
中文字体使用系统字体，或 LOGHELP_FONT 指定 TTF/TTC。`)
}
