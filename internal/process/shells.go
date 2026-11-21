package process

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"go-bridge/internal/envutil"
)

func DetectShells() (ShellSet, error) {
	return DetectShellsWithEnv(os.Environ())
}

func DetectShellsWithEnv(env []string) (ShellSet, error) {
	lookup := envutil.NewLookup(env)
	if runtime.GOOS == "windows" {
		return detectWindowsShells(lookup)
	}
	return detectUnixShells(lookup)
}

func detectWindowsShells(lookup envutil.Lookup) (ShellSet, error) {
	set := ShellSet{Available: map[string]Shell{}, PathStyle: "windows"}
	add := func(alias, kind, path string) {
		if path == "" || !fileExists(path) {
			return
		}
		set.Available[alias] = Shell{Alias: alias, Kind: kind, Path: path, PathStyle: "windows"}
	}
	add("powershell", "powershell", lookup.LookPath("powershell.exe"))
	add("pwsh", "powershell", lookup.LookPath("pwsh.exe"))
	add("cmd", "cmd", findCmd(lookup))
	add("git-bash", "posix", findGitBash(lookup))
	for _, alias := range []string{"powershell", "pwsh", "cmd"} {
		if _, ok := set.Available[alias]; ok {
			set.Default = alias
			break
		}
	}
	if set.Default == "" {
		return set, NewError("invalid_shell", "no supported Windows shell found (powershell, pwsh, cmd)")
	}
	for _, alias := range []string{"powershell", "pwsh", "cmd", "git-bash"} {
		if _, ok := set.Available[alias]; ok {
			set.Order = append(set.Order, alias)
		}
	}
	return set, nil
}

func detectUnixShells(lookup envutil.Lookup) (ShellSet, error) {
	set := ShellSet{Available: map[string]Shell{}, PathStyle: "posix"}
	for _, alias := range []string{"bash", "zsh", "sh", "fish"} {
		if path := lookup.LookPath(alias); path != "" {
			set.Available[alias] = Shell{Alias: alias, Kind: "posix", Path: path, PathStyle: "posix"}
		}
	}
	if _, ok := set.Available["sh"]; !ok && fileExists("/bin/sh") {
		set.Available["sh"] = Shell{Alias: "sh", Kind: "posix", Path: "/bin/sh", PathStyle: "posix"}
	}
	login := strings.TrimSpace(lookup.Get("SHELL"))
	loginAlias := ""
	if login != "" {
		loginAlias = strings.ToLower(filepath.Base(login))
		if _, ok := set.Available[loginAlias]; !ok && fileExists(login) {
			if loginAlias == "bash" || loginAlias == "zsh" || loginAlias == "sh" || loginAlias == "fish" {
				set.Available[loginAlias] = Shell{Alias: loginAlias, Kind: "posix", Path: absolutePath(login), PathStyle: "posix"}
			}
		}
	}
	if _, ok := set.Available[loginAlias]; ok {
		set.Default = loginAlias
	} else if _, ok := set.Available["bash"]; ok {
		set.Default = "bash"
	} else if _, ok := set.Available["sh"]; ok {
		set.Default = "sh"
	}
	if set.Default == "" {
		return set, NewError("invalid_shell", "no supported Unix shell found (bash, zsh, sh, fish)")
	}
	for _, alias := range []string{"bash", "zsh", "sh", "fish"} {
		if _, ok := set.Available[alias]; ok {
			set.Order = append(set.Order, alias)
		}
	}
	return set, nil
}

func ResolveShell(alias string, set ShellSet) (Shell, error) {
	normalized := strings.ToLower(strings.TrimSpace(alias))
	if normalized == "" {
		normalized = set.Default
	}
	if set.PathStyle == "windows" && (normalized == "bash" || normalized == "bash.exe") {
		return Shell{}, NewError("invalid_shell", "shell %q is not supported on Windows; use git-bash (WSL bash is not allowed)", normalized)
	}
	shell, ok := set.Available[normalized]
	if !ok {
		names := make([]string, 0, len(set.Available))
		for name := range set.Available {
			names = append(names, name)
		}
		return Shell{}, NewError("invalid_shell", "shell %q is not available; available: %s", normalized, strings.Join(names, ", "))
	}
	return shell, nil
}

func ShellArgs(shell Shell, command string) []string {
	switch shell.Alias {
	case "powershell", "pwsh":
		return []string{"-NoLogo", "-NoProfile", "-Command", command}
	case "cmd":
		return []string{"/c", command}
	case "bash", "zsh", "fish", "git-bash":
		return []string{"-lc", command}
	case "sh":
		return []string{"-c", command}
	default:
		return []string{"-c", command}
	}
}

func commandWithWorkdir(shell Shell, workdir, command string) (string, error) {
	if workdir == "" {
		return command, nil
	}
	switch shell.Alias {
	case "powershell", "pwsh":
		return "Set-Location -LiteralPath " + quotePowerShell(workdir) + " -ErrorAction Stop; " + command, nil
	case "cmd":
		if strings.Contains(workdir, "%") {
			return "", NewError("workdir_unsupported_character", "workdir %q contains %% which cmd would expand", workdir)
		}
		return `cd /d "` + workdir + `" && ` + command, nil
	case "git-bash":
		return "cd -- " + quotePOSIX(msysPath(workdir)) + " && " + command, nil
	case "bash", "zsh":
		return "cd -- " + quotePOSIX(workdir) + " && " + command, nil
	case "sh":
		return "cd " + quotePOSIX(workdir) + " && " + command, nil
	case "fish":
		return "cd -- " + quotePOSIX(workdir) + "; and " + command, nil
	default:
		return "cd " + quotePOSIX(workdir) + " && " + command, nil
	}
}

func quotePowerShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func quotePOSIX(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func msysPath(value string) string {
	return strings.ReplaceAll(value, `\`, "/")
}

func findCmd(lookup envutil.Lookup) string {
	if path := lookup.LookPath("cmd.exe"); path != "" {
		return path
	}
	comspec := strings.Trim(strings.TrimSpace(lookup.Get("ComSpec")), "\"")
	if comspec != "" && strings.EqualFold(filepath.Base(comspec), "cmd.exe") && fileExists(comspec) {
		return absolutePath(comspec)
	}
	return ""
}

func findGitBash(lookup envutil.Lookup) string {
	candidates := []string{}
	for _, key := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
		if base := strings.TrimSpace(lookup.Get(key)); base != "" {
			candidates = append(candidates, filepath.Join(base, "Git", "bin", "bash.exe"))
		}
	}
	if local := strings.TrimSpace(lookup.Get("LOCALAPPDATA")); local != "" {
		candidates = append(candidates, filepath.Join(local, "Programs", "Git", "bin", "bash.exe"))
	}
	if git := lookup.LookPath("git.exe"); git != "" {
		candidates = append(candidates, filepath.Join(filepath.Dir(git), "..", "bin", "bash.exe"))
	}
	wsl := ""
	if systemRoot := strings.TrimSpace(lookup.Get("SystemRoot")); systemRoot != "" {
		wsl = filepath.Join(systemRoot, "System32", "bash.exe")
	}
	for _, candidate := range candidates {
		abs, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if wsl != "" && strings.EqualFold(abs, absolutePath(wsl)) {
			continue
		}
		if fileExists(abs) {
			return abs
		}
	}
	return ""
}

func absolutePath(value string) string {
	abs, err := filepath.Abs(value)
	if err != nil {
		return value
	}
	return abs
}

func fileExists(value string) bool {
	if value == "" {
		return false
	}
	info, err := os.Stat(value)
	return err == nil && !info.IsDir()
}
