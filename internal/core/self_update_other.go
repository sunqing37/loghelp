//go:build !windows

package core

import (
	"fmt"
	"os/exec"
)

func configureUpdateProcess(command *exec.Cmd) {}

func applyPreparedUpdate(filename string) error {
	return fmt.Errorf("此平台暂不支持自动安装")
}

func finishPreparedUpdate(filename string) error { return nil }
