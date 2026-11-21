package files

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	MaxTextResultBytes  = 4 * 1024 * 1024
	MaxTextFileBytes    = 32 * 1024 * 1024
	MaxReadLines        = 5000
	DefaultReadLines    = 2000
	MaxDirectoryEntries = 1000
)

type Workspace struct {
	displayRoot string
	realRoot    string
}

func NewWorkspace(root string) (*Workspace, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, NewError("io_error", "cannot resolve workspace %s: %v", root, err)
	}
	abs = filepath.Clean(abs)
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, NewError("io_error", "cannot create workspace %s: %v", abs, err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, mapOSError(err, "cannot resolve workspace")
	}
	real, err = filepath.Abs(real)
	if err != nil {
		return nil, NewError("io_error", "cannot resolve workspace %s: %v", root, err)
	}
	return &Workspace{
		displayRoot: filepath.Clean(abs),
		realRoot:    filepath.Clean(real),
	}, nil
}

func (w *Workspace) DisplayRoot() string {
	return w.displayRoot
}

func (w *Workspace) ReadFile(opts ReadFileOptions) (*ReadFileResult, error) {
	offset := opts.Offset
	if offset == 0 {
		offset = 1
	}
	limit := opts.Limit
	if limit == 0 {
		limit = DefaultReadLines
	}
	if offset < 1 {
		return nil, NewError("invalid_arguments", "offset must be at least 1")
	}
	if limit < 1 || limit > MaxReadLines {
		return nil, NewError("invalid_arguments", "limit must be between 1 and %d", MaxReadLines)
	}
	target, resolved, err := w.resolveExisting(opts.Path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, mapOSError(err, "cannot stat "+target)
	}
	if !info.Mode().IsRegular() {
		return nil, NewError("not_a_file", "path is not a regular file: %s", target)
	}
	if info.Size() > MaxTextFileBytes {
		return nil, NewError("file_too_large", "file exceeds %d bytes: %s", MaxTextFileBytes, target)
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return nil, mapOSError(err, "cannot read "+target)
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return nil, NewError("binary_file", "file is not valid UTF-8 text: %s", target)
	}
	if bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}) {
		data = data[3:]
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := splitLines(text)
	total := len(lines)
	result := &ReadFileResult{
		Path:         opts.Path,
		AbsolutePath: target,
		Offset:       offset,
		Limit:        limit,
		TotalLines:   total,
	}
	start := offset - 1
	if start >= total {
		return result, nil
	}
	end := start + limit
	if end > total {
		end = total
	}
	selected := lines[start:end]
	content, returned, truncatedByBytes, truncatedLine := buildContent(selected)
	result.Content = content
	result.ReturnedLines = returned
	result.TruncatedByBytes = truncatedByBytes
	result.TruncatedLine = truncatedLine
	result.Truncated = truncatedByBytes || truncatedLine || end < total
	return result, nil
}

