//go:build darwin || linux

package core

import (
	"fmt"
	"path/filepath"
	"runtime"

	"github.com/ebitengine/purego"
)

func loadVideoLibraries(directory string) (func(string, string) (uintptr, error), error) {
	libraries := map[string]uintptr{}
	for _, library := range []struct{ alias, linux, mac string }{
		{"avutil-59.dll", "libavutil.so.59", "libavutil.59.dylib"},
		{"avcodec-61.dll", "libavcodec.so.61", "libavcodec.61.dylib"},
	} {
		name := library.linux
		if runtime.GOOS == "darwin" {
			name = library.mac
		}
		paths := []string{filepath.Join(directory, name), name}
		if runtime.GOOS == "darwin" {
			for _, root := range []string{"/opt/homebrew/opt/ffmpeg@7/lib", "/usr/local/opt/ffmpeg@7/lib", "/opt/homebrew/opt/ffmpeg/lib", "/usr/local/opt/ffmpeg/lib"} {
				paths = append(paths, filepath.Join(root, name))
			}
		}
		var handle uintptr
		var last error
		for _, path := range paths {
			handle, last = purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
			if last == nil {
				break
			}
		}
		if last != nil {
			for _, h := range libraries {
				purego.Dlclose(h)
			}
			return nil, fmt.Errorf("加载投屏解码库 %s：%w", name, last)
		}
		libraries[library.alias] = handle
	}
	return func(library, name string) (uintptr, error) { return purego.Dlsym(libraries[library], name) }, nil
}
