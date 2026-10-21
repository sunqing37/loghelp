package core

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"unicode/utf16"
)

// Only manifest identity is needed: Android remains responsible for validating
// certificates, SDK requirements and the complete split dependency graph.
type apkIdentity struct {
	Package, Split string
	Version        uint64
}

func parseAPKIdentity(data []byte) (apkIdentity, error) {
	attrs := map[string]string{}
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte("<")) {
		d := xml.NewDecoder(bytes.NewReader(data))
		for {
			t, err := d.Token()
			if err != nil {
				return apkIdentity{}, fmt.Errorf("读取 APK manifest：%w", err)
			}
			if e, ok := t.(xml.StartElement); ok && e.Name.Local == "manifest" {
				for _, a := range e.Attr {
					attrs[a.Name.Local] = a.Value
				}
				break
			}
		}
	} else {
		var err error
		attrs, err = binaryManifestAttrs(data)
		if err != nil {
			return apkIdentity{}, err
		}
	}
	version, err := strconv.ParseUint(attrs["versionCode"], 10, 32)
	if err != nil || attrs["package"] == "" {
		return apkIdentity{}, fmt.Errorf("APK manifest 缺少有效的 package/versionCode")
	}
	if major := attrs["versionCodeMajor"]; major != "" {
		v, err := strconv.ParseUint(major, 10, 32)
		if err != nil {
			return apkIdentity{}, fmt.Errorf("APK versionCodeMajor 无效")
		}
		version |= v << 32
	}
	return apkIdentity{Package: attrs["package"], Split: attrs["split"], Version: version}, nil
}

func binaryManifestAttrs(data []byte) (map[string]string, error) {
	bad := fmt.Errorf("APK AndroidManifest.xml 二进制格式无效")
	if len(data) < 8 || binary.LittleEndian.Uint16(data) != 3 || int(binary.LittleEndian.Uint32(data[4:])) != len(data) {
		return nil, bad
	}
	var pool []string
	for off := int(binary.LittleEndian.Uint16(data[2:])); off+8 <= len(data); {
		chunk := data[off:]
		kind := binary.LittleEndian.Uint16(chunk)
		header, size := int(binary.LittleEndian.Uint16(chunk[2:])), int(binary.LittleEndian.Uint32(chunk[4:]))
		if header < 8 || size < header || size > len(chunk) {
			return nil, bad
		}
		chunk = chunk[:size]
		if kind == 1 {
			var err error
			pool, err = apkStringPool(chunk, header)
			if err != nil {
				return nil, err
			}
		}
		str := func(i uint32) string {
			if uint64(i) < uint64(len(pool)) {
				return pool[i]
			}
			return ""
		}
		if kind == 0x102 {
			if header < 16 || header+20 > size {
				return nil, bad
			}
			ext := chunk[header:]
			if str(binary.LittleEndian.Uint32(ext[4:])) == "manifest" {
				start, stride, count := int(binary.LittleEndian.Uint16(ext[8:])), int(binary.LittleEndian.Uint16(ext[10:])), int(binary.LittleEndian.Uint16(ext[12:]))
				if start < 20 || stride < 20 || start+stride*count > len(ext) {
					return nil, bad
				}
				attrs := map[string]string{}
				for i := 0; i < count; i++ {
					a := ext[start+i*stride:]
					name := str(binary.LittleEndian.Uint32(a[4:]))
					value := str(binary.LittleEndian.Uint32(a[8:]))
					if value == "" {
						v := binary.LittleEndian.Uint32(a[16:])
						switch a[15] {
						case 3:
							value = str(v)
						case 0x10, 0x11:
							value = strconv.FormatUint(uint64(v), 10)
						}
					}
					attrs[name] = value
				}
				return attrs, nil
			}
		}
		off += size
	}
	return nil, bad
}

func apkStringPool(chunk []byte, header int) ([]string, error) {
	bad := fmt.Errorf("APK manifest 字符串池无效")
	if header < 28 || len(chunk) < header {
		return nil, bad
	}
	count := int(binary.LittleEndian.Uint32(chunk[8:]))
	start := int(binary.LittleEndian.Uint32(chunk[20:]))
	if count > (len(chunk)-header)/4 || start < header+count*4 || start > len(chunk) {
		return nil, bad
	}
	utf8 := binary.LittleEndian.Uint32(chunk[16:])&0x100 != 0
	result := make([]string, count)
	// Offsets may alias the same long string. Bound the decoded allocation as
	// well as the input size so malformed pools cannot amplify memory usage.
	remainingBytes := 16 << 20
	for i := range result {
		off := uint64(start) + uint64(binary.LittleEndian.Uint32(chunk[header+i*4:]))
		if off >= uint64(len(chunk)) {
			return nil, bad
		}
		r := bytes.NewReader(chunk[off:])
		length := func() (int, error) {
			if utf8 {
				b, err := r.ReadByte()
				if err != nil || b&0x80 == 0 {
					return int(b), err
				}
				c, err := r.ReadByte()
				return int(b&0x7f)<<8 | int(c), err
			}
			var b, c uint16
			if err := binary.Read(r, binary.LittleEndian, &b); err != nil {
				return 0, err
			}
			if b&0x8000 == 0 {
				return int(b), nil
			}
			err := binary.Read(r, binary.LittleEndian, &c)
			return int(b&0x7fff)<<16 | int(c), err
		}
		n, err := length()
		if utf8 && err == nil {
			n, err = length() // UTF-8 byte length follows the UTF-16 length.
		}
		if err != nil || n > r.Len() {
			return nil, bad
		}
		decodedBytes := n
		if !utf8 {
			decodedBytes *= 3 // Maximum UTF-8 bytes per UTF-16 code unit.
		}
		if decodedBytes > remainingBytes {
			return nil, fmt.Errorf("APK manifest 字符串池解码超过 16 MiB 限制")
		}
		remainingBytes -= decodedBytes
		if utf8 {
			b := make([]byte, n)
			if _, err := io.ReadFull(r, b); err != nil {
				return nil, bad
			}
			result[i] = string(b)
		} else {
			if n > r.Len()/2 {
				return nil, bad
			}
			b := make([]uint16, n)
			if err := binary.Read(r, binary.LittleEndian, b); err != nil {
				return nil, bad
			}
			result[i] = string(utf16.Decode(b))
		}
	}
	return result, nil
}
