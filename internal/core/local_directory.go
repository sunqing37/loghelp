package core

import (
	"fmt"
	"os"
	"path/filepath"
)

// OpenLocalDirectory uses the same platform opener as release links, with an
// absolute local directory instead of a URL. No shell command is constructed.
func OpenLocalDirectory(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("不是有效目录")
	}
	return openReleaseURL(abs)
}
