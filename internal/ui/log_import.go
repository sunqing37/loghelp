package ui

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/ebitenui/ebitenui/widget"
	"github.com/ncruces/zenity"
	"loghelp/internal/core"
	"loghelp/internal/support"
)

func (w *app) logImportSettings(initial core.LogImportOptions) (*widget.Container, func() (core.LogImportOptions, error)) {
	messageField := w.input(initial.JSONLFields.Message, "留空自动识别 message / msg / log / text", nil, nil)
	levelField := w.input(initial.JSONLFields.Level, "留空自动识别 level / severity / log_level", nil, nil)
	timeField := w.input(initial.JSONLFields.Time, "留空自动识别 time / timestamp / @timestamp / ts", nil, nil)
	keyFile := w.input(initial.XlogKeyFile, "保存 64 位十六进制私钥的文本文件", nil, nil)
	keyEnv := w.input(initial.XlogKeyEnv, "或填写保存私钥的环境变量名称", nil, nil)
	box := w.column(w.heading("日志解压与解码"),
		w.diagnosticText("普通 JSONL 保留完整原文，自动识别常见字段。可指定区分大小写的顶层字段名（不是嵌套路径）；保存后重新打开文件生效，不影响 LogHelp 录制格式。"),
		w.inputRow("JSONL 正文字段", messageField), w.inputRow("JSONL 级别字段", levelField), w.inputRow("JSONL 时间字段", timeField),
		w.diagnosticText("内置 ZIP、TAR、Gzip、Bzip2、Zstd 解压和腾讯 Mars xlog 解码，不依赖 Python。加密 xlog 需原配私钥；未加密文件无需配置。"),
		w.inputRow("Mars 私钥文件", keyFile),
		w.row(w.button("选择私钥文件", func() {
			w.fileDialog("open", "选择 Mars 私钥文本文件", w.dialogDirectory(keyFile.GetText()), zenity.FileFilters{{Name: "所有文件", Patterns: []string{"*"}}}, func(path string) { keyFile.SetText(path) })
		}), w.button("清除私钥引用", func() { keyFile.SetText(""); keyEnv.SetText("") })),
		w.inputRow("Mars 私钥环境变量", keyEnv),
		w.diagnosticText("文件与环境变量二选一；设置仅保存引用，不保存私钥内容。私钥是 64 位十六进制文本，不是应用中的公钥。"))
	list := w.column()
	type decoderInputs struct {
		name, extensions, program, arguments, output, keyFile, keyEnv *boundedInput
		removed                                                       bool
	}
	var cards []*decoderInputs
	add := func(d core.LogDecoder) {
		args, _ := json.Marshal(d.Arguments)
		fields := &decoderInputs{
			name:       w.input(d.Name, "解码器名称", nil, nil),
			extensions: w.input(strings.Join(d.Extensions, ","), ".customlog,.binlog", nil, nil),
			program:    w.input(d.Program, "可执行程序绝对路径或 PATH 中的名称", nil, nil),
			arguments:  w.input(string(args), `["/path/decode.py","{input}","{output}"]`, nil, nil),
			output:     w.input(d.Output, "file / stdout", nil, nil),
			keyFile:    w.input(d.KeyFile, "可选：工具使用的密钥文件路径", nil, nil),
			keyEnv:     w.input(d.KeyEnv, "可选：密钥环境变量名称", nil, nil),
		}
		cards = append(cards, fields)
		card := w.column(w.heading("外部解码器"), w.inputRow("名称", fields.name), w.inputRow("扩展名（逗号分隔）", fields.extensions), w.inputRow("程序", fields.program), w.inputRow("参数 JSON 数组", fields.arguments), w.inputRow("输出方式", fields.output), w.inputRow("工具密钥文件", fields.keyFile), w.inputRow("工具密钥环境变量", fields.keyEnv))
		card.AddChild(w.row(w.button("选择程序", func() {
			w.fileDialog("open", "选择日志解码程序", w.dialogDirectory(fields.program.GetText()), nil, func(path string) { fields.program.SetText(path) })
		}), w.button("选择工具密钥文件", func() {
			w.fileDialog("open", "选择工具密钥文件", w.dialogDirectory(fields.keyFile.GetText()), nil, func(path string) { fields.keyFile.SetText(path) })
		}), w.action("移除解码器", "danger", func() {
			fields.removed = true
			list.RemoveChild(card)
			list.RequestRelayout()
			if w.modalWindow != nil {
				w.modalWindow.RequestRelayout()
			}
		})))
		list.AddChild(card)
		list.RequestRelayout()
		if w.modalWindow != nil {
			w.modalWindow.RequestRelayout()
		}
	}
	for _, d := range initial.Decoders {
		add(d)
	}
	box.AddChild(w.diagnosticText("其他日志格式可按扩展名配置本机工具。参数逐项使用 JSON 字符串；支持 {input}、{output}、{keyfile}。file 模式由工具写入 {output}；stdout 模式将标准输出作为 UTF-8 文本日志。配置 .xlog 会覆盖内置解码。"),
		w.diagnosticText("密钥环境变量的值通过 LOGHELP_LOG_KEY 传给工具。只配置可信工具，程序和脚本建议使用绝对路径；每次执行最长 2 分钟。"), list,
		w.button("添加外部解码器", func() {
			add(core.LogDecoder{Name: "自定义日志", Extensions: []string{".customlog"}, Program: "python", Arguments: []string{"/path/decode.py", "{input}", "{output}"}, Output: "file"})
		}))
	return box, func() (core.LogImportOptions, error) {
		options := core.LogImportOptions{XlogKeyFile: strings.TrimSpace(keyFile.GetText()), XlogKeyEnv: strings.TrimSpace(keyEnv.GetText())}
		options.JSONLFields = core.JSONLFields{Message: strings.TrimSpace(messageField.GetText()), Level: strings.TrimSpace(levelField.GetText()), Time: strings.TrimSpace(timeField.GetText())}
		for _, card := range cards {
			if card.removed {
				continue
			}
			d := core.LogDecoder{Name: strings.TrimSpace(card.name.GetText()), Program: strings.TrimSpace(card.program.GetText()), Output: strings.TrimSpace(card.output.GetText()), KeyFile: strings.TrimSpace(card.keyFile.GetText()), KeyEnv: strings.TrimSpace(card.keyEnv.GetText())}
			for _, extension := range strings.Split(card.extensions.GetText(), ",") {
				d.Extensions = append(d.Extensions, strings.ToLower(strings.TrimSpace(extension)))
			}
			if err := json.Unmarshal([]byte(card.arguments.GetText()), &d.Arguments); err != nil {
				return options, fmt.Errorf("解码器 %s 的参数需为 JSON 字符串数组", d.Name)
			}
			options.Decoders = append(options.Decoders, d)
		}
		return options, core.ValidateLogImport(options)
	}
}