func (w *Workspace) WriteFile(opts WriteFileOptions) (*WriteFileResult, error) {
	if strings.ContainsRune(opts.Content, 0) {
		return nil, NewError("invalid_text", "content must not contain NUL")
	}
	if !utf8.ValidString(opts.Content) {
		return nil, NewError("invalid_text", "content must be valid UTF-8")
	}
	if len(opts.Content) > MaxTextResultBytes {
		return nil, NewError("content_too_large", "content exceeds %d bytes", MaxTextResultBytes)
	}
	target, err := w.resolveInput(opts.Path)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(target)
	existed := err == nil
	if err == nil && info.IsDir() {
		resolved, resolveErr := filepath.EvalSymlinks(target)
		if resolveErr != nil {
			return nil, mapOSError(resolveErr, "cannot resolve "+target)
		}
		if !w.within(resolved) {
			return nil, NewError("path_escape", "path escapes workspace: %s", opts.Path)
		}
		return nil, NewError("not_a_file", "path is a directory: %s", target)
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, mapOSError(err, "cannot stat "+target)
	}
	parent := filepath.Dir(target)
	if opts.CreateDirs {
		if err := w.ensureParentDir(parent); err != nil {
			return nil, err
		}
	} else {
		if err := w.checkParentDir(parent); err != nil {
			return nil, err
		}
	}
	replaced := false
	if existed {
		if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			return nil, NewError("not_a_file", "path is not a regular file: %s", target)
		}
		replaced = true
	}
	temp, err := os.CreateTemp(parent, "."+filepath.Base(target)+".*.tmp")
	if err != nil {
		return nil, mapOSError(err, "cannot create temporary file in "+parent)
	}
	tempName := temp.Name()
	cleanup := func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}
	if _, err := temp.WriteString(opts.Content); err != nil {
		cleanup()
		return nil, mapOSError(err, "cannot write temporary file")
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempName)
		return nil, mapOSError(err, "cannot close temporary file")
	}
	if err := os.Rename(tempName, target); err != nil {
		if existing, statErr := os.Lstat(target); statErr == nil && !existing.IsDir() {
			if removeErr := os.Remove(target); removeErr == nil {
				err = os.Rename(tempName, target)
			}
		}
	}
	if err != nil {
		_ = os.Remove(tempName)
		return nil, mapOSError(err, "cannot replace "+target)
	}
	return &WriteFileResult{
		Path:         opts.Path,
		AbsolutePath: target,
		Bytes:        len(opts.Content),
		Created:      !replaced,
		Replaced:     replaced,
	}, nil
}

func (w *Workspace) ListDirectory(opts ListDirectoryOptions) (*ListDirectoryResult, error) {
	input := opts.Path
	if input == "" {
		input = "."
	}
	target, resolved, err := w.resolveExisting(input)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, mapOSError(err, "cannot stat "+target)
	}
	if !info.IsDir() {
		return nil, NewError("not_a_directory", "path is not a directory: %s", target)
	}
	rawEntries, err := os.ReadDir(resolved)
	if err != nil {
		return nil, mapOSError(err, "cannot list "+target)
	}
	entries := make([]DirectoryEntry, 0, len(rawEntries))
	for _, raw := range rawEntries {
		entryInfo, err := raw.Info()
		if err != nil {
			return nil, mapOSError(err, "cannot stat directory entry")
		}
		entry := DirectoryEntry{
			Name: entryInfo.Name(),
			Type: typeName(entryInfo.Mode()),
		}
		if entry.Type != "directory" {
			size := entryInfo.Size()
			modified := formatTime(entryInfo.ModTime())
			entry.Size = &size
			entry.ModifiedAt = &modified
		}
		entries = append(entries, entry)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		leftDir := entries[i].Type == "directory"
		rightDir := entries[j].Type == "directory"
		if leftDir != rightDir {
			return leftDir
		}
		left := strings.ToLower(entries[i].Name)
		right := strings.ToLower(entries[j].Name)
		if left != right {
			return left < right
		}
		return entries[i].Name < entries[j].Name
	})
	total := len(entries)
	truncated := total > MaxDirectoryEntries
	if truncated {
		entries = entries[:MaxDirectoryEntries]
	}
	return &ListDirectoryResult{
		Path:         input,
		AbsolutePath: target,
		Entries:      entries,
		Total:        total,
		Truncated:    truncated,
	}, nil
}

