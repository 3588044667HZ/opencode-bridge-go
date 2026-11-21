package fileserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-bridge/internal/files"
)

const testFileToken = "file-token"

func newTestServer(t *testing.T, maxBytes, quota int64) (*Server, *files.Service) {
	t.Helper()
	root := t.TempDir()
	service, err := files.New(files.Config{
		WorkspaceRoot:     filepath.Join(root, "workspace"),
		StagingInbox:      filepath.Join(root, "staging", "inbox"),
		StagingOutbox:     filepath.Join(root, "staging", "outbox"),
		MaxFileBytes:      maxBytes,
		StagingQuotaBytes: quota,
	})
	if err != nil {
		t.Fatal(err)
	}
	server, err := New("127.0.0.1:0", testFileToken, service)
	if err != nil {
		t.Fatal(err)
	}
	return server, service
}

func doRequest(server *Server, method, target string, body io.Reader, token string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, body)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	return recorder
}

func decodeError(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("cannot decode error body: %v\n%s", err, recorder.Body.String())
	}
	return body.Error.Code
}

func uploadContent(t *testing.T, server *Server, name, content string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	return doRequest(server, http.MethodPut, "/files/inbox/"+name, strings.NewReader(content), testFileToken, headers)
}

func moveToOutbox(t *testing.T, service *files.Service, name string) {
	t.Helper()
	inbox, err := service.StagingPath("inbox")
	if err != nil {
		t.Fatal(err)
	}
	outbox, err := service.StagingPath("outbox")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(inbox, name), filepath.Join(outbox, name)); err != nil {
		t.Fatal(err)
	}
}

func TestAuthentication(t *testing.T) {
	server, _ := newTestServer(t, 1<<20, 4<<20)
	response := doRequest(server, http.MethodPut, "/files/inbox/a.txt", strings.NewReader("x"), "", nil)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", response.Code)
	}
	if response.Header().Get("WWW-Authenticate") != `Bearer realm="bridge-files"` {
		t.Fatalf("unexpected challenge: %q", response.Header().Get("WWW-Authenticate"))
	}
	if code := decodeError(t, response); code != "unauthorized" {
		t.Fatalf("unexpected error code: %s", code)
	}
	response = doRequest(server, http.MethodPut, "/files/inbox/a.txt", strings.NewReader("x"), "mcp-token", nil)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected mcp token to be rejected, got %d", response.Code)
	}
	response = uploadContent(t, server, "a.txt", "x", nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", response.Code, response.Body.String())
	}
}

