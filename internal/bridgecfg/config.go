package bridgecfg

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	DefaultMCPPort      = 8765
	DefaultFilesPort    = 8766
	DefaultMaxFileBytes = int64(536870912)
	DefaultStagingQuota = int64(2147483648)
	FilesHost           = "127.0.0.1"
	FilesPath           = "/files"
)

type Config struct {
	Root              string
	DataDir           string
	Workspace         string
	MCPPort           int
	FilesPort         int
	FilesEnabled      bool
	MaxFileBytes      int64
	StagingQuotaBytes int64
	ScreenshotBackend string
}

type Secrets struct {
	MCPToken   string
	FilesToken string
}

func Load(getenv func(string) string, root string) (Config, Secrets, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return Config{}, Secrets{}, fmt.Errorf("invalid root %q: %w", root, err)
	}
	rootAbs = filepath.Clean(rootAbs)
	mcpToken := strings.TrimSpace(getenv("LAPTOP_MCP_TOKEN"))
	if mcpToken == "" {
		return Config{}, Secrets{}, fmt.Errorf("LAPTOP_MCP_TOKEN is required")
	}
	dataDir, err := resolvePath(rootAbs, strings.TrimSpace(getenv("BRIDGE_DATA_DIR")), filepath.Join(rootAbs, "data"))
	if err != nil {
		return Config{}, Secrets{}, fmt.Errorf("BRIDGE_DATA_DIR: %w", err)
	}
	workspace, err := resolvePath(rootAbs, strings.TrimSpace(getenv("BRIDGE_WORKSPACE")), filepath.Join(dataDir, "workspace"))
	if err != nil {
		return Config{}, Secrets{}, fmt.Errorf("BRIDGE_WORKSPACE: %w", err)
	}
	mcpPort, err := parsePort("BRIDGE_PORT", getenv("BRIDGE_PORT"), DefaultMCPPort)
	if err != nil {
		return Config{}, Secrets{}, err
	}
	filesPort, err := parsePort("BRIDGE_FILES_PORT", getenv("BRIDGE_FILES_PORT"), DefaultFilesPort)
	if err != nil {
		return Config{}, Secrets{}, err
	}
	if filesPort == mcpPort {
		return Config{}, Secrets{}, fmt.Errorf("BRIDGE_FILES_PORT must differ from BRIDGE_PORT")
	}
	maxFileBytes, err := parseBytes("BRIDGE_MAX_FILE_BYTES", getenv("BRIDGE_MAX_FILE_BYTES"), DefaultMaxFileBytes)
	if err != nil {
		return Config{}, Secrets{}, err
	}
	stagingQuota, err := parseBytes("BRIDGE_STAGING_QUOTA_BYTES", getenv("BRIDGE_STAGING_QUOTA_BYTES"), DefaultStagingQuota)
	if err != nil {
		return Config{}, Secrets{}, err
	}
	if stagingQuota < maxFileBytes {
		return Config{}, Secrets{}, fmt.Errorf("BRIDGE_STAGING_QUOTA_BYTES must be greater than or equal to BRIDGE_MAX_FILE_BYTES")
	}
	screenshotBackend, err := parseScreenshotBackend(getenv("BRIDGE_SCREENSHOT_BACKEND"))
	if err != nil {
		return Config{}, Secrets{}, err
	}
	filesToken := strings.TrimSpace(getenv("BRIDGE_FILES_TOKEN"))
	filesEnabled := filesToken != ""
	if filesEnabled && filesToken == mcpToken {
		return Config{}, Secrets{}, fmt.Errorf("BRIDGE_FILES_TOKEN must differ from LAPTOP_MCP_TOKEN")
	}
	cfg := Config{
		Root:              rootAbs,
		DataDir:           dataDir,
		Workspace:         workspace,
		MCPPort:           mcpPort,
		FilesPort:         filesPort,
		FilesEnabled:      filesEnabled,
		MaxFileBytes:      maxFileBytes,
		StagingQuotaBytes: stagingQuota,
		ScreenshotBackend: screenshotBackend,
	}
	return cfg, Secrets{MCPToken: mcpToken, FilesToken: filesToken}, nil
}

func (c Config) StagingInbox() string {
	return filepath.Join(c.DataDir, "staging", "inbox")
}

func (c Config) StagingOutbox() string {
	return filepath.Join(c.DataDir, "staging", "outbox")
}

func (c Config) ScreenshotsDir() string {
	return filepath.Join(c.DataDir, "screenshots")
}

func (c Config) MCPAddr() string {
	return FilesHost + ":" + strconv.Itoa(c.MCPPort)
}

func (c Config) FilesAddr() string {
	return FilesHost + ":" + strconv.Itoa(c.FilesPort)
}

func (c Config) EnsureDirs() error {
	dirs := []string{
		c.DataDir,
		c.Workspace,
		c.StagingInbox(),
		c.StagingOutbox(),
		c.ScreenshotsDir(),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("cannot create directory %s: %w", dir, err)
		}
	}
	info, err := os.Stat(c.Workspace)
	if err != nil {
		return fmt.Errorf("cannot stat BRIDGE_WORKSPACE %s: %w", c.Workspace, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("BRIDGE_WORKSPACE is not a directory: %s", c.Workspace)
	}
	return nil
}

func (c Config) WorkspaceCoversDataDir() bool {
	workspace := c.Workspace
	if resolved, err := filepath.EvalSymlinks(workspace); err == nil {
		workspace = resolved
	}
	data := c.DataDir
	if resolved, err := filepath.EvalSymlinks(data); err == nil {
		data = resolved
	}
	rel, err := filepath.Rel(workspace, data)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func resolvePath(root, value, fallback string) (string, error) {
	if value == "" {
		value = fallback
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(root, value)
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func parsePort(name, value string, fallback int) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback, nil
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("%s must be an integer between 1 and 65535", name)
	}
	return port, nil
}

func parseBytes(name, value string, fallback int64) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback, nil
	}
	size, err := strconv.ParseInt(value, 10, 64)
	if err != nil || size <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return size, nil
}

func parseScreenshotBackend(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "auto", nil
	}
	switch value {
	case "auto", "ffmpeg", "powershell":
		return value, nil
	default:
		return "", fmt.Errorf("BRIDGE_SCREENSHOT_BACKEND must be one of auto, ffmpeg, powershell")
	}
}
