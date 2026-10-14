package ui

import (
	"os"
	"unsafe"

	"github.com/ncruces/zenity"
	"golang.org/x/sys/windows"
)

var dialogUser32 = windows.NewLazySystemDLL("user32.dll")
var dialogForegroundWindow = dialogUser32.NewProc("GetForegroundWindow")
var dialogWindowProcess = dialogUser32.NewProc("GetWindowThreadProcessId")

func nativeDialogOwner() []zenity.Option {
	window, _, _ := dialogForegroundWindow.Call()
	if window != 0 {
		var pid uint32
		dialogWindowProcess.Call(window, uintptr(unsafe.Pointer(&pid)))
		if pid == uint32(os.Getpid()) {
			return []zenity.Option{zenity.Attach(window)}
		}
	}
	return nil
}
