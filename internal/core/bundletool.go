package core

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type bundletoolRunner struct{ java, jar string }

func findBundletool(options AppInstallOptions) (*bundletoolRunner, error) {
	java := strings.TrimSpace(options.JavaPath)
	if java == "" {
		java = "java"
		if home := os.Getenv("JAVA_HOME"); home != "" {
			name := "java"
			if runtime.GOOS == "windows" {
				name += ".exe"
			}
			candidate := filepath.Join(home, "bin", name)
			if st, err := os.Stat(candidate); err == nil && st.Mode().IsRegular() {
				java = candidate
			}
		}
	}
	java, err := exec.LookPath(java)
	if err != nil {
		return nil, fmt.Errorf("AAB/官方 APKS 需要 Java，请安装 JDK 17+，或在安装工具设置中选择 java 可执行文件：%w", err)
	}
	jar := strings.TrimSpace(options.BundletoolPath)
	if jar == "" {
		jar = os.Getenv("BUNDLETOOL_JAR")
	}
	if jar == "" {
		if exe, err := os.Executable(); err == nil {
			candidate := filepath.Join(filepath.Dir(exe), "tools", "bundletool.jar")
			if st, err := os.Stat(candidate); err == nil && st.Mode().IsRegular() {
				jar = candidate
			}
		}
	}
	if jar == "" {
		return nil, fmt.Errorf("AAB/官方 APKS 需要 bundletool：请从 github.com/google/bundletool/releases 下载 bundletool-all JAR，在安装工具设置中选择，或放到程序旁 tools/bundletool.jar")
	}
	jar, err = filepath.Abs(jar)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(jar)
	if err != nil || !st.Mode().IsRegular() || !strings.EqualFold(filepath.Ext(jar), ".jar") {
		return nil, fmt.Errorf("bundletool JAR 路径无效：%s", jar)
	}
	return &bundletoolRunner{java: java, jar: jar}, nil
}

func (r *bundletoolRunner) run(ctx context.Context, args ...string) error {
	return r.runProgress(ctx, nil, args...)
}

func (r *bundletoolRunner) runProgress(ctx context.Context, progress func(string), args ...string) error {
	args = append([]string{"-Dfile.encoding=UTF-8", "-jar", r.jar}, args...)
	cmd := exec.CommandContext(ctx, r.java, args...)
	configureProcess(cmd)
	_, err := runInstallCommand(cmd, progress)
	return err
}

func (r *bundletoolRunner) extractInstallGroup(ctx context.Context, archive, spec, output string, progress func(string)) (installGroup, error) {
	if err := r.runProgress(ctx, progress, "extract-apks", "--apks="+archive, "--device-spec="+spec, "--output-dir="+output); err != nil {
		return installGroup{}, fmt.Errorf("选择设备适配 APK %s：%w", filepath.Base(archive), err)
	}
	var apks []installAPK
	err := filepath.WalkDir(output, func(file string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("bundletool 输出包含符号链接：%s", file)
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(file), ".apk") {
			return nil
		}
		if len(apks) >= 512 {
			return fmt.Errorf("单个安装包最多支持 512 个 APK")
		}
		apk, err := inspectInstallAPK(file, filepath.Base(file))
		if err != nil {
			return err
		}
		apks = append(apks, apk)
		return nil
	})
	if err != nil {
		return installGroup{}, fmt.Errorf("读取 bundletool 输出：%w", err)
	}
	groups, err := groupInstallAPKs(apks)
	if err != nil {
		return installGroup{}, err
	}
	if len(groups) != 1 {
		return installGroup{}, fmt.Errorf("APKS 需包含一个应用，设备适配结果为 %d 个应用", len(groups))
	}
	return groups[0], nil
}
