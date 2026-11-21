package clipboard

import (
	"os"
	"runtime"
	"strings"

	"go-bridge/internal/envutil"
)

func detect(env []string) (*Backend, string) {
	env = serviceEnv(env)
	lookup := envutil.NewLookup(env)
	switch runtime.GOOS {
	case "windows":
		if path := lookup.LookPath("powershell.exe"); path != "" {
			return powershellBackend("powershell", path), ""
		}
		if path := lookup.LookPath("pwsh.exe"); path != "" {
			return powershellBackend("pwsh", path), ""
		}
		return nil, "powershell and pwsh are not available"
	case "darwin":
		paste := lookup.LookPath("pbpaste")
		copyPath := lookup.LookPath("pbcopy")
		if paste == "" || copyPath == "" {
			return nil, "pbpaste and pbcopy are required"
		}
		return &Backend{Name: "pbpaste", ReadPath: paste, WritePath: copyPath}, ""
	case "linux":
		return detectLinux(lookup)
	default:
		return nil, "clipboard is not supported on " + runtime.GOOS
	}
}

func detectLinux(lookup envutil.Lookup) (*Backend, string) {
	wayland := strings.TrimSpace(lookup.Get("WAYLAND_DISPLAY")) != ""
	x11 := strings.TrimSpace(lookup.Get("DISPLAY")) != ""
	if wayland {
		paste := lookup.LookPath("wl-paste")
		copyPath := lookup.LookPath("wl-copy")
		if paste != "" && copyPath != "" {
			return &Backend{
				Name:      "wl-clipboard",
				ReadPath:  paste,
				ReadArgs:  []string{"--no-newline"},
				WritePath: copyPath,
			}, ""
		}
	}
	if x11 {
		if path := lookup.LookPath("xclip"); path != "" {
			return &Backend{
				Name:      "xclip",
				ReadPath:  path,
				ReadArgs:  []string{"-selection", "clipboard", "-o"},
				WritePath: path,
				WriteArgs: []string{"-selection", "clipboard", "-i"},
			}, ""
		}
		if path := lookup.LookPath("xsel"); path != "" {
			return &Backend{
				Name:      "xsel",
				ReadPath:  path,
				ReadArgs:  []string{"--clipboard", "--output"},
				WritePath: path,
				WriteArgs: []string{"--clipboard", "--input"},
			}, ""
		}
		return nil, "xclip and xsel are not available"
	}
	if wayland {
		return nil, "wl-clipboard is not available"
	}
	return nil, "no clipboard display environment detected"
}

func serviceEnv(env []string) []string {
	if env == nil {
		return os.Environ()
	}
	return env
}

func powershellBackend(name, path string) *Backend {
	readScript := `[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; [Console]::Out.Write([string](Get-Clipboard -Raw))`
	writeScript := `[Console]::InputEncoding = [System.Text.Encoding]::UTF8; [Console]::OutputEncoding = [System.Text.Encoding]::UTF8; $text = [Console]::In.ReadToEnd(); if ($text.Length -eq 0) { Set-Clipboard -Value $null } else { Set-Clipboard -Value $text }`
	args := func(script string) []string {
		return []string{"-NoLogo", "-NoProfile", "-STA", "-Command", script}
	}
	return &Backend{
		Name:      name,
		ReadPath:  path,
		ReadArgs:  args(readScript),
		WritePath: path,
		WriteArgs: args(writeScript),
	}
}