func (w *Workspace) StatFile(path string) (*StatFileResult, error) {
	target, err := w.resolveInput(path)
	if err != nil {
		return nil, err
	}
	lstat, err := os.Lstat(target)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, NewError("not_found", "path not found: %s", target)
		}
		return nil, mapOSError(err, "cannot stat "+target)
	}
	real, err := filepath.EvalSymlinks(target)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, NewError("not_found", "path not found: %s", target)
		}
		return nil, mapOSError(err, "cannot resolve "+target)
	}
	real, err = filepath.Abs(real)
	if err != nil {
		return nil, NewError("io_error", "cannot resolve %s: %v", target, err)
	}
	real = filepath.Clean(real)
	if !w.within(real) {
		return nil, NewError("path_escape", "path escapes workspace: %s", path)
	}
	info, err := os.Stat(real)
	if err != nil {
		return nil, mapOSError(err, "cannot stat "+target)
	}
	return &StatFileResult{
		Path:         path,
		AbsolutePath: target,
		RealPath:     real,
		Type:         typeName(info.Mode()),
		Size:         info.Size(),
		IsSymlink:    lstat.Mode()&os.ModeSymlink != 0,
		CreatedAt:    formatTime(createdAt(info)),
		ModifiedAt:   formatTime(info.ModTime()),
	}, nil
}

func (w *Workspace) resolveInput(path string) (string, error) {
	if path == "" {
		return "", NewError("invalid_arguments", "path must not be empty")
	}
	if strings.ContainsRune(path, 0) {
		return "", NewError("invalid_path", "path must not contain NUL")
	}
	if strings.Contains(path, "://") {
		return "", NewError("invalid_path", "URLs are not accepted")
	}
	if hasParentSegment(path) {
		return "", NewError("path_escape", "path must not contain .. segments: %s", path)
	}
	if runtime.GOOS == "windows" {
		if filepath.VolumeName(path) != "" && !filepath.IsAbs(path) {
			return "", NewError("path_escape", "drive-relative paths are not allowed: %s", path)
		}
	}
	var target string
	if filepath.IsAbs(path) {
		target = filepath.Clean(path)
	} else {
		target = filepath.Join(w.displayRoot, path)
	}
	target, err := filepath.Abs(target)
	if err != nil {
		return "", NewError("invalid_path", "cannot resolve path %s: %v", path, err)
	}
	target = filepath.Clean(target)
	if filepath.IsAbs(path) && !w.withinDisplay(target) && !w.within(target) {
		return "", NewError("path_escape", "path escapes workspace: %s", path)
	}
	return target, nil
}

func (w *Workspace) resolveExisting(path string) (string, string, error) {
	target, err := w.resolveInput(path)
	if err != nil {
		return "", "", err
	}
	if _, err := os.Lstat(target); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", "", NewError("not_found", "path not found: %s", target)
		}
		return "", "", mapOSError(err, "cannot stat "+target)
	}
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", "", NewError("not_found", "path not found: %s", target)
		}
		return "", "", mapOSError(err, "cannot resolve "+target)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", "", NewError("io_error", "cannot resolve %s: %v", target, err)
	}
	resolved = filepath.Clean(resolved)
	if !w.within(resolved) {
		return "", "", NewError("path_escape", "path escapes workspace: %s", path)
	}
	return target, resolved, nil
}

func (w *Workspace) checkParentDir(parent string) error {
	info, err := os.Stat(parent)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			escapes, ancestorErr := w.ancestorEscapes(parent)
			if ancestorErr != nil {
				return ancestorErr
			}
			if escapes {
				return NewError("path_escape", "parent directory escapes workspace: %s", parent)
			}
			return NewError("parent_not_found", "parent directory not found: %s", parent)
		}
		return mapOSError(err, "cannot stat parent directory "+parent)
	}
	if !info.IsDir() {
		return NewError("not_a_directory", "parent path is not a directory: %s", parent)
	}
	resolved, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return mapOSError(err, "cannot resolve parent directory "+parent)
	}
	if !w.within(resolved) {
		return NewError("path_escape", "parent directory escapes workspace: %s", parent)
	}
	return nil
}

