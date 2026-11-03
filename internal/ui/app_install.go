package ui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	uiimage "github.com/ebitenui/ebitenui/image"
	"github.com/ebitenui/ebitenui/widget"
	"github.com/ncruces/zenity"
	"loghelp/internal/core"
	"loghelp/internal/support"
)

type appInstaller struct {
	paths                        []string
	device                       core.Device
	busy                         bool
	cancel                       context.CancelFunc
	status                       string
	label                        *widget.Text
	body                         *widget.Container
	owner                        *widget.Window
	progress                     core.AppInstallProgress
	progressLabel                *widget.Text
	progressBar                  *widget.ProgressBar
	currentLabel                 *widget.Text
	currentBar                   *widget.ProgressBar
	started, finished, refreshed time.Time
	cancelling                   bool
}

func (w *app) openAppInstaller() {
	w.syncInputs()
	if w.installer == nil {
		w.installer = &appInstaller{status: "选择设备和安装文件，或把安装文件拖入此窗口。"}
	}
	w.renderAppInstaller()
}

func (w *app) acceptInstallPaths(paths []string) {
	if w.installer != nil && w.installer.busy {
		w.openAppInstaller()
		return
	}
	if len(paths) == 0 || len(paths) > 128 {
		w.message("选择安装文件", "一次请选择 1–128 个安装文件。")
		return
	}
	for _, path := range paths {
		if !core.IsAppInstallPath(path) {
			w.message("选择安装文件", "不支持的安装格式："+filepath.Base(path)+"\n请选择 APK、AAB、APKS、XAPK、APKM 或 multi-apk。")
			return
		}
	}
	if w.installer == nil {
		w.installer = &appInstaller{}
	}
	w.installer.paths = append([]string(nil), paths...)
	w.installer.started, w.installer.finished = time.Time{}, time.Time{}
	w.installer.progress = core.AppInstallProgress{}
	w.installer.status = "安装文件已就绪；多个 APK 自动按应用归组，base 与 splits 一起安装。"
	w.renderAppInstaller()
}

func (w *app) renderAppInstaller() {
	s := w.installer
	target := "尚未选择目标设备"
	if s.device.Serial != "" {
		target = s.device.Name + " · " + s.device.Serial
	}
	box := w.column(w.diagnosticText("安装目标：" + target))
	if !s.busy {
		box.AddChild(w.row(w.button("选择目标设备…", func() {
			w.deviceChoices("选择安装设备", "未发现在线设备，连接并授权后点击刷新。", func(d core.Device) bool { return d.State == "device" }, func(d core.Device) {
				s.device = d
				s.started, s.finished = time.Time{}, time.Time{}
				s.progress = core.AppInstallProgress{}
				w.renderAppInstaller()
			})
		}), w.button("选择安装文件…", func() {
			w.fileDialogPaths("open", "选择应用安装包（可多选 APK）", w.dialogDirectory(""), zenity.FileFilters{
				{Name: "Android 安装包", Patterns: []string{"*.apk", "*.aab", "*.apks", "*.xapk", "*.apkm", "*.multi-apk"}, CaseFold: true},
				{Name: "所有文件", Patterns: []string{"*"}},
			}, true, func(paths []string) {
				if w.installer == s && !s.busy {
					w.acceptInstallPaths(paths)
				}
			})
		}), w.button("安装工具设置…", w.appInstallSettings)))
	}
	s.label = w.diagnosticText(s.status)
	s.progressLabel, s.progressBar, s.currentLabel, s.currentBar = nil, nil, nil, nil
	if !s.started.IsZero() {
		s.progressLabel = w.diagnosticText(w.appInstallProgressText(s))
		box.AddChild(s.progressLabel)
		if s.progress.Total > 0 {
			s.progressBar = w.appInstallBar(s.progress.Total, s.progress.Completed)
			box.AddChild(s.progressBar)
		}
		s.currentLabel = w.diagnosticText(w.currentAppInstallText(s))
		s.currentBar = w.appInstallBar(1000, appInstallTransferValue(s.progress))
		box.AddChild(s.currentLabel, s.currentBar)
	}
	box.AddChild(s.label)
	box.AddChild(w.diagnosticText("支持 APK、多 APK / split APK、AAB、APKS、XAPK、APKM、multi-apk。可直接拖入安装文件（最多 128 个）；再次选择或拖入会替换文件列表。"))
	if len(s.paths) == 0 {
		box.AddChild(w.muted("尚未选择安装文件"))
	} else {
		names := make([]string, 0, len(s.paths))
		for _, path := range s.paths {
			names = append(names, path)
		}
		box.AddChild(w.diagnosticText(fmt.Sprintf("已选择 %d 个文件：\n%s", len(names), strings.Join(names, "\n"))))
	}
	box.AddChild(w.diagnosticText("覆盖安装保留应用数据，仍需签名一致且版本允许。AAB 使用 bundletool 调试签名，通常不能覆盖商店版；AAB/官方 APKS 需要 Java 与 bundletool。XAPK 的 OBB 写入 Android/obb，失败会单独提示。"))
	s.body = box
	var primary *widget.Button
	if s.busy {
		primary = w.action("取消安装", "danger", func() {
			if s.cancel != nil {
				s.cancelling = true
				s.cancel()
				w.setAppInstallStatus(s, "正在取消；设备已经完成的安装或文件写入不会回滚。")
			}
		})
		box.AddChild(w.muted("关闭弹窗后继续在后台安装；顶部「安装应用」可重新查看。"))
	} else {
		primary = w.action("开始安装", "primary", w.startAppInstall)
		primary.GetWidget().Disabled = s.device.Serial == "" || len(s.paths) == 0 || w.client == nil
	}
	w.showDialog("安装应用", box, primary)
	s.owner = w.modalWindow
}

