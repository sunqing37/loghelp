package ui

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ebitenui/ebitenui"
	uiimage "github.com/ebitenui/ebitenui/image"
	uiinput "github.com/ebitenui/ebitenui/input"
	"github.com/ebitenui/ebitenui/widget"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
	"loghelp"
	"loghelp/internal/core"
	"loghelp/internal/diagnostics"
	"loghelp/internal/support"
)

var appVersion = loghelp.Version
var appRevision = loghelp.Revision
var appVersionLabel = fmt.Sprintf("v%s · r%d", appVersion, appRevision)

type page struct {
	triggerCancel                                    context.CancelFunc
	triggerID                                        uint64
	triggerStatus                                    string
	timeAnchor                                       *timeAnchor
	pauseSeq, pauseDropped                           uint64
	state                                            core.TabState
	collector                                        *core.Collector
	recordingPaths                                   []string
	hub                                              *core.Hub
	offline                                          *core.Offline
	shell                                            *shellState
	query                                            core.Query
	search                                           core.Search
	result                                           core.Page
	rawResult                                        core.Page
	groups                                           []logGroup
	groupScratch                                     []logGroup
	groupRowsScratch                                 []*core.Entry
	groupDetail                                      *core.Entry
	groupDetailSource                                *core.Entry
	groupDetailLastSeq                               uint64
	keepSelectionVisible                             bool
	frozen, stopped                                  []*core.Entry
	paused, follow, caseSensitive, regex, dead, busy bool
	clearSeq, version, job                           uint64
	selectionSeq, viewportSeq                        uint64
	offset, scroll, selected, detailScroll, detailX  int
	viewportRows                                     int
	refreshAt, editAt                                time.Time
	dirty                                            bool
	cancel                                           context.CancelFunc
	status                                           string
	selectFirst                                      bool
	detailCache                                      detailTextCache
	detailSelection                                  detailSelection
}
type app struct {
	installer                                                     *appInstaller
	triggerWorkers                                                []<-chan struct{}
	restart                                                       *core.RestartRequest
	restarting                                                    bool
	support                                                       *support.Session
	diagnosticErr                                                 error
	diagnosticWarning                                             string
	logToolsLabel                                                 *widget.Text
	backgroundDiagnostic                                          bool
	profileScene                                                  string
	profileSnapshotSaved                                          bool
	profileSelectionApplied                                       bool
	ui                                                            *ebitenui.UI
	config                                                        core.Config
	systemColorMode, detectedSystemColorMode                      ebiten.ColorMode
	client                                                        *core.ADB
	adb                                                           atomic.Pointer[core.ADB]
	hub                                                           *core.Hub
	ctx                                                           context.Context
	cancel                                                        context.CancelFunc
	tasks                                                         chan func()
	closing                                                       atomic.Bool
	pages                                                         []*page
	selected                                                      int
	tabWidgets                                                    []tabWidget
	tabScroll                                                     *widget.ScrollContainer
	tabDrag                                                       tabDragState
	devices                                                       []core.Device
	wirelessCancel                                                context.CancelFunc
	listCancel                                                    context.CancelFunc
	wirelessCleanup                                               func()
	preview                                                       *devicePreview
	foregroundFollow                                              *foregroundFollow
	foregroundLabel                                               *widget.Text
	previewStreams                                                []*core.ScreenStream
	profileDeviceCopyDone                                         bool
	face                                                          text.Face
	uiFace, smallFace, titleFace                                  text.Face
	fontSource, monoSource                                        *text.GoTextFaceSource
	skins                                                         map[skinKey]*uiimage.NineSlice
	sourceLabel, resultLabel                                      *widget.Text
	connectionLabel                                               *widget.Text
	startButton, followButton, recordButton, pauseButton          *widget.Button
	copyDetailButton                                              *widget.Button
	startActive, followActive, recordActive, pauseActive          bool
	rowHeader                                                     *widget.Container
	tableX                                                        int
	columnDrag                                                    columnResizeState
	serialInput, appInput, buffersInput, filterInput, searchInput *boundedInput
	commandInput                                                  *boundedInput
	fileDialogOpen                                                bool
	updateInProgress                                              bool
	updateRequests                                                int
	updateCheckCancel                                             context.CancelFunc
	fileDirectory                                                 string
	cursorShape                                                   ebiten.CursorShapeType
	statusLabel                                                   *widget.Text
	logPanel, detailPanel                                         *widget.Container
	detailArea, detailSplitter                                    *widget.Container
	detailDragging, detailDragChanged                             bool
	detailAnchorY, detailAnchorHeight, detailMaxHeight            int
	width, height                                                 int
	displayScale                                                  float64
	modal                                                         bool
	removeModal                                                   widget.RemoveWindowFunc
	modalWindow                                                   *widget.Window
	modalReturn                                                   func()
	analysisCancel                                                func()
	analysisPage                                                  *page
	analysisJob                                                   uint64
	analysisLabel                                                 *widget.Text
	terminalFocus                                                 bool
	configWritable                                                bool
	canvas                                                        *ebiten.Image
	renderDirty, hadKeys                                          bool
	paintedAt                                                     time.Time
	pressedKeys                                                   []ebiten.Key
	lastMouseX, lastMouseY                                        int
}

