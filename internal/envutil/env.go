package envutil

import (
	"os"
	"strings"
)

var blockedEnvNames = map[string]bool{
	"laptop_mcp_token":          true,
	"bridge_files_token":        true,
	"supergateway_api_key":      true,
	"supergateway_api_key_file": true,
}

func Clean(env []string) []string {
	result := make([]string, 0, len(env))
	for _, entry := range env {
		name, _, ok := strings.Cut(entry, "=")
		if ok && blockedEnvNames[strings.ToLower(name)] {
			continue
		}
		result = append(result, entry)
	}
	return result
}

func Cleaned() []string {
	return Clean(os.Environ())
}
