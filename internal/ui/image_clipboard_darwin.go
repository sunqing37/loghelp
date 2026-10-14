package ui

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"loghelp/internal/core"
)

var imagePasteboard struct {
	once     sync.Once
	err      error
	class    func(string) uintptr
	selector func(string) uintptr
	send0    func(uintptr, uintptr) uintptr
	send1    func(uintptr, uintptr, uintptr) uintptr
	send2    func(uintptr, uintptr, uintptr, uintptr) uintptr
}

// AppKit's NSPasteboard/NSImage publish native image representations. Runtime
// binding keeps both Intel and Apple Silicon builds free of Objective-C/CGO.
func copyScreenshotToClipboard(frame *core.ScreenFrame) error {
	if frame == nil || len(frame.PNG) == 0 {
		return fmt.Errorf("没有可复制的截图")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	imagePasteboard.once.Do(func() {
		if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
			imagePasteboard.err = fmt.Errorf("加载图片剪贴板 AppKit：%w", err)
			return
		}
		library, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			imagePasteboard.err = err
			return
		}
		for _, binding := range []struct {
			name   string
			target any
		}{
			{"objc_getClass", &imagePasteboard.class}, {"sel_registerName", &imagePasteboard.selector},
			{"objc_msgSend", &imagePasteboard.send0}, {"objc_msgSend", &imagePasteboard.send1}, {"objc_msgSend", &imagePasteboard.send2},
		} {
			address, err := purego.Dlsym(library, binding.name)
			if err != nil {
				imagePasteboard.err = err
				return
			}
			purego.RegisterFunc(binding.target, address)
		}
	})
	if imagePasteboard.err != nil {
		return imagePasteboard.err
	}
	p := &imagePasteboard
	sel := p.selector
	pool := p.send0(p.class("NSAutoreleasePool"), sel("new"))
	if pool == 0 {
		return fmt.Errorf("无法创建图片剪贴板内存池")
	}
	defer p.send0(pool, sel("drain"))
	data := p.send2(p.class("NSData"), sel("dataWithBytes:length:"), uintptr(unsafe.Pointer(&frame.PNG[0])), uintptr(len(frame.PNG)))
	runtime.KeepAlive(frame)
	if data == 0 {
		return fmt.Errorf("无法创建剪贴板 PNG 数据")
	}
	img := p.send1(p.send0(p.class("NSImage"), sel("alloc")), sel("initWithData:"), data)
	if img == 0 {
		return fmt.Errorf("AppKit 未能读取截图 PNG")
	}
	defer p.send0(img, sel("release"))
	objects := p.send1(p.class("NSArray"), sel("arrayWithObject:"), img)
	board := p.send0(p.class("NSPasteboard"), sel("generalPasteboard"))
	if objects == 0 || board == 0 {
		return fmt.Errorf("macOS 图片剪贴板不可用")
	}
	p.send0(board, sel("clearContents"))
	if p.send1(board, sel("writeObjects:"), objects)&0xff == 0 {
		return fmt.Errorf("macOS 拒绝写入截图到图片剪贴板")
	}
	return nil
}
