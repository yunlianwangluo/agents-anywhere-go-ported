// Package staging downloads the attachments a turn references from the backend
// and places them where the DSH bridge expects to read them. The bridge validates
// each reference (file id, upload id, size, checksum) and reads the bytes while
// the turn call runs, so staged copies are removed once that call returned.
package staging

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// maxAttachmentBytes matches the backend's own upload limit.
const maxAttachmentBytes = 25 << 20

// Stage downloads every referenced attachment into <bridge>/attachments/staging
// and returns the payloads the bridge accepts, plus a cleanup that removes the
// staged copies. Call cleanup only after the bridge call that consumed them.
func Stage(serverURL, clientKey, bridgeEndpoint, sessionID string, attachments []any) ([]map[string]any, func(), error) {
	noop := func() {}
	if bridgeEndpoint == "" {
		return nil, noop, fmt.Errorf("bridge endpoint is not configured")
	}
	directory := filepath.Join(filepath.Dir(bridgeEndpoint), "attachments", "staging")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, noop, err
	}
	client := &http.Client{Timeout: 60 * time.Second}
	payloads := make([]map[string]any, 0, len(attachments))
	staged := make([]string, 0, len(attachments))
	cleanup := func() {
		for _, path := range staged {
			_ = os.Remove(path)
		}
	}

	for _, value := range attachments {
		reference, ok := value.(map[string]any)
		if !ok {
			continue
		}
		fileID := stringValue(reference["fileId"])
		if fileID == "" {
			continue
		}
		data, name, mediaType, err := fetch(client, serverURL, clientKey, sessionID, fileID)
		if err != nil {
			cleanup()
			return nil, noop, err
		}
		digest := sha256.Sum256(data)
		checksum := hex.EncodeToString(digest[:])
		if declared := stringValue(reference["sha256"]); declared != "" && declared != checksum {
			cleanup()
			return nil, noop, fmt.Errorf("attachment %s failed its checksum", fileID)
		}
		uploadID := newUploadID()
		path := filepath.Join(directory, uploadID)
		if err := writeFile(path, data); err != nil {
			cleanup()
			return nil, noop, err
		}
		staged = append(staged, path)
		payloads = append(payloads, map[string]any{
			"uploadId":  uploadID,
			"fileId":    fileID,
			"name":      firstNonEmpty(stringValue(reference["name"]), name, fileID),
			"mediaType": firstNonEmpty(stringValue(reference["mediaType"]), mediaType, "application/octet-stream"),
			"size":      len(data),
			"sha256":    checksum,
		})
	}
	return payloads, cleanup, nil
}

// fetch downloads one attachment from the backend.
func fetch(client *http.Client, serverURL, clientKey, sessionID, fileID string) ([]byte, string, string, error) {
	address := fmt.Sprintf("%s/api/v2/connector/sessions/%s/attachments/%s/content",
		strings.TrimRight(serverURL, "/"), sessionID, fileID)
	request, err := http.NewRequest(http.MethodGet, address, nil)
	if err != nil {
		return nil, "", "", err
	}
	request.Header.Set("Authorization", "Bearer "+clientKey)
	response, err := client.Do(request)
	if err != nil {
		return nil, "", "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, "", "", fmt.Errorf("attachment %s download failed: %s", fileID, response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxAttachmentBytes+1))
	if err != nil {
		return nil, "", "", err
	}
	if len(data) > maxAttachmentBytes {
		return nil, "", "", fmt.Errorf("attachment %s exceeds %d bytes", fileID, maxAttachmentBytes)
	}
	return data, response.Header.Get("X-File-Name"), response.Header.Get("Content-Type"), nil
}

// writeFile stages the bytes with the permissions the bridge expects.
func writeFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// newUploadID is the 32 character lowercase hex id the bridge requires.
func newUploadID() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

func stringValue(value any) string {
	result, _ := value.(string)
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
