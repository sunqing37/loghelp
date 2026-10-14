//go:build !windows

package core

import (
	"os/exec"
	"runtime"
)

func configureProcess(cmd *exec.Cmd) {}

func openReleaseURL(raw string) error {
	command := "xdg-open"
	if runtime.GOOS == "darwin" {
		command = "open"
	}
	return exec.Command(command, raw).Run()
}
