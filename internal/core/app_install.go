package core

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type AppInstallOptions struct {
	JavaPath       string `json:"java_path,omitempty"`
	BundletoolPath string `json:"bundletool_path,omitempty"`
}

// Completed counts applications whose APKs and optional OBB files all finished.
// Total is zero while preparing inputs; it is not a byte-transfer percentage.
type AppInstallProgress struct {
	Message                        string
	Completed, Total               int
	CurrentApp, CurrentFile, Stage string
	SentBytes, TotalBytes          int64
}

func IsAppInstallPath(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".apk", ".aab", ".apks", ".xapk", ".apkm", ".multi-apk":
		return true
	}
	return false
}

type installAPK struct {
	path, name string
	id         apkIdentity
	abis       map[string]bool
}

type installOBB struct{ local, name string }
type installGroup struct {
	apks []installAPK
	obbs []installOBB
}

type installPlan struct {
	groups  []installGroup
	bundles []string
}

// All work is scoped to a fixed serial, including bundletool's ADB calls.
// Preparation of every input completes before the first device mutation.
func (a *ADB) InstallApplications(ctx context.Context, serial string, paths []string, options AppInstallOptions, progress func(AppInstallProgress)) (resultErr error) {
	if a == nil || strings.TrimSpace(serial) == "" {
		return fmt.Errorf("请先选择在线安装设备并配置 ADB")
	}
	if len(paths) == 0 || len(paths) > 128 {
		return fmt.Errorf("请选择 1–128 个安装文件")
	}
	stateProgress := AppInstallProgress{}
	report := func(s string) {
		stateProgress.Message = s
		if progress != nil {
			progress(stateProgress)
		}
	}
	commandProgress := func(stage string) func(string) {
		report(stage)
		return func(line string) { report(stage + "\n" + line) }
	}
	report("正在确认目标设备…")
	state, err := a.Run(ctx, serial, "get-state")
	if err != nil {
		return fmt.Errorf("确认设备 %s 状态：%w", serial, err)
	}
	if strings.TrimSpace(state) != "device" {
		return fmt.Errorf("设备 %s 不在线或未授权：%s", serial, state)
	}
	dir, err := os.MkdirTemp("", "loghelp-install-")
	if err != nil {
		return fmt.Errorf("创建安装临时目录：%w", err)
	}
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("清理安装临时目录 %s：%w", dir, err))
		}
	}()
	plan := installPlan{}
	var loose []installAPK
	seen := map[string]bool{}
	for i, file := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		file, err = filepath.Abs(file)
		if err != nil {
			return err
		}
		if seen[file] {
			continue
		}
		seen[file] = true
		st, err := os.Stat(file)
		if err != nil {
			return fmt.Errorf("读取安装文件 %s：%w", file, err)
		}
		if !st.Mode().IsRegular() || !IsAppInstallPath(file) {
			return fmt.Errorf("不是支持的安装文件：%s", file)
		}
		report(fmt.Sprintf("正在解析 %d/%d：%s", i+1, len(paths), filepath.Base(file)))
		ext := strings.ToLower(filepath.Ext(file))
		if ext == ".aab" {
			plan.bundles = append(plan.bundles, file)
		} else if ext == ".apk" {
			apk, err := inspectInstallAPK(file, filepath.Base(file))
			if err != nil {
				return err
			}
			loose = append(loose, apk)
		} else {
			groups, bundle, err := unpackInstallArchive(ctx, file, dir, i)
			if err != nil {
				return err
			}
			plan.groups = append(plan.groups, groups...)
			if bundle {
				plan.bundles = append(plan.bundles, file)
			}
		}
	}
	groups, err := groupInstallAPKs(loose)
	if err != nil {
		return err
	}
	plan.groups = append(groups, plan.groups...)
	stateProgress.Total = len(plan.groups) + len(plan.bundles)
	var runner *bundletoolRunner
	if len(plan.bundles) > 0 {
		runner, err = findBundletool(options)
		if err != nil {
			return err
		}
		report("正在检查 Java 与 bundletool…")
		if err := runner.run(ctx, "version"); err != nil {
			return fmt.Errorf("Java / bundletool 不可用，请检查安装工具设置：%w", err)
		}
	}
	// Filter native ABI configuration splits; retain all language/density splits
	// so Android can resolve resources without guessing the user's locale.
	for i := range plan.groups {
		plan.groups[i].apks, err = a.matchInstallAPKs(ctx, serial, plan.groups[i].apks)
		if err != nil {
			return err
		}
	}
	for i, file := range plan.bundles {
		if strings.EqualFold(filepath.Ext(file), ".aab") {
			output := filepath.Join(dir, fmt.Sprintf("bundle-%d.apks", i))
			if err := runner.runProgress(ctx, commandProgress("正在从 AAB 生成设备匹配的 APK（调试签名）："+filepath.Base(file)), "build-apks", "--bundle="+file, "--output="+output, "--connected-device", "--device-id="+serial, "--adb="+a.Path); err != nil {
				return fmt.Errorf("AAB 转换失败（需可用的调试签名密钥）：%w", err)
			}
			plan.bundles[i] = output
		}
	}
	// Let bundletool select device-compatible APKs, then upload them through the
	// same measured install session as ordinary APKs (all preparation first).
	if len(plan.bundles) > 0 {
		report("正在读取设备配置以选择 APKS 分包…")
		spec := filepath.Join(dir, "device-spec.json")
		if err := runner.runProgress(ctx, commandProgress("正在读取设备配置…"), "get-device-spec", "--output="+spec, "--device-id="+serial, "--adb="+a.Path); err != nil {
			return fmt.Errorf("读取 bundletool 设备配置：%w", err)
		}
		for i, file := range plan.bundles {
			group, err := runner.extractInstallGroup(ctx, file, spec, filepath.Join(dir, fmt.Sprintf("selected-%d", i)), commandProgress("正在提取设备匹配的 APK："+filepath.Base(file)))
			if err != nil {
				return err
			}
			plan.groups = append(plan.groups, group)
		}
	}
	// Shell-v2 carries binary stdin, EOF and a remote exit status reliably. Keep
	// the previous ADB install/push path for older devices or ADB versions.
	features, featureErr := a.Run(ctx, serial, "features")
	if err := ctx.Err(); err != nil {
		return err
	}
	measured := featureErr == nil && hasInstallFeature(features, "shell_v2")
	completed := 0
	defer func() {
		if resultErr != nil && completed > 0 {
			resultErr = fmt.Errorf("已完成 %d 项安装，后续失败；已安装的应用或 OBB 不会回滚：%w", completed, resultErr)
		}
	}()
	for _, g := range plan.groups {
		if err := ctx.Err(); err != nil {
			return err
		}
		pkg := g.apks[0].id.Package
		stateProgress.CurrentApp = pkg
		stateProgress.CurrentFile, stateProgress.Stage = "", "准备安装"
		stateProgress.SentBytes, stateProgress.TotalBytes = 0, 0
		report(fmt.Sprintf("正在安装 %d/%d：%s（%d 个 APK）", stateProgress.Completed+1, stateProgress.Total, pkg, len(g.apks)))
		install := a.installMeasuredAPKs
		if !measured {
			install = a.installLegacyAPKs
		}
		if err := install(ctx, serial, g.apks, func(stage, file string, sent, total int64) {
			stateProgress.Stage, stateProgress.CurrentFile = stage, file
			stateProgress.SentBytes, stateProgress.TotalBytes = sent, total
			report(stage + " · " + pkg)
		}); err != nil {
			return fmt.Errorf("安装 %s 失败：%w\n%s", pkg, err, installFailureHint(err.Error()))
		}
		completed++
		for _, obb := range g.obbs {
			stateProgress.Stage, stateProgress.CurrentFile = "复制附加文件", obb.name
			stateProgress.SentBytes, stateProgress.TotalBytes = 0, 0
			report("应用已安装，正在准备复制 OBB：" + obb.name)
			target := "/sdcard/Android/obb/" + pkg
			if _, err := a.Run(ctx, serial, "shell", "mkdir", "-p", target); err != nil {
				return fmt.Errorf("应用已安装，但创建 OBB 目录失败（检查设备存储权限）：%w", err)
			}
			var copyErr error
			if !measured {
				stateProgress.Stage = "旧版设备通过 ADB 复制 OBB，无法提供字节进度"
				report(stateProgress.Stage + "：" + obb.name)
				_, copyErr = runInstallCommand(a.Command(ctx, serial, "push", obb.local, target+"/"+obb.name), commandProgress("正在复制 OBB："+obb.name))
			} else {
				copyErr = a.installMeasuredOBB(ctx, serial, obb.local, target+"/"+obb.name, func(stage string, sent, total int64) {
					stateProgress.Stage = stage
					stateProgress.SentBytes, stateProgress.TotalBytes = sent, total
					report("应用已安装 · " + stage + "：" + obb.name)
				})
			}
			if copyErr != nil {
				return fmt.Errorf("应用已安装，但复制 OBB %s 失败：%w", obb.name, copyErr)
			}
		}
		stateProgress.Completed++
		stateProgress.Stage = "安装完成"
		report("已完成：" + pkg)
	}
	report(fmt.Sprintf("安装完成：%d 项应用安装，设备 %s", completed, serial))
	return nil
}

