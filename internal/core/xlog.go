package core

import (
	"bufio"
	"bytes"
	"compress/flate"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/klauspost/compress/zstd"
)

// Mars wire format and 16-round TEA compatibility:
// https://github.com/Tencent/mars/blob/master/mars/xlog/crypt/log_crypt.cc
// https://github.com/Tencent/mars/blob/master/mars/xlog/crypt/decode_mars_crypt_log_file.py
// Sync blocks (0x06/0x0a) contain plain text, including when encryption is on.
const maxXlogBlock = 16 << 20
const maxXlogDecodedBlock = 64 << 20

func newLogZstdReader(reader io.Reader) (io.ReadCloser, error) {
	z, err := zstd.NewReader(reader, zstd.WithDecoderConcurrency(1), zstd.WithDecoderLowmem(true), zstd.WithDecoderMaxMemory(128<<20), zstd.WithDecoderMaxWindow(64<<20))
	if err != nil {
		return nil, fmt.Errorf("打开 Zstd 日志：%w", err)
	}
	return z.IOReadCloser(), nil
}

func loadXlogKey(options LogImportOptions) (*secp256k1.PrivateKey, error) {
	var value []byte
	if options.XlogKeyEnv != "" {
		value = []byte(os.Getenv(options.XlogKeyEnv))
	} else if options.XlogKeyFile != "" {
		f, err := os.Open(options.XlogKeyFile)
		if err != nil {
			return nil, fmt.Errorf("无法读取 xlog 私钥文件，请检查设置中的路径和权限")
		}
		value, err = io.ReadAll(io.LimitReader(f, 4097))
		closeErr := f.Close()
		if err != nil || closeErr != nil || len(value) > 4096 {
			return nil, fmt.Errorf("xlog 私钥文件读取失败或超过 4 KiB")
		}
	} else {
		return nil, fmt.Errorf("该 xlog 已加密，请在设置「日志解压与解码」中配置 Mars 私钥文件或环境变量")
	}
	defer clear(value)
	// A Mars private scalar is 32 bytes / 64 hex characters. The public key
	// used by the app cannot decrypt a log and is not a substitute.
	value = bytes.TrimSpace(value)
	if len(value) != 64 {
		return nil, fmt.Errorf("Mars 私钥应为 64 位十六进制文本，请勿填写公钥或 Python 脚本")
	}
	var key [32]byte
	defer clear(key[:])
	if _, err := hex.Decode(key[:], value); err != nil {
		return nil, fmt.Errorf("Mars 私钥不是有效的十六进制文本")
	}
	var scalar secp256k1.ModNScalar
	defer scalar.Zero()
	if scalar.SetByteSlice(key[:]) || scalar.IsZero() {
		return nil, fmt.Errorf("Mars 私钥不在 secp256k1 有效范围内")
	}
	return secp256k1.NewPrivateKey(&scalar), nil
}

func decryptXlogTEA(ctx context.Context, data, key []byte) error {
	var k [4]uint32
	for n := range k {
		k[n] = binary.LittleEndian.Uint32(key[n*4:])
	}
	defer clear(k[:])
	for n := 0; n+8 <= len(data); n += 8 {
		if n%65536 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		v0, v1 := binary.LittleEndian.Uint32(data[n:]), binary.LittleEndian.Uint32(data[n+4:])
		sum := uint32(0xe3779b90) // delta * 16 modulo 2^32
		for round := 0; round < 16; round++ {
			v1 -= ((v0 << 4) + k[2]) ^ (v0 + sum) ^ ((v0 >> 5) + k[3])
			v0 -= ((v1 << 4) + k[0]) ^ (v1 + sum) ^ ((v1 >> 5) + k[1])
			sum -= 0x9e3779b9
		}
		binary.LittleEndian.PutUint32(data[n:], v0)
		binary.LittleEndian.PutUint32(data[n+4:], v1)
	}
	// Mars leaves the final incomplete 8-byte block unencrypted.
	return nil
}

func finishXlogZstd(data []byte) ([]byte, error) {
	// Mars calls ZSTD_e_flush into the log buffer, then ZSTD_e_end with a
	// zero-sized output buffer. Consequently complete data blocks can lack
	// the final empty block. Never accept a partially written data block.
	// https://github.com/Tencent/mars/blob/master/mars/xlog/src/log_zstd_buffer.cc
	var header zstd.Header
	if err := header.Decode(data); err != nil {
		return nil, err
	}
	if header.Skippable {
		return data, nil
	}
	for offset := header.HeaderSize; ; {
		if offset == len(data) {
			if header.HasCheckSum {
				return nil, fmt.Errorf("Zstd 帧缺少结束块及校验值")
			}
			return append(data, 1, 0, 0), nil // empty, final raw block
		}
		if len(data)-offset < 3 {
			return nil, io.ErrUnexpectedEOF
		}
		block := uint32(data[offset]) | uint32(data[offset+1])<<8 | uint32(data[offset+2])<<16
		size, kind := int(block>>3), (block>>1)&3
		if kind == 3 {
			return nil, fmt.Errorf("无效的 Zstd 块类型")
		}
		if kind == 1 { // run-length block stores one byte, not size bytes
			size = 1
		}
		offset += 3
		if size > len(data)-offset {
			return nil, io.ErrUnexpectedEOF
		}
		offset += size
		if block&1 != 0 {
			return data, nil // complete frames retain normal checksum validation
		}
	}
}

