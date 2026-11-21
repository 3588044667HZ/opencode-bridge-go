package screenshot

import (
	"os"
	"runtime"
	"strings"

	"go-bridge/internal/envutil"
)

func detect(configured string, env []string) (string, string, string) {
	env = serviceEnv(env)
	lookup := envutil.NewLookup(env)
	configured = strings.ToLower(strings.TrimSpace(configured))
	switch runtime.GOOS {
	case "windows":
		powerShellPath := lookPowerShell(lookup)
		ffmpegPath := lookup.LookPath("ffmpeg")
		switch configured {
		case "ffmpeg":
			if ffmpegPath != "" {
				return "ffmpeg", "", powerShellPath
			}
			return "", "ffmpeg is not available", powerShellPath
		case "powershell":
			if powerShellPath != "" {
				return "powershell", "", powerShellPath
			}
			return "", "powershell and pwsh are not available", ""
		default:
			if ffmpegPath != "" {
				return "ffmpeg", "", powerShellPath
			}
			if powerShellPath != "" {
				return "powershell", "", powerShellPath
			}
			return "", "no screenshot backend is available", ""
		}
	case "darwin":
		if lookup.LookPath("screencapture") != "" {
			return "screencapture", "", ""
		}
		return "", "screencapture is not available", ""
	case "linux":
		wayland := strings.TrimSpace(lookup.Get("WAYLAND_DISPLAY")) != ""
		x11 := strings.TrimSpace(lookup.Get("DISPLAY")) != ""
		if wayland {
			if lookup.LookPath("grim") != "" {
				return "grim", "", ""
			}
		}
		if x11 {
			for _, name := range []string{"scrot", "maim", "gnome-screenshot", "import"} {
				if lookup.LookPath(name) != "" {
					return name, "", ""
				}
			}
			return "", "no supported screenshot backend is available", ""
		}
		if wayland {
			return "", "grim is not available", ""
		}
		return "", "no display environment detected", ""
	default:
		return "", "screenshot is not supported on " + runtime.GOOS, ""
	}
}

func lookPowerShell(lookup envutil.Lookup) string {
	if path := lookup.LookPath("powershell.exe"); path != "" {
		return path
	}
	return lookup.LookPath("pwsh.exe")
}

func serviceEnv(env []string) []string {
	if env == nil {
		return os.Environ()
	}
	return env
}