func installOutputSucceeded(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "Success" {
			return true
		}
	}
	return false
}

func installFailureHint(out string) string {
	switch {
	case strings.Contains(out, "UPDATE_INCOMPATIBLE"), strings.Contains(out, "INCONSISTENT_CERTIFICATES"):
		return "签名不一致，请使用与已安装应用相同签名的安装包；工具不会自动卸载应用。"
	case strings.Contains(out, "VERSION_DOWNGRADE"):
		return "版本低于已安装应用，请选择更新版本。"
	case strings.Contains(out, "MISSING_SPLIT"), strings.Contains(out, "INVALID_APK"):
		return "请确认 base APK 与必需的 split 来自同一版本，且已完整选择。"
	case strings.Contains(out, "NO_MATCHING_ABIS"):
		return "安装包 CPU 架构与设备不匹配，请选择适配此设备的版本。"
	case strings.Contains(out, "INSUFFICIENT_STORAGE"):
		return "设备存储空间不足，请清理后重试。"
	case strings.Contains(out, "USER_RESTRICTED"):
		return "安装被设备限制，请检查 USB 安装权限并在手机上确认。"
	}
	return "请检查手机安装确认、包签名、Android 版本及存储空间；完整错误见上方。"
}

func inspectInstallAPK(file, name string) (installAPK, error) {
	r, err := zip.OpenReader(file)
	if err != nil {
		return installAPK{}, fmt.Errorf("读取 APK %s：%w", name, err)
	}
	defer r.Close()
	a := installAPK{path: file, name: name, abis: map[string]bool{}}
	found := false
	for _, f := range r.File {
		if f.Name == "AndroidManifest.xml" {
			if found {
				return a, fmt.Errorf("APK %s 有重复 manifest", name)
			}
			found = true
			data, err := readInstallEntry(f, 4<<20)
			if err != nil {
				return a, err
			}
			a.id, err = parseAPKIdentity(data)
			if err != nil {
				return a, fmt.Errorf("解析 %s：%w", name, err)
			}
		}
		parts := strings.Split(f.Name, "/")
		if len(parts) >= 3 && parts[0] == "lib" && strings.HasSuffix(f.Name, ".so") {
			a.abis[parts[1]] = true
		}
	}
	if !found || !installPackageName.MatchString(a.id.Package) {
		return a, fmt.Errorf("APK %s 缺少有效 manifest/package", name)
	}
	return a, nil
}

