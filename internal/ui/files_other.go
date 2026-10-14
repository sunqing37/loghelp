//go:build !windows

package ui

import (
	"os"
	"runtime"

	"github.com/ncruces/zenity"
)

func nativeDialogOwner() []zenity.Option {
	if runtime.GOOS == "darwin" {
		return []zenity.Option{zenity.Attach(os.Getpid())}
	}
	return nil
}
