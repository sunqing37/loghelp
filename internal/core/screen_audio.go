package core

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/Eyevinn/mp4ff/aac"
)

func validateScreenAAC(config []byte) error {
	header, err := aac.DecodeAudioSpecificConfigHeader(bytes.NewReader(config))
	if err != nil {
		return fmt.Errorf("读取 AAC 配置：%w", err)
	}
	if header.ObjectType != aac.AAClc || header.SamplingFrequency != 48000 || header.ChannelConfiguration != 2 {
		return fmt.Errorf("设备音频格式不兼容：需要 AAC-LC / 48000 Hz / 双声道，实际为 %d / %d Hz / %d 声道", header.ObjectType, header.SamplingFrequency, header.ChannelConfiguration)
	}
	return nil
}

func readScreenAudioConfig(conn net.Conn) ([]byte, error) {
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer conn.SetReadDeadline(time.Time{})
	var codec [4]byte
	if _, err := io.ReadFull(conn, codec[:]); err != nil {
		return nil, fmt.Errorf("读取音频编码信息：%w", err)
	}
	if binary.BigEndian.Uint32(codec[:]) != 0x00616163 {
		return nil, fmt.Errorf("设备未能提供 AAC 系统声音，请检查内录权限或关闭「录制系统声音」")
	}
	flags, data, err := readScreenAudioPacket(conn)
	if err != nil {
		return nil, err
	}
	if flags&(uint64(1)<<63) == 0 {
		return nil, fmt.Errorf("设备未返回 AAC 音频配置")
	}
	if err := validateScreenAAC(data); err != nil {
		return nil, err
	}
	return data, nil
}

func readScreenAudioPacket(reader io.Reader) (uint64, []byte, error) {
	var header [12]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return 0, nil, fmt.Errorf("读取系统声音数据头：%w", err)
	}
	size := binary.BigEndian.Uint32(header[8:])
	if size < 1 || size > 64<<10 {
		return 0, nil, fmt.Errorf("音频数据包大小异常：%d", size)
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(reader, data); err != nil {
		return 0, nil, fmt.Errorf("读取系统声音数据：%w", err)
	}
	return binary.BigEndian.Uint64(header[:8]), data, nil
}

func (s *ScreenStream) readScreenAudio(conn net.Conn, stopping <-chan struct{}) {
	for {
		flags, data, err := readScreenAudioPacket(conn)
		if err != nil {
			select {
			case <-stopping:
				return
			default:
			}
			s.mu.Lock()
			if s.ctx.Err() == nil {
				s.err = fmt.Errorf("系统声音录制已中断：%w", err)
			}
			s.mu.Unlock()
			s.cancel()
			return
		}
		s.mu.Lock()
		config := flags&(uint64(1)<<63) != 0
		if config {
			s.audioConfig = bytes.Clone(data)
		}
		if s.recording != nil {
			s.recording.offerPacket(recordingPacket{data: data, pts: flags & ((uint64(1) << 62) - 1), config: config, audio: true, at: time.Now()})
		}
		s.mu.Unlock()
	}
}
