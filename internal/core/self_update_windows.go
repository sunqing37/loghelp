//go:build windows

package core

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

func configureUpdateProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true,
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW}
}

func readUpdateManifest(filename string) (updateManifest, error) {
	var manifest updateManifest
	file, err := os.Open(filename)
	if err != nil {
		return manifest, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	if err != nil || len(data) > 64<<10 {
		return manifest, fmt.Errorf("无法读取更新安装信息")
	}
	if err = json.Unmarshal(data, &manifest); err != nil {
		return manifest, err
	}
	dir := filepath.Dir(filename)
	if !filepath.IsAbs(filename) || filepath.Base(filename) != "manifest.json" ||
		!strings.HasPrefix(filepath.Base(dir), ".loghelp-update-") ||
		!filepath.IsAbs(manifest.Target) || !strings.EqualFold(filepath.Ext(manifest.Target), ".exe") ||
		!sameUpdatePath(filepath.Dir(dir), filepath.Dir(manifest.Target)) ||
		!sameUpdatePath(manifest.Staged, filepath.Join(dir, "loghelp-new.exe")) ||
		!sameUpdatePath(manifest.Backup, manifest.Target+".previous") ||
		!filepath.IsAbs(manifest.WorkDir) || manifest.ParentPID <= 0 {
		return manifest, fmt.Errorf("更新安装路径无效")
	}
	if _, err = releaseDigest(manifest.SHA256); err != nil {
		return manifest, fmt.Errorf("更新安装校验值无效")
	}
	return manifest, nil
}

func sameUpdatePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

func parentProcess(manifest updateManifest) (windows.Handle, error) {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(manifest.ParentPID))
	if err != nil {
		return 0, fmt.Errorf("无法等待原程序退出：%w", err)
	}
	var name [32768]uint16
	length := uint32(len(name))
	err = windows.QueryFullProcessImageName(handle, 0, &name[0], &length)
	if err != nil || !sameUpdatePath(windows.UTF16ToString(name[:length]), manifest.Target) {
		_ = windows.CloseHandle(handle)
		return 0, fmt.Errorf("原程序身份发生变化，已取消更新")
	}
	return handle, nil
}

func applyPreparedUpdate(filename string) (err error) {
	manifest, err := readUpdateManifest(filename)
	if err != nil {
		return err
	}
	dir := filepath.Dir(filename)
	defer func() {
		if err != nil {
			message := time.Now().Format("2006-01-02 15:04:05") + " 更新安装失败：" + err.Error() +
				"\r\n原程序备份：" + manifest.Backup
			_ = os.WriteFile(filepath.Join(dir, "failed"), []byte(message), 0600)
			_ = os.WriteFile(filepath.Join(filepath.Dir(manifest.Target), "loghelp-update-error.log"), []byte(message), 0600)
		}
	}()
	helper, err := os.Executable()
	if err != nil || !sameUpdatePath(helper, filepath.Join(dir, "updater.exe")) {
		return fmt.Errorf("更新助手路径无效")
	}
	info, err := os.Stat(manifest.Staged)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxUpdateSize {
		return fmt.Errorf("待安装程序文件无效")
	}
	digest, err := fileDigest(manifest.Staged)
	if err != nil || digest != manifest.SHA256 {
		return fmt.Errorf("待安装程序校验失败，已取消更新")
	}
	parent, err := parentProcess(manifest)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(parent)
	manifest.HelperPID = os.Getpid()
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if err = os.WriteFile(filename, data, 0600); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(dir, "ready"), []byte("ready"), 0600); err != nil {
		return err
	}
	result, err := windows.WaitForSingleObject(parent, 120000)
	if err != nil || result != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("原程序未在两分钟内退出，已取消更新")
	}
	// Every failure after the parent exits attempts to reopen the installed app.
	restartOld := func(reason error) error {
		if startErr := startUpdatedApplication(manifest, filename); startErr != nil {
			return fmt.Errorf("%v；重新启动原程序失败：%w，请手动启动 %s", reason, startErr, manifest.Target)
		}
		return reason
	}
	if removeErr := os.Remove(manifest.Backup); removeErr != nil && !os.IsNotExist(removeErr) {
		return restartOld(fmt.Errorf("无法替换之前的程序备份：%w", removeErr))
	}
	if err = renameUpdateFile(manifest.Target, manifest.Backup); err != nil {
		return restartOld(fmt.Errorf("无法备份原程序：%w", err))
	}
	rollback := func(reason error) error {
		// If the new program exists, keep it in staging before restoring the backup.
		if _, statErr := os.Stat(manifest.Target); statErr == nil {
			if moveErr := renameUpdateFile(manifest.Target, manifest.Staged); moveErr != nil {
				return fmt.Errorf("%v；无法移走新程序：%w；请使用备份 %s 恢复", reason, moveErr, manifest.Backup)
			}
		}
		if restoreErr := renameUpdateFile(manifest.Backup, manifest.Target); restoreErr != nil {
			return fmt.Errorf("%v；恢复原程序失败：%w；请使用备份 %s 恢复", reason, restoreErr, manifest.Backup)
		}
		return restartOld(reason)
	}
	if err = renameUpdateFile(manifest.Staged, manifest.Target); err != nil {
		return rollback(fmt.Errorf("替换程序失败：%w", err))
	}
	if err = startUpdatedApplication(manifest, filename); err != nil {
		return rollback(fmt.Errorf("启动新程序失败：%w", err))
	}
	_ = os.Remove(filepath.Join(filepath.Dir(manifest.Target), "loghelp-update-error.log"))
	return nil
}

func renameUpdateFile(source, destination string) error {
	var err error
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err = os.Rename(source, destination); err == nil || time.Now().After(deadline) {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func startUpdatedApplication(manifest updateManifest, filename string) error {
	command := exec.Command(manifest.Target, "--finish-update", filename)
	command.Dir = manifest.WorkDir
	command.Env = updateEnvironment()
	configureUpdateProcess(command)
	if err := command.Start(); err != nil {
		return err
	}
	_ = command.Process.Release()
	return nil
}

func finishPreparedUpdate(filename string) error {
	manifest, err := readUpdateManifest(filename)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil || !sameUpdatePath(exe, manifest.Target) || manifest.HelperPID <= 0 || manifest.HelperPID == os.Getpid() {
		return fmt.Errorf("更新清理路径无效")
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(manifest.HelperPID))
	if err == nil {
		defer windows.CloseHandle(handle)
		result, waitErr := windows.WaitForSingleObject(handle, 10000)
		if waitErr != nil || result != windows.WAIT_OBJECT_0 {
			return fmt.Errorf("更新助手尚未退出，稍后可手动清理 %s", filepath.Dir(filename))
		}
	} else if err != windows.ERROR_INVALID_PARAMETER {
		return err
	}
	return os.RemoveAll(filepath.Dir(filename))
}
