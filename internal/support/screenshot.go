package support

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"

	"golang.org/x/image/draw"
)

const MaxScreenshots = 3
const screenshotLimit = 8 << 20

type Screenshot struct {
	PNG           []byte
	Thumbnail     image.Image
	Width, Height int
}

// LoadScreenshot reads only the explicitly selected file. Re-encoding drops
// EXIF, text chunks and original filenames, but never claims to redact pixels.
func LoadScreenshot(ctx context.Context, path string) (Screenshot, error) {
	var result Screenshot
	info, err := os.Lstat(path)
	if err != nil {
		return result, err
	}
	if !info.Mode().IsRegular() || info.Size() > screenshotLimit {
		return result, fmt.Errorf("请选择不超过 8 MiB 的 PNG 或 JPEG 普通文件")
	}
	f, err := os.Open(path)
	if err != nil {
		return result, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, screenshotLimit+1))
	if err != nil {
		return result, err
	}
	if len(data) > screenshotLimit {
		return result, fmt.Errorf("截图超过 8 MiB")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != "png" && format != "jpeg" {
		return result, fmt.Errorf("截图必须是有效的 PNG 或 JPEG 图片")
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > 8192 || config.Height > 8192 || int64(config.Width)*int64(config.Height) > 16_000_000 {
		return result, fmt.Errorf("截图最长边不得超过 8192 像素，总像素不得超过 1600 万")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	pixels, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return result, fmt.Errorf("读取截图像素：%w", err)
	}
	var encoded screenshotBuffer
	if err := png.Encode(&encoded, pixels); err != nil {
		return result, fmt.Errorf("规范化截图失败：%w", err)
	}
	width, height := config.Width, config.Height
	if width > 560 {
		height = max(1, height*560/width)
		width = 560
	}
	if height > 300 {
		width = max(1, width*300/height)
		height = 300
	}
	thumb := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.ApproxBiLinear.Scale(thumb, thumb.Bounds(), pixels, pixels.Bounds(), draw.Src, nil)
	result = Screenshot{PNG: encoded.Bytes(), Thumbnail: thumb, Width: config.Width, Height: config.Height}
	return result, ctx.Err()
}

type screenshotBuffer struct{ bytes.Buffer }

func (b *screenshotBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > screenshotLimit {
		return 0, fmt.Errorf("转换后的 PNG 超过 8 MiB，请缩小截图后重试")
	}
	return b.Buffer.Write(p)
}