func Run(activations <-chan struct{}, session *support.Session, diagnosticErr error, restart *core.RestartRequest) error {
	config, configErr := core.LoadConfig()
	deviceDiagnostic := os.Getenv("LOGHELP_PROFILE_DIR") != "" && os.Getenv("LOGHELP_PROFILE_SCENE") == "device-preview"
	if deviceDiagnostic {
		config, configErr = core.DefaultConfig(), nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &app{config: config, ctx: ctx, cancel: cancel, tasks: make(chan func(), 128), displayScale: monitorScale(), configWritable: configErr == nil}
	w.support, w.diagnosticErr = session, diagnosticErr
	w.restart = restart
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case _, open := <-activations:
				if !open {
					return
				}
				w.post(func() {
					if ebiten.IsWindowMinimized() {
						ebiten.RestoreWindow()
					}
					ebiten.RequestAttention()
					w.renderDirty = true
				})
			}
		}
	}()
	w.systemColorMode = ebiten.SystemColorMode()
	w.detectedSystemColorMode = w.systemColorMode
	if deviceDiagnostic {
		w.configWritable = false
	}
	w.width, w.height = w.px(1440), w.px(900)
	w.backgroundDiagnostic = os.Getenv("LOGHELP_PROFILE_DIR") != "" && os.Getenv("LOGHELP_PROFILE_BACKGROUND") == "1"
	w.loadFace()
	w.client, _ = core.DiscoverADB(config.ADB)
	w.adb.Store(w.client)
	w.hub = core.NewHub(w.client, config)
	states := append([]core.TabState(nil), config.Tabs...)
	if len(states) == 0 {
		states = []core.TabState{{Name: "Logcat", Buffers: "main,system,crash"}}
	}
	for _, state := range states {
		p := w.newPage(state)
		if state.File != "" {
			w.openOffline(p, state.File)
		} else if state.Serial != "" && w.client != nil {
			w.start(p)
		}
	}
	w.rebuild()
	if deviceDiagnostic {
		w.post(func() { w.startDevicePreview(os.Getenv("LOGHELP_PROFILE_DEVICE")) })
	}
	if os.Getenv("LOGHELP_PROFILE_DIR") != "" && os.Getenv("LOGHELP_PROFILE_SCENE") == "settings" {
		w.post(w.settings)
	}
	if configErr != nil {
		w.post(func() { w.message("配置未覆盖", configErr.Error()) })
	}
	if w.client == nil {
		w.post(w.settings)
	}
	if updateError := core.PendingUpdateError(); updateError != "" {
		w.post(func() { w.message("上次更新未完成", updateError) })
	}
	if os.Getenv("LOGHELP_PROFILE_DIR") != "" && os.Getenv("LOGHELP_PROFILE_SCENE") == "updates" {
		w.post(w.settings)
		w.post(w.showUpdates)
	}
	if os.Getenv("LOGHELP_PROFILE_DIR") != "" && os.Getenv("LOGHELP_PROFILE_SCENE") == "device-picker" {
		// A local sample makes the one-row picker layout inspectable without a device.
		w.post(func() {
			w.refreshableChoices("设备", "没有设备，连接后点击刷新。", func(context.Context) ([]refreshChoice, error) {
				return []refreshChoice{{label: "Android 设备 · emulator-5554 · device · USB", choose: func() {}}}, nil
			})
		})
	}
	if w.backgroundDiagnostic && os.Getenv("LOGHELP_PROFILE_SCENE") == "device-picker-live" {
		w.post(w.chooseDevice)
	}
	if w.backgroundDiagnostic && os.Getenv("LOGHELP_PROFILE_SCENE") == "wireless-pair-error" {
		// Offline layout preview only: no pairing request or credentials.
		w.post(func() {
			w.wirelessFormStatus("pair", "", "配对失败。请核对手机配对页面的 IP、配对端口和新配对码；检查 VPN/TUN 是否接管局域网连接。\nADB 原始信息：error: protocol fault (couldn't read status message): No error")
		})
	}
	w.post(w.showDiagnosticNotice)
	if w.backgroundDiagnostic {
		w.post(w.profileInvestigationScene)
	}
	go w.monitor()
	go w.monitorSystemTheme(w.systemColorMode)
	defer w.shutdown()
	ebiten.SetWindowSize(1440, 900)
	ebiten.SetWindowTitle(fmt.Sprintf("LogHelp %s r%d — Android 日志工作台", appVersion, appRevision))
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetWindowSizeLimits(1000, 720, -1, -1)
	ebiten.SetWindowClosingHandled(true)
	ebiten.SetWindowIcon([]image.Image{brandIcon(32), brandIcon(64)})
	if os.Getenv("LOGHELP_PROFILE_DIR") != "" {
		// Profiling can continue while the user works in another application.
		if duration, err := time.ParseDuration(os.Getenv("LOGHELP_PROFILE_DURATION")); err == nil && duration > 0 {
			go func() {
				timer := time.NewTimer(duration)
				defer timer.Stop()
				select {
				case <-w.ctx.Done():
				case <-timer.C:
					w.cancel()
				}
			}()
		}
		if w.backgroundDiagnostic {
			ebiten.SetRunnableOnUnfocused(true)
			return ebiten.RunGameWithOptions(w, &ebiten.RunGameOptions{InitUnfocused: true})
		}
	}
	return ebiten.RunGame(w)
}
func (w *app) loadFace() {
	if w.fontSource == nil {
		w.fontSource = loadFontSource([]string{os.Getenv("LOGHELP_FONT"), `C:\Windows\Fonts\msyh.ttc`, `C:\Windows\Fonts\simhei.ttf`, "/System/Library/Fonts/PingFang.ttc", "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc"}, goregular.TTF)
		w.monoSource = loadFontSource([]string{`C:\Windows\Fonts\consola.ttf`, "/System/Library/Fonts/SFNSMono.ttf", "/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf"}, gomono.TTF)
	}
	w.uiFace = &text.GoTextFace{Source: w.fontSource, Size: 14 * w.displayScale}
	w.smallFace = &text.GoTextFace{Source: w.fontSource, Size: 12 * w.displayScale}
	w.titleFace = &text.GoTextFace{Source: w.fontSource, Size: 20 * w.displayScale}
	if w.config.Theme == "system" {
		ebiten.SetPreferredColorMode(ebiten.ColorModeUnknown)
	} else if w.lightTheme() {
		ebiten.SetPreferredColorMode(ebiten.ColorModeLight)
	} else {
		ebiten.SetPreferredColorMode(ebiten.ColorModeDark)
	}
	w.face, _ = text.NewMultiFace(&text.GoTextFace{Source: w.monoSource, Size: w.fontSize()}, &text.GoTextFace{Source: w.fontSource, Size: w.fontSize()})
}

