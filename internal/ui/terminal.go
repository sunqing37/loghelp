package ui

import (
	"bufio"
	"context"
	"fmt"
	"image"
	"io"
	"runtime"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"loghelp/internal/core"
	"loghelp/internal/vt10x"
)

type shellState struct {
	vt                vt10x.Terminal
	connection        *core.Shell
	cancel            context.CancelFunc
	input             chan []byte
	cols, rows        int
	generation        uint64
	selecting         bool
	selectionScrollAt time.Time
	selectionPoint    image.Point
}

func (s *shellState) close() {
	s.selecting = false
	s.generation++
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.connection != nil {
		s.connection.Close()
		s.connection = nil
	}
}
func (w *app) anyShell() bool {
	for _, p := range w.pages {
		if p.shell != nil {
			return true
		}
	}
	return false
}
func (w *app) openTerminal() {
	if w.client == nil {
		w.settings()
		return
	}
	w.deviceChoices("终端固定设备", "没有在线设备。连接并授权后点击刷新。", func(d core.Device) bool { return d.State == "device" }, func(d core.Device) {
		serial := d.Serial
		w.syncInputs()
		p := w.newPage(core.TabState{Name: "Shell · " + serial, Serial: serial})
		p.shell = &shellState{cols: 80, rows: 24, vt: vt10x.New(vt10x.WithSize(80, 24))}
		w.rebuild()
		w.connectShell(p)
	})
}
func (w *app) connectShell(p *page) {
	if p.dead || p.shell == nil {
		return
	}
	s := p.shell
	s.close()
	job := s.generation
	ctx, cancel := context.WithCancel(w.ctx)
	s.cancel = cancel
	client, serial := w.client, p.state.Serial
	p.status = "连接终端 · " + serial
	go func() {
		conn, err := core.OpenShell(ctx, client, serial)
		w.post(func() {
			if p.dead || s.generation != job || ctx.Err() != nil {
				if conn != nil {
					conn.Close()
				}
				return
			}
			if err != nil {
				cancel()
				p.status = err.Error()
				return
			}
			s.connection = conn
			s.input = make(chan []byte, 128)
			s.vt = vt10x.New(vt10x.WithSize(s.cols, s.rows), vt10x.WithWriter(conn))
			terminal, queue := s.vt, s.input
			p.status = "终端已连接 · " + serial
			w.terminalFocus = p == w.active()
			w.ui.ClearFocus()
			go conn.Resize(uint(s.rows), uint(s.cols))
			go func() {
				for {
					select {
					case data := <-queue:
						if _, err := conn.Write(data); err != nil {
							conn.Close()
							return
						}
					case <-ctx.Done():
						conn.Close()
						return
					}
				}
			}()
			go func() {
				reader := bufio.NewReaderSize(conn, 32768)
				var readErr error
				for {
					if readErr = terminal.Parse(reader); readErr != nil {
						break
					}
				}
				cancel()
				conn.Close()
				exit := conn.ExitCode
				w.post(func() {
					if p.dead || s.generation != job {
						return
					}
					s.connection = nil
					if readErr == io.EOF {
						p.status = fmt.Sprintf("终端退出 · 状态 %d · 可手动重连", exit)
					} else {
						p.status = "终端断开 · " + readErr.Error() + " · 不重放命令"
					}
				})
			}()
		})
	}()
}
func (w *app) shellInput(p *page, value string) {
	if p.shell == nil || p.shell.connection == nil {
		p.status = "终端未连接，请手动重连"
		return
	}
	if len(value) > 1<<20 {
		p.status = "粘贴内容超过 1 MiB，未发送"
		return
	}
	select {
	case p.shell.input <- []byte(value):
		p.shell.selecting = false
		p.shell.vt.Lock()
		p.shell.vt.ClearSelection()
		p.shell.vt.Unlock()
		p.shell.vt.ScrollTo(0)
	default:
		p.status = "终端输入队列已满，此次输入未发送"
	}
}
func (w *app) submitShell(p *page, value string) {
	if value == "" || p.shell.connection == nil {
		return
	}
	w.shellInput(p, value+"\r")
	if w.config.SaveShellHistory {
		w.config.ShellHistory = core.Remember(w.config.ShellHistory, value, 100)
		w.save()
	}
}
func (w *app) copyTerminal(p *page) {
	if p.shell == nil || p.shell.vt == nil {
		return
	}
	terminal := p.shell.vt
	terminal.Lock()
	if value, selected := terminal.SelectionText(); selected {
		terminal.Unlock()
		if err := clipboard.WriteAll(value); err != nil {
			w.message("剪贴板", err.Error())
		}
		return
	}
	cols, rows := terminal.Size()
	lines := make([]string, rows)
	for row := 0; row < rows; row++ {
		var line strings.Builder
		for col := 0; col < cols; col++ {
			char := terminal.ViewCell(col, row).Char
			if char == 0 {
				char = ' '
			}
			line.WriteRune(char)
		}
		lines[row] = line.String()
	}
	terminal.Unlock()
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \x00")
	}
	if err := clipboard.WriteAll(strings.TrimRight(strings.Join(lines, "\n"), "\n")); err != nil {
		w.message("剪贴板", err.Error())
	}
}
func keyRepeat(key ebiten.Key) bool {
	d := inpututil.KeyPressDuration(key)
	return d == 1 || d > 24 && d%3 == 0
}
func (w *app) terminalKeys(p *page) {
	ctrl, shift := ebiten.IsKeyPressed(ebiten.KeyControl), ebiten.IsKeyPressed(ebiten.KeyShift)
	if shift && p.shell.vt != nil {
		switch {
		case keyRepeat(ebiten.KeyPageUp):
			p.shell.vt.Scroll(max(p.shell.rows-1, 1))
			return
		case keyRepeat(ebiten.KeyPageDown):
			p.shell.vt.Scroll(-max(p.shell.rows-1, 1))
			return
		case keyRepeat(ebiten.KeyHome):
			p.shell.vt.ScrollTo(10000)
			return
		case keyRepeat(ebiten.KeyEnd):
			p.shell.vt.ScrollTo(0)
			return
		}
	}
	copyMod := ctrl && shift
	if runtime.GOOS == "darwin" {
		copyMod = ebiten.IsKeyPressed(ebiten.KeyMeta)
	}
	if copyMod && ebiten.IsKeyPressed(ebiten.KeyC) {
		if inpututil.IsKeyJustPressed(ebiten.KeyC) {
			w.copyTerminal(p)
		}
		return
	}
	if copyMod && inpututil.IsKeyJustPressed(ebiten.KeyV) {
		value, err := clipboard.ReadAll()
		if err != nil {
			w.message("剪贴板", err.Error())
		} else {
			w.shellInput(p, value)
		}
		return
	}
	if ctrl {
		if ebiten.IsKeyPressed(ebiten.KeyC) && w.hasTerminalSelection(p) {
			if inpututil.IsKeyJustPressed(ebiten.KeyC) {
				w.copyTerminal(p)
			}
			return
		}
		for i := 0; i < 26; i++ {
			if keyRepeat(ebiten.KeyA + ebiten.Key(i)) {
				w.shellInput(p, string([]byte{byte(i + 1)}))
			}
		}
		return
	}
	if chars := ebiten.AppendInputChars(nil); len(chars) > 0 {
		value := string(chars)
		if ebiten.IsKeyPressed(ebiten.KeyAlt) {
			value = "\x1b" + value
		}
		w.shellInput(p, value)
	}
	prefix := "\x1b["
	if p.shell.vt != nil {
		p.shell.vt.Lock()
		mode := p.shell.vt.Mode()
		p.shell.vt.Unlock()
		if mode&vt10x.ModeAppCursor != 0 {
			prefix = "\x1bO"
		}
	}
	keys := []struct {
		key   ebiten.Key
		value string
	}{{ebiten.KeyEnter, "\r"}, {ebiten.KeyBackspace, "\x7f"}, {ebiten.KeyTab, "\t"}, {ebiten.KeyArrowUp, prefix + "A"}, {ebiten.KeyArrowDown, prefix + "B"}, {ebiten.KeyArrowRight, prefix + "C"}, {ebiten.KeyArrowLeft, prefix + "D"}, {ebiten.KeyHome, "\x1b[H"}, {ebiten.KeyEnd, "\x1b[F"}, {ebiten.KeyDelete, "\x1b[3~"}, {ebiten.KeyPageUp, "\x1b[5~"}, {ebiten.KeyPageDown, "\x1b[6~"}}
	for _, key := range keys {
		if keyRepeat(key.key) {
			w.shellInput(p, key.value)
		}
	}
}
