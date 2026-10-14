package core

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const maxUpdateSize int64 = 512 << 20

type updateManifest struct {
	Target, Staged, Backup, WorkDir, SHA256 string
	ParentPID, HelperPID                    int
}

// PreparedUpdate owns a verified executable staged beside the installed app.
type PreparedUpdate struct {
	mu      sync.Mutex
	dir     string
	started bool
}

func AutomaticUpdateSupported() bool {
	return runtime.GOOS == "windows" && runtime.GOARCH == "amd64"
}

// PrepareUpdate downloads and verifies the release without changing the running app.
func PrepareUpdate(ctx context.Context, release ReleaseUpdate, options UpdateNetworkOptions) (_ *PreparedUpdate, err error) {
	if !AutomaticUpdateSupported() {
		return nil, fmt.Errorf("此平台暂不支持自动安装，请从发布页面手动更新")
	}
	if !release.DownloadAvailable || !validReleaseURL(release.DownloadURL) ||
		!strings.HasPrefix(release.DownloadURL, releaseRepository+"download/") ||
		!strings.HasSuffix(release.DownloadURL, ".zip") {
		return nil, fmt.Errorf("该版本没有可自动安装的 Windows 便携包")
	}
	digest, err := releaseDigest(release.Digest)
	if err != nil {
		return nil, err
	}
	if release.Size <= 0 || release.Size > maxUpdateSize {
		return nil, fmt.Errorf("更新包大小无效或超过 512 MB")
	}
	target, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("无法定位当前程序：%w", err)
	}
	target, err = filepath.EvalSymlinks(target)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(filepath.Dir(target), ".loghelp-update-")
	if err != nil {
		return nil, fmt.Errorf("程序所在目录不可写，请将便携版移动到有写入权限的目录后重试：%w", err)
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dir)
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	archive := filepath.Join(dir, "release.zip")
	if err = downloadUpdate(ctx, release, digest, archive, options); err != nil {
		return nil, err
	}
	staged := filepath.Join(dir, "loghelp-new.exe")
	if err = extractUpdate(archive, staged); err != nil {
		return nil, err
	}
	_ = os.Remove(archive)
	exeDigest, err := fileDigest(staged)
	if err != nil {
		return nil, err
	}
	// The helper uses the current, trusted executable, never the downloaded code.
	if err = copyUpdateFile(target, filepath.Join(dir, "updater.exe")); err != nil {
		return nil, fmt.Errorf("准备更新助手失败：%w", err)
	}
	workDir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	manifest := updateManifest{Target: target, Staged: staged, Backup: target + ".previous",
		WorkDir: workDir, SHA256: exeDigest, ParentPID: os.Getpid()}
	data, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	if err = os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0600); err != nil {
		return nil, err
	}
	return &PreparedUpdate{dir: dir}, nil
}

func releaseDigest(value string) (string, error) {
	value = strings.TrimPrefix(value, "sha256:")
	data, err := hex.DecodeString(value)
	if err != nil || len(data) != sha256.Size {
		return "", fmt.Errorf("GitHub 未提供有效的 SHA-256 校验值，请从发布页面手动更新")
	}
	return strings.ToLower(value), nil
}

func downloadUpdate(ctx context.Context, release ReleaseUpdate, digest, destination string, options UpdateNetworkOptions) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, release.DownloadURL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "LogHelp-Updater")
	client, err := newUpdateClient(options.Proxy, 5*time.Minute)
	if err != nil {
		return err
	}
	defer client.CloseIdleConnections()
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		host := req.URL.Hostname()
		if len(via) >= 5 || req.URL.Scheme != "https" || req.URL.User != nil ||
			(host != "github.com" && host != "release-assets.githubusercontent.com" && host != "objects.githubusercontent.com") {
			return &updateRequestError{message: "更新下载重定向地址无效，已取消下载"}
		}
		return nil
	}
	response, err := client.Do(request)
	if err != nil {
		networkErr, _ := updateConnectionError(err)
		return fmt.Errorf("下载更新失败：%w", networkErr)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		statusErr, _ := releaseHTTPError(response, data)
		return fmt.Errorf("下载更新失败：%w", statusErr)
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, release.Size+1))
	closeErr := file.Close()
	if copyErr != nil {
		return fmt.Errorf("下载更新失败：%w", copyErr)
	}
	if closeErr != nil {
		return closeErr
	}
	if size != release.Size || hex.EncodeToString(hash.Sum(nil)) != digest {
		return fmt.Errorf("更新包大小或 SHA-256 校验不一致，已取消安装")
	}
	return nil
}