func loadFontSource(paths []string, fallback []byte) *text.GoTextFaceSource {
	var data []byte
	for _, path := range paths {
		if path == "" {
			continue
		}
		if b, err := os.ReadFile(path); err == nil {
			data = b
			break
		}
	}
	if len(data) == 0 {
		data = fallback
	}
	source, err := text.NewGoTextFaceSource(bytes.NewReader(data))
	if err != nil {
		sources, e := text.NewGoTextFaceSourcesFromCollection(bytes.NewReader(data))
		if e == nil && len(sources) > 0 {
			source = sources[0]
		} else {
			source, _ = text.NewGoTextFaceSource(bytes.NewReader(fallback))
		}
	}
	return source
}
func (w *app) post(action func()) {
	select {
	case w.tasks <- action:
	case <-w.ctx.Done():
	}
}
func (w *app) async(work func() error, done func()) {
	support.Record("INFO", "task.start", "")
	go func() {
		err := work()
		if err != nil {
			support.Record("ERROR", "task.failed", err.Error())
		} else {
			support.Record("INFO", "task.complete", "")
		}
		w.post(func() {
			if err != nil {
				w.message("操作失败", err.Error())
			} else if done != nil {
				done()
			}
		})
	}()
}
func (w *app) active() *page {
	if w.selected < 0 || w.selected >= len(w.pages) {
		return nil
	}
	return w.pages[w.selected]
}
func (w *app) newPage(state core.TabState) *page {
	if state.Name == "" {
		state.Name = "Logcat"
	}
	if state.Buffers == "" {
		state.Buffers = "main,system,crash"
	}
	p := &page{state: state, selected: -1, follow: state.File == "", caseSensitive: state.CaseSensitive, regex: state.SearchRegex}
	p.query, _ = core.CompileQuery(state.Filter, p.caseSensitive)
	if p.query.Match == nil {
		p.query, _ = core.CompileQuery("", false)
	}
	p.search, _ = core.CompileSearch(state.Search, p.regex, p.caseSensitive)
	w.pages = append(w.pages, p)
	w.selected = len(w.pages) - 1
	return p
}
func (w *app) syncInputs() {
	p := w.active()
	if p == nil || p.shell != nil || w.serialInput == nil {
		return
	}
	serial := strings.TrimSpace(w.serialInput.GetText())
	if p.collector != nil && p.collector.Serial != serial {
		w.stop(p)
	}
	p.state.Serial = serial
	p.state.Package = strings.TrimSpace(w.appInput.GetText())
	p.state.Buffers = w.buffersInput.GetText()
	p.state.Filter = w.filterInput.GetText()
	p.state.Search = w.searchInput.GetText()
}
func (w *app) selectPage(index int) {
	w.syncInputs()
	if len(w.pages) == 0 {
		return
	}
	w.selected = (index + len(w.pages)) % len(w.pages)
	w.terminalFocus = false
	w.rebuild()
	w.refresh(w.active(), true)
	w.save()
}
func (w *app) closePage() {
	w.closeTab(w.active())
}

