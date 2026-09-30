package logic

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"regexp"
	"testing"
)

// TestSaveAttachmentsMatchesTheBridgeContract covers what the DSH bridge will
// later validate: the file id shape, the checksum, and the reference fields the
// client renders.
func TestSaveAttachmentsMatchesTheBridgeContract(t *testing.T) {
	server, _, _ := newTestServer(t)
	content := []byte("fake-image-bytes")

	status, body := server.SaveAttachments("session-1", []AttachmentInput{{
		Name: "shot.png", MediaType: "image/png", Data: content,
	}})
	if status != 200 {
		t.Fatalf("status = %d, want 200 (%v)", status, body)
	}
	references, _ := body.(map[string]any)["attachments"].([]map[string]any)
	if len(references) != 1 {
		t.Fatalf("references = %v, want one", body)
	}
	reference := references[0]
	fileID, _ := reference["fileId"].(string)
	if !regexp.MustCompile(`^file_[A-Za-z0-9._-]{1,122}$`).MatchString(fileID) {
		t.Fatalf("fileId = %q, want the bridge's file_ prefix", fileID)
	}
	digest := sha256.Sum256(content)
	if reference["sha256"] != hex.EncodeToString(digest[:]) {
		t.Fatalf("sha256 = %v, want the bytes' own checksum", reference["sha256"])
	}
	if reference["size"] != int64(len(content)) || reference["mediaType"] != "image/png" {
		t.Fatalf("reference = %v, want size and media type carried through", reference)
	}

	// The connector reads the same bytes back through its own route.
	meta, blob, err := server.AttachmentBlob("session-1", fileID)
	if err != nil {
		t.Fatalf("blob: %v", err)
	}
	if string(blob) != string(content) || meta.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("blob = %q (%+v), want the stored bytes", blob, meta)
	}
	// Another session must not be able to read it.
	if _, _, err := server.AttachmentBlob("session-2", fileID); err == nil {
		t.Fatal("a different session read the attachment")
	}
}

// TestResolveAttachmentsExpandsBothForms covers what the bridge receives: an
// uploaded reference is looked up, and an inline upload is stored first.
func TestResolveAttachmentsExpandsBothForms(t *testing.T) {
	server, _, _ := newTestServer(t)
	const session = "session-1"

	stored, err := server.storeAttachment(session, AttachmentInput{Name: "a.png", MediaType: "image/png", Data: []byte("one")})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	inline := base64.StdEncoding.EncodeToString([]byte("two"))

	payloads := server.resolveAttachments(session, []any{
		map[string]any{"fileId": stored.FileID},
		map[string]any{"name": "b.jpg", "mediaType": "image/jpeg", "contentBase64": inline},
		map[string]any{"fileId": "file_missing"},
	})
	if len(payloads) != 2 {
		t.Fatalf("payloads = %v, want the uploaded and the inline file only", payloads)
	}
	for _, payload := range payloads {
		for _, key := range []string{"fileId", "name", "mediaType", "size", "sha256"} {
			if payload[key] == nil {
				t.Fatalf("payload %v is missing %s", payload, key)
			}
		}
		if _, leaked := payload["contentBase64"]; leaked {
			t.Fatalf("payload %v still carries inline bytes", payload)
		}
	}
	if payloads[0]["fileId"] != stored.FileID {
		t.Fatalf("first payload = %v, want the uploaded file", payloads[0])
	}
	digest := sha256.Sum256([]byte("two"))
	if payloads[1]["sha256"] != hex.EncodeToString(digest[:]) {
		t.Fatalf("inline payload = %v, want the decoded bytes' checksum", payloads[1])
	}
}

// TestSaveAttachmentsEnforcesUploadLimits keeps a request from filling the disk.
func TestSaveAttachmentsEnforcesUploadLimits(t *testing.T) {
	server, _, _ := newTestServer(t)
	inputs := make([]AttachmentInput, maxAttachmentsPerUpload+1)
	for index := range inputs {
		inputs[index] = AttachmentInput{Name: "x", MediaType: "image/png", Data: []byte("x")}
	}
	if status, _ := server.SaveAttachments("session-1", inputs); status != 400 {
		t.Fatalf("status = %d, want 400 for too many files", status)
	}
	if status, _ := server.SaveAttachments("session-1", []AttachmentInput{{Name: "big", MediaType: "image/png", Data: make([]byte, maxAttachmentBytes+1)}}); status != 400 {
		t.Fatalf("status = %d, want 400 for an oversized file", status)
	}
}
