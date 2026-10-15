package core

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"loghelp/internal/markdownassets"
)

type MarkdownOptions struct {
	Source  string            `json:"source"`
	BaseURL string            `json:"baseURL"`
	Width   int               `json:"width"`
	Scale   float64           `json:"scale"`
	Dark    bool              `json:"dark"`
	Colors  map[string]string `json:"colors"`
	Details []int             `json:"details"`
}

type MarkdownLink struct {
	URL                 string `json:"url"`
	X, Y, Width, Height float64
}

type MarkdownTile struct {
	Y, Height int
	PNG       []byte
}

type MarkdownDocument struct {
	Width, Height int
	Links         []MarkdownLink
	Anchors       map[string]float64
	Details       []int
	Warnings      []string
	Tiles         []MarkdownTile
}

var markdownResources = sync.OnceValues(func() (map[string][]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(markdownassets.Runtime), int64(len(markdownassets.Runtime)))
	if err != nil {
		return nil, err
	}
	resources := make(map[string][]byte, len(reader.File))
	for _, file := range reader.File {
		stream, err := file.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(stream)
		stream.Close()
		if err != nil {
			return nil, err
		}
		resources[file.Name] = data
	}
	return resources, nil
})

var markdownRenderGate = make(chan struct{}, 1)

func markdownBrowser() (string, error) {
	if value := os.Getenv("LOGHELP_MARKDOWN_BROWSER"); value != "" {
		if path, err := exec.LookPath(value); err == nil {
			return path, nil
		}
		return "", fmt.Errorf("LOGHELP_MARKDOWN_BROWSER 指定的浏览器不存在")
	}
	var paths []string
	switch runtime.GOOS {
	case "windows":
		for _, root := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles"), os.Getenv("LOCALAPPDATA")} {
			if root != "" {
				paths = append(paths, filepath.Join(root, "Microsoft/Edge/Application/msedge.exe"), filepath.Join(root, "Google/Chrome/Application/chrome.exe"))
			}
		}
	case "darwin":
		paths = append(paths, "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge", "/Applications/Chromium.app/Contents/MacOS/Chromium")
	}
	paths = append(paths, "msedge", "google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome")
	for _, value := range paths {
		if path, err := exec.LookPath(value); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("完整 Markdown 渲染需要 Edge、Chrome 或 Chromium；请安装浏览器，或用 LOGHELP_MARKDOWN_BROWSER 指定路径")
}

