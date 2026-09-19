package integration

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"grokmcp/internal/protocol"
)

var mcpArgs = []string{"mcp"}

func aliases() []string {
	return []string{protocol.MCPServerID, protocol.MCPServerLegacyID}
}

func isAlias(id string) bool {
	switch id {
	case protocol.MCPServerID, protocol.MCPServerLegacyID:
		return true
	default:
		return false
	}
}

func isGrokMcpCommand(command, currentExe string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	if currentExe != "" && filepath.Clean(command) == filepath.Clean(currentExe) {
		return true
	}
	base := filepath.Base(command)
	return base == "GrokMcp" || strings.EqualFold(base, "GrokMcp.exe")
}

type stdioServer struct {
	Type              string            `json:"type,omitempty"`
	Command           string            `json:"command"`
	Args              []string          `json:"args"`
	Env               map[string]string `json:"env,omitempty"`
	StartupTimeoutSec int               `json:"startup_timeout_sec,omitempty"`
	ToolTimeoutSec    int               `json:"tool_timeout_sec,omitempty"`
}

func serverSpec(exe string) stdioServer {
	return stdioServer{
		Type:              "stdio",
		Command:           exe,
		Args:              append([]string(nil), mcpArgs...),
		StartupTimeoutSec: protocol.MCPStartupTimeoutSec,
		ToolTimeoutSec:    protocol.MCPToolTimeoutSec,
	}
}

func mcpServersJSON(id, exe string) (string, error) {
	root := map[string]any{
		"mcpServers": map[string]any{
			id: serverSpec(exe),
		},
	}
	b, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

func DeepLink(id, exe string) (string, error) {
	raw, err := mcpServersJSON(id, exe)
	if err != nil {
		return "", err
	}
	cfg := base64.RawURLEncoding.EncodeToString([]byte(raw))
	u := &url.URL{
		Scheme: "ccswitch",
		Host:   "v1",
		Path:   "/import",
	}
	q := url.Values{}
	q.Set("resource", "mcp")
	q.Set("apps", "codex")
	q.Set("config", cfg)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func ParseDeepLinkConfig(link string) (id, command string, args []string, err error) {
	u, err := url.Parse(link)
	if err != nil {
		return "", "", nil, err
	}
	cfg := u.Query().Get("config")
	if cfg == "" {
		return "", "", nil, fmt.Errorf("missing config")
	}
	b, err := base64.RawURLEncoding.DecodeString(cfg)
	if err != nil {
		b, err = base64.URLEncoding.DecodeString(cfg)
	}
	if err != nil {
		b, err = base64.StdEncoding.DecodeString(cfg)
	}
	if err != nil {
		return "", "", nil, err
	}
	var root struct {
		MCPServers map[string]stdioServer `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &root); err != nil {
		return "", "", nil, err
	}
	for k, v := range root.MCPServers {
		return k, v.Command, v.Args, nil
	}
	return "", "", nil, fmt.Errorf("no mcpServers")
}

func CodexAddCommand(exe string, windows bool) string {
	if windows {
		return fmt.Sprintf("codex mcp add %s -- %s mcp", protocol.MCPServerID, powershellQuote(exe))
	}
	return fmt.Sprintf("codex mcp add %s -- %s mcp", protocol.MCPServerID, posixQuote(exe))
}

func CodexTOML(exe string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[mcp_servers.%s]\n", protocol.MCPServerID)
	fmt.Fprintf(&b, "command = %s\n", tomlQuote(exe))
	b.WriteString("args = [\"mcp\"]\n")
	fmt.Fprintf(&b, "startup_timeout_sec = %d\n", protocol.MCPStartupTimeoutSec)
	fmt.Fprintf(&b, "tool_timeout_sec = %d\n", protocol.MCPToolTimeoutSec)
	return b.String()
}

func FormatCLIConfig(exe string, windows bool) string {
	var b strings.Builder
	if windows {
		fmt.Fprintf(&b, "%s\n", CodexAddCommand(exe, true))
		b.WriteString("# PowerShell quoting; paste the TOML below into config if unsure.\n\n")
	} else {
		fmt.Fprintf(&b, "%s\n\n", CodexAddCommand(exe, false))
	}
	b.WriteString(CodexTOML(exe))
	return b.String()
}

func deepLinkSupported(goos string) bool {
	return goos == "darwin" || goos == "windows"
}

func BuildBundle(exe string, goos string, jsonID string) (protocol.MCPConfigBundle, error) {
	if jsonID == "" {
		jsonID = protocol.MCPServerID
	}
	js, err := mcpServersJSON(protocol.MCPServerID, exe)
	if err != nil {
		return protocol.MCPConfigBundle{}, err
	}
	updateJSON := js
	if jsonID != protocol.MCPServerID {
		updateJSON, err = mcpServersJSON(jsonID, exe)
		if err != nil {
			return protocol.MCPConfigBundle{}, err
		}
	}
	link, err := DeepLink(protocol.MCPServerID, exe)
	if err != nil {
		return protocol.MCPConfigBundle{}, err
	}
	windows := goos == "windows"
	return protocol.MCPConfigBundle{
		ServerID:          protocol.MCPServerID,
		Exe:               exe,
		Args:              append([]string(nil), mcpArgs...),
		StartupTimeoutSec: protocol.MCPStartupTimeoutSec,
		ToolTimeoutSec:    protocol.MCPToolTimeoutSec,
		JSON:              js,
		UpdateJSON:        updateJSON,
		DeepLink:          link,
		CodexAddCommand:   CodexAddCommand(exe, windows),
		TOML:              CodexTOML(exe),
		Platform:          goos,
		DeepLinkSupported: deepLinkSupported(goos),
	}, nil
}
