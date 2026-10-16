package ui

import "loghelp/internal/support"

func (w *app) confirmRestart() {
	if w.restarting || w.updateInProgress {
		w.showNestedDialog("暂时无法重启", w.column(w.diagnosticText("正在准备重启或安装更新，请等待当前操作完成。")))
		return
	}
	w.showNestedDialog("重启 LogHelp", w.column(
		w.diagnosticText("重启将结束采集、录制、终端和投屏，再打开当前程序路径。已保存的设置和日志标签会恢复；未保存的设置、反馈草稿和未录制日志不会保留。请先保存需要的内容。"),
		w.button("确认重启", func() {
			if w.restarting || w.updateInProgress {
				return
			}
			w.restarting = true
			w.show("正在重启", w.column(w.diagnosticText("正在准备新进程，随后结束当前会话并保存设置…")))
			go func() {
				err := w.restart.Prepare(w.ctx)
				w.post(func() {
					if err != nil {
						w.restarting = false
						support.Record("ERROR", "app.restart_failed", err.Error())
						w.message("重启失败", err.Error())
						return
					}
					support.Record("INFO", "app.restart", "")
					w.cancel()
				})
			}()
		}),
	))
}