func (w *app) closeTab(p *page) {
	if p == nil || p.dead {
		return
	}
	index := -1
	for i, candidate := range w.pages {
		if candidate == p {
			index = i
			break
		}
	}
	if index < 0 {
		return
	}
	w.syncInputs()
	p.dead = true
	if p.triggerCancel != nil {
		p.triggerCancel()
		p.triggerCancel = nil
	}
	p.triggerID++
	p.job++
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	// Closing discards the view; unlike stopping, it needs no final snapshot.
	if p.collector != nil {
		p.hub.Release(p.collector)
		p.collector = nil
	}
	if p.shell != nil {
		p.shell.close()
		p.shell.vt, p.shell.input = nil, nil
		p.shell = nil
	}
	p.hub, p.offline = nil, nil
	p.query, p.search = core.Query{}, core.Search{}
	copy(w.pages[index:], w.pages[index+1:])
	w.pages[len(w.pages)-1] = nil
	w.pages = w.pages[:len(w.pages)-1]
	p.result = core.Page{}
	p.rawResult = core.Page{}
	p.groups, p.groupDetail = nil, nil
	p.groupScratch, p.groupRowsScratch, p.groupDetailSource = nil, nil, nil
	p.detailCache = detailTextCache{}
	p.detailSelection = detailSelection{}
	p.frozen, p.stopped = nil, nil
	p.busy = false
	if index < w.selected {
		w.selected--
	}
	w.selected = min(w.selected, len(w.pages)-1)
	if len(w.pages) == 0 {
		w.newPage(core.TabState{Name: "Logcat"})
	}
	w.terminalFocus = false
	w.rebuild()
	w.refresh(w.active(), true)
	w.save()
}
func (w *app) save() {
	if !w.configWritable {
		return
	}
	w.syncInputs()
	w.config.Tabs = nil
	for _, p := range w.pages {
		if p.shell == nil {
			state := p.state
			state.Filter = p.query.Text
			state.Search = p.search.Text
			state.CaseSensitive = p.caseSensitive
			state.SearchRegex = p.regex
			w.config.Tabs = append(w.config.Tabs, state)
		}
	}
	if err := core.SaveConfig(w.config); err != nil {
		support.Record("ERROR", "config.save_failed", err.Error())
		if p := w.active(); p != nil {
			p.status = "保存设置失败：" + err.Error()
		}
	}
}
func (w *app) shutdown() {
	if w.closing.Swap(true) {
		return
	}
	w.save()
	w.cancel()
	w.stopForegroundFollow()
	w.cancelWirelessOperation()
	w.cancelListRefresh()
	w.disposeDevicePreview()
	for _, p := range w.pages {
		if p.cancel != nil {
			p.cancel()
		}
		if p.shell != nil {
			p.shell.close()
		}
	}
	w.hub.Close()
	// Allow our stream workers to finish removing their own ADB forwarding and
	// temporary server files before the process exits.
	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	for _, done := range w.triggerWorkers {
		select {
		case <-done:
		case <-deadline.C:
			return
		}
	}
	for _, stream := range w.previewStreams {
		select {
		case <-stream.Done():
		case <-deadline.C:
			return
		}
	}
}
func (w *app) monitor() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		client := w.adb.Load()
		if client != nil {
			ctx, cancel := context.WithTimeout(w.ctx, 5*time.Second)
			devices, err := client.Devices(ctx)
			cancel()
			w.post(func() {
				if err == nil {
					w.devices = devices
				} else if p := w.active(); p != nil {
					p.status = "设备枚举失败：" + err.Error()
				}
			})
		}
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (w *app) Update() error {
	if ebiten.IsWindowBeingClosed() || w.ctx.Err() != nil {
		return ebiten.Termination
	}
	for i := 0; i < 128; i++ {
		select {
		case action := <-w.tasks:
			action()
			w.renderDirty = true
		default:
			goto ready
		}
	}
ready:
	alive := w.triggerWorkers[:0]
	for _, done := range w.triggerWorkers {
		select {
		case <-done:
		default:
			alive = append(alive, done)
		}
	}
	clear(w.triggerWorkers[len(alive):])
	w.triggerWorkers = alive
	w.dropLogs()
	if w.support != nil && !w.modal {
		if warning := w.support.Warning(); warning != "" && warning != w.diagnosticWarning {
			w.showDiagnosticNotice()
		}
	}
	w.applySystemTheme()
	allowInput := ebiten.IsFocused() && !w.backgroundDiagnostic && !w.fileDialogOpen
	if w.columnDrag.key != "" && (!allowInput || w.modal) {
		w.finishColumnResize()
	}
	if w.detailDragging && (!allowInput || w.modal) {
		w.finishDetailResize()
	}
	if !allowInput && w.tabDrag.page != nil {
		w.tabDrag.canceled = true
	}
	if allowInput {
		w.cursorShape = ebiten.CursorShapeDefault
		mx, my := ebiten.CursorPosition()
		wx, wy := ebiten.Wheel()
		w.pressedKeys = inpututil.AppendPressedKeys(w.pressedKeys[:0])
		hasKeys := len(w.pressedKeys) > 0
		if mx != w.lastMouseX || my != w.lastMouseY || wx != 0 || wy != 0 || hasKeys || w.hadKeys {
			w.renderDirty = true
		}
		for _, button := range []ebiten.MouseButton{ebiten.MouseButtonLeft, ebiten.MouseButtonMiddle, ebiten.MouseButtonRight} {
			if ebiten.IsMouseButtonPressed(button) || inpututil.IsMouseButtonJustReleased(button) {
				w.renderDirty = true
			}
		}
		w.lastMouseX, w.lastMouseY, w.hadKeys = mx, my, hasKeys
		w.updateTabDrag()
		w.keyboard()
		w.ui.Update()
		w.finishTabDrag()
	}
	p := w.active()
	if p != nil {
		if !allowInput || w.modal {
			p.detailSelection.dragging = false
		}
		if !p.editAt.IsZero() && time.Since(p.editAt) > 350*time.Millisecond && p.dirty {
			p.dirty = false
			w.apply(p)
		}
		if p.shell == nil {
			w.refresh(p, false)
		}
		if allowInput {
			w.mouse(p)
			w.updatePanelCursor(p)
		}
		w.applyProfileSelection(p)
		w.updateChrome()
	}
	w.updateForegroundFollow()
	w.updateDevicePreview(allowInput)
	activeStreams := w.previewStreams[:0]
	for _, stream := range w.previewStreams {
		select {
		case <-stream.Done():
		default:
			activeStreams = append(activeStreams, stream)
		}
	}
	clear(w.previewStreams[len(activeStreams):])
	w.previewStreams = activeStreams
	return nil
}
func (w *app) Draw(screen *ebiten.Image) {
	diagnostics.RecordUI(screen.Bounds().Dx(), screen.Bounds().Dy(), w.profileScene)
	if w.canvas == nil || w.canvas.Bounds() != screen.Bounds() {
		if w.canvas != nil {
			w.canvas.Dispose()
		}
		w.canvas = ebiten.NewImageWithOptions(screen.Bounds(), &ebiten.NewImageOptions{Unmanaged: true})
		w.renderDirty = true
	}
	// Reuse the finished workspace between updates. Input and posted results
	// repaint immediately; the periodic repaint also updates caret/terminal
	// blinking without reformatting every log cell at 60 FPS.
	if w.renderDirty || time.Since(w.paintedAt) >= 250*time.Millisecond {
		w.canvas.Fill(w.background())
		manageCursor := uiinput.CursorManagementEnabled
		uiinput.CursorManagementEnabled = false
		w.ui.Draw(w.canvas)
		uiinput.CursorManagementEnabled = manageCursor
		w.renderDirty = false
		w.paintedAt = time.Now()
		if !w.profileSnapshotSaved && os.Getenv("LOGHELP_PROFILE_SNAPSHOT") == "1" && os.Getenv("LOGHELP_PROFILE_DIR") != "" && w.profileSnapshotReady() {
			w.profileSnapshotSaved = true
			w.drawDeviceScreen(w.canvas)
			w.saveProfileSnapshot()
		}
	}
	screen.DrawImage(w.canvas, nil)
	w.drawDeviceScreen(screen)
	w.drawTabDrag(screen)
	w.applyCursor()
}