func (w *app) appInstallBar(total, current int) *widget.ProgressBar {
	c := w.colors()
	return widget.NewProgressBar(
		widget.ProgressBarOpts.WidgetOpts(widget.WidgetOpts.MinSize(w.px(240), w.px(8))),
		widget.ProgressBarOpts.Images(
			&widget.ProgressBarImage{Idle: uiimage.NewNineSliceColor(c.line)},
			&widget.ProgressBarImage{Idle: uiimage.NewNineSliceColor(c.accent)}),
		widget.ProgressBarOpts.Values(0, total, current))
}

func appInstallTransferValue(progress core.AppInstallProgress) int {
	if progress.TotalBytes <= 0 {
		return 0
	}
	return max(0, min(1000, int(float64(progress.SentBytes)/float64(progress.TotalBytes)*1000)))
}

func (w *app) currentAppInstallText(s *appInstaller) string {
	p := s.progress
	if p.CurrentApp == "" {
		if !s.busy {
			return "安装包准备未完成，结果见下方"
		}
		return "当前应用：正在准备安装包…"
	}
	stage := p.Stage
	if s.cancelling && s.busy {
		stage = "正在取消…"
	} else if !s.busy && p.Stage != "安装完成" {
		stage = "任务已停止，结果见下方"
	}
	value := "当前应用：" + p.CurrentApp + "\n" + stage
	if p.CurrentFile != "" {
		value += "\n" + p.CurrentFile
	}
	if p.TotalBytes > 0 {
		value += fmt.Sprintf("\n发送进度 %.1f%% · %.2f / %.2f MiB", float64(appInstallTransferValue(p))/10, float64(p.SentBytes)/(1<<20), float64(p.TotalBytes)/(1<<20))
	}
	return value
}

func (w *app) appInstallProgressText(s *appInstaller) string {
	end := s.finished
	if end.IsZero() {
		end = time.Now()
	}
	elapsed := max(int(end.Sub(s.started).Seconds()), 0)
	state := "已结束"
	if s.busy {
		state = "正在处理" + strings.Repeat("·", 1+int(time.Since(s.started).Milliseconds()/400)%3)
		if s.cancelling {
			state = "正在取消…"
		}
	}
	count := "正在准备安装文件"
	if !s.busy {
		count = "准备阶段已结束"
	}
	if s.progress.Total > 0 {
		count = fmt.Sprintf("已完成 %d/%d 项应用（含附加文件）", s.progress.Completed, s.progress.Total)
	}
	return fmt.Sprintf("%s  ·  %s  ·  耗时 %02d:%02d", state, count, elapsed/60, elapsed%60)
}

func (w *app) updateAppInstallProgress() {
	s := w.installer
	if s == nil || !s.busy || s.owner != w.modalWindow || s.progressLabel == nil || time.Since(s.refreshed) < 250*time.Millisecond {
		return
	}
	s.refreshed = time.Now()
	s.progressLabel.Label = w.wirelessText(w.appInstallProgressText(s), min(w.px(620), w.width-w.px(128)))
	if s.currentLabel != nil {
		s.currentLabel.Label = w.wirelessText(w.currentAppInstallText(s), min(w.px(620), w.width-w.px(128)))
	}
	s.body.RequestRelayout()
	s.owner.RequestRelayout()
	w.renderDirty = true
}

func (w *app) setAppInstallProgress(s *appInstaller, progress core.AppInstallProgress) {
	if s.cancelling {
		return // Queued process output must not overwrite the cancellation notice.
	}
	previousTotal := s.progress.Total
	s.progress = progress
	w.setAppInstallStatus(s, progress.Message)
	if w.modalWindow != s.owner {
		return
	}
	if previousTotal != progress.Total {
		w.renderAppInstaller()
	} else {
		if s.progressBar != nil {
			s.progressBar.SetCurrent(progress.Completed)
		}
		if s.currentBar != nil {
			s.currentBar.SetCurrent(appInstallTransferValue(progress))
			s.currentLabel.Label = w.wirelessText(w.currentAppInstallText(s), min(w.px(620), w.width-w.px(128)))
		}
	}
}

