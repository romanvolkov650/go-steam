package steam

import (
	"encoding/base64"
	"testing"
)

// TestPbEncodeDecodeRoundtrip verifies that encode → decode produces the same fields.
func TestPbEncodeDecodeRoundtrip(t *testing.T) {
	// Build a message with all wire types used in auth flow.
	var pb []byte
	pb = append(pb, pbString(1, "hello")...)       // wire 2
	pb = append(pb, pbVarint(2, 12345678901234)...) // wire 0
	pb = append(pb, pbInt64(3, 76561198781725508)...) // wire 1
	pb = append(pb, pbBytes(4, []byte{0xde, 0xad, 0xbe, 0xef})...) // wire 2

	fields, err := pbDecode(pb)
	if err != nil {
		t.Fatalf("pbDecode error: %v", err)
	}
	if len(fields) != 4 {
		t.Fatalf("expected 4 fields, got %d", len(fields))
	}

	if s := pbGetString(fields, 1); s != "hello" {
		t.Errorf("field 1: want 'hello', got %q", s)
	}
	if v := pbGetUint64(fields, 2); v != 12345678901234 {
		t.Errorf("field 2: want 12345678901234, got %d", v)
	}
	if v := pbGetUint64(fields, 3); v != 76561198781725508 {
		t.Errorf("field 3: want 76561198781725508, got %d", v)
	}
	if b := pbGetBytes(fields, 4); len(b) != 4 || b[0] != 0xde || b[3] != 0xef {
		t.Errorf("field 4: want [de ad be ef], got %v", b)
	}
}

// TestPbDecodeHARRSAResponse verifies we can decode the real RSA response from HAR.
// The raw binary from HAR entry 201 response (base64-encoded for storage).
func TestPbDecodeHARRSAResponse(t *testing.T) {
	// This is the base64 of the actual binary protobuf RSA response from HAR.
	harRespB64 := "CoAEZTkwNTRlMzc1OGFkMmZjNGQ0MGM2ZDkyZDkxZDkyODIyNzA0MjYwZWUxN2ExMGE2N2RhZWZlYzk2MzcxMjIwZDJlNjg3M2NmYjRiMWE3NWEyNTQ2ODAzYjFjMmM0MmY3NzM4OTI4NjQ4ODgwZDYwZmE3ZWIyNWVjYzJlYjBhNzhkOGIyNjEwMTVjYmIxNDk5MjQ4NGYzNjA0ZGZhZmZhN2M2ODk3NzNhOWUxNjZiOTBmNzAxMWY5OTdiZjYyOWFhZWY3YjY0NWMxYmU3YTEwYmFjYWJkMDg2ZTk5MmI5NWY1Y2FkOWZhODlmYmUyZTkzNWQxNjY4YzZkYTk4Y2Q3MTU3ZmI2MDQ4NGU5N2VmMTNlOGU3MWRkZGQ5ZmU4M2NiYTAyZWE2NjhlNjg0ZGM0NDYyM2YzZTU2ODhiMmIwYThkNDExYjU4MjFkZjEyODFmNzVjMmJiYmUxODdkYjMxOGQ0YzBiMDViZmQzNDJiYjg4NDVjODYyODNhNjlmYTRiNjAxYWNiODUyZGY1MWVkZGFhMTA5YzkxNWQ4ZjcwOWViYzczNjdmZmUxOTc3N2Y5MTQzMTk3ODNjYTRiZjAzMzNlNWQyODViMjhiODdjYWEwZjYxZWEzZjhiMzM3ZWNkNzk0MDMyZjgwNzAxOTY3NjM1ZWE1NjBkZmJmODljMzcSBjAxMDAwMRiAgbaA+Aw="

	data, err := base64.StdEncoding.DecodeString(harRespB64)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}

	fields, err := pbDecode(data)
	if err != nil {
		t.Fatalf("pbDecode: %v", err)
	}

	mod := pbGetString(fields, 1)
	exp := pbGetString(fields, 2)
	ts := pbGetUint64(fields, 3)

	if len(mod) < 100 {
		t.Errorf("publickey_mod too short: %q", mod)
	}
	if exp != "010001" {
		t.Errorf("publickey_exp: want '010001', got %q", exp)
	}
	if ts == 0 {
		t.Error("timestamp is 0")
	}
	t.Logf("mod len=%d, exp=%s, timestamp=%d", len(mod), exp, ts)
}

// TestPbDecodeHARBeginAuthResponse verifies we can decode the real BeginAuth response from HAR.
// Only the first ~55 bytes contain client_id and request_id, so we test with the full raw bytes directly.
func TestPbDecodeHARBeginAuthResponse(t *testing.T) {
	// First 55 bytes of the BeginAuth response protobuf (from HAR entry 202):
	//   [0] field 1 (client_id varint)
	//   [11] field 2 (request_id 16 bytes)
	//   [29] field 3 (interval float32 = 5.0)
	rawPrefix := []byte{
		0x08, 0xda, 0x9d, 0xc6, 0xa5, 0x90, 0xe4, 0xba, 0xa0, 0xc7, 0x01, // field 1 client_id varint
		0x12, 0x10, 0xa1, 0xae, 0xbb, 0x25, 0x61, 0x62, 0xc1, 0xe3, 0x12, 0x73, 0x46, 0x89, 0xbc, 0x58, 0xc7, 0x9f, // field 2 request_id 16 bytes
		0x1d, 0x00, 0x00, 0xa0, 0x40, // field 3 interval float32 = 5.0
	}

	fields, err := pbDecode(rawPrefix)
	if err != nil {
		t.Fatalf("pbDecode: %v", err)
	}

	clientID := pbGetUint64(fields, 1)
	requestID := pbGetBytes(fields, 2)

	if clientID != 14357734139102334682 {
		t.Errorf("client_id: want 14357734139102334682, got %d", clientID)
	}
	if len(requestID) != 16 {
		t.Errorf("request_id len: want 16, got %d", len(requestID))
	}
	t.Logf("client_id=%d, request_id=%x", clientID, requestID)
}

// TestPbNestedEncoding verifies nested message encoding matches expected bytes.
func TestPbNestedEncoding(t *testing.T) {
	// Encode: field [1] string "test", field [2] varint 2
	nested := append(pbString(1, "test"), pbVarint(2, 2)...)
	outer := pbNested(9, nested)

	// Decode outer
	fields, err := pbDecode(outer)
	if err != nil {
		t.Fatalf("pbDecode outer: %v", err)
	}
	if len(fields) != 1 || fields[0].FieldNum != 9 {
		t.Fatalf("expected field 9, got %+v", fields)
	}

	// Decode inner
	inner, err := pbDecode(fields[0].Bytes)
	if err != nil {
		t.Fatalf("pbDecode inner: %v", err)
	}
	if s := pbGetString(inner, 1); s != "test" {
		t.Errorf("inner field 1: want 'test', got %q", s)
	}
	if v := pbGetUint64(inner, 2); v != 2 {
		t.Errorf("inner field 2: want 2, got %d", v)
	}
}