func (w *app) profileSnapshotReady() bool {
	if os.Getenv("LOGHELP_PROFILE_SCENE") == "device-preview" {
		d := w.preview
		if d == nil || d.texture == nil || !d.ready || d.statsFrames == 0 || time.Since(d.statsAt) > time.Second {
			return false
		}
		if os.Getenv("LOGHELP_PROFILE_COPY_SCREENSHOT") == "1" && (d.busy || !strings.HasPrefix(d.notice, "截图已复制")) {
			return false
		}
	}
	if os.Getenv("LOGHELP_PROFILE_SCENE") == "updates" && (w.profileScene != "版本更新" || w.updateRequests > 0) {
		return false
	}
	if strings.HasPrefix(os.Getenv("LOGHELP_PROFILE_SCENE"), "device-picker") && (w.profileScene != "设备" || w.listCancel != nil) {
		return false
	}
	p := w.active()
	if w.backgroundDiagnostic && os.Getenv("LOGHELP_PROFILE_SELECTION") != "" && !w.profileSelectionApplied {
		return false
	}
	return p == nil || p.state.File == "" || p.offline != nil && !p.busy
}

// Capture the app's own rendered surface for local, unattended diagnostics;
// this reads neither the desktop nor any other application's window.
func (w *app) saveProfileSnapshot() {
	pixels := image.NewRGBA(w.canvas.Bounds())
	w.canvas.ReadPixels(pixels.Pix)
	file, err := os.Create(filepath.Join(os.Getenv("LOGHELP_PROFILE_DIR"), "workspace.png"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "profile snapshot:", err)
		return
	}
	err = png.Encode(file, pixels)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		fmt.Fprintln(os.Stderr, "profile snapshot:", err, closeErr)
	}
	if w.profileSelectionApplied {
		if err := os.WriteFile(filepath.Join(os.Getenv("LOGHELP_PROFILE_DIR"), "selection.txt"), []byte(w.selectedDetailText(w.active())), 0600); err != nil {
			fmt.Fprintln(os.Stderr, "profile selection:", err)
		}
	}
}
func (w *app) Layout(width, height int) (int, int) {
	w.LayoutF(float64(width), float64(height))
	return w.width, w.height
}
func (w *app) background() color.NRGBA { return w.colors().bg }
func (w *app) foreground() color.NRGBA { return w.colors().text }

