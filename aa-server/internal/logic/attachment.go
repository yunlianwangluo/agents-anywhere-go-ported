package logic

import (
	"fmt"
	"net/http"
	"time"
)

// SaveAttachment stores an uploaded file, generating an id when none was given.
func (s *Server) SaveAttachment(id string, data []byte) (int, any) {
	if id == "" {
		id = fmt.Sprintf("attachment-%d", time.Now().UnixNano())
	}
	if err := s.repo.SaveAttachment(id, data); err != nil {
		return http.StatusBadRequest, map[string]any{"error": err.Error()}
	}
	return http.StatusOK, map[string]any{"fileId": id, "size": len(data)}
}

// AttachmentPath resolves a stored attachment for serving.
func (s *Server) AttachmentPath(id string) (string, error) {
	return s.repo.AttachmentPath(id)
}
