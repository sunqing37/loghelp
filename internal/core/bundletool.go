package core

import (
	"context"
	"fmt"
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
	args = append([]string{"-Dfile.encoding=UTF-8", "-jar", r.jar}, args...)
	cmd := exec.CommandContext(ctx, r.java, args...)
	configureProcess(cmd)
	out := &limitedBuffer{limit: 1 << 20}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w：%s", err, strings.TrimSpace(out.b.String()))
	}
	return nil
}