func (w *app) start(p *page) {
	if p == nil || p.shell != nil {
		return
	}
	if p == w.active() {
		w.syncInputs()
	}
	if w.client == nil {
		w.settings()
		return
	}
	if p.state.Serial == "" {
		w.message("选择设备", "先从设备列表选择在线设备，或输入序列号。")
		return
	}
	buffers, err := core.ParseBuffers(p.state.Buffers)
	if err != nil {
		w.message("缓冲区错误", err.Error())
		return
	}
	w.stop(p)
	p.offline = nil
	p.state.File = ""
	p.paused = false
	p.pauseSeq, p.pauseDropped = 0, 0
	p.timeAnchor = nil
	p.frozen = nil
	p.stopped = nil
	p.clearSeq = 0
	p.offset = 0
	p.version = 0
	p.hub = w.hub
	p.collector = w.hub.Acquire(p.state.Serial, buffers)
	p.status = "连接中 · " + p.state.Serial
	w.refresh(p, true)
	w.save()
}
func (w *app) stop(p *page) {
	if p == nil {
		return
	}
	if p.triggerCancel != nil {
		p.triggerCancel()
		p.triggerCancel = nil
		p.triggerStatus = "采集停止，监听已取消"
	}
	p.triggerID++
	if p.cancel != nil {
		p.cancel()
	}
	p.job++
	p.busy = false
	p.refreshAt = time.Time{}
	if p.collector != nil {
		p.stopped, _, _ = p.collector.Ring.Snapshot()
		p.recordingPaths = p.collector.RecordingDirectories()
		p.hub.Release(p.collector)
		p.collector = nil
	}
	p.paused = false
	p.frozen = nil
	p.pauseSeq, p.pauseDropped = 0, 0
	p.status = "当前页已停止，已有日志仍可查询；后台录制继续"
}
func (w *app) apply(p *page) {
	if p == nil || p.shell != nil {
		return
	}
	if p == w.active() {
		w.syncInputs()
	}
	q, err := core.CompileQuery(p.state.Filter, p.caseSensitive)
	if err != nil {
		p.status = "过滤未生效，保留上次结果：" + err.Error()
		return
	}
	search, err := core.CompileSearch(p.state.Search, p.regex, p.caseSensitive)
	if err != nil {
		p.status = "搜索未生效：" + err.Error()
		return
	}
	p.query = q
	p.search = search
	p.offset = 0
	w.config.History = core.Remember(w.config.History, q.Text, 40)
	w.refresh(p, true)
	w.save()
}
func (w *app) refresh(p *page, force bool) {
	if p == nil || p.dead || p.shell != nil || p.query.Match == nil {
		return
	}
	if p.busy && !force {
		return
	}
	if p.offline != nil && !force {
		return
	}
	if p.collector == nil && p.offline == nil && len(p.stopped) == 0 {
		return
	}
	version := uint64(0)
	if p.collector != nil {
		version = p.collector.Ring.Version()
	}
	if !force && time.Since(p.refreshAt) < 600*time.Millisecond {
		return
	}
	if !force && !p.refreshAt.IsZero() && !p.query.TimeDependent && (p.paused || p.collector == nil || version == p.version) {
		// A paused/stopped view has fixed entries. Unchanged live data also
		// keeps its result; age filters must still expire with wall time.
		w.updateLiveStatus(p)
		p.refreshAt = time.Now()
		return
	}
	if p.cancel != nil {
		p.cancel()
	}
	ctx, cancel := context.WithCancel(w.ctx)
	p.cancel = cancel
	p.job++
	job := p.job
	p.busy = true
	source, collector := p.offline, p.collector
	q, search, pkg, after, offset := p.query, p.search, p.state.Package, p.clearSeq, p.offset
	mergeLines := p.state.MergeLines
	groupScratch, rowScratch := p.groupScratch, p.groupRowsScratch
	p.groupScratch, p.groupRowsScratch = nil, nil
	var entries []*core.Entry
	var dropped uint64
	if collector != nil {
		if p.paused {
			entries = p.frozen
			version, dropped = p.pauseSeq, p.pauseDropped
		} else {
			entries, version, dropped = collector.Ring.Snapshot()
		}
	} else {
		entries = p.stopped
	}
	go func() {
		var result core.Page
		var err error
		if source != nil {
			result, err = source.Query(ctx, q, search, pkg, offset, 2000, func(done int64) {
				w.post(func() {
					if p.job == job {
						p.status = fmt.Sprintf("扫描 %.1f%% · 可取消", 100*float64(done)/float64(max(source.Bytes, 1)))
					}
				})
			})
		} else {
			result = core.QueryLive(entries, q, search, pkg, after)
			result.Dropped = dropped
			result.Version = version
		}
		rawResult := result
		var groups []logGroup
		if err == nil && mergeLines {
			result, groups, err = mergeLogRows(ctx, result, groupScratch, rowScratch)
		}
		w.post(func() {
			if p.dead || job != p.job {
				return
			}
			p.busy = false
			if err != nil {
				if err != context.Canceled {
					p.status = err.Error()
				}
				return
			}
			// Read the current view at delivery time: clicks, keyboard navigation
			// and wheel scrolling may have happened while the query was running.
			selectedSeq, topSeq := p.selectionSeq, p.scrollAnchorSeq()
			// Explicit offline paging must keep its requested scroll reset.
			samePage := source == nil || result.Offset == p.result.Offset
			visible := p.viewportRows
			if p == w.active() || visible <= 0 {
				visible = w.visibleRows()
			}
			selectedOffset := p.selected - p.scroll
			selectedVisible := p.selected >= 0 && selectedOffset >= 0 && selectedOffset < visible
			if len(p.groups) > 0 {
				p.groupScratch, p.groupRowsScratch = p.groups, p.result.Rows
				// The retired display array is exclusively owned by the UI.
				// Clear pointers before handing it to the next query worker.
				clear(p.groupRowsScratch[:cap(p.groupRowsScratch)])
			}
			p.result = result
			p.rawResult = rawResult
			p.groups = groups
			p.version = version
			p.refreshAt = time.Now()
			p.selected = p.rowContainingSeq(selectedSeq)
			if p.selected < 0 {
				p.selectionSeq = 0
			}
			if !p.follow && samePage {
				if selectedVisible && p.selected >= 0 {
					// Keep the selected log at the same screen row when earlier
					// entries expire or the number of merged groups changes.
					p.scroll = max(p.selected-selectedOffset, 0)
				} else if topSeq != 0 {
					// Scrolling away from the selection anchors the top visible
					// log instead; if it expires, use its nearest successor.
					p.scroll = min(p.rowAtOrAfterSeq(topSeq), max(len(result.Rows)-1, 0))
				}
			}
			if p.selectFirst && len(result.Rows) > 0 {
				p.selectFirst = false
				p.selectRow(0)
				p.scroll = 0
				p.detailScroll = 0
			}
			if p.keepSelectionVisible {
				if p.selected >= 0 {
					p.scroll = max(p.selected-visible/2, 0)
				}
				p.keepSelectionVisible = false
			}
			if p.follow && !p.paused {
				p.scroll = max(len(result.Rows)-visible, 0)
			}
			p.scroll = max(0, min(p.scroll, max(len(result.Rows)-1, 0)))
			p.viewportSeq = topSeq
			p.viewportSeq = p.scrollAnchorSeq()
			if source != nil {
				scope := fmt.Sprintf("离线 %d–%d · age 锚点 %s", offset+1, offset+len(rawResult.Rows), result.Anchor.Format(time.RFC3339))
				state := ""
				if warnings := source.WarningList(); len(warnings) > 0 {
					state = warnings[0]
				}
				p.status = fmt.Sprintf("%s · %d/%d 条 · 搜索 %d 处 · 淘汰 %d · %s", scope, len(rawResult.Rows), result.Total, result.SearchCount, result.Dropped, state)
			} else {
				w.updateLiveStatus(p)
			}
		})
	}()
}
func (w *app) updateLiveStatus(p *page) {
	state := "采集已停止"
	if p.collector != nil {
		state = p.collector.Status()
		r := p.collector.RecordingStatus()
		if r.Active {
			state += fmt.Sprintf(" · 录制 %d MiB", r.Bytes>>20)
		}
		if r.Error != "" {
			state += " · " + r.Error
		}
	}
	result := p.result
	if p.triggerStatus != "" {
		state += " · " + shortText(p.triggerStatus, 80)
	}
	if p.paused && p.collector != nil {
		state += fmt.Sprintf(" · 显示已暂停，后台新增 %s 条（全部记录，含事件）", number(int(p.collector.Ring.Version()-p.pauseSeq)))
	}
	p.status = fmt.Sprintf("实时缓存 · %d/%d 条 · 搜索 %d 处 · 淘汰 %d · %s", len(p.rawResult.Rows), result.Total, result.SearchCount, result.Dropped, state) + cacheWindowText(p)
}
func (w *app) openOffline(p *page, path string) {
	w.openOfflineAt(p, path, nil)
}