func (w *app) setAppInstallStatus(s *appInstaller, status string) {
	// Keep very large ADB diagnostics from creating an unbounded text widget.
	if len([]rune(status)) > 12000 {
		runes := []rune(status)
		status = string(runes[:6000]) + "\n…输出过长，已省略中间部分…\n" + string(runes[len(runes)-6000:])
	}
	s.status = status
	if w.modalWindow == s.owner && s.label != nil {
		s.label.Label = w.wirelessText(status, min(w.px(620), w.width-w.px(128)))
		s.body.RequestRelayout()
		s.owner.RequestRelayout()
		w.renderDirty = true
	}
}

func (w *app) startAppInstall() {
	s := w.installer
	if s == nil || s.busy || w.client == nil || s.device.Serial == "" || len(s.paths) == 0 {
		return
	}
	client, serial := w.client, s.device.Serial
	paths := append([]string(nil), s.paths...)
	options := w.config.AppInstall
	ctx, cancel := context.WithTimeout(w.ctx, 30*time.Minute)
	s.cancel, s.busy, s.status = cancel, true, "准备安装…"
	s.progress = core.AppInstallProgress{}
	s.started, s.finished, s.refreshed = time.Now(), time.Time{}, time.Time{}
	s.cancelling = false
	w.renderAppInstaller()
	done := make(chan struct{})
	w.triggerWorkers = append(w.triggerWorkers, done)
	go func() {
		defer close(done)
		defer cancel()
		lastStatus := ""
		var lastProgress core.AppInstallProgress
		err := client.InstallApplications(ctx, serial, paths, options, func(progress core.AppInstallProgress) {
			lastStatus = progress.Message
			lastProgress = progress
			w.post(func() { w.setAppInstallProgress(s, progress) })
		})
		if err != nil {
			status := "安装失败：" + err.Error()
			if errors.Is(ctx.Err(), context.Canceled) {
				status = "安装已取消，已完成的安装或 OBB 写入不会回滚；请检查设备状态。\n" + err.Error()
			} else if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				status = "安装超过 30 分钟，已停止本地任务；请检查设备状态。\n" + err.Error()
			}
			lastStatus = status
			support.Record("ERROR", "app.install_failed", status)
		} else {
			support.Record("INFO", "app.install_completed", "")
		}
		w.post(func() {
			s.busy, s.cancel = false, nil
			s.progress = lastProgress
			s.finished = time.Now()
			w.setAppInstallStatus(s, lastStatus)
			if w.modalWindow == s.owner || !w.modal {
				w.renderAppInstaller()
			}
		})
	}()
}

func (w *app) appInstallSettings() {
	java := w.input(w.config.AppInstall.JavaPath, "留空使用 JAVA_HOME 或 PATH 中的 java", nil, nil)
	jar := w.input(w.config.AppInstall.BundletoolPath, "bundletool-all JAR 文件路径", nil, nil)
	box := w.column(w.diagnosticText("仅 AAB 和包含 toc.pb 的官方 APKS 需要这两个工具。请使用 JDK 17+，bundletool 请从 github.com/google/bundletool/releases 下载 all JAR。工具不会自动联网下载或修改 PATH。"),
		w.label("Java 可执行文件"), java, w.button("选择 Java…", func() {
			w.fileDialog("open", "选择 Java 可执行文件", w.dialogDirectory(java.GetText()), nil, java.SetText)
		}),
		w.label("bundletool JAR"), jar, w.button("选择 bundletool JAR…", func() {
			w.fileDialog("open", "选择 bundletool-all JAR", w.dialogDirectory(jar.GetText()), zenity.FileFilters{{Name: "Java JAR", Patterns: []string{"*.jar"}, CaseFold: true}}, jar.SetText)
		}), w.diagnosticText("bundletool 路径留空时依次读取 BUNDLETOOL_JAR、程序旁 tools/bundletool.jar。AAB 会生成调试签名的 APK；如需覆盖正式应用，请先用原签名生成 APKS 再安装。"))
	button := w.action("保存并返回", "primary", func() {
		if !w.configWritable {
			w.message("保存安装工具设置失败", "配置当前不可写。")
			return
		}
		previous := w.config.AppInstall
		w.config.AppInstall = core.AppInstallOptions{JavaPath: strings.TrimSpace(java.GetText()), BundletoolPath: strings.TrimSpace(jar.GetText())}
		if err := core.SaveConfig(w.config); err != nil {
			w.config.AppInstall = previous
			w.showNestedDialog("保存安装工具设置失败", w.column(w.diagnosticText(err.Error())))
			return
		}
		w.renderAppInstaller()
	})
	w.showDialog("安装工具设置", box, button)
}