var installPackageName = regexp.MustCompile(`^[A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+)*$`)

func readInstallEntry(f *zip.File, limit int64) ([]byte, error) {
	if f.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("安装包条目过大：%s", f.Name)
	}
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err == nil && int64(len(b)) > limit {
		err = fmt.Errorf("安装包条目超过大小限制：%s", f.Name)
	}
	return b, err
}

func groupInstallAPKs(apks []installAPK) ([]installGroup, error) {
	var groups []installGroup
	indices := map[string]int{}
	for _, apk := range apks {
		key := apk.id.Package
		i, ok := indices[key]
		if !ok {
			i = len(groups)
			indices[key] = i
			groups = append(groups, installGroup{})
		}
		for _, previous := range groups[i].apks {
			if previous.id.Version != apk.id.Version || previous.id.Split == apk.id.Split {
				return nil, fmt.Errorf("%s 与 %s 的版本冲突或 split 重复；同一应用只选择一个版本、一份 base 及其 splits", previous.name, apk.name)
			}
		}
		groups[i].apks = append(groups[i].apks, apk)
	}
	for i := range groups {
		g := &groups[i]
		sort.SliceStable(g.apks, func(i, j int) bool { return g.apks[i].id.Split < g.apks[j].id.Split })
		if g.apks[0].id.Split != "" {
			return nil, fmt.Errorf("%s 缺少 base APK，请同时选择基础包和分包", g.apks[0].id.Package)
		}
	}
	return groups, nil
}

