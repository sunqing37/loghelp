package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"os"
	"time"

	"loghelp/internal/core"
)

// A manual, read-only diagnostic: receive/decode video without changing the
// phone UI, clipboard, log sessions or opening a desktop window.
func handleMirrorCheck(args []string) (bool, error) {
	if len(args) == 0 || args[0] != "--check-mirror" {
		return false, nil
	}
	flags := flag.NewFlagSet("--check-mirror", flag.ContinueOnError)
	serial := flags.String("serial", "", "设备序列号")
	duration := flags.Duration("duration", 8*time.Second, "只读抓屏时长（含连接，最长 30 秒）")
	if err := flags.Parse(args[1:]); err != nil {
		return true, err
	}
	if *serial == "" || flags.NArg() != 0 || *duration < time.Second || *duration > 30*time.Second {
		return true, fmt.Errorf("请指定 --serial，--duration 范围为 1s–30s")
	}
	adb, err := core.DiscoverADB("")
	if err != nil {
		return true, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	stream := core.StartScreenStream(ctx, adb, *serial)
	defer func() {
		cancel()
		stream.Close()
		select {
		case <-stream.Done():
		case <-time.After(8 * time.Second):
		}
	}()
	start := time.Now()
	end := time.NewTimer(*duration)
	defer end.Stop()
	ticker := time.NewTicker(time.Second / 60)
	defer ticker.Stop()
	var firstFrame time.Duration
	var size image.Point
	var fullRange, bt709 bool
	for {
		select {
		case <-stream.Done():
			_, _, err := stream.State()
			return true, fmt.Errorf("投屏诊断提前结束：%v", err)
		case <-ticker.C:
			if frame := stream.TakeFrame(); frame != nil {
				if firstFrame == 0 {
					firstFrame = time.Since(start)
				}
				size, fullRange, bt709 = frame.Size, frame.FullRange, frame.BT709
				stream.ReleaseFrame(frame)
			}
		case <-end.C:
			ready, frames, err := stream.State()
			if err != nil {
				return true, err
			}
			if !ready || frames == 0 {
				return true, fmt.Errorf("诊断期间未收到视频帧：%s", stream.Diagnostic())
			}
			return true, json.NewEncoder(os.Stdout).Encode(map[string]any{"frames": frames, "width": size.X, "height": size.Y, "first_frame_ms": firstFrame.Milliseconds(), "elapsed_ms": time.Since(start).Milliseconds(), "full_range": fullRange, "bt709": bt709})
		}
	}
}
