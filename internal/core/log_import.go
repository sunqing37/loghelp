package core

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type LogImportOptions struct {
	XlogKeyFile string       `json:"xlog_key_file,omitempty"`
	XlogKeyEnv  string       `json:"xlog_key_env,omitempty"`
	Decoders    []LogDecoder `json:"decoders,omitempty"`
}

// Arguments are an argv array, never a shell command. Keys stay in local files
// or the process environment rather than the settings file or command line.
type LogDecoder struct {
	Name       string   `json:"name"`
	Extensions []string `json:"extensions"`
	Program    string   `json:"program"`
	Arguments  []string `json:"arguments"`
	Output     string   `json:"output"` // file or stdout
	KeyFile    string   `json:"key_file,omitempty"`
	KeyEnv     string   `json:"key_env,omitempty"`
}

func ValidateLogImport(options LogImportOptions) error {
	if err := ValidateXlogKeySource(options); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, d := range options.Decoders {
		if strings.TrimSpace(d.Name) == "" || strings.TrimSpace(d.Program) == "" || len(d.Extensions) == 0 {
			return fmt.Errorf("解码器需要名称、扩展名和程序路径")
		}
		if d.Output != "file" && d.Output != "stdout" {
			return fmt.Errorf("解码器 %s 的输出方式需为 file 或 stdout", d.Name)
		}
		input, output := false, false
		for _, arg := range d.Arguments {
			if strings.IndexByte(arg, 0) >= 0 {
				return fmt.Errorf("解码器 %s 的参数包含无效字符", d.Name)
			}
			input = input || strings.Contains(arg, "{input}")
			output = output || strings.Contains(arg, "{output}")
			if strings.Contains(arg, "{keyfile}") && d.KeyFile == "" {
				return fmt.Errorf("解码器 %s 使用了 {keyfile}，请设置密钥文件", d.Name)
			}
		}
		if !input || d.Output == "file" && !output || d.Output == "stdout" && output {
			return fmt.Errorf("解码器 %s 需要 {input}；file 输出还需 {output}，stdout 输出不能使用 {output}", d.Name)
		}
		for _, extension := range d.Extensions {
			if extension != strings.ToLower(strings.TrimSpace(extension)) || len(extension) < 2 || extension[0] != '.' || strings.ContainsAny(extension, `/\:*?[] `) || archiveKind("file"+extension) != "" {
				return fmt.Errorf("解码器 %s 的扩展名需为小写点号格式，且不能覆盖内置压缩格式", d.Name)
			}
			if seen[extension] {
				return fmt.Errorf("扩展名 %s 配置了多个解码器", extension)
			}
			seen[extension] = true
		}
	}
	return nil
}

const (
	maxImportBytes = int64(4 << 30)
	maxImportFiles = 4096
)

type logImporter struct {
	ctx      context.Context
	options  LogImportOptions
	dir      string
	bytes    int64
	files    int
	entries  int
	logs     []string
	warnings []string
	progress func(int64)
}

func archiveKind(path string) string {
	path = strings.ToLower(path)
	for _, kind := range []string{".tar.gz", ".tar.bz2", ".tar.zst", ".tgz", ".tbz2", ".zip", ".tar", ".gz", ".bz2", ".zst"} {
		if strings.HasSuffix(path, kind) {
			return kind
		}
	}
	return ""
}

func (options LogImportOptions) decoder(path string) *LogDecoder {
	path = strings.ToLower(path)
	var match *LogDecoder
	length := 0
	for i := range options.Decoders {
		for _, extension := range options.Decoders[i].Extensions {
			if strings.HasSuffix(path, extension) && len(extension) > length {
				match, length = &options.Decoders[i], len(extension)
			}
		}
	}
	return match
}

// OpenLogProgress preserves the original source path for recent files,
// bookmarks and restored tabs; only the indexed files live in a private temp dir.
func OpenLogProgress(ctx context.Context, path string, options LogImportOptions, progress func(int64)) (*Offline, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() || archiveKind(path) == "" && !strings.EqualFold(filepath.Ext(path), ".xlog") && options.decoder(path) == nil {
		return OpenOfflineProgress(ctx, path, progress)
	}
	if err = ValidateLogImport(options); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "loghelp-import-*")
	if err != nil {
		return nil, fmt.Errorf("创建日志临时目录：%w", err)
	}
	o := &Offline{Path: abs, stride: 512, reference: time.Now(), tempDir: dir}
	i := &logImporter{ctx: ctx, options: options, dir: dir, progress: progress}
	if err = i.visit(abs, 0, true); err == nil {
		o.Files = i.logs
		o.Warnings = i.warnings
		if len(o.Files) == 0 {
			err = fmt.Errorf("压缩包中没有支持的日志文件；自定义格式请先在设置中添加解码器")
		} else {
			_, err = indexOffline(ctx, o, progress)
		}
	}
	if err != nil {
		return nil, errors.Join(err, o.Close())
	}
	return o, nil
}

