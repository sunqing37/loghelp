package core

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// scrcpy 3.3.4 control message limit, including SET_CLIPBOARD's 14-byte header.
const ScreenClipboardTextLimit = (1 << 18) - 14

func ValidateScreenClipboardText(text string) error {
	if len(text) > ScreenClipboardTextLimit {
		return fmt.Errorf("剪贴板文本超过 %d 字节，未同步", ScreenClipboardTextLimit)
	}
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return fmt.Errorf("剪贴板文本包含不支持的编码或空字符，未同步")
	}
	return nil
}

func (s *ScreenStream) ClipboardChanges() <-chan string { return s.clipboardChanges }

func (s *ScreenStream) ClipboardError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clipboardReadErr
}

// The ACK confirms protocol processing, not that Android granted clipboard
// access. It must not be presented as proof that the target app can paste.
func (s *ScreenStream) SetClipboardText(ctx context.Context, text string) error {
	if err := ValidateScreenClipboardText(text); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	if s.clipboardReadErr != nil || s.clipboardAck != nil {
		err := s.clipboardReadErr
		s.mu.Unlock()
		if err != nil {
			return fmt.Errorf("设备剪贴板连接不可用：%w", err)
		}
		return fmt.Errorf("设备剪贴板传输尚未完成")
	}
	s.clipboardSequence++
	sequence := s.clipboardSequence
	ack := make(chan uint64, 1)
	s.clipboardAck = ack
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.clipboardAck = nil; s.mu.Unlock() }()
	message := make([]byte, 14+len(text))
	message[0] = 9 // SET_CLIPBOARD; paste=false (never inject into a focused app).
	binary.BigEndian.PutUint64(message[1:9], sequence)
	binary.BigEndian.PutUint32(message[10:14], uint32(len(text)))
	copy(message[14:], text)
	if err := s.sendControl(message); err != nil {
		return err
	}
	select {
	case <-ack:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("等待设备剪贴板响应：%w", ctx.Err())
	case <-s.ctx.Done():
		return fmt.Errorf("设备投屏已断开")
	}
}

func (s *ScreenStream) readClipboardMessages(reader io.Reader) error {
	var kind [1]byte
	for {
		if _, err := io.ReadFull(reader, kind[:]); err != nil {
			return fmt.Errorf("读取设备剪贴板：%w", err)
		}
		switch kind[0] {
		case 0: // CLIPBOARD
			var length [4]byte
			if _, err := io.ReadFull(reader, length[:]); err != nil {
				return err
			}
			size := binary.BigEndian.Uint32(length[:])
			if size > (1<<18)-5 {
				return fmt.Errorf("设备剪贴板消息过大")
			}
			data := make([]byte, int(size))
			if _, err := io.ReadFull(reader, data); err != nil {
				return err
			}
			// Keep only the latest copy operation; never block touch/video.
			select {
			case <-s.clipboardChanges:
			default:
			}
			s.clipboardChanges <- string(data)
		case 1: // ACK_CLIPBOARD
			var sequence [8]byte
			if _, err := io.ReadFull(reader, sequence[:]); err != nil {
				return err
			}
			s.mu.Lock()
			if s.clipboardAck != nil && binary.BigEndian.Uint64(sequence[:]) == s.clipboardSequence {
				// Notifications received before this ACK predate completion of
				// our computer-side copy. Do not replay them over the new text.
				select {
				case <-s.clipboardChanges:
				default:
				}
				select {
				case s.clipboardAck <- s.clipboardSequence:
				default:
				}
			}
			s.mu.Unlock()
		default:
			return fmt.Errorf("不支持的设备控制消息类型：%d", kind[0])
		}
	}
}
