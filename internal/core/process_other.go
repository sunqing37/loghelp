//go:build !windows

package core

import "os/exec"

func configureProcess(cmd *exec.Cmd) {}