func sourcePreview(value string, limit int) string {
	value = strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(value)
	runes := []rune(value)
	if len(runes) > limit {
		return "…" + string(runes[len(runes)-limit:])
	}
	return value
}

func sourceLineText(p *page) string {
	e := selectedEntry(p)
	if e == nil || e.SourceFile == "" {
		return "来源：选择一条离线日志查看文件与行号"
	}
	line := fmt.Sprintf("第 %d 行", e.SourceLine)
	rows := p.rowMembers(p.selected)
	if len(rows) > 1 {
		line = fmt.Sprintf("第 %d–%d 行", e.SourceLine, rows[len(rows)-1].SourceLine)
	}
	if e.SourceDecoded {
		line = "解码后" + line
	}
	return line
}

func (w *app) logSource() {
	p := w.active()
	e := selectedEntry(p)
	if e == nil || e.SourceFile == "" {
		w.message("日志来源", "先选择一条离线日志。")
		return
	}
	location := e.SourceFile + "\n" + sourceLineText(p)
	w.show("日志来源", w.column(w.diagnosticText(location),
		w.muted("!/ 分隔压缩包与内部条目；行号从 1 开始。"),
		w.button("复制来源与行号", func() {
			if err := clipboard.WriteAll(location); err != nil {
				w.message("复制失败", err.Error())
				return
			}
			w.message("复制完成", "已复制来源路径与行号。")
		}), w.button("仅查看此来源", func() {
			pattern := strings.NewReplacer("\t", `\t`, "\n", `\n`, "\r", `\r`).Replace(regexp.QuoteMeta(e.SourceFile))
			w.applyQuickFilter(p, "source~:"+quoteFilter("^"+pattern+"$"))
		})))
}

func closeLogSource(source *core.Offline) {
	if err := source.Close(); err != nil {
		support.Record("ERROR", "log.cleanup_failed", err.Error())
	}
}

func (w *app) releaseOffline(p *page) {
	if source := p.offline; source != nil {
		p.offline = nil
		go w.disposeOffline(source)
	}
}

func (w *app) disposeOffline(source *core.Offline) {
	closeLogSource(source)
	w.importMu.Lock()
	delete(w.importSources, source)
	w.importMu.Unlock()
}
