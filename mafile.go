package steam

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// MAFile represents the Steam Desktop Authenticator (SDA) .maFile JSON structure.
type MAFile struct {
	SharedSecret   string      `json:"shared_secret"`
	SerialNumber   string      `json:"serial_number,omitempty"`
	RevocationCode string      `json:"revocation_code,omitempty"`
	URI            string      `json:"uri,omitempty"`
	ServerTime     int64       `json:"server_time,omitempty"`
	AccountName    string      `json:"account_name"`
	TokenGID       string      `json:"token_gid,omitempty"`
	IdentitySecret string      `json:"identity_secret"`
	Secret1        string      `json:"secret_1,omitempty"`
	Status         int         `json:"status,omitempty"`
	DeviceID       string      `json:"device_id,omitempty"`
	FullyEnrolled  bool        `json:"fully_enrolled,omitempty"`
	Session        *MASession  `json:"Session,omitempty"`
}

// MASession holds web session tokens stored inside the maFile.
type MASession struct {
	SessionID        string         `json:"SessionID"`
	SteamLogin       string         `json:"SteamLogin"`
	SteamLoginSecure string         `json:"SteamLoginSecure"`
	WebCookie        string         `json:"WebCookie"`
	OAuthToken       string         `json:"OAuthToken"`
	AccessToken      string         `json:"AccessToken,omitempty"`
	RefreshToken     string         `json:"RefreshToken,omitempty"`
	SteamID          FlexibleUint64 `json:"SteamID"`
}

// ToClientConfig extracts ClientConfig parameters from the MAFile.
func (m *MAFile) ToClientConfig(password, proxyURL string) ClientConfig {
	steamID := ""
	if m.Session != nil {
		if m.Session.SteamID != 0 {
			steamID = strconv.FormatUint(uint64(m.Session.SteamID), 10)
		}
	}

	return ClientConfig{
		Username:       m.AccountName,
		Password:       password,
		SharedSecret:   m.SharedSecret,
		IdentitySecret: m.IdentitySecret,
		SteamID:        steamID,
		DeviceID:       m.DeviceID,
		ProxyURL:       proxyURL,
	}
}

// LoadMAFile parses an SDA .maFile from a given file path.
func LoadMAFile(filePath string) (*MAFile, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read mafile '%s': %w", filePath, err)
	}

	var maFile MAFile
	if err := json.Unmarshal(data, &maFile); err != nil {
		return nil, fmt.Errorf("failed to parse mafile json '%s': %w", filePath, err)
	}

	return &maFile, nil
}

// LoadMAFilesFromDir scans a directory for all .maFile JSON files.
func LoadMAFilesFromDir(dirPath string) (map[string]*MAFile, error) {
	result := make(map[string]*MAFile)

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return nil, fmt.Errorf("failed to read dir '%s': %w", dirPath, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) == ".maFile" || filepath.Ext(entry.Name()) == ".json" {
			fullPath := filepath.Join(dirPath, entry.Name())
			maFile, err := LoadMAFile(fullPath)
			if err != nil {
				continue // skip invalid files
			}
			if maFile.AccountName != "" {
				result[maFile.AccountName] = maFile
			}
		}
	}

	return result, nil
}
