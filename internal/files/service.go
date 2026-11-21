package files

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Config struct {
	WorkspaceRoot     string
	StagingInbox      string
	StagingOutbox     string
	MaxFileBytes      int64
	StagingQuotaBytes int64
}

type Service struct {
	workspace   *Workspace
	inbox       string
	outbox      string
	maxFileSize int64
	quotaBytes  int64
	quotaMu     sync.Mutex
	reserved    int64
}

type ReadFileOptions struct {
	Path   string
	Offset int
	Limit  int
}

type ReadFileResult struct {
	Path             string `json:"path"`
	AbsolutePath     string `json:"absolutePath"`
	Offset           int    `json:"offset"`
	Limit            int    `json:"limit"`
	TotalLines       int    `json:"totalLines"`
	ReturnedLines    int    `json:"returnedLines"`
	Truncated        bool   `json:"truncated"`
	TruncatedByBytes bool   `json:"truncatedByBytes"`
	TruncatedLine    bool   `json:"truncatedLine"`
	Content          string `json:"content"`
}

type WriteFileOptions struct {
	Path       string
	Content    string
	CreateDirs bool
}

type WriteFileResult struct {
	Path         string `json:"path"`
	AbsolutePath string `json:"absolutePath"`
	Bytes        int    `json:"bytes"`
	Created      bool   `json:"created"`
	Replaced     bool   `json:"replaced"`
}

type ListDirectoryOptions struct {
	Path string
}

type DirectoryEntry struct {
	Name       string  `json:"name"`
	Type       string  `json:"type"`
	Size       *int64  `json:"size"`
	ModifiedAt *string `json:"modifiedAt"`
}

type ListDirectoryResult struct {
	Path         string           `json:"path"`
	AbsolutePath string           `json:"absolutePath"`
	Entries      []DirectoryEntry `json:"entries"`
	Total        int              `json:"total"`
	Truncated    bool             `json:"truncated"`
}

type StatFileResult struct {
	Path         string `json:"path"`
	AbsolutePath string `json:"absolutePath"`
	RealPath     string `json:"realPath"`
	Type         string `json:"type"`
	Size         int64  `json:"size"`
	IsSymlink    bool   `json:"isSymlink"`
	CreatedAt    string `json:"createdAt"`
	ModifiedAt   string `json:"modifiedAt"`
}

type StagingFile struct {
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modifiedAt"`
}

type StagingList struct {
	Direction      string        `json:"direction"`
	Path           string        `json:"path"`
	Files          []StagingFile `json:"files"`
	TotalBytes     int64         `json:"totalBytes"`
	IgnoredInvalid int           `json:"ignoredInvalid"`
	Truncated      bool          `json:"truncated"`
}

type DeleteStagedFileResult struct {
	Deleted   bool   `json:"deleted"`
	Direction string `json:"direction"`
	Name      string `json:"name"`
}

type UploadResult struct {
	Direction string `json:"direction"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	Replaced  bool   `json:"replaced"`
}

type CleanupStats struct {
	Files     int
	Bytes     int64
	PartFiles int
	PartBytes int64
}

func New(cfg Config) (*Service, error) {
	if cfg.MaxFileBytes <= 0 {
		return nil, NewError("invalid_config", "max file bytes must be positive")
	}
	if cfg.StagingQuotaBytes < cfg.MaxFileBytes {
		return nil, NewError("invalid_config", "staging quota must be at least max file bytes")
	}
	workspace, err := NewWorkspace(cfg.WorkspaceRoot)
	if err != nil {
		return nil, err
	}
	inbox, err := ensureDirectory(cfg.StagingInbox)
	if err != nil {
		return nil, err
	}
	outbox, err := ensureDirectory(cfg.StagingOutbox)
	if err != nil {
		return nil, err
	}
	return &Service{
		workspace:   workspace,
		inbox:       inbox,
		outbox:      outbox,
		maxFileSize: cfg.MaxFileBytes,
		quotaBytes:  cfg.StagingQuotaBytes,
	}, nil
}

func (s *Service) WorkspaceRoot() string {
	return s.workspace.DisplayRoot()
}

func (s *Service) ReadFile(opts ReadFileOptions) (*ReadFileResult, error) {
	return s.workspace.ReadFile(opts)
}

func (s *Service) WriteFile(opts WriteFileOptions) (*WriteFileResult, error) {
	return s.workspace.WriteFile(opts)
}

func (s *Service) ListDirectory(opts ListDirectoryOptions) (*ListDirectoryResult, error) {
	return s.workspace.ListDirectory(opts)
}

func (s *Service) StatFile(path string) (*StatFileResult, error) {
	return s.workspace.StatFile(path)
}

func (s *Service) MaxFileBytes() int64 {
	return s.maxFileSize
}

func (s *Service) StagingQuotaBytes() int64 {
	return s.quotaBytes
}

func (s *Service) StagingPath(direction string) (string, error) {
	dir, err := s.directionDir(direction)
	if err != nil {
		return "", err
	}
	return dir, nil
}

func (s *Service) directionDir(direction string) (string, error) {
	switch direction {
	case "inbox":
		return s.inbox, nil
	case "outbox":
		return s.outbox, nil
	default:
		return "", NewError("invalid_direction", "direction must be inbox or outbox: %q", direction)
	}
}

func (s *Service) stagingBytes() (int64, error) {
	var total int64
	for _, dir := range []string{s.inbox, s.outbox} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return 0, mapOSError(err, "cannot read staging directory")
		}
		for _, entry := range entries {
			if isPartName(entry.Name()) {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return 0, mapOSError(err, "cannot stat staging entry")
			}
			if info.Mode().IsRegular() {
				total += info.Size()
			}
		}
	}
	return total, nil
}

func (s *Service) reserve(size int64, existing int64) error {
	s.quotaMu.Lock()
	defer s.quotaMu.Unlock()
	total, err := s.stagingBytes()
	if err != nil {
		return err
	}
	available := s.quotaBytes - total - s.reserved + existing
	if size > available {
		return NewError("quota_exceeded", "staging quota exceeded")
	}
	s.reserved += size
	return nil
}

func (s *Service) release(size int64) {
	s.quotaMu.Lock()
	s.reserved -= size
	if s.reserved < 0 {
		s.reserved = 0
	}
	s.quotaMu.Unlock()
}

func ensureDirectory(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", NewError("io_error", "cannot resolve directory %s: %v", path, err)
	}
	abs = filepath.Clean(abs)
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", NewError("io_error", "cannot create directory %s: %v", abs, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", mapOSError(err, "cannot stat directory "+abs)
	}
	if !info.IsDir() {
		return "", NewError("io_error", "not a directory: %s", abs)
	}
	return abs, nil
}

func fileExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, mapOSError(err, "cannot stat "+path)
}

func formatTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000Z")
}
