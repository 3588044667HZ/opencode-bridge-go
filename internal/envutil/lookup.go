package envutil

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type Lookup struct {
	values  map[string]string
	windows bool
}

func NewLookup(env []string) Lookup {
	windows := runtime.GOOS == "windows"
	values := make(map[string]string, len(env))
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if windows {
			key = strings.ToLower(key)
		}
		values[key] = value
	}
	return Lookup{values: values, windows: windows}
}

func (l Lookup) Get(name string) string {
	if l.windows {
		name = strings.ToLower(name)
	}
	return l.values[name]
}

func (l Lookup) LookPath(file string) string {
	if file == "" {
		return ""
	}
	if l.windows {
		extensions := l.pathExtensions()
		if strings.ContainsAny(file, `\/`) || filepath.IsAbs(file) {
			return checkedPath(file, extensions, true)
		}
		for _, dir := range strings.Split(l.Get("PATH"), ";") {
			dir = strings.Trim(strings.TrimSpace(dir), "\"")
			if dir == "" {
				continue
			}
			if found := checkedPath(filepath.Join(dir, file), extensions, true); found != "" {
				return found
			}
		}
		return ""
	}
	if strings.Contains(file, "/") {
		return checkedPath(file, nil, false)
	}
	for _, dir := range strings.Split(l.Get("PATH"), ":") {
		if dir == "" {
			dir = "."
		}
		if found := checkedPath(filepath.Join(dir, file), nil, false); found != "" {
			return found
		}
	}
	return ""
}

func (l Lookup) pathExtensions() []string {
	raw := l.Get("PATHEXT")
	if raw == "" {
		raw = ".COM;.EXE;.BAT;.CMD"
	}
	parts := strings.Split(raw, ";")
	extensions := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !strings.HasPrefix(part, ".") {
			part = "." + part
		}
		extensions = append(extensions, part)
	}
	return extensions
}

func checkedPath(path string, extensions []string, windows bool) string {
	candidates := []string{path}
	if windows && filepath.Ext(path) == "" {
		for _, extension := range extensions {
			candidates = append(candidates, path+extension)
		}
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		if !windows && info.Mode()&0o111 == 0 {
			continue
		}
		abs, err := filepath.Abs(candidate)
		if err != nil {
			return candidate
		}
		return abs
	}
	return ""
}
