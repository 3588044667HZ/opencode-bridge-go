package fileserver

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go-bridge/internal/files"
	"go-bridge/internal/jsonutil"
)

type Server struct {
	httpServer *http.Server
	listener   net.Listener
	token      string
	service    *files.Service
	handler    http.Handler
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorResponse struct {
	Error apiError `json:"error"`
}

type outboxList struct {
	Direction  string              `json:"direction"`
	Files      []files.StagingFile `json:"files"`
	TotalBytes int64               `json:"totalBytes"`
	Truncated  bool                `json:"truncated"`
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(data []byte) (int, error) {
	written, err := w.ResponseWriter.Write(data)
	w.bytes += int64(written)
	return written, err
}

func New(addr, token string, service *files.Service) (*Server, error) {
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("file service token is required")
	}
	if service == nil {
		return nil, fmt.Errorf("file service is required")
	}
	server := &Server{
		token:   token,
		service: service,
	}
	server.handler = http.HandlerFunc(server.handle)
	server.httpServer = &http.Server{
		Addr:              addr,
		Handler:           server.handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return server, nil
}

func (s *Server) Handler() http.Handler {
	return s.handler
}

func (s *Server) Listen() error {
	listener, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return err
	}
	s.listener = listener
	return nil
}

func (s *Server) Serve() error {
	if s.listener == nil {
		return fmt.Errorf("file service is not listening")
	}
	return s.httpServer.Serve(s.listener)
}

func (s *Server) Addr() net.Addr {
	if s.listener != nil {
		return s.listener.Addr()
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) Close() error {
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	writer := &statusWriter{ResponseWriter: w, status: http.StatusOK}
	name := "-"
	defer func() {
		if !files.ValidName(name) {
			name = "-"
		}
		log.Printf("files %s %s %d %d %s", r.Method, name, writer.status, writer.bytes, time.Since(start).Round(time.Millisecond))
	}()

	escaped := r.URL.EscapedPath()
	if escaped != "/files" && !strings.HasPrefix(escaped, "/files/") {
		s.writeError(writer, http.StatusNotFound, "not_found", "route not found")
		return
	}
	if !s.authorized(r) {
		writer.Header().Set("WWW-Authenticate", `Bearer realm="bridge-files"`)
		s.writeError(writer, http.StatusUnauthorized, "unauthorized", "invalid or missing bearer token")
		return
	}
	if escaped == "/files" || escaped == "/files/" {
		s.writeError(writer, http.StatusBadRequest, "bad_request", "missing file path")
		return
	}
	rest := strings.TrimPrefix(escaped, "/files/")
	if rest == "outbox" {
		if r.Method != http.MethodGet {
			s.writeError(writer, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		s.handleListOutbox(writer)
		return
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		s.writeError(writer, http.StatusBadRequest, "bad_request", "invalid file path")
		return
	}
	direction := parts[0]
	if direction != "inbox" && direction != "outbox" {
		s.writeError(writer, http.StatusNotFound, "not_found", "route not found")
		return
	}
	if strings.Contains(parts[1], "\\") {
		s.writeError(writer, http.StatusBadRequest, "bad_request", "invalid file path")
		return
	}
	decoded, err := url.PathUnescape(parts[1])
	if err != nil {
		s.writeError(writer, http.StatusBadRequest, "bad_request", "invalid file name encoding")
		return
	}
	if strings.ContainsAny(decoded, "/\\\x00") {
		s.writeError(writer, http.StatusBadRequest, "bad_request", "invalid file name")
		return
	}
	valid := files.ValidName(decoded)
	if valid {
		name = decoded
	}
	switch direction {
	case "inbox":
		switch r.Method {
		case http.MethodPut:
			if !valid {
				s.writeError(writer, http.StatusBadRequest, "invalid_name", "invalid staging file name")
				return
			}
			s.handleUpload(writer, r, decoded)
		case http.MethodDelete:
			if !valid {
				s.writeError(writer, http.StatusBadRequest, "invalid_name", "invalid staging file name")
				return
			}
			s.handleDelete(writer, direction, decoded)
		default:
			s.writeError(writer, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
	case "outbox":
		switch r.Method {
		case http.MethodGet:
			if !valid {
				s.writeError(writer, http.StatusNotFound, "not_found", "file not found")
				return
			}
			s.handleDownload(writer, decoded)
		case http.MethodDelete:
			if !valid {
				s.writeError(writer, http.StatusBadRequest, "invalid_name", "invalid staging file name")
				return
			}
			s.handleDelete(writer, direction, decoded)
		default:
			s.writeError(writer, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
	}
}

func (s *Server) authorized(r *http.Request) bool {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	provided := header[len(prefix):]
	expectedHash := sha256.Sum256([]byte(s.token))
	providedHash := sha256.Sum256([]byte(provided))
	return subtle.ConstantTimeCompare(expectedHash[:], providedHash[:]) == 1
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request, name string) {
	if r.ContentLength < 0 || (r.ContentLength == 0 && r.Header.Get("Content-Length") == "") {
		s.writeError(w, http.StatusLengthRequired, "length_required", "Content-Length is required")
		return
	}
	if r.ContentLength > s.service.MaxFileBytes() {
		s.writeError(w, http.StatusRequestEntityTooLarge, "file_too_large", "file exceeds maximum size")
		return
	}
	checksum, err := parseChecksum(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_checksum_header", err.Error())
		return
	}
	result, err := s.service.Upload("inbox", name, r.ContentLength, r.Body, checksum)
	if err != nil {
		s.writeServiceError(w, err)
		return
	}
	status := http.StatusCreated
	if result.Replaced {
		status = http.StatusOK
	}
	s.writeJSON(w, status, result)
}

func (s *Server) handleDownload(w http.ResponseWriter, name string) {
	file, info, err := s.service.OpenOutbox(name)
	if err != nil {
		s.writeServiceError(w, err)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.Header().Set("Content-Disposition", contentDisposition(name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, file)
}

func (s *Server) handleListOutbox(w http.ResponseWriter) {
	list, err := s.service.ListStaged("outbox")
	if err != nil {
		s.writeServiceError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, outboxList{
		Direction:  "outbox",
		Files:      list.Files,
		TotalBytes: list.TotalBytes,
		Truncated:  list.Truncated,
	})
}

func (s *Server) handleDelete(w http.ResponseWriter, direction, name string) {
	if _, err := s.service.DeleteStaged(direction, name); err != nil {
		s.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) writeServiceError(w http.ResponseWriter, err error) {
	var fileErr *files.Error
	if !errors.As(err, &fileErr) {
		s.writeError(w, http.StatusInternalServerError, "io_error", err.Error())
		return
	}
	switch fileErr.Code {
	case "not_found":
		s.writeError(w, http.StatusNotFound, fileErr.Code, fileErr.Message)
	case "quota_exceeded":
		s.writeError(w, http.StatusInsufficientStorage, fileErr.Code, fileErr.Message)
	case "file_too_large":
		s.writeError(w, http.StatusRequestEntityTooLarge, fileErr.Code, fileErr.Message)
	case "checksum_mismatch":
		s.writeError(w, http.StatusUnprocessableEntity, fileErr.Code, fileErr.Message)
	case "length_mismatch", "not_a_file", "invalid_name", "invalid_direction", "invalid_checksum_header", "bad_request":
		s.writeError(w, http.StatusBadRequest, fileErr.Code, fileErr.Message)
	default:
		s.writeError(w, http.StatusInternalServerError, "io_error", fileErr.Message)
	}
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, value any) {
	data, err := jsonutil.Marshal(value)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "io_error", "cannot encode response")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

func (s *Server) writeError(w http.ResponseWriter, status int, code, message string) {
	data, err := jsonutil.Marshal(errorResponse{Error: apiError{Code: code, Message: message}})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

func parseChecksum(r *http.Request) (string, error) {
	values := r.Header.Values("X-Checksum-SHA256")
	if len(values) == 0 {
		return "", nil
	}
	if len(values) != 1 {
		return "", fmt.Errorf("checksum header must appear once")
	}
	value := strings.TrimSpace(values[0])
	if len(value) != 64 {
		return "", fmt.Errorf("checksum header must be 64 hex characters")
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') && (char < 'A' || char > 'F') {
			return "", fmt.Errorf("checksum header must be 64 hex characters")
		}
	}
	return strings.ToLower(value), nil
}

func contentDisposition(name string) string {
	var fallback strings.Builder
	for _, char := range name {
		if char < 0x20 || char > 0x7e || char == '"' || char == '\\' {
			fallback.WriteByte('_')
			continue
		}
		fallback.WriteRune(char)
	}
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, fallback.String(), encodeRFC5987(name))
}

func encodeRFC5987(value string) string {
	const attrChars = "!#$&+-.^_`|~"
	var builder strings.Builder
	for _, char := range []byte(value) {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || strings.ContainsRune(attrChars, rune(char)) {
			builder.WriteByte(char)
			continue
		}
		fmt.Fprintf(&builder, "%%%02X", char)
	}
	return builder.String()
}