// RenderMarkdown uses a fresh, hidden browser profile, never the user's browser
// session. All rendering libraries/fonts are served from embedded local assets.
func RenderMarkdown(parent context.Context, options MarkdownOptions) (*MarkdownDocument, error) {
	select {
	case markdownRenderGate <- struct{}{}:
		defer func() { <-markdownRenderGate }()
	case <-parent.Done():
		return nil, parent.Err()
	}
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	browser, err := markdownBrowser()
	if err != nil {
		return nil, err
	}
	if options.Width < 100 || options.Width > 4096 || options.Scale < .5 || options.Scale > 4 || len(options.Source) > 2<<20 {
		return nil, fmt.Errorf("Markdown 内容或渲染尺寸超出范围")
	}
	resources, err := markdownResources()
	if err != nil {
		return nil, fmt.Errorf("读取 Markdown 内嵌资源失败：%w", err)
	}
	payload, err := json.Marshal(options)
	if err != nil {
		return nil, err
	}
	handler := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		switch request.URL.Path {
		case "/":
			response.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; font-src 'self'; img-src https: http: data:; connect-src 'self'; base-uri 'none'; form-action 'none'")
			response.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.WriteString(response, `<!doctype html><html><head><meta charset="utf-8"><link rel="stylesheet" href="/katex.css"><link rel="stylesheet" href="/style.css"></head><body><main></main><script type="module" src="/bundle.js"></script></body></html>`)
		case "/document.json":
			response.Header().Set("Content-Type", "application/json")
			response.Write(payload)
		default:
			name := strings.TrimPrefix(request.URL.Path, "/")
			data, ok := resources[name]
			if !ok {
				http.NotFound(response, request)
				return
			}
			contentType := mime.TypeByExtension(filepath.Ext(name))
			switch filepath.Ext(name) {
			case ".js":
				contentType = "text/javascript; charset=utf-8"
			case ".css":
				contentType = "text/css; charset=utf-8"
			case ".woff2":
				contentType = "font/woff2"
			}
			response.Header().Set("Content-Type", contentType)
			response.Write(data)
		}
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("启动本地 Markdown 资源服务失败：%w", err)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 3 * time.Second}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("Markdown 本地资源服务失败：%v", err)
		}
	}()
	defer server.Close()
	address := "http://" + listener.Addr().String()
	profile, err := os.MkdirTemp("", "loghelp-markdown-")
	if err != nil {
		return nil, fmt.Errorf("创建 Markdown 浏览器目录失败：%w", err)
	}
	defer func() {
		if err := os.RemoveAll(profile); err != nil {
			log.Printf("清理 Markdown 临时浏览器目录失败：%v", err)
		}
	}()
	command := exec.CommandContext(ctx, browser, "--headless=new", "--disable-gpu", "--no-first-run", "--no-default-browser-check",
		"--disable-extensions", "--disable-background-mode", "--disable-background-networking", "--disable-sync",
		"--remote-debugging-address=127.0.0.1", "--remote-debugging-port=0", "--user-data-dir="+profile,
		fmt.Sprintf("--window-size=%d,800", options.Width), "about:blank")
	configureProcess(command)
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("启动 Markdown 渲染浏览器失败：%w", err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	var connection *markdownCDP
	defer func() {
		if connection != nil {
			connection.socket.SetWriteDeadline(time.Now().Add(time.Second))
			connection.socket.WriteJSON(map[string]any{"id": connection.id + 1, "method": "Browser.close"})
			connection.socket.Close()
		}
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			command.Process.Kill()
			<-done
		}
	}()
	port := ""
	for port == "" {
		data, _ := os.ReadFile(filepath.Join(profile, "DevToolsActivePort"))
		if value, _, found := strings.Cut(string(data), "\n"); found {
			if number, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && number > 0 && number <= 65535 {
				port = strconv.Itoa(number)
				break
			}
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("Markdown 渲染浏览器启动超时或已取消：%w", ctx.Err())
		case err := <-done:
			done <- err
			return nil, fmt.Errorf("Markdown 渲染浏览器提前退出，请检查浏览器安装或 LOGHELP_MARKDOWN_BROWSER")
		case <-time.After(50 * time.Millisecond):
		}
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+port+"/json/list", nil)
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("连接 Markdown 渲染浏览器失败：%w", err)
	}
	var targets []struct {
		Type                 string
		WebSocketDebuggerURL string
	}
	err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&targets)
	response.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("读取 Markdown 渲染浏览器状态失败：%w", err)
	}
	for _, target := range targets {
		if target.Type == "page" && strings.HasPrefix(target.WebSocketDebuggerURL, "ws://127.0.0.1:"+port+"/") {
			socket, _, err := (&websocket.Dialer{HandshakeTimeout: 5 * time.Second}).DialContext(ctx, target.WebSocketDebuggerURL, nil)
			if err != nil {
				return nil, fmt.Errorf("连接 Markdown 页面失败：%w", err)
			}
			socket.SetReadLimit(16 << 20)
			connection = &markdownCDP{socket: socket, ctx: ctx}
			break
		}
	}
	if connection == nil {
		return nil, fmt.Errorf("Markdown 渲染浏览器未创建页面")
	}
	stop := context.AfterFunc(ctx, func() { connection.socket.Close() })
	defer stop()
	if err = connection.call("Emulation.setDeviceMetricsOverride", map[string]any{"width": options.Width, "height": 800, "deviceScaleFactor": 1, "mobile": false}, nil); err != nil {
		return nil, err
	}
	if err = connection.call("Page.navigate", map[string]any{"url": address}, nil); err != nil {
		return nil, err
	}
	var document MarkdownDocument
	for {
		var state struct {
			Result struct {
				Value struct {
					Ready    bool
					Error    string
					Document MarkdownDocument
				}
			}
		}
		err = connection.call("Runtime.evaluate", map[string]any{"expression": "({ready:window.markdownReady===true,error:window.markdownError||'',document:window.renderedDocument||{}})", "returnByValue": true}, &state)
		if err != nil {
			return nil, err
		}
		if state.Result.Value.Error != "" {
			return nil, fmt.Errorf("Markdown 排版失败：%s", state.Result.Value.Error)
		}
		if state.Result.Value.Ready {
			document = state.Result.Value.Document
			break
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("Markdown 排版超时或已取消：%w", ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
	if document.Width != options.Width || document.Height < 1 || int64(document.Width)*int64(document.Height) > 32<<20 {
		return nil, fmt.Errorf("发布说明渲染结果过大，请打开发布页查看完整内容")
	}
	for y := 0; y < document.Height; y += 1024 {
		height := min(1024, document.Height-y)
		var shot struct{ Data string }
		err = connection.call("Page.captureScreenshot", map[string]any{"format": "png", "captureBeyondViewport": true, "fromSurface": true, "clip": map[string]any{"x": 0, "y": y, "width": document.Width, "height": height, "scale": 1}}, &shot)
		if err != nil {
			return nil, err
		}
		data, err := base64.StdEncoding.DecodeString(shot.Data)
		if err != nil {
			return nil, fmt.Errorf("读取 Markdown 排版图像失败：%w", err)
		}
		document.Tiles = append(document.Tiles, MarkdownTile{Y: y, Height: height, PNG: data})
	}
	return &document, nil
}

type markdownCDP struct {
	socket *websocket.Conn
	ctx    context.Context
	id     int
}

func (client *markdownCDP) call(method string, params any, result any) error {
	if err := client.ctx.Err(); err != nil {
		return err
	}
	deadline, _ := client.ctx.Deadline()
	client.socket.SetReadDeadline(deadline)
	client.socket.SetWriteDeadline(deadline)
	client.id++
	if err := client.socket.WriteJSON(map[string]any{"id": client.id, "method": method, "params": params}); err != nil {
		return fmt.Errorf("发送 Markdown 排版请求失败：%w", err)
	}
	for {
		var message struct {
			ID     int
			Result json.RawMessage
			Error  *struct{ Message string }
		}
		if err := client.socket.ReadJSON(&message); err != nil {
			return fmt.Errorf("读取 Markdown 排版结果失败：%w", err)
		}
		if message.ID != client.id {
			continue
		}
		if message.Error != nil {
			return fmt.Errorf("Markdown 渲染浏览器错误：%s", message.Error.Message)
		}
		if result != nil {
			if err := json.Unmarshal(message.Result, result); err != nil {
				return fmt.Errorf("读取 Markdown 排版结构失败：%w", err)
			}
		}
		return nil
	}
}