func (i *logImporter) newPath(name string) (string, error) {
	i.files++
	if i.files > maxImportFiles {
		return "", fmt.Errorf("日志解压/解码超过 %d 个临时文件限制", maxImportFiles)
	}
	// Entry names never become destination paths, even for nested archives.
	name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, name)
	if len(name) > 120 {
		name = name[len(name)-120:]
	}
	return filepath.Join(i.dir, fmt.Sprintf("%04d-%s", i.files, name)), nil
}

type importWriter struct {
	i *logImporter
	w io.Writer
}

func (w importWriter) Write(p []byte) (int, error) {
	if err := w.i.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(p)) > maxImportBytes-w.i.bytes {
		return 0, fmt.Errorf("日志解压/解码累计超过 4 GiB 限制")
	}
	n, err := w.w.Write(p)
	w.i.bytes += int64(n)
	if w.i.progress != nil {
		w.i.progress(w.i.bytes)
	}
	return n, err
}

func (i *logImporter) extract(name string, reader io.Reader) (string, error) {
	path, err := i.newPath(name)
	if err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(importWriter{i, f}, reader)
	return path, errors.Join(err, f.Close())
}

func (i *logImporter) supported(name string) bool {
	if archiveKind(name) != "" || i.options.decoder(name) != nil {
		return true
	}
	if strings.EqualFold(filepath.Base(name), "manifest.json") {
		return false // JSONL segments are imported directly, once each.
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".log", ".txt", ".jsonl", ".json", ".xlog":
		return true
	}
	return false
}

