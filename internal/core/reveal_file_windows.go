package core

import (
	"context"
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

func revealFileNative(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ole := windows.NewLazySystemDLL("ole32.dll")
	shell := windows.NewLazySystemDLL("shell32.dll")
	hr, _, _ := ole.NewProc("CoInitializeEx").Call(0, 2)
	if int32(hr) >= 0 {
		defer ole.NewProc("CoUninitialize").Call()
	} else if uint32(hr) != 0x80010106 { // existing COM apartment is usable
		return fmt.Errorf("初始化文件管理器：HRESULT 0x%08x", uint32(hr))
	}
	parse := func(value string) (uintptr, error) {
		text, err := windows.UTF16PtrFromString(value)
		if err != nil {
			return 0, err
		}
		var pidl uintptr
		hr, _, _ := shell.NewProc("SHParseDisplayName").Call(uintptr(unsafe.Pointer(text)), 0, uintptr(unsafe.Pointer(&pidl)), 0, 0)
		if int32(hr) < 0 {
			return 0, fmt.Errorf("解析文件位置：HRESULT 0x%08x", uint32(hr))
		}
		return pidl, nil
	}
	file, err := parse(path)
	if err != nil {
		return err
	}
	defer ole.NewProc("CoTaskMemFree").Call(file)
	hr, _, _ = shell.NewProc("SHOpenFolderAndSelectItems").Call(file, 0, 0, 0)
	if int32(hr) < 0 {
		return fmt.Errorf("选择录屏文件：HRESULT 0x%08x", uint32(hr))
	}
	return nil
}
