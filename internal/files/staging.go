package files

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	MaxStagingFiles = 10000
	stagingTTL      = 7 * 24 * time.Hour
	partTTL         = time.Hour
)

func (s *Service) ListStaged(direction string) (*StagingList, error) {
	dir, err := s.directionDir(direction)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, mapOSError(err, "cannot read staging directory")
	}
	files := make([]StagingFile, 0, len(entries))
	var totalBytes int64
	ignoredInvalid := 0
	for _, entry := range entries {
		name := entry.Name()
		if isPartName(name) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, mapOSError(err, "cannot stat staging entry")
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if !ValidName(name) {
			ignoredInvalid++
			continue
		}
		files = append(files, StagingFile{
			Name:       name,
			Size:       info.Size(),
			ModifiedAt: formatTime(info.ModTime()),
		})
		totalBytes += info.Size()
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name < files[j].Name
	})
	truncated := len(files) > MaxStagingFiles
	if truncated {
		files = files[:MaxStagingFiles]
	}
	return &StagingList{
		Direction:      direction,
		Path:           dir,
		Files:          files,
		TotalBytes:     totalBytes,
		IgnoredInvalid: ignoredInvalid,
		Truncated:      truncated,
	}, nil
}

func (s *Service) DeleteStaged(direction, name string) (*DeleteStagedFileResult, error) {
	dir, err := s.directionDir(direction)
	if err != nil {
		return nil, err
	}
	if !ValidName(name) {
		return nil, NewError("invalid_name", "invalid staging file name: %q", name)
	}
	target := filepath.Join(dir, name)
	info, err := os.Lstat(target)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, NewError("not_found", "staged file not found: %s", name)
		}
		return nil, mapOSError(err, "cannot stat staged file "+name)
	}
	if info.IsDir() {
		return nil, NewError("not_a_file", "staged path is a directory: %s", name)
	}
	if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
		return nil, NewError("not_a_file", "staged path is not a file: %s", name)
	}
	if err := os.Remove(target); err != nil {
		return nil, mapOSError(err, "cannot delete staged file "+name)
	}
	return &DeleteStagedFileResult{Deleted: true, Direction: direction, Name: name}, nil
}

func (s *Service) Upload(direction, name string, size int64, body io.Reader, expectedSHA string) (*UploadResult, error) {
	dir, err := s.directionDir(direction)
	if err != nil {
		return nil, err
	}
	if direction != "inbox" {
		return nil, NewError("invalid_direction", "upload direction must be inbox")
	}
	if !ValidName(name) {
		return nil, NewError("invalid_name", "invalid staging file name: %q", name)
	}
	if size < 0 {
		return nil, NewError("length_mismatch", "content length is invalid")
	}
	if size > s.maxFileSize {
		return nil, NewError("file_too_large", "file exceeds %d bytes", s.maxFileSize)
	}
	target := filepath.Join(dir, name)
	replaced := false
	existing := int64(0)
	info, err := os.Lstat(target)
	if err == nil {
		if info.IsDir() {
			return nil, NewError("not_a_file", "upload target is a directory: %s", name)
		}
		switch {
		case info.Mode().IsRegular():
			existing = info.Size()
			replaced = true
		case info.Mode()&os.ModeSymlink != 0:
			replaced = true
		default:
			return nil, NewError("not_a_file", "upload target is not a file: %s", name)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, mapOSError(err, "cannot stat upload target "+name)
	}
	if err := s.reserve(size, existing); err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			s.release(size)
		}
	}()
	temp, err := os.CreateTemp(dir, "."+name+".*.part")
	if err != nil {
		return nil, mapOSError(err, "cannot create staging temporary file")
	}
	tempName := temp.Name()
	cleanup := func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temp, hasher), io.LimitReader(body, size))
	if copyErr != nil {
		cleanup()
		if errors.Is(copyErr, io.ErrUnexpectedEOF) {
			return nil, NewError("length_mismatch", "upload length does not match content length")
		}
		return nil, NewError("io_error", "cannot read upload body: %v", copyErr)
	}
	if written != size {
		cleanup()
		return nil, NewError("length_mismatch", "upload length does not match content length")
	}
	sum := hex.EncodeToString(hasher.Sum(nil))
	if expectedSHA != "" && !strings.EqualFold(sum, expectedSHA) {
		cleanup()
		return nil, NewError("checksum_mismatch", "uploaded checksum does not match")
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempName)
		return nil, mapOSError(err, "cannot close staging temporary file")
	}
	committed, err = s.commitUpload(tempName, target, size)
	if err != nil {
		_ = os.Remove(tempName)
		return nil, mapOSError(err, "cannot store uploaded file")
	}
	return &UploadResult{
		Direction: direction,
		Name:      name,
		Size:      size,
		SHA256:    sum,
		Replaced:  replaced,
	}, nil
}

func (s *Service) commitUpload(tempName, target string, size int64) (bool, error) {
	s.quotaMu.Lock()
	defer s.quotaMu.Unlock()
	err := os.Rename(tempName, target)
	if err != nil {
		if existingInfo, statErr := os.Lstat(target); statErr == nil && !existingInfo.IsDir() {
			if removeErr := os.Remove(target); removeErr == nil {
				err = os.Rename(tempName, target)
			}
		}
	}
	if err != nil {
		return false, err
	}
	s.reserved -= size
	if s.reserved < 0 {
		s.reserved = 0
	}
	return true, nil
}

func (s *Service) OpenOutbox(name string) (*os.File, os.FileInfo, error) {
	if !ValidName(name) {
		return nil, nil, NewError("not_found", "staged file not found: %s", name)
	}
	target := filepath.Join(s.outbox, name)
	info, err := os.Lstat(target)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, NewError("not_found", "staged file not found: %s", name)
		}
		return nil, nil, mapOSError(err, "cannot stat staged file "+name)
	}
	if !info.Mode().IsRegular() {
		return nil, nil, NewError("not_found", "staged file not found: %s", name)
	}
	file, err := os.Open(target)
	if err != nil {
		return nil, nil, mapOSError(err, "cannot open staged file "+name)
	}
	return file, info, nil
}

func (s *Service) Cleanup(now time.Time) (CleanupStats, error) {
	var stats CleanupStats
	for _, dir := range []string{s.inbox, s.outbox} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return stats, mapOSError(err, "cannot read staging directory")
		}
		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			if !info.Mode().IsRegular() {
				continue
			}
			age := now.Sub(info.ModTime())
			name := entry.Name()
			target := filepath.Join(dir, name)
			if isPartName(name) {
				if age > partTTL && os.Remove(target) == nil {
					stats.PartFiles++
					stats.PartBytes += info.Size()
				}
				continue
			}
			if age > stagingTTL && os.Remove(target) == nil {
				stats.Files++
				stats.Bytes += info.Size()
			}
		}
	}
	return stats, nil
}