func (i *logImporter) entry(name string, reader io.Reader, depth int) error {
	i.entries++
	if i.entries > maxImportFiles {
		return fmt.Errorf("压缩包超过 %d 个文件条目限制", maxImportFiles)
	}
	if err := i.ctx.Err(); err != nil {
		return err
	}
	entryName := strings.ReplaceAll(name, `\`, "/")
	if strings.HasPrefix(entryName, "/") || strings.Contains(entryName, ":") {
		return fmt.Errorf("压缩包包含绝对路径或无效路径：%s", name)
	}
	for _, part := range strings.Split(entryName, "/") {
		if part == ".." {
			return fmt.Errorf("压缩包包含越界路径：%s", name)
		}
	}
	if !i.supported(name) {
		return nil
	}
	path, err := i.extract(name, reader)
	if err != nil {
		return fmt.Errorf("解压 %s：%w", name, err)
	}
	return i.visit(path, depth+1, false)
}

func (i *logImporter) visit(path string, depth int, direct bool) error {
	if err := i.ctx.Err(); err != nil {
		return err
	}
	kind := archiveKind(path)
	if kind != "" {
		if depth >= 4 {
			return fmt.Errorf("压缩包嵌套超过 4 层限制")
		}
		return i.unpack(path, kind, depth)
	}
	var err error
	if decoder := i.options.decoder(path); decoder != nil {
		path, err = i.decodeExternal(path, *decoder)
	} else if strings.EqualFold(filepath.Ext(path), ".xlog") {
		path, err = i.decodeXlog(path)
	} else if !direct && !i.supported(path) {
		return nil
	}
	if err != nil {
		return err
	}
	i.logs = append(i.logs, path)
	return nil
}

func (i *logImporter) unpack(path, kind string, depth int) error {
	if kind == ".zip" {
		z, err := zip.OpenReader(path)
		if err != nil {
			return fmt.Errorf("打开 ZIP：%w", err)
		}
		defer z.Close()
		if len(z.File) > maxImportFiles {
			return fmt.Errorf("压缩包超过 %d 个条目限制", maxImportFiles)
		}
		sort.SliceStable(z.File, func(a, b int) bool { return z.File[a].Name < z.File[b].Name })
		for _, entry := range z.File {
			if entry.FileInfo().IsDir() {
				continue
			}
			if !entry.Mode().IsRegular() {
				return fmt.Errorf("压缩包含非普通文件：%s", entry.Name)
			}
			r, err := entry.Open()
			if err != nil {
				return err
			}
			err = i.entry(entry.Name, r, depth)
			closeErr := r.Close()
			if err = errors.Join(err, closeErr); err != nil {
				return err
			}
		}
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var reader io.Reader = f
	switch {
	case strings.HasSuffix(kind, ".gz") || kind == ".tgz":
		gz, err := gzip.NewReader(f)
		if err != nil {
			return fmt.Errorf("打开 Gzip：%w", err)
		}
		defer gz.Close()
		reader = gz
	case strings.HasSuffix(kind, ".bz2") || kind == ".tbz2":
		reader = bzip2.NewReader(f)
	case strings.HasSuffix(kind, ".zst"):
		z, err := newLogZstdReader(f)
		if err != nil {
			return err
		}
		defer z.Close()
		reader = z
	}
	if strings.HasPrefix(kind, ".tar") || kind == ".tgz" || kind == ".tbz2" {
		t := tar.NewReader(reader)
		for {
			if err := i.ctx.Err(); err != nil {
				return err
			}
			header, err := t.Next()
			if err == io.EOF {
				// Drain through the decompressor to verify its checksum and bound
				// even padding or skipped entries by the shared expansion budget.
				_, err = io.Copy(importWriter{i, io.Discard}, reader)
				return err
			}
			if err != nil {
				return err
			}
			if header.Typeflag == tar.TypeDir {
				i.entries++
				if i.entries > maxImportFiles {
					return fmt.Errorf("压缩包超过 %d 个条目限制", maxImportFiles)
				}
				continue
			}
			if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
				return fmt.Errorf("压缩包含非普通文件：%s", header.Name)
			}
			if err = i.entry(header.Name, t, depth); err != nil {
				return err
			}
			// Bound skipped files as well; tar.Next would otherwise drain them
			// without cancellation or an expansion limit.
			if _, err = io.Copy(importWriter{i, io.Discard}, t); err != nil {
				return err
			}
		}
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if !i.supported(name) {
		name += ".log"
	}
	return i.entry(name, reader, depth)
}

func (i *logImporter) decodeExternal(path string, decoder LogDecoder) (string, error) {
	output, err := i.newPath("decoded.log")
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(i.ctx, 2*time.Minute)
	defer cancel()
	keyFile := decoder.KeyFile
	if keyFile != "" {
		keyFile, err = filepath.Abs(keyFile)
		if err != nil {
			return "", err
		}
		if info, err := os.Stat(keyFile); err != nil || !info.Mode().IsRegular() {
			return "", fmt.Errorf("解码器 %s 的密钥文件不可读", decoder.Name)
		}
	}
	replace := strings.NewReplacer("{input}", path, "{output}", output, "{keyfile}", keyFile)
	args := make([]string, len(decoder.Arguments))
	for n, arg := range decoder.Arguments {
		args[n] = replace.Replace(arg)
	}
	command := exec.CommandContext(ctx, decoder.Program, args...)
	configureProcess(command)
	command.Dir = i.dir
	command.Env = os.Environ()
	if decoder.KeyEnv != "" {
		key, exists := os.LookupEnv(decoder.KeyEnv)
		if !exists || key == "" {
			return "", fmt.Errorf("解码器 %s 的密钥环境变量未设置", decoder.Name)
		}
		command.Env = append(command.Env, "LOGHELP_LOG_KEY="+key)
	}
	command.Stderr = io.Discard // Tool output may contain keys or raw logs.
	command.WaitDelay = time.Second
	var out *os.File
	if decoder.Output == "stdout" {
		out, err = os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return "", err
		}
		command.Stdout = importWriter{i, out}
	} else {
		// The trusted tool writes a file itself. Stop it if that file grows
		// beyond the remaining import budget instead of waiting for timeout.
		remaining := maxImportBytes - i.bytes
		go func() {
			tick := time.NewTicker(100 * time.Millisecond)
			defer tick.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
					if info, err := os.Stat(output); err == nil && info.Size() > remaining {
						cancel()
						return
					}
				}
			}
		}()
	}
	runErr := command.Run()
	if out != nil {
		runErr = errors.Join(runErr, out.Close())
	}
	if runErr != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("解码器 %s 已取消、超时或输出超过 4 GiB 限制", decoder.Name)
		}
		return "", fmt.Errorf("解码器 %s 执行失败；请检查程序、参数和密钥（工具输出未记录）", decoder.Name)
	}
	info, err := os.Lstat(output)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return "", fmt.Errorf("解码器 %s 未生成有效日志文件", decoder.Name)
	}
	if decoder.Output == "file" {
		if info.Size() > maxImportBytes-i.bytes {
			return "", fmt.Errorf("日志解压/解码累计超过 4 GiB 限制")
		}
		i.bytes += info.Size()
	}
	return output, nil
}
