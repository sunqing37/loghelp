//go:build windows

package core

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func configureProcess(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }

func openReleaseURL(raw string) error {
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	link, err := windows.UTF16PtrFromString(raw)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, link, nil, nil, windows.SW_SHOWNORMAL)
}
