package core

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// NormalizeADBAddress requires an explicit port: Android wireless debugging
// uses different, dynamically assigned ports for pairing and connecting.
func NormalizeADBAddress(value string) (string, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(value))
	if err != nil || host == "" {
		return "", fmt.Errorf("请输入设备 IP:端口，例如 192.168.1.10:5555；IPv6 使用 [地址]:端口")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("端口必须为 1–65535")
	}
	if net.ParseIP(host) == nil {
		// Allow DNS/mDNS host names, but never flags, URLs or shell syntax.
		if len(host) > 253 || host[0] == '-' || host[0] == '.' {
			return "", fmt.Errorf("设备地址必须为 IP 或有效主机名")
		}
		for _, c := range host {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '.') {
				return "", fmt.Errorf("设备地址必须为 IP 或有效主机名")
			}
		}
	}
	return net.JoinHostPort(host, strconv.Itoa(n)), nil
}

// ADB can report connect/pair failures on stdout with a successful exit code.
func (a *ADB) wirelessCommand(ctx context.Context, serial, input, success string, args ...string) error {
	if a == nil {
		return fmt.Errorf("请先在设置中配置 ADB")
	}
	cmd := a.Command(ctx, serial, args...)
	if input != "" {
		cmd.Stdin = strings.NewReader(input + "\n")
	}
	out := &limitedBuffer{limit: 16 << 10}
	cmd.Stdout, cmd.Stderr = out, out
	err := cmd.Run()
	if ctx.Err() != nil {
		return fmt.Errorf("操作已取消或超时，请检查设备地址、同一 Wi-Fi 网络及调试开关")
	}
	response := strings.TrimSpace(out.b.String())
	if input != "" {
		response = strings.ReplaceAll(response, input, "******")
	}
	if err == nil {
		for _, line := range strings.Split(strings.ToLower(response), "\n") {
			line = strings.TrimSpace(line)
			line = strings.TrimSpace(strings.TrimPrefix(line, "enter pairing code:"))
			if strings.HasPrefix(line, success) || success == "connected to " && strings.HasPrefix(line, "already connected to ") {
				return nil
			}
		}
	}
	if response == "" {
		response = "未收到预期的成功响应"
		if err != nil {
			response = err.Error()
		}
	}
	return fmt.Errorf("ADB %s 失败：%s", args[0], response)
}

func (a *ADB) ConnectWireless(ctx context.Context, address string) error {
	address, err := NormalizeADBAddress(address)
	if err != nil {
		return err
	}
	return a.wirelessCommand(ctx, "", "", "connected to ", "connect", address)
}

func (a *ADB) PairWireless(ctx context.Context, address, code string) error {
	address, err := NormalizeADBAddress(address)
	if err != nil {
		return err
	}
	code = strings.TrimSpace(code)
	if len(code) != 6 || strings.IndexFunc(code, func(c rune) bool { return c < '0' || c > '9' }) >= 0 {
		return fmt.Errorf("请输入手机显示的 6 位数字配对码")
	}
	// Feed the code on stdin so it is absent from process command lines.
	return a.wirelessCommand(ctx, "", code, "successfully paired to ", "pair", address)
}

func (a *ADB) EnableWireless(ctx context.Context, serial, address string) error {
	address, err := NormalizeADBAddress(address)
	if err != nil {
		return err
	}
	devices, err := a.Devices(ctx)
	if err != nil {
		return fmt.Errorf("检查 USB 设备失败：%w", err)
	}
	found := false
	for _, device := range devices {
		if device.Serial == serial && device.State == "device" && device.Connection == "USB" {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("请选择已授权且在线的 USB 真机")
	}
	_, port, _ := net.SplitHostPort(address)
	if err := a.wirelessCommand(ctx, serial, "", "restarting in tcp mode port:", "tcpip", port); err != nil {
		return err
	}
	// adbd needs a moment to restart after tcpip. Retry only this transition.
	for attempt := 0; attempt < 3; attempt++ {
		if err = a.ConnectWireless(ctx, address); err == nil {
			return nil
		}
		if attempt < 2 {
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return fmt.Errorf("已请求开启 TCP 调试，但连接已取消或超时；请使用地址直连重试")
			case <-timer.C:
			}
		}
	}
	return fmt.Errorf("已开启 TCP 调试，但 Wi-Fi 连接失败：%w；请核对手机 Wi-Fi IP 后使用地址直连重试", err)
}

func (a *ADB) DisconnectWireless(ctx context.Context, serial string) error {
	// Use the exact enumerated transport, including Android mDNS service names;
	// never call bare `adb disconnect`, which would affect every network device.
	devices, err := a.Devices(ctx)
	if err != nil {
		return fmt.Errorf("检查无线设备失败：%w", err)
	}
	for _, device := range devices {
		if device.Serial == serial && device.Connection == "Wi-Fi" {
			return a.wirelessCommand(ctx, "", "", "disconnected ", "disconnect", serial)
		}
	}
	return fmt.Errorf("该无线连接已不在设备列表中，请刷新后重试")
}