func TestUploadDownloadListDelete(t *testing.T) {
	server, service := newTestServer(t, 1<<20, 4<<20)
	content := "hello"
	sum := sha256.Sum256([]byte(content))
	response := uploadContent(t, server, "a.txt", content, map[string]string{
		"X-Checksum-SHA256": strings.ToUpper(hex.EncodeToString(sum[:])),
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", response.Code, response.Body.String())
	}
	var uploaded files.UploadResult
	if err := json.Unmarshal(response.Body.Bytes(), &uploaded); err != nil {
		t.Fatal(err)
	}
	if uploaded.Name != "a.txt" || uploaded.Size != int64(len(content)) || uploaded.Replaced || uploaded.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("unexpected upload response: %#v", uploaded)
	}
	response = doRequest(server, http.MethodPut, "/files/inbox/a.txt", strings.NewReader(content), testFileToken, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200 on replace, got %d", response.Code)
	}
	moveToOutbox(t, service, "a.txt")
	response = doRequest(server, http.MethodGet, "/files/outbox/a.txt", nil, testFileToken, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	if response.Body.String() != content {
		t.Fatalf("unexpected body: %q", response.Body.String())
	}
	if response.Header().Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("unexpected content type: %q", response.Header().Get("Content-Type"))
	}
	if response.Header().Get("Content-Length") != "5" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("unexpected headers: %#v", response.Header())
	}
	if !strings.Contains(response.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("unexpected disposition: %q", response.Header().Get("Content-Disposition"))
	}
	response = doRequest(server, http.MethodGet, "/files/outbox", nil, testFileToken, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	var list struct {
		Direction  string              `json:"direction"`
		Files      []files.StagingFile `json:"files"`
		TotalBytes int64               `json:"totalBytes"`
		Truncated  bool                `json:"truncated"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Direction != "outbox" || len(list.Files) != 1 || list.Files[0].Name != "a.txt" || list.TotalBytes != 5 || list.Truncated {
		t.Fatalf("unexpected list: %#v", list)
	}
	response = doRequest(server, http.MethodDelete, "/files/outbox/a.txt", nil, testFileToken, nil)
	if response.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", response.Code)
	}
	response = doRequest(server, http.MethodDelete, "/files/outbox/a.txt", nil, testFileToken, nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", response.Code)
	}
	if response = uploadContent(t, server, "b.txt", "x", nil); response.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", response.Code)
	}
	response = doRequest(server, http.MethodDelete, "/files/inbox/b.txt", nil, testFileToken, nil)
	if response.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", response.Code)
	}
}

func TestUploadErrors(t *testing.T) {
	server, service := newTestServer(t, 4, 6)
	response := doRequest(server, http.MethodPut, "/files/inbox/a.txt", nil, testFileToken, nil)
	if response.Code != http.StatusLengthRequired {
		t.Fatalf("expected 411, got %d", response.Code)
	}
	response = uploadContent(t, server, "big.txt", "12345", nil)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", response.Code)
	}
	response = uploadContent(t, server, "bad.txt", "x", map[string]string{"X-Checksum-SHA256": "not-a-checksum"})
	if response.Code != http.StatusBadRequest || decodeError(t, response) != "invalid_checksum_header" {
		t.Fatalf("expected invalid_checksum_header, got %d: %s", response.Code, response.Body.String())
	}
	response = uploadContent(t, server, "bad.txt", "x", map[string]string{"X-Checksum-SHA256": strings.Repeat("0", 64)})
	if response.Code != http.StatusUnprocessableEntity || decodeError(t, response) != "checksum_mismatch" {
		t.Fatalf("expected checksum_mismatch, got %d: %s", response.Code, response.Body.String())
	}
	inbox, err := service.StagingPath("inbox")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(inbox)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("upload failure left files: %#v", entries)
	}
	request := httptest.NewRequest(http.MethodPut, "/files/inbox/short.txt", strings.NewReader("abc"))
	request.Header.Set("Authorization", "Bearer "+testFileToken)
	request.ContentLength = 4
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || decodeError(t, recorder) != "length_mismatch" {
		t.Fatalf("expected length_mismatch, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if response = uploadContent(t, server, "a.txt", "1234", nil); response.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", response.Code)
	}
	response = uploadContent(t, server, "b.txt", "1234", nil)
	if response.Code != http.StatusInsufficientStorage || decodeError(t, response) != "quota_exceeded" {
		t.Fatalf("expected quota_exceeded, got %d: %s", response.Code, response.Body.String())
	}
}

func TestInvalidNamesAndMethods(t *testing.T) {
	server, _ := newTestServer(t, 1<<20, 4<<20)
	for _, target := range []string{
		"/files/inbox/..%2Fevil",
		"/files/inbox/a%2Fb",
		"/files/inbox/a%5Cb",
		"/files/inbox/CON",
		"/files/inbox/a%20b",
		"/files/inbox/x%00y",
		"/files/inbox/a/b",
	} {
		response := doRequest(server, http.MethodPut, target, strings.NewReader("x"), testFileToken, nil)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d", target, response.Code)
		}
	}
	if response := doRequest(server, http.MethodGet, "/files/inbox/a.txt", nil, testFileToken, nil); response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", response.Code)
	}
	if response := doRequest(server, http.MethodPost, "/files/outbox", nil, testFileToken, nil); response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", response.Code)
	}
	if response := doRequest(server, http.MethodHead, "/files/outbox/a.txt", nil, testFileToken, nil); response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", response.Code)
	}
	if response := doRequest(server, http.MethodGet, "/unknown", nil, testFileToken, nil); response.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", response.Code)
	}
}

func TestDownloadHeadersAndFilters(t *testing.T) {
	server, service := newTestServer(t, 1<<20, 4<<20)
	response := uploadContent(t, server, "测试.txt", "x", nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", response.Code)
	}
	moveToOutbox(t, service, "测试.txt")
	response = doRequest(server, http.MethodGet, "/files/outbox/%E6%B5%8B%E8%AF%95.txt", nil, testFileToken, map[string]string{"Range": "bytes=0-0"})
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	if response.Body.String() != "x" {
		t.Fatalf("range should be ignored: %q", response.Body.String())
	}
	disposition := response.Header().Get("Content-Disposition")
	if !strings.Contains(disposition, "filename*=UTF-8''%E6%B5%8B%E8%AF%95.txt") {
		t.Fatalf("unexpected disposition: %q", disposition)
	}
	outbox, err := service.StagingPath("outbox")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outbox, "bad name.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outbox, "x.part"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(outbox, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outbox, "测试.txt"), filepath.Join(outbox, "link.txt")); err == nil {
		if response := doRequest(server, http.MethodGet, "/files/outbox/link.txt", nil, testFileToken, nil); response.Code != http.StatusNotFound {
			t.Fatalf("symlink download: expected 404, got %d", response.Code)
		}
	}
	response = doRequest(server, http.MethodGet, "/files/outbox", nil, testFileToken, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	var list struct {
		Files []files.StagingFile `json:"files"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Files) != 1 || list.Files[0].Name != "测试.txt" {
		t.Fatalf("unexpected outbox list: %#v", list)
	}
}

func TestNewRequiresToken(t *testing.T) {
	_, service := newTestServer(t, 1<<20, 4<<20)
	if _, err := New("127.0.0.1:0", "", service); err == nil {
		t.Fatal("expected empty token to fail")
	}
}
