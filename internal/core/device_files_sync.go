package core

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"strings"
	"time"
)

// AOSP file_sync_protocol.h: LIS2 returns packed 76-byte DNT2 records,
// followed by length-delimited filenames. Sizes and timestamps are 64-bit.
// LIST v1 is deliberately not used: it truncates sizes above 4 GiB.
func listDeviceFilesSync(ctx context.Context, serial, directory string) ([]DeviceFile, error) {
	if len(directory) > 1024 {
		return nil, fmt.Errorf("路径超过 ADB sync 的 1024 字节限制")
	}
	network, address := "tcp", ""
	var err error
	if socket := os.Getenv("ADB_SERVER_SOCKET"); strings.HasPrefix(socket, "localfilesystem:") {
		network, address = "unix", strings.TrimPrefix(socket, "localfilesystem:")
	} else {
		address, err = serverAddress()
	}
	if err != nil {
		return nil, err
	}
	conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, address)
	if err != nil {
		return nil, fmt.Errorf("连接 ADB 目录通道：%w", err)
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err = adbRequest(conn, "host:transport:"+serial); err == nil {
		err = adbRequest(conn, "sync:")
	}
	if err != nil {
		return nil, fmt.Errorf("打开 ADB 目录通道：%w", err)
	}
	deadline := time.Now().Add(45 * time.Second)
	if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
		deadline = limit
	}
	conn.SetDeadline(deadline)
	request := make([]byte, 8+len(directory))
	copy(request, "LIS2")
	binary.LittleEndian.PutUint32(request[4:8], uint32(len(directory)))
	copy(request[8:], directory)
	if err := writeAll(conn, request); err != nil {
		return nil, fmt.Errorf("请求设备目录：%w", err)
	}
	reader := bufio.NewReaderSize(conn, 64<<10)
	var header [76]byte
	var entries []DeviceFile
	total := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, err := io.ReadFull(reader, header[:4]); err != nil {
			return nil, fmt.Errorf("读取设备目录响应：%w", err)
		}
		kind := string(header[:4])
		if kind == "FAIL" {
			if _, err := io.ReadFull(reader, header[4:8]); err != nil {
				return nil, err
			}
			size := binary.LittleEndian.Uint32(header[4:8])
			if size > 16<<10 {
				return nil, fmt.Errorf("ADB 目录错误信息过长")
			}
			message := make([]byte, size)
			if _, err := io.ReadFull(reader, message); err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("ADB 批量读取目录：%s", message)
		}
		if kind != "DNT2" && kind != "DONE" {
			return nil, fmt.Errorf("不支持的 ADB 目录响应 %q", kind)
		}
		if _, err := io.ReadFull(reader, header[4:]); err != nil {
			return nil, fmt.Errorf("目录属性不完整：%w", err)
		}
		if kind == "DONE" {
			// adbd also sends DONE when opendir fails, and some devices omit
			// dot entries entirely. Verify an empty result with the shell path
			// so protected or missing directories are not shown as empty.
			if len(entries) == 0 {
				return nil, fmt.Errorf("无法确认目录 %s 可读", directory)
			}
			return entries, nil
		}
		nameSize := binary.LittleEndian.Uint32(header[72:76])
		if nameSize == 0 || nameSize > 4096 {
			return nil, fmt.Errorf("设备返回了无效文件名长度")
		}
		total += len(header) + int(nameSize)
		if total > 8<<20 {
			return nil, fmt.Errorf("目录内容过大，请进入更具体的目录")
		}
		nameBytes := make([]byte, nameSize)
		if _, err := io.ReadFull(reader, nameBytes); err != nil {
			return nil, fmt.Errorf("文件名不完整：%w", err)
		}
		name := string(nameBytes)
		if strings.ContainsAny(name, "/\x00") {
			return nil, fmt.Errorf("设备返回了无效文件名")
		}
		if code := binary.LittleEndian.Uint32(header[4:8]); code != 0 {
			return nil, fmt.Errorf("读取文件 %q 属性失败（设备 errno %d）", name, code)
		}
		if name == "." || name == ".." {
			continue
		}
		if len(entries) >= 10000 {
			return nil, fmt.Errorf("目录超过 10000 项，请进入更具体的目录")
		}
		size := binary.LittleEndian.Uint64(header[40:48])
		if size > math.MaxInt64 {
			return nil, fmt.Errorf("文件 %q 大小超出支持范围", name)
		}
		entries = append(entries, DeviceFile{
			Name:     name,
			Mode:     binary.LittleEndian.Uint32(header[24:28]),
			Size:     int64(size),
			Modified: time.Unix(int64(binary.LittleEndian.Uint64(header[56:64])), 0),
		})
	}
}
