package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/mail"
	"net/url"
	"runtime"
	"strconv"
	"strings"
)

const releaseRepository = "https://github.com/sunqing37/loghelp/releases/"

type ReleaseUpdate struct {
	Version, Name, Notes, ReleaseURL, DownloadURL string
	Digest                                        string
	Size                                          int64
	Revision                                      uint64
	Newer, DownloadAvailable                      bool
}

func stableVersion(value string) ([3]uint64, error) {
	var version [3]uint64
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	value, _, _ = strings.Cut(value, "+")
	parts := strings.Split(value, ".")
	if len(parts) != len(version) {
		return version, fmt.Errorf("无法识别正式版本号 %q", value)
	}
	for i, part := range parts {
		if part == "" || len(part) > 1 && part[0] == '0' {
			return version, fmt.Errorf("无法识别正式版本号 %q", value)
		}
		for _, digit := range part {
			if digit < '0' || digit > '9' {
				return version, fmt.Errorf("无法识别正式版本号 %q", value)
			}
		}
		number, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return version, fmt.Errorf("无法识别正式版本号 %q", value)
		}
		version[i] = number
	}
	return version, nil
}

func CheckRelease(ctx context.Context, current string, currentRevision uint64, options UpdateNetworkOptions) (ReleaseUpdate, error) {
	var update ReleaseUpdate
	installed, err := stableVersion(current)
	if err != nil {
		return update, err
	}
	data, err := fetchReleaseWithRetry(ctx, current, currentRevision, options)
	if err != nil {
		return update, err
	}
	var release struct {
		Tag        string `json:"tag_name"`
		Name       string `json:"name"`
		Body       string `json:"body"`
		URL        string `json:"html_url"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name    string `json:"name"`
			URL     string `json:"browser_download_url"`
			Created string `json:"created_at"`
			Digest  string `json:"digest"`
			Size    int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(data, &release); err != nil {
		return update, fmt.Errorf("GitHub 版本信息格式错误：%w", err)
	}
	if release.Draft || release.Prerelease {
		return update, fmt.Errorf("GitHub 没有返回正式发布版本")
	}
	latest, err := stableVersion(release.Tag)
	if err != nil {
		return update, err
	}
	if !strings.HasPrefix(release.URL, releaseRepository+"tag/") || !validReleaseURL(release.URL) {
		return update, fmt.Errorf("GitHub 返回了无效的发布链接")
	}
	update = ReleaseUpdate{Version: strings.TrimPrefix(release.Tag, "v"), Name: release.Name, Notes: release.Body, ReleaseURL: release.URL, DownloadURL: release.URL}
	for i := range latest {
		if latest[i] != installed[i] {
			update.Newer = latest[i] > installed[i]
			break
		}
	}
	assetPlatform := "-" + runtime.GOOS + "-" + runtime.GOARCH
	newestAsset := ""
	for _, asset := range release.Assets {
		revision, matches := releaseAssetRevision(asset.Name, update.Version, assetPlatform)
		if matches && strings.HasPrefix(asset.URL, releaseRepository+"download/") && validReleaseURL(asset.URL) &&
			(!update.DownloadAvailable || revision > update.Revision || revision == update.Revision && asset.Created > newestAsset) {
			update.DownloadURL, newestAsset = asset.URL, asset.Created
			update.Digest, update.Size, update.DownloadAvailable = asset.Digest, asset.Size, true
			update.Revision = revision
		}
	}
	if latest == installed && update.DownloadAvailable {
		update.Newer = update.Revision > currentRevision
	}
	return update, nil
}

// Legacy filenames without an r suffix are revision zero. Revision numbers,
// rather than upload timestamps, determine which same-version package is newer.
func releaseAssetRevision(name, version, platform string) (uint64, bool) {
	prefix := "loghelp-" + version + platform
	if !strings.HasPrefix(name, prefix) {
		return 0, false
	}
	remaining := strings.TrimPrefix(name, prefix)
	switch {
	case strings.HasSuffix(remaining, ".zip"):
		remaining = strings.TrimSuffix(remaining, ".zip")
	case strings.HasSuffix(remaining, ".tar.gz") && runtime.GOOS != "windows":
		remaining = strings.TrimSuffix(remaining, ".tar.gz")
	default:
		return 0, false
	}
	if remaining == "" {
		return 0, true
	}
	if !strings.HasPrefix(remaining, "-r") {
		return 0, false
	}
	value := strings.TrimPrefix(remaining, "-r")
	if value == "" || len(value) > 1 && value[0] == '0' {
		return 0, false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return 0, false
		}
	}
	revision, err := strconv.ParseUint(value, 10, 64)
	return revision, err == nil && revision > 0
}

func (update ReleaseUpdate) Label() string {
	if update.Revision == 0 {
		return "v" + update.Version
	}
	return fmt.Sprintf("v%s · r%d", update.Version, update.Revision)
}

func validReleaseURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Host == "github.com" && parsed.User == nil &&
		strings.HasPrefix(parsed.Path, "/sunqing37/loghelp/releases/")
}

// OpenReleaseURL only opens this application's GitHub release pages and assets.
func OpenReleaseURL(raw string) error {
	if !validReleaseURL(raw) {
		return fmt.Errorf("无效的发布链接")
	}
	return openReleaseURL(raw)
}

// OpenReleaseNotesURL opens documentation links in Markdown release notes.
// Keep this separate from the restricted URLs used for release assets.
func OpenReleaseNotesURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err == nil && parsed.Scheme == "mailto" && parsed.Opaque != "" && parsed.RawQuery == "" && parsed.Fragment == "" {
		address, decodeErr := url.PathUnescape(parsed.Opaque)
		if decodeErr == nil {
			if mailbox, parseErr := mail.ParseAddress(address); parseErr == nil && mailbox.Address == address {
				return openReleaseURL(parsed.String())
			}
		}
	}
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" || parsed.User != nil {
		return fmt.Errorf("无效的说明链接")
	}
	return openReleaseURL(parsed.String())
}
