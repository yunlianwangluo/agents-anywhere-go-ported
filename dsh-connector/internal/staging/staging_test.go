package staging

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestStagePlacesVerifiedBytes covers the contract the bridge enforces: the
// staged file is named by a 32 character hex upload id, holds exactly the bytes
// the backend served, and is removed after the turn call returned.
func TestStagePlacesVerifiedBytes(t *testing.T) {
	content := []byte("not-really-a-png")
	digest := sha256.Sum256(content)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if want := "/api/v2/connector/sessions/session-1/attachments/file_abc/content"; request.URL.Path != want {
			t.Errorf("path = %s, want %s", request.URL.Path, want)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("authorization = %q, want the connector key", got)
		}
		writer.Header().Set("X-File-Name", "shot.png")
		writer.Header().Set("Content-Type", "image/png")
		_, _ = writer.Write(content)
	}))
	defer server.Close()

	bridgeEndpoint := filepath.Join(t.TempDir(), "bridge", "endpoint.json")
	payloads, cleanup, err := Stage(server.URL, "secret", bridgeEndpoint, "session-1", []any{map[string]any{
		"fileId": "file_abc", "name": "shot.png", "mediaType": "image/png",
		"sha256": hex.EncodeToString(digest[:]),
	}})
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("payloads = %d, want 1", len(payloads))
	}
	payload := payloads[0]
	uploadID, _ := payload["uploadId"].(string)
	if !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(uploadID) {
		t.Fatalf("uploadId = %q, want 32 lowercase hex characters", uploadID)
	}
	if payload["fileId"] != "file_abc" || payload["mediaType"] != "image/png" {
		t.Fatalf("payload = %v, want the backend's identity carried through", payload)
	}
	if payload["sha256"] != hex.EncodeToString(digest[:]) {
		t.Fatalf("sha256 = %v, want the bytes' own checksum", payload["sha256"])
	}
	if size, _ := payload["size"].(int); size != len(content) {
		t.Fatalf("size = %v, want %d", payload["size"], len(content))
	}

	staged := filepath.Join(filepath.Dir(bridgeEndpoint), "attachments", "staging", uploadID)
	stored, err := os.ReadFile(staged)
	if err != nil {
		t.Fatalf("read staged file: %v", err)
	}
	if string(stored) != string(content) {
		t.Fatalf("staged bytes = %q, want %q", stored, content)
	}
	info, err := os.Stat(staged)
	if err != nil {
		t.Fatalf("stat staged file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("staged permissions = %v, want 0600", info.Mode().Perm())
	}

	cleanup()
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatalf("staged file survived cleanup: %v", err)
	}
}

// TestStageRejectsABadChecksum keeps the bridge from reading bytes the backend
// did not intend to send.
func TestStageRejectsABadChecksum(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte("tampered"))
	}))
	defer server.Close()

	_, cleanup, err := Stage(server.URL, "secret", filepath.Join(t.TempDir(), "endpoint.json"), "session-1", []any{map[string]any{
		"fileId": "file_abc",
		"sha256": "0000000000000000000000000000000000000000000000000000000000000000",
	}})
	if err == nil {
		t.Fatal("stage accepted bytes that do not match the declared checksum")
	}
	cleanup()
}
