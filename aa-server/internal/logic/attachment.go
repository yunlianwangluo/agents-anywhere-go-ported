package logic

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"aa-server/internal/storage"
	"aa-server/internal/view"
)

// Upload limits match the client's own contract: at most five files, 25 MiB each.
const (
	maxAttachmentsPerUpload = 5
	maxAttachmentBytes      = 25 << 20
)

// AttachmentInput is one uploaded file, already read into memory.
type AttachmentInput struct {
	Name      string
	MediaType string
	Data      []byte
}

// SaveAttachments stores uploads and renders the client's upload response. The
// bytes land on disk before a reference is returned, so a reference can never
// point at a file that is not there.
func (s *Server) SaveAttachments(sessionID string, inputs []AttachmentInput) (int, any) {
	if len(inputs) == 0 {
		return http.StatusBadRequest, map[string]any{"error": "no files were uploaded"}
	}
	if len(inputs) > maxAttachmentsPerUpload {
		return http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("at most %d files per upload", maxAttachmentsPerUpload)}
	}
	references := make([]map[string]any, 0, len(inputs))
	for _, input := range inputs {
		meta, err := s.storeAttachment(sessionID, input)
		if err != nil {
			return http.StatusBadRequest, map[string]any{"error": err.Error()}
		}
		references = append(references, attachmentReference(meta))
	}
	return http.StatusOK, map[string]any{"attachments": references, "serverTime": view.Now()}
}

// storeAttachment writes one file and its metadata.
func (s *Server) storeAttachment(sessionID string, input AttachmentInput) (storage.AttachmentMeta, error) {
	if len(input.Data) == 0 {
		return storage.AttachmentMeta{}, fmt.Errorf("empty file")
	}
	if len(input.Data) > maxAttachmentBytes {
		return storage.AttachmentMeta{}, fmt.Errorf("file exceeds %d bytes", maxAttachmentBytes)
	}
	digest := sha256.Sum256(input.Data)
	meta := storage.AttachmentMeta{
		FileID:    newFileID(),
		Name:      input.Name,
		MediaType: input.MediaType,
		Size:      int64(len(input.Data)),
		SHA256:    hex.EncodeToString(digest[:]),
		SessionID: sessionID,
		CreatedAt: time.Now().UTC(),
		Status:    "stored",
	}
	if err := s.repo.SaveAttachment(meta, input.Data); err != nil {
		return storage.AttachmentMeta{}, err
	}
	return meta, nil
}

// newFileID matches the identifier the DSH bridge accepts: file_ followed by
// lowercase hex.
func newFileID() string {
	buf := make([]byte, 12)
	_, _ = rand.Read(buf)
	return "file_" + hex.EncodeToString(buf)
}

// attachmentReference is the upload response element the client decodes.
func attachmentReference(meta storage.AttachmentMeta) map[string]any {
	return map[string]any{
		"fileId":      meta.FileID,
		"sessionId":   meta.SessionID,
		"name":        meta.Name,
		"mediaType":   meta.MediaType,
		"size":        meta.Size,
		"sha256":      meta.SHA256,
		"createdAt":   view.Timestamp(meta.CreatedAt),
		"downloadUrl": attachmentURL(meta.SessionID, meta.FileID),
		"openUrl":     attachmentURL(meta.SessionID, meta.FileID),
	}
}

// AttachmentDownload renders a stored attachment as the client's download
// document, which carries the bytes inline as base64.
func (s *Server) AttachmentDownload(fileID string) (int, any) {
	meta, data, err := s.AttachmentBlob("", fileID)
	if err != nil {
		return http.StatusNotFound, map[string]any{"error": "attachment not found"}
	}
	return http.StatusOK, map[string]any{
		"fileId":        meta.FileID,
		"sessionId":     meta.SessionID,
		"path":          meta.Name,
		"name":          meta.Name,
		"size":          meta.Size,
		"sha256":        meta.SHA256,
		"contentBase64": base64.StdEncoding.EncodeToString(data),
		"createdAt":     view.Timestamp(meta.CreatedAt),
		"serverTime":    view.Now(),
	}
}

// AttachmentBlob returns the stored bytes and metadata. A non-empty sessionID
// must match the owning session, so one session cannot read another's file.
func (s *Server) AttachmentBlob(sessionID, fileID string) (storage.AttachmentMeta, []byte, error) {
	meta, err := s.repo.ReadAttachmentMeta(fileID)
	if err != nil {
		return storage.AttachmentMeta{}, nil, err
	}
	if sessionID != "" && meta.SessionID != "" && meta.SessionID != sessionID {
		return storage.AttachmentMeta{}, nil, fmt.Errorf("attachment belongs to another session")
	}
	path, err := s.repo.AttachmentPath(fileID)
	if err != nil {
		return storage.AttachmentMeta{}, nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return storage.AttachmentMeta{}, nil, err
	}
	return meta, data, nil
}

// attachmentURL is the path the client can fetch the attachment from.
func attachmentURL(sessionID, fileID string) string {
	if sessionID == "" {
		return "/api/v2/attachments/" + fileID
	}
	return "/api/v2/sessions/" + sessionID + "/attachments/" + fileID
}

// resolveAttachments expands the client's references into the payload the bridge
// accepts. An element is either an already uploaded reference ({fileId}) or an
// inline upload sent with a session creation ({name, mediaType, contentBase64}).
func (s *Server) resolveAttachments(sessionID string, raw any) []map[string]any {
	list, _ := raw.([]any)
	if len(list) == 0 {
		return nil
	}
	payloads := make([]map[string]any, 0, len(list))
	for _, value := range list {
		reference, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if view.StringValue(reference["fileId"]) == "" && view.StringValue(reference["contentBase64"]) == "" {
			continue
		}
		meta, err := s.attachmentMeta(sessionID, reference)
		if err != nil {
			log.Printf("resolve attachment: %v", err)
			continue
		}
		payloads = append(payloads, map[string]any{
			"fileId": meta.FileID, "name": meta.Name, "mediaType": meta.MediaType,
			"size": meta.Size, "sha256": meta.SHA256,
		})
	}
	return payloads
}

// attachmentMeta resolves one reference, storing it first when the client sent
// the bytes inline instead of uploading them.
func (s *Server) attachmentMeta(sessionID string, reference map[string]any) (storage.AttachmentMeta, error) {
	if encoded := view.StringValue(reference["contentBase64"]); encoded != "" {
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return storage.AttachmentMeta{}, err
		}
		return s.storeAttachment(sessionID, AttachmentInput{
			Name:      view.FirstNonEmpty(view.StringValue(reference["name"]), "attachment"),
			MediaType: view.StringValue(reference["mediaType"]),
			Data:      data,
		})
	}
	fileID := view.StringValue(reference["fileId"])
	meta, err := s.repo.ReadAttachmentMeta(fileID)
	if err != nil {
		return storage.AttachmentMeta{}, fmt.Errorf("attachment %s not found", fileID)
	}
	if meta.SessionID != "" && sessionID != "" && meta.SessionID != sessionID {
		return storage.AttachmentMeta{}, fmt.Errorf("attachment %s belongs to another session", fileID)
	}
	return meta, nil
}