func (w *Workspace) ancestorEscapes(path string) (bool, error) {
	ancestor := path
	for {
		info, err := os.Lstat(ancestor)
		if err == nil {
			if !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
				return false, NewError("not_a_directory", "parent path is not a directory: %s", ancestor)
			}
			resolved, err := filepath.EvalSymlinks(ancestor)
			if err != nil {
				return false, mapOSError(err, "cannot resolve parent directory "+ancestor)
			}
			return !w.within(resolved), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return false, mapOSError(err, "cannot stat parent directory "+ancestor)
		}
		next := filepath.Dir(ancestor)
		if next == ancestor {
			return false, nil
		}
		ancestor = next
	}
}

func (w *Workspace) ensureParentDir(parent string) error {
	ancestor := parent
	for {
		info, err := os.Lstat(ancestor)
		if err == nil {
			if !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
				return NewError("not_a_directory", "parent path is not a directory: %s", ancestor)
			}
			break
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return mapOSError(err, "cannot stat parent directory "+ancestor)
		}
		next := filepath.Dir(ancestor)
		if next == ancestor {
			return NewError("parent_not_found", "parent directory not found: %s", parent)
		}
		ancestor = next
	}
	resolvedAncestor, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return mapOSError(err, "cannot resolve parent directory "+ancestor)
	}
	ancestorInfo, err := os.Stat(resolvedAncestor)
	if err != nil {
		return mapOSError(err, "cannot stat parent directory "+ancestor)
	}
	if !ancestorInfo.IsDir() {
		return NewError("not_a_directory", "parent path is not a directory: %s", ancestor)
	}
	if !w.within(resolvedAncestor) {
		return NewError("path_escape", "parent directory escapes workspace: %s", ancestor)
	}
	rel, err := filepath.Rel(ancestor, parent)
	if err != nil {
		return NewError("path_escape", "cannot resolve parent directory: %s", parent)
	}
	current := ancestor
	if rel != "." {
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			current = filepath.Join(current, part)
			if err := os.Mkdir(current, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
				return mapOSError(err, "cannot create parent directory "+current)
			}
			info, err := os.Lstat(current)
			if err != nil {
				return mapOSError(err, "cannot stat parent directory "+current)
			}
			if !info.IsDir() {
				return NewError("not_a_directory", "parent path is not a directory: %s", current)
			}
		}
	}
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return mapOSError(err, "cannot resolve parent directory "+parent)
	}
	if !w.within(resolvedParent) {
		return NewError("path_escape", "parent directory escapes workspace: %s", parent)
	}
	return nil
}

func (w *Workspace) within(path string) bool {
	return isWithin(w.realRoot, path)
}

func (w *Workspace) withinDisplay(path string) bool {
	return isWithin(w.displayRoot, path)
}

func isWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func hasParentSegment(path string) bool {
	for _, part := range strings.FieldsFunc(path, func(r rune) bool {
		if r == '/' {
			return true
		}
		return runtime.GOOS == "windows" && r == '\\'
	}) {
		if part == ".." {
			return true
		}
	}
	return false
}

func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func buildContent(lines []string) (string, int, bool, bool) {
	if len(lines) == 0 {
		return "", 0, false, false
	}
	first := lines[0]
	if len(first) > MaxTextResultBytes {
		return truncateUTF8(first, MaxTextResultBytes), 1, false, true
	}
	var builder strings.Builder
	builder.Grow(len(first))
	builder.WriteString(first)
	returned := 1
	for _, line := range lines[1:] {
		if builder.Len()+1+len(line) > MaxTextResultBytes {
			return builder.String(), returned, true, false
		}
		builder.WriteByte('\n')
		builder.WriteString(line)
		returned++
	}
	return builder.String(), returned, false, false
}

func truncateUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	bytes := []byte(value)[:limit]
	for len(bytes) > 0 && !utf8.Valid(bytes) {
		bytes = bytes[:len(bytes)-1]
	}
	return string(bytes)
}

func typeName(mode os.FileMode) string {
	switch {
	case mode&os.ModeSymlink != 0:
		return "symlink"
	case mode.IsDir():
		return "directory"
	case mode.IsRegular():
		return "file"
	default:
		return "other"
	}
}