func (a *ADB) matchInstallAPKs(ctx context.Context, serial string, apks []installAPK) ([]installAPK, error) {
	// A base APK may itself be universal; do not filter it or feature modules.
	abiFor := func(apk installAPK) string {
		split := strings.ReplaceAll(apk.id.Split, "-", "_")
		for _, abi := range []string{"arm64-v8a", "armeabi-v7a", "armeabi", "x86_64", "x86", "riscv64"} {
			if strings.HasSuffix(split, "config."+strings.ReplaceAll(abi, "-", "_")) {
				return abi
			}
		}
		if strings.Contains(split, "config.") && len(apk.abis) == 1 {
			for abi := range apk.abis {
				return abi
			}
		}
		return ""
	}
	available := map[string]bool{}
	for _, apk := range apks {
		if abi := abiFor(apk); abi != "" {
			available[abi] = true
		}
	}
	if len(available) == 0 {
		return apks, nil
	}
	out, err := a.Run(ctx, serial, "shell", "getprop", "ro.product.cpu.abilist")
	if err != nil {
		return nil, fmt.Errorf("读取设备 CPU 架构：%w", err)
	}
	if strings.TrimSpace(out) == "" {
		out, err = a.Run(ctx, serial, "shell", "getprop", "ro.product.cpu.abi")
		if err != nil {
			return nil, err
		}
	}
	chosen := ""
	for _, abi := range strings.Split(strings.TrimSpace(out), ",") {
		if available[strings.TrimSpace(abi)] {
			chosen = strings.TrimSpace(abi)
			break
		}
	}
	if chosen == "" {
		return nil, fmt.Errorf("%s 的 ABI 分包不适配设备架构 %s", apks[0].id.Package, out)
	}
	var result []installAPK
	for _, apk := range apks {
		if abi := abiFor(apk); abi == "" || abi == chosen {
			result = append(result, apk)
		}
	}
	return result, nil
}

type installContextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r installContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func unpackInstallArchive(ctx context.Context, file, dir string, index int) ([]installGroup, bool, error) {
	r, err := zip.OpenReader(file)
	if err != nil {
		return nil, false, fmt.Errorf("读取 %s 失败；仅支持未加密的 ZIP 安装包：%w", filepath.Base(file), err)
	}
	defer r.Close()
	if len(r.File) > 8192 {
		return nil, false, fmt.Errorf("安装包文件数超过 8192：%s", file)
	}
	for _, f := range r.File {
		if f.Name == "toc.pb" {
			return nil, true, nil // Official APK set; bundletool selects variants.
		}
	}
	var apks []installAPK
	var obbs []installOBB
	var total uint64
	for i, f := range r.File {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		ext := strings.ToLower(path.Ext(f.Name))
		if ext != ".apk" && ext != ".obb" {
			continue
		}
		// Never use an archive entry as a local filesystem path.
		if strings.Contains(f.Name, "\\") || path.IsAbs(f.Name) || path.Clean(f.Name) != f.Name || strings.HasPrefix(f.Name, "../") || f.Mode()&os.ModeSymlink != 0 || f.FileInfo().IsDir() {
			return nil, false, fmt.Errorf("安装包中有非法路径：%s", f.Name)
		}
		if f.UncompressedSize64 > 8<<30 || total > 16<<30-f.UncompressedSize64 {
			return nil, false, fmt.Errorf("安装包解压超过限制（单文件 8 GiB，总计 16 GiB）")
		}
		total += f.UncompressedSize64
		local := filepath.Join(dir, fmt.Sprintf("%d-%d%s", index, i, ext))
		if err := extractInstallEntry(ctx, f, local); err != nil {
			return nil, false, fmt.Errorf("解压 %s：%w", f.Name, err)
		}
		if ext == ".apk" {
			apk, err := inspectInstallAPK(local, f.Name)
			if err != nil {
				return nil, false, err
			}
			apks = append(apks, apk)
			if len(apks) > 512 {
				return nil, false, fmt.Errorf("单个安装包最多支持 512 个 APK")
			}
		} else {
			obbs = append(obbs, installOBB{local: local, name: path.Base(f.Name)})
		}
	}
	groups, err := groupInstallAPKs(apks)
	if err != nil {
		return nil, false, err
	}
	if len(groups) != 1 {
		return nil, false, fmt.Errorf("%s 应包含一个应用的 base APK 与 splits，实际发现 %d 个应用", filepath.Base(file), len(groups))
	}
	pkg := groups[0].apks[0].id.Package
	obbPattern := regexp.MustCompile(`^(main|patch)\.[0-9]+\.` + regexp.QuoteMeta(pkg) + `\.obb$`)
	seen := map[string]bool{}
	for _, obb := range obbs {
		if !obbPattern.MatchString(obb.name) || seen[obb.name] {
			return nil, false, fmt.Errorf("OBB 文件名重复或与应用 %s 不匹配：%s", pkg, obb.name)
		}
		seen[obb.name] = true
	}
	groups[0].obbs = obbs
	return groups, false, nil
}

func extractInstallEntry(ctx context.Context, f *zip.File, local string) error {
	r, err := f.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	w, err := os.OpenFile(local, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(w, io.LimitReader(installContextReader{ctx, r}, int64(f.UncompressedSize64)+1))
	closeErr := w.Close()
	if copyErr == nil && uint64(n) != f.UncompressedSize64 {
		copyErr = fmt.Errorf("条目解压大小不匹配")
	}
	return errors.Join(copyErr, closeErr)
}