func extractUpdate(archive, destination string) error {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("无法打开更新包：%w", err)
	}
	defer reader.Close()
	var executable *zip.File
	for _, entry := range reader.File {
		// No archive path is used as a destination; only the single executable is extracted.
		if strings.EqualFold(path.Base(strings.ReplaceAll(entry.Name, "\\", "/")), "loghelp.exe") && !entry.FileInfo().IsDir() {
			if executable != nil || entry.Mode()&os.ModeSymlink != 0 || entry.UncompressedSize64 == 0 || entry.UncompressedSize64 > uint64(maxUpdateSize) {
				return fmt.Errorf("更新包中的程序文件无效")
			}
			executable = entry
		}
	}
	if executable == nil {
		return fmt.Errorf("更新包中未找到 loghelp.exe")
	}
	source, err := executable.Open()
	if err != nil {
		return err
	}
	defer source.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return err
	}
	count, copyErr := io.Copy(output, io.LimitReader(source, maxUpdateSize+1))
	closeErr := output.Close()
	if copyErr != nil || count != int64(executable.UncompressedSize64) {
		return fmt.Errorf("解压更新程序失败或文件不完整：%v", copyErr)
	}
	return closeErr
}

func copyUpdateFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func fileDigest(filename string) (string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, io.LimitReader(file, maxUpdateSize+1)); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// Start waits until the independent helper has a handle to the parent process.
// After success the UI must gracefully exit so replacement can proceed.
func (prepared *PreparedUpdate) Start() error {
	prepared.mu.Lock()
	defer prepared.mu.Unlock()
	if prepared.started || prepared.dir == "" {
		return fmt.Errorf("更新安装已经启动或准备文件已清理")
	}
	for _, name := range []string{"ready", "failed"} {
		if err := os.Remove(filepath.Join(prepared.dir, name)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("无法准备更新助手状态：%w", err)
		}
	}
	command := exec.Command(filepath.Join(prepared.dir, "updater.exe"), "--apply-update", filepath.Join(prepared.dir, "manifest.json"))
	command.Env = updateEnvironment()
	configureUpdateProcess(command)
	if err := command.Start(); err != nil {
		return fmt.Errorf("启动更新助手失败：%w", err)
	}
	finished := make(chan error, 1)
	go func() { finished <- command.Wait() }()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if _, err := os.Stat(filepath.Join(prepared.dir, "ready")); err == nil {
				prepared.started = true
				return nil
			}
		case err := <-finished:
			if detail, readErr := os.ReadFile(filepath.Join(prepared.dir, "failed")); readErr == nil {
				return fmt.Errorf("更新助手启动失败：%s", detail)
			}
			return fmt.Errorf("更新助手提前退出：%v", err)
		case <-timer.C:
			_ = command.Process.Kill()
			<-finished
			return fmt.Errorf("更新助手启动超时，请重试")
		}
	}
}

// Cleanup abandons a prepared update. Once Start succeeds, the helper owns it.
func (prepared *PreparedUpdate) Cleanup() {
	prepared.mu.Lock()
	defer prepared.mu.Unlock()
	if !prepared.started && prepared.dir != "" {
		_ = os.RemoveAll(prepared.dir)
		prepared.dir = ""
	}
}

func updateEnvironment() []string {
	var env []string
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if !strings.HasPrefix(strings.ToUpper(key), "LOGHELP_PROFILE_") {
			env = append(env, value)
		}
	}
	return env
}

// HandleUpdateCommand runs before UI and diagnostics, also in the helper copy.
func HandleUpdateCommand(args []string) (bool, error) {
	if len(args) == 2 && args[0] == "--apply-update" {
		return true, applyPreparedUpdate(args[1])
	}
	if len(args) == 2 && args[0] == "--finish-update" {
		// Cleanup failure must not prevent an otherwise successful app launch.
		_ = finishPreparedUpdate(args[1])
	}
	return false, nil
}

// PendingUpdateError exposes the helper's persistent rollback/failure report to UI.
// The report is retained until a successful installation for manual recovery.
func PendingUpdateError() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	file, err := os.Open(filepath.Join(filepath.Dir(exe), "loghelp-update-error.log"))
	if err != nil {
		return ""
	}
	defer file.Close()
	data, _ := io.ReadAll(io.LimitReader(file, 8<<10))
	return strings.TrimSpace(string(data))
}
