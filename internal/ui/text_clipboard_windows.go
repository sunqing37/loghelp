package ui

import (
	"context"
	"encoding/binary"
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"loghelp/internal/core"
)

var sharedClipboardFormat = imageClipboardUser.NewProc("IsClipboardFormatAvailable")
var sharedClipboardGet = imageClipboardUser.NewProc("GetClipboardData")
var sharedClipboardSize = imageClipboardKernel.NewProc("GlobalSize")

func openSharedClipboard(ctx context.Context, owner uintptr) error {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("剪贴板被占用或操作已取消：%w", err)
		}
		if ok, _, _ := imageClipboardOpen.Call(owner); ok != 0 {
			return nil
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}

func readSharedClipboard(ctx context.Context) (string, bool, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := openSharedClipboard(ctx, 0); err != nil {
		return "", false, err
	}
	defer imageClipboardClose.Call()
	if ok, _, _ := sharedClipboardFormat.Call(13); ok == 0 {
		return "", false, nil
	}
	handle, _, err := sharedClipboardGet.Call(13)
	if handle == 0 {
		return "", false, fmt.Errorf("读取文本剪贴板：%w", err)
	}
	size, _, _ := sharedClipboardSize.Call(handle)
	if size < 2 || size > 2*(core.ScreenClipboardTextLimit+1) || size%2 != 0 {
		return "", false, fmt.Errorf("文本剪贴板大小无效或超过共享上限")
	}
	pointer, _, err := imageClipboardLock.Call(handle)
	if pointer == 0 {
		return "", false, fmt.Errorf("读取文本剪贴板内存：%w", err)
	}
	defer imageClipboardUnlock.Call(handle)
	words := unsafe.Slice((*uint16)(unsafe.Pointer(pointer)), int(size/2))
	for i, word := range words {
		if word == 0 {
			return windows.UTF16ToString(words[:i]), true, nil
		}
	}
	return "", false, fmt.Errorf("文本剪贴板缺少结束标记")
}

func writeSharedClipboard(ctx context.Context, text string) error {
	if err := core.ValidateScreenClipboardText(text); err != nil {
		return err
	}
	words, err := windows.UTF16FromString(text)
	if err != nil {
		return err
	}
	data := make([]byte, len(words)*2)
	for i, word := range words {
		binary.LittleEndian.PutUint16(data[i*2:], word)
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var owner uintptr
	imageClipboardEnum.Call(imageClipboardWindowCallback, uintptr(unsafe.Pointer(&owner)))
	if owner == 0 {
		return fmt.Errorf("找不到 LogHelp 窗口，无法写入剪贴板")
	}
	handle, err := clipboardMemory(data)
	if err != nil {
		return err
	}
	defer func() {
		if handle != 0 {
			imageClipboardFree.Call(handle)
		}
	}()
	if err := openSharedClipboard(ctx, owner); err != nil {
		return err
	}
	defer imageClipboardClose.Call()
	if err := ctx.Err(); err != nil {
		return err
	}
	if ok, _, err := imageClipboardEmpty.Call(); ok == 0 {
		return fmt.Errorf("清空文本剪贴板：%w", err)
	}
	if ok, _, err := imageClipboardSet.Call(13, handle); ok == 0 {
		return fmt.Errorf("写入文本剪贴板：%w", err)
	}
	handle = 0
	return nil
}
