package core

import (
	"fmt"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func loadVideoLibraries(directory string) (func(string, string) (uintptr, error), error) {
	libraries := map[string]windows.Handle{}
	for _, name := range []string{"avutil-59.dll", "swresample-5.dll", "avcodec-61.dll"} {
		h, err := windows.LoadLibraryEx(filepath.Join(directory, name), 0, windows.LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR|windows.LOAD_LIBRARY_SEARCH_SYSTEM32)
		if err != nil {
			for _, previous := range libraries {
				windows.FreeLibrary(previous)
			}
			return nil, fmt.Errorf("加载投屏解码库 %s：%w", name, err)
		}
		libraries[name] = h
	}
	return func(library, name string) (uintptr, error) { return windows.GetProcAddress(libraries[library], name) }, nil
}
