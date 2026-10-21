package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type TabState struct {
	Name          string `json:"name"`
	Serial        string `json:"serial,omitempty"`
	File          string `json:"file,omitempty"`
	Filter        string `json:"filter,omitempty"`
	Search        string `json:"search,omitempty"`
	Package       string `json:"package,omitempty"`
	Buffers       string `json:"buffers,omitempty"`
	CaseSensitive bool   `json:"case_sensitive,omitempty"`
	SearchRegex   bool   `json:"search_regex,omitempty"`
	MergeLines    bool   `json:"merge_lines,omitempty"`
}
type Config struct {
	AppInstall       AppInstallOptions      `json:"app_install,omitempty"`
	ScreenRecording  ScreenRecordingOptions `json:"screen_recording"`
	RecentFiles      []string               `json:"recent_files,omitempty"`
	Workspaces       []Workspace            `json:"workspaces,omitempty"`
	Bookmarks        []Bookmark             `json:"bookmarks,omitempty"`
	Version          int                    `json:"version"`
	ADB              string                 `json:"adb,omitempty"`
	UpdateProxy      string                 `json:"update_proxy,omitempty"`
	Directory        string                 `json:"directory,omitempty"`
	Theme            string                 `json:"theme"`
	FontSize         float32                `json:"font_size"`
	Wrap             bool                   `json:"wrap"`
	DetailHeight     int                    `json:"detail_height"`
	Columns          []string               `json:"columns"`
	ColumnWidths     map[string]int         `json:"column_widths,omitempty"`
	History          []string               `json:"history,omitempty"`
	Favorites        map[string]string      `json:"favorites,omitempty"`
	ShellHistory     []string               `json:"shell_history,omitempty"`
	SaveShellHistory bool                   `json:"save_shell_history"`
	Tabs             []TabState             `json:"tabs,omitempty"`
	CacheCount       int                    `json:"cache_count"`
	CacheBytes       int64                  `json:"cache_bytes"`
	SegmentBytes     int64                  `json:"segment_bytes"`
	SegmentMinutes   int                    `json:"segment_minutes"`
	QuotaBytes       int64                  `json:"quota_bytes"`
	RetentionDays    int                    `json:"retention_days"`
	Cleanup          bool                   `json:"cleanup"`
	Shortcuts        map[string]string      `json:"shortcuts,omitempty"`
	ContextBefore    int                    `json:"context_before"`
	ContextAfter     int                    `json:"context_after"`
}

type Workspace struct {
	Name         string         `json:"name"`
	Tabs         []TabState     `json:"tabs"`
	Columns      []string       `json:"columns"`
	ColumnWidths map[string]int `json:"column_widths,omitempty"`
	Wrap         bool           `json:"wrap"`
}

func DefaultConfig() Config {
	return Config{ScreenRecording: ScreenRecordingOptions{KeepDevicePlayback: true, RevealAfterSave: true}, Version: 1, Theme: "system", FontSize: 14, Wrap: true, DetailHeight: 180, Columns: []string{"time", "pid", "tid", "tag", "package"}, Favorites: map[string]string{}, SaveShellHistory: false, CacheCount: 200000, CacheBytes: 64 << 20, SegmentBytes: 100 << 20, QuotaBytes: 2 << 30, Shortcuts: map[string]string{}, ContextBefore: 20, ContextAfter: 20}
}
func ConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "loghelp", "settings.json"), nil
}
func LoadConfig() (Config, error) {
	c := DefaultConfig()
	path, err := ConfigPath()
	if err != nil {
		return c, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(data, &c); err != nil {
		return DefaultConfig(), fmt.Errorf("配置文件损坏（未覆盖）：%w", err)
	}
	if c.Version != 1 {
		return DefaultConfig(), fmt.Errorf("不支持的配置版本 %d（未覆盖）", c.Version)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) == nil {
		if _, exists := fields["detail_height"]; !exists {
			// Older releases defaulted the detail pane to a single line.
			c.Wrap = true
		}
	}
	if c.DetailHeight <= 0 {
		c.DetailHeight = 180
	}
	c.DetailHeight = max(72, min(c.DetailHeight, 4096))
	if c.Favorites == nil {
		c.Favorites = map[string]string{}
	}
	if c.Shortcuts == nil {
		c.Shortcuts = map[string]string{}
	}
	if c.CacheCount < 1 {
		c.CacheCount = 200000
	}
	if c.CacheBytes < 1024 {
		c.CacheBytes = 64 << 20
	}
	if c.FontSize < 9 {
		c.FontSize = 14
	}
	if c.Theme != "dark" && c.Theme != "light" && c.Theme != "system" {
		c.Theme = "system"
	}
	c.ContextBefore = max(0, min(c.ContextBefore, 500))
	c.ContextAfter = max(0, min(c.ContextAfter, 500))
	return c, nil
}
func SaveConfig(c Config) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	c.Version = 1
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWrite(path, data)
}

func BackupConfig() error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return AtomicWrite(path+".backup-"+time.Now().Format("20060102-150405.000000000"), data)
}
func AtomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".loghelp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
func Remember(list []string, value string, limit int) []string {
	if value == "" {
		return list
	}
	out := []string{value}
	for _, s := range list {
		if s != value {
			out = append(out, s)
		}
		if len(out) >= limit {
			break
		}
	}
	return out
}
