package steam

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"time"
)

// steamCharset contains the 26 characters used by Steam Guard 2FA codes.
const steamCharset = "23456789BCDFGHJKMNPQRTVWXY"

func decodeSecret(secret string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(secret)
	if err != nil {
		return nil, fmt.Errorf("failed to decode secret base64: %w", err)
	}
	return b, nil
}

// GenerateTwoFactorCode computes a 5-character Steam Guard 2FA code for the given sharedSecret at timestamp t.
// If t is 0, current UTC time is used.
func GenerateTwoFactorCode(sharedSecret string, t int64) (string, error) {
	if t == 0 {
		t = time.Now().Unix()
	}

	secretBytes, err := decodeSecret(sharedSecret)
	if err != nil {
		return "", err
	}

	// Steam uses 30-second time intervals.
	timeInterval := uint64(t / 30)

	// Pack time interval as 8-byte big-endian integer.
	timeBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(timeBytes, timeInterval)

	// HMAC-SHA1
	mac := hmac.New(sha1.New, secretBytes)
	mac.Write(timeBytes)
	hmacHash := mac.Sum(nil)

	// Dynamic Truncation
	offset := hmacHash[19] & 0x0F
	fullCode := ((uint32(hmacHash[offset]) & 0x7F) << 24) |
		((uint32(hmacHash[offset+1]) & 0xFF) << 16) |
		((uint32(hmacHash[offset+2]) & 0xFF) << 8) |
		(uint32(hmacHash[offset+3]) & 0xFF)

	// Convert to 5-character Steam code
	code := make([]byte, 5)
	for i := 0; i < 5; i++ {
		code[i] = steamCharset[fullCode%26]
		fullCode /= 26
	}

	return string(code), nil
}

// GenerateConfirmationHash computes the base64-encoded HMAC-SHA1 confirmation signature
// using identitySecret for a given tag (e.g. "conf", "details", "allow", "cancel") and timestamp t.
func GenerateConfirmationHash(identitySecret string, tag string, t int64) (string, error) {
	if t == 0 {
		t = time.Now().Unix()
	}

	secretBytes, err := decodeSecret(identitySecret)
	if err != nil {
		return "", err
	}

	// Pack timestamp as 8-byte big-endian integer
	timeBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(timeBytes, uint64(t))

	// Data to sign: 8-byte timestamp + tag bytes
	data := append(timeBytes, []byte(tag)...)

	mac := hmac.New(sha1.New, secretBytes)
	mac.Write(data)
	hash := mac.Sum(nil)

	return base64.StdEncoding.EncodeToString(hash), nil
}
