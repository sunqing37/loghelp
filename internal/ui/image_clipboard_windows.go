//go:build windows

package ui

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"
	"os"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"loghelp/internal/core"
)

var imageClipboardUser = windows.NewLazySystemDLL("user32.dll")
var imageClipboardKernel = windows.NewLazySystemDLL("kernel32.dll")
var imageClipboardOpen = imageClipboardUser.NewProc("OpenClipboard")
var imageClipboardClose = imageClipboardUser.NewProc("CloseClipboard")
var imageClipboardEmpty = imageClipboardUser.NewProc("EmptyClipboard")
var imageClipboardSet = imageClipboardUser.NewProc("SetClipboardData")
var imageClipboardRegister = imageClipboardUser.NewProc("RegisterClipboardFormatW")
var imageClipboardEnum = imageClipboardUser.NewProc("EnumWindows")
var imageClipboardAlloc = imageClipboardKernel.NewProc("GlobalAlloc")
var imageClipboardFree = imageClipboardKernel.NewProc("GlobalFree")
var imageClipboardLock = imageClipboardKernel.NewProc("GlobalLock")
var imageClipboardUnlock = imageClipboardKernel.NewProc("GlobalUnlock")

var imageClipboardWindowCallback = windows.NewCallback(func(window, parameter uintptr) uintptr {
	var pid uint32
	dialogWindowProcess.Call(window, uintptr(unsafe.Pointer(&pid)))
	if pid == uint32(os.Getpid()) {
		*(*uintptr)(unsafe.Pointer(parameter)) = window
		return 0
	}
	return 1
})

func clipboardMemory(data []byte) (uintptr, error) {
	handle, _, err := imageClipboardAlloc.Call(2, uintptr(len(data))) // GMEM_MOVEABLE
	if handle == 0 {
		return 0, fmt.Errorf("分配剪贴板图片：%w", err)
	}
	pointer, _, err := imageClipboardLock.Call(handle)
	if pointer == 0 {
		imageClipboardFree.Call(handle)
		return 0, fmt.Errorf("锁定剪贴板图片：%w", err)
	}
	copy(unsafe.Slice((*byte)(unsafe.Pointer(pointer)), len(data)), data)
	imageClipboardUnlock.Call(handle)
	return handle, nil
}

func copyScreenshotToClipboard(frame *core.ScreenFrame) error {
	if frame == nil || frame.Image == nil {
		return fmt.Errorf("没有可复制的截图")
	}
	width, height := frame.Image.Bounds().Dx(), frame.Image.Bounds().Dy()
	if width < 1 || height < 1 || int64(width)*int64(height) > 20_000_000 {
		return fmt.Errorf("剪贴板图片尺寸不支持")
	}
	nrgba, ok := frame.Image.(*image.NRGBA)
	if !ok {
		nrgba = image.NewNRGBA(image.Rect(0, 0, width, height))
		draw.Draw(nrgba, nrgba.Bounds(), frame.Image, frame.Image.Bounds().Min, draw.Src)
	}
	// Standard CF_DIB works with Paint, Office and chat apps; also publish PNG
	// so applications which understand it receive the untouched original bytes.
	dib := make([]byte, 40+width*height*4)
	binary.LittleEndian.PutUint32(dib[0:4], 40)
	binary.LittleEndian.PutUint32(dib[4:8], uint32(width))
	binary.LittleEndian.PutUint32(dib[8:12], uint32(height))
	binary.LittleEndian.PutUint16(dib[12:14], 1)
	binary.LittleEndian.PutUint16(dib[14:16], 32)
	binary.LittleEndian.PutUint32(dib[20:24], uint32(width*height*4))
	for y := 0; y < height; y++ {
		source := nrgba.Pix[y*nrgba.Stride:]
		dest := dib[40+(height-1-y)*width*4:]
		for x := 0; x < width; x++ {
			offset := x * 4
			dest[offset], dest[offset+1], dest[offset+2], dest[offset+3] = source[offset+2], source[offset+1], source[offset], 255
		}
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var owner uintptr
	imageClipboardEnum.Call(imageClipboardWindowCallback, uintptr(unsafe.Pointer(&owner)))
	if owner == 0 {
		return fmt.Errorf("找不到 LogHelp 窗口，无法复制图片")
	}
	name, _ := windows.UTF16PtrFromString("PNG")
	pngFormat, _, err := imageClipboardRegister.Call(uintptr(unsafe.Pointer(name)))
	if pngFormat == 0 {
		return fmt.Errorf("注册剪贴板 PNG 格式：%w", err)
	}
	dibHandle, err := clipboardMemory(dib)
	if err != nil {
		return err
	}
	defer func() {
		if dibHandle != 0 {
			imageClipboardFree.Call(dibHandle)
		}
	}()
	pngHandle, err := clipboardMemory(frame.PNG)
	if err != nil {
		return err
	}
	defer func() {
		if pngHandle != 0 {
			imageClipboardFree.Call(pngHandle)
		}
	}()
	opened := false
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		result, _, openErr := imageClipboardOpen.Call(owner)
		if result != 0 {
			opened = true
			break
		}
		err = openErr
		time.Sleep(10 * time.Millisecond)
	}
	if !opened {
		return fmt.Errorf("剪贴板正在被占用，请重试：%w", err)
	}
	defer imageClipboardClose.Call()
	if result, _, err := imageClipboardEmpty.Call(); result == 0 {
		return fmt.Errorf("清空剪贴板失败：%w", err)
	}
	if result, _, err := imageClipboardSet.Call(8, dibHandle); result == 0 {
		return fmt.Errorf("复制截图位图失败：%w", err)
	}
	dibHandle = 0 // Clipboard now owns this allocation.
	if result, _, err := imageClipboardSet.Call(pngFormat, pngHandle); result == 0 {
		return fmt.Errorf("截图位图已复制，但 PNG 格式写入失败：%w", err)
	}
	pngHandle = 0
	return nil
}