func expandXlogBlock(magic byte, data []byte) ([]byte, error) {
	if magic == 0x05 {
		joined := make([]byte, 0, len(data))
		for len(data) > 0 {
			if len(data) < 2 {
				return nil, fmt.Errorf("分段压缩长度损坏")
			}
			n := int(binary.LittleEndian.Uint16(data))
			if n > len(data)-2 {
				return nil, fmt.Errorf("分段压缩内容不完整")
			}
			joined = append(joined, data[2:2+n]...)
			data = data[2+n:]
		}
		data = joined
	}
	var reader io.ReadCloser
	var err error
	deflate := magic == 0x04 || magic == 0x05 || magic == 0x07 || magic == 0x09
	switch {
	case deflate:
		reader = flate.NewReader(bytes.NewReader(data))
	case magic == 0x0c || magic == 0x0d:
		data, err = finishXlogZstd(data)
		if err != nil {
			return nil, err
		}
		reader, err = newLogZstdReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
	default:
		return data, nil
	}
	defer reader.Close()
	decoded, err := io.ReadAll(io.LimitReader(reader, maxXlogDecodedBlock+1))
	if len(decoded) > maxXlogDecodedBlock {
		return nil, fmt.Errorf("单个 xlog 块解压超过 64 MiB 限制")
	}
	// Async Mars buffers end with Z_SYNC_FLUSH, not a final Deflate stream
	// marker. Accept that specific boundary, but reject other truncation.
	if deflate && errors.Is(err, io.ErrUnexpectedEOF) && bytes.HasSuffix(data, []byte{0, 0, 0xff, 0xff}) {
		err = nil
	}
	return decoded, err
}

func (i *logImporter) decodeXlog(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	output, err := i.newPath("xlog-decoded.log")
	if err != nil {
		return "", err
	}
	out, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	defer out.Close()
	writer := importWriter{i, out}
	reader := bufio.NewReaderSize(f, 128<<10)
	var private *secp256k1.PrivateKey
	var peer [64]byte
	var shared []byte
	defer func() {
		if private != nil {
			private.Zero()
		}
		clear(shared)
	}()
	var offset, skipped int64
	var lastSeq uint16
	blocks := 0
	warn := func(message string) {
		if len(i.warnings) < 20 {
			i.warnings = append(i.warnings, i.origins[path].name+"："+message)
		}
	}
	for offset < info.Size() {
		if err = i.ctx.Err(); err != nil {
			return "", err
		}
		header, peekErr := reader.Peek(9)
		valid := peekErr == nil && header[0] >= 0x03 && header[0] <= 0x0d && header[3] < 24 && header[4] < 24
		headerLength, length := 13, int64(0)
		if valid {
			if header[0] >= 0x06 {
				headerLength = 73
			}
			length = int64(binary.LittleEndian.Uint32(header[5:9]))
			valid = length <= maxXlogBlock && offset+int64(headerLength)+length < info.Size()
			if valid {
				var tail [1]byte
				_, err = f.ReadAt(tail[:], offset+int64(headerLength)+length)
				valid = err == nil && tail[0] == 0
			}
		}
		if !valid {
			if _, err = reader.ReadByte(); err != nil {
				return "", fmt.Errorf("读取 xlog 偏移 %d：%w", offset, err)
			}
			offset++
			skipped++
			continue
		}
		if skipped > 0 {
			warn(fmt.Sprintf("xlog 偏移 %d 前跳过 %d 字节损坏/非日志内容", offset, skipped))
			skipped = 0
		}
		header, err = reader.Peek(headerLength)
		if err != nil {
			return "", err
		}
		magic, seq := header[0], binary.LittleEndian.Uint16(header[1:3])
		var public [64]byte
		if headerLength == 73 {
			copy(public[:], header[9:73])
		}
		reader.Discard(headerLength)
		data := make([]byte, int(length)+1)
		if _, err = io.ReadFull(reader, data); err != nil {
			return "", err
		}
		data = data[:len(data)-1]
		if magic == 0x07 || magic == 0x0c {
			if private == nil {
				private, err = loadXlogKey(i.options)
				if err != nil {
					return "", err
				}
			}
			if shared == nil || public != peer {
				encoded := append([]byte{4}, public[:]...)
				pub, parseErr := secp256k1.ParsePubKey(encoded)
				if parseErr != nil {
					return "", fmt.Errorf("xlog 偏移 %d 的块公钥损坏", offset)
				}
				clear(shared)
				shared = secp256k1.GenerateSharedSecret(private, pub)
				peer = public
			}
			if err = decryptXlogTEA(i.ctx, data, shared[:16]); err != nil {
				return "", err
			}
		}
		decoded, err := expandXlogBlock(magic, data)
		if err != nil {
			return "", fmt.Errorf("xlog 偏移 %d（类型 0x%02x）解压失败，请检查私钥或文件完整性：%w", offset, magic, err)
		}
		if seq > 1 && lastSeq != 0 && seq != lastSeq+1 {
			warn(fmt.Sprintf("xlog 序号从 %d 跳至 %d，原文件可能缺少日志", lastSeq, seq))
		}
		if seq != 0 {
			lastSeq = seq
		}
		if _, err = writer.Write(decoded); err != nil {
			return "", err
		}
		blocks++
		offset += int64(headerLength) + length + 1
	}
	if blocks == 0 {
		return "", fmt.Errorf("没有识别到 Mars xlog 数据块，文件可能损坏或使用了自定义格式")
	}
	if skipped > 0 {
		warn(fmt.Sprintf("xlog 尾部 %d 字节不完整或为填充数据，已保留此前有效块", skipped))
	}
	if err = out.Close(); err != nil {
		return "", err
	}
	return output, nil
}

func ValidateXlogKeySource(options LogImportOptions) error {
	if strings.TrimSpace(options.XlogKeyFile) != "" && strings.TrimSpace(options.XlogKeyEnv) != "" {
		return fmt.Errorf("Mars 私钥文件和环境变量请选择一种")
	}
	return nil
}
