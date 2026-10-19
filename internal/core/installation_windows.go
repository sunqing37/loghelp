//go:build windows

package core

import (
	"errors"
	"fmt"
	"loghelp"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Keep this name in sync with scripts/installer/loghelp.iss. The OS closes the
// handle at process exit, including after all application shutdown work.
func GuardInstallation() error {
	name, err := windows.UTF16PtrFromString(`Local\LogHelp.Application`)
	if err != nil {
		return err
	}
	_, err = windows.CreateMutex(nil, false, name)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return fmt.Errorf("创建安装维护保护失败：%w", err)
	}
	return nil
}

// Only the registered installed executable may update the uninstall entry.
// Portable copies and read-only CLI queries must not register themselves.
func SyncInstalledVersion() error {
	key, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Uninstall\LogHelp_is1`,
		registry.QUERY_VALUE|registry.SET_VALUE|registry.WOW64_64KEY)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取安装版本登记失败：%w", err)
	}
	defer key.Close()
	location, _, err := key.GetStringValue("InstallLocation")
	if err != nil {
		return fmt.Errorf("读取安装位置失败：%w", err)
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if location == "" || !sameUpdatePath(executable, filepath.Join(location, "loghelp.exe")) {
		return nil
	}
	version := fmt.Sprintf("%s r%d", loghelp.Version, loghelp.Revision)
	if err := key.SetStringValue("DisplayVersion", version); err != nil {
		return fmt.Errorf("同步系统应用列表版本失败：%w", err)
	}
	return nil
}