func (w *app) openOfflineAt(p *page, path string, target *core.Entry) {
	w.openOfflineTarget(p, path, target, nil)
}

func (w *app) openOfflineTarget(p *page, path string, target *core.Entry, bookmark *core.Bookmark) {
	w.stop(p)
	p.timeAnchor = nil
	p.state.File = path
	p.offline = nil
	p.stopped, p.frozen = nil, nil
	p.result, p.rawResult = core.Page{}, core.Page{}
	p.groups, p.groupDetail = nil, nil
	p.groupScratch, p.groupRowsScratch, p.groupDetailSource = nil, nil, nil
	p.detailCache = detailTextCache{}
	p.detailSelection = detailSelection{}
	p.selectRow(-1)
	p.clearSeq, p.offset, p.scroll = 0, 0, 0
	ctx, job, progress := w.beginPageJob(p, "建立离线索引", 0)
	go func() {
		source, err := core.OpenOfflineProgress(ctx, path, progress)
		var seq uint64
		var locateErr error
		if err == nil && target != nil {
			seq, locateErr = source.FindRecorded(ctx, target, progress)
		}
		if err == nil && bookmark != nil {
			seq, locateErr = source.FindBookmark(ctx, *bookmark, progress)
		}
		w.post(func() {
			if p.dead || job != p.job || ctx.Err() != nil {
				return
			}
			p.busy = false
			if err != nil {
				p.cancel()
				p.status = "打开失败：" + err.Error()
				return
			}
			p.offline = source
			w.config.RecentFiles = core.Remember(w.config.RecentFiles, source.Path, 15)
			p.state.Name = filepath.Base(path)
			p.offset = 0
			if seq != 0 {
				p.offset = int(seq - 1)
			}
			p.follow = false
			p.scroll = 0
			p.selectFirst = locateErr == nil
			w.refresh(p, true)
			if p == w.active() {
				w.rebuild()
			}
			w.save()
			if locateErr != nil {
				w.message("录制定位", locateErr.Error())
			}
		})
	}()
}
func (w *app) visibleRows() int {
	if w.logPanel == nil {
		return 10
	}
	return max(w.logPanel.GetWidget().Rect.Dy()/w.rowHeight(), 1)
}
func (w *app) rowHeight() int { return w.px(int(w.config.FontSize) + 18) }
func (w *app) panelRect() image.Rectangle {
	if w.logPanel == nil {
		return image.Rectangle{}
	}
	return w.logPanel.GetWidget().Rect
}
