package steam

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"math"
	"mime/multipart"
	"net/http"
	"strings"
)

// ── Protobuf encoder ─────────────────────────────────────────────────────────

// pbVarint encodes a field with wire type 0 (varint).
func pbVarint(field int, v uint64) []byte {
	return append(pbTag(field, 0), encodeVarint(v)...)
}

// pbInt64 encodes a field with wire type 1 (64-bit little-endian).
// Used for steamid in UpdateAuthSessionWithSteamGuardCode.
func pbInt64(field int, v uint64) []byte {
	b := pbTag(field, 1)
	for i := 0; i < 8; i++ {
		b = append(b, byte(v>>(uint(i)*8)))
	}
	return b
}

// pbString encodes a field with wire type 2 (length-delimited string).
func pbString(field int, s string) []byte {
	return pbBytes(field, []byte(s))
}

// pbBytes encodes a field with wire type 2 (length-delimited bytes).
func pbBytes(field int, data []byte) []byte {
	b := pbTag(field, 2)
	b = append(b, encodeVarint(uint64(len(data)))...)
	return append(b, data...)
}

// pbNested encodes a nested message as a length-delimited field.
func pbNested(field int, nested []byte) []byte {
	return pbBytes(field, nested)
}

func pbTag(field int, wireType int) []byte {
	return encodeVarint(uint64(field<<3) | uint64(wireType))
}

func encodeVarint(v uint64) []byte {
	var b []byte
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}

// ── Protobuf decoder ─────────────────────────────────────────────────────────

// PbField represents a single decoded protobuf field.
type PbField struct {
	FieldNum int
	WireType int
	// For wire type 0 (varint) and 1 (64-bit):
	Uint64 uint64
	// For wire type 2 (length-delimited):
	Bytes []byte
	// For wire type 5 (32-bit float):
	Float32 float32
}

// pbDecode parses a binary protobuf message into a slice of PbField.
func pbDecode(data []byte) ([]PbField, error) {
	var fields []PbField
	pos := 0
	for pos < len(data) {
		tag, n := decodeVarint(data, pos)
		if n <= 0 {
			return fields, fmt.Errorf("steamproto: malformed varint at byte %d", pos)
		}
		pos += n
		fieldNum := int(tag >> 3)
		wireType := int(tag & 7)

		f := PbField{FieldNum: fieldNum, WireType: wireType}

		switch wireType {
		case 0: // varint
			v, n := decodeVarint(data, pos)
			if n <= 0 {
				return fields, fmt.Errorf("steamproto: malformed varint value at byte %d", pos)
			}
			f.Uint64 = v
			pos += n
		case 1: // 64-bit
			if pos+8 > len(data) {
				return fields, fmt.Errorf("steamproto: truncated int64 at byte %d", pos)
			}
			var v uint64
			for i := 0; i < 8; i++ {
				v |= uint64(data[pos+i]) << (uint(i) * 8)
			}
			f.Uint64 = v
			pos += 8
		case 2: // length-delimited
			l, n := decodeVarint(data, pos)
			if n <= 0 {
				return fields, fmt.Errorf("steamproto: malformed length at byte %d", pos)
			}
			pos += n
			if pos+int(l) > len(data) {
				return fields, fmt.Errorf("steamproto: truncated bytes field at byte %d", pos)
			}
			f.Bytes = make([]byte, l)
			copy(f.Bytes, data[pos:pos+int(l)])
			pos += int(l)
		case 5: // 32-bit float
			if pos+4 > len(data) {
				return fields, fmt.Errorf("steamproto: truncated float32 at byte %d", pos)
			}
			var bits uint32
			for i := 0; i < 4; i++ {
				bits |= uint32(data[pos+i]) << (uint(i) * 8)
			}
			f.Float32 = math.Float32frombits(bits)
			pos += 4
		case 3, 4:
			return fields, fmt.Errorf("steamproto: unsupported wire type %d at byte %d", wireType, pos)
		default:
			return fields, fmt.Errorf("steamproto: unknown wire type %d at byte %d", wireType, pos)
		}

		fields = append(fields, f)
	}
	return fields, nil
}

// pbGetString returns the first string value for the given field number.
func pbGetString(fields []PbField, fieldNum int) string {
	for _, f := range fields {
		if f.FieldNum == fieldNum && f.WireType == 2 {
			return string(f.Bytes)
		}
	}
	return ""
}

// pbGetBytes returns the first raw bytes value for the given field number.
func pbGetBytes(fields []PbField, fieldNum int) []byte {
	for _, f := range fields {
		if f.FieldNum == fieldNum && f.WireType == 2 {
			return f.Bytes
		}
	}
	return nil
}

// pbGetUint64 returns the first uint64 for the given field number (wire types 0 and 1).
func pbGetUint64(fields []PbField, fieldNum int) uint64 {
	for _, f := range fields {
		if f.FieldNum == fieldNum && (f.WireType == 0 || f.WireType == 1) {
			return f.Uint64
		}
	}
	return 0
}

func decodeVarint(data []byte, pos int) (uint64, int) {
	var result uint64
	var shift uint
	for i := pos; i < len(data) && i < pos+10; i++ {
		b := data[i]
		result |= uint64(b&0x7F) << shift
		if b&0x80 == 0 {
			return result, i - pos + 1
		}
		shift += 7
	}
	return 0, -1
}

// ── HTTP helpers ─────────────────────────────────────────────────────────────

// newMultipartProtoRequest creates a POST request with Content-Type multipart/form-data
// containing a single field "input_protobuf_encoded" = base64(pbData).
// This matches the exact format used by the Steam web client in Chrome.
func (c *Client) newMultipartProtoRequest(ctx context.Context, reqURL string, pbData []byte, referer string) (*http.Request, error) {
	encoded := base64.StdEncoding.EncodeToString(pbData)

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("input_protobuf_encoded", encoded); err != nil {
		return nil, err
	}
	w.Close()

	req, err := c.newFetchRequestWithContext(ctx, "POST", reqURL, strings.NewReader(buf.String()), referer)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Origin", "https://steamcommunity.com")
	return req, nil
}
