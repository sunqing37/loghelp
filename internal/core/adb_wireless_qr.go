package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// WirelessQRPairing is a single dialog's ephemeral Android Studio-compatible
// pairing identity. Neither its payload nor secret should be logged or saved.
type WirelessQRPairing struct {
	service, secret string
}

func NewWirelessQRPairing() (*WirelessQRPairing, error) {
	var random [24]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, fmt.Errorf("生成扫码配对凭据失败：%w", err)
	}
	return &WirelessQRPairing{
		service: "studio-" + hex.EncodeToString(random[:8]),
		secret:  hex.EncodeToString(random[8:]),
	}, nil
}

func (q *WirelessQRPairing) Payload() string {
	return "WIFI:T:ADB;S:" + q.service + ";P:" + q.secret + ";;"
}

func (a *ADB) wirelessMDNS(ctx context.Context, command string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := a.Run(ctx, "", "mdns", command)
	failed := err != nil
	for _, line := range strings.Split(out, "\n") {
		line = strings.ToLower(strings.TrimSpace(line))
		// Service instance names come from peers. Words in those names must
		// not be interpreted as diagnostics for the entire mDNS backend.
		if strings.HasPrefix(line, "error:") || strings.HasPrefix(line, "adb: error:") ||
			command == "check" && (strings.Contains(line, "error") || strings.Contains(line, "disabled") || strings.Contains(line, "unavailable")) {
			failed = true
		}
	}
	if failed {
		return "", fmt.Errorf("ADB mDNS 发现不可用，请更新 SDK Platform-Tools 并确认 mDNS 已启用；也可使用配对码配对")
	}
	return out, nil
}

// PairWirelessQR waits only for this QR's exact mDNS pairing service. The ADB
// server handles TLS pairing and attempts to auto-connect the paired device.
func (a *ADB) PairWirelessQR(ctx context.Context, q *WirelessQRPairing, progress func(string)) error {
	if q == nil || q.service == "" || q.secret == "" {
		return fmt.Errorf("二维码无效，请刷新二维码后重试")
	}
	if _, err := a.wirelessMDNS(ctx, "check"); err != nil {
		return err
	}
	for {
		out, err := a.wirelessMDNS(ctx, "services")
		if ctx.Err() != nil {
			return fmt.Errorf("扫码等待已取消或超时，请刷新二维码重试；若已扫码，请检查同一网络、网络隔离和防火墙的 mDNS 通信")
		}
		if err != nil {
			return err
		}
		for _, line := range strings.Split(out, "\n") {
			fields := strings.Fields(line)
			if len(fields) < 3 || strings.TrimSuffix(fields[1], ".") != "_adb-tls-pairing._tcp" {
				continue
			}
			name := strings.TrimSuffix(fields[0], ".")
			name = strings.TrimSuffix(name, ".local")
			name = strings.TrimSuffix(name, "._adb-tls-pairing._tcp")
			if name != q.service {
				continue
			}
			address, err := NormalizeADBAddress(fields[2])
			if err != nil {
				return fmt.Errorf("扫码设备的 mDNS 地址无效，请刷新二维码后重试")
			}
			if progress != nil {
				progress("已发现扫码设备，正在配对…")
			}
			pairCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			err = a.wirelessCommand(pairCtx, "", q.secret, "successfully paired to ", "pair", address)
			cancel()
			if err != nil {
				return fmt.Errorf("扫码配对失败：%w；请刷新二维码后重新扫描", err)
			}
			return nil
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("扫码等待已取消或超时，请刷新二维码重试；若已扫码，请检查同一网络、网络隔离和防火墙的 mDNS 通信")
		case <-timer.C:
		}
	}
}
