package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const releaseRepository = "https://github.com/sunqing37/loghelp/releases/"

type ReleaseUpdate struct {
	Version, Name, Notes, ReleaseURL, DownloadURL string
	Digest                                        string
	Size                                          int64
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

func CheckRelease(ctx context.Context, current string) (ReleaseUpdate, error) {
	var update ReleaseUpdate
	installed, err := stableVersion(current)
	if err != nil {
		return update, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/sunqing37/loghelp/releases/latest", nil)
	if err != nil {
		return update, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "LogHelp/"+current)
	client := &http.Client{Timeout: 15 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return update, fmt.Errorf("无法连接 GitHub，请检查网络后重试：%w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		switch response.StatusCode {
		case http.StatusNotFound:
			return update, fmt.Errorf("仓库暂时没有正式发布版本")
		case http.StatusForbidden, http.StatusTooManyRequests:
			return update, fmt.Errorf("GitHub 暂时限制请求，请稍后重试")
		default:
			return update, fmt.Errorf("GitHub 返回 HTTP %d，请稍后重试", response.StatusCode)
		}
	}
	const maxResponse = 2 << 20
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
	if err != nil {
		return update, fmt.Errorf("读取版本信息失败：%w", err)
	}
	if len(data) > maxResponse {
		return update, fmt.Errorf("GitHub 版本信息过大")
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
		if strings.HasPrefix(asset.Name, "loghelp-"+update.Version+assetPlatform) &&
			(strings.HasSuffix(asset.Name, ".zip") || strings.HasSuffix(asset.Name, ".tar.gz")) &&
			strings.HasPrefix(asset.URL, releaseRepository+"download/") && validReleaseURL(asset.URL) && asset.Created >= newestAsset {
			update.DownloadURL, newestAsset = asset.URL, asset.Created
			update.Digest, update.Size, update.DownloadAvailable = asset.Digest, asset.Size, true
		}
	}
	return update, nil
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
