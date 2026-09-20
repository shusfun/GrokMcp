package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"grokmcp/internal/protocol"
)

type codexServer struct {
	Name              string          `json:"name"`
	Enabled           bool            `json:"enabled"`
	Transport         *codexTransport `json:"transport"`
	StartupTimeoutSec *float64        `json:"startup_timeout_sec"`
	ToolTimeoutSec    *float64        `json:"tool_timeout_sec"`
}

type codexTransport struct {
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	EnvVars []string          `json:"env_vars"`
	Cwd     string            `json:"cwd"`
}

func (s *Service) probeCodex(ctx context.Context, exe string) protocol.MCPCodexStatus {
	bin, err := s.lookPath("codex")
	if err != nil || bin == "" {
		return protocol.MCPCodexStatus{CLIFound: false, Error: "未找到 Codex CLI"}
	}
	st := protocol.MCPCodexStatus{CLIFound: true}
	stdout, stderr, err := s.run(ctx, bin, "mcp", "list", "--json")
	if err != nil {
		st.Error = strings.TrimSpace(stderr)
		if st.Error == "" {
			st.Error = err.Error()
		}
		return st
	}
	list, err := parseCodexList(stdout)
	if err != nil {
		st.Error = "无法解析 codex mcp list"
		return st
	}
	srv, ok := findCodexAlias(list, exe)
	if !ok {
		st.NeedsUpdate = false
		return st
	}
	st.LiveVisible = true
	st.LiveName = srv.Name
	st.Enabled = srv.Enabled
	cmd := ""
	args := []string(nil)
	if srv.Transport != nil {
		cmd = srv.Transport.Command
		args = srv.Transport.Args
	}
	st.CommandMatch = filepath.Clean(cmd) == filepath.Clean(exe) && len(args) == 1 && args[0] == "mcp"
	st.TimeoutsPresent = timeoutPresent(srv.StartupTimeoutSec, protocol.MCPStartupTimeoutSec) &&
		timeoutPresent(srv.ToolTimeoutSec, protocol.MCPToolTimeoutSec)
	st.NeedsUpdate = !isStableID(srv.Name) || !st.CommandMatch || !st.TimeoutsPresent
	return st
}

func timeoutPresent(v *float64, want int) bool {
	if v == nil {
		return false
	}
	return int(*v) == want
}

func parseCodexList(raw string) ([]codexServer, error) {
	trim := bytes.TrimSpace([]byte(raw))
	if len(trim) == 0 {
		return nil, nil
	}
	var list []codexServer
	if err := json.Unmarshal(trim, &list); err == nil {
		return list, nil
	}
	var wrap struct {
		Servers []codexServer `json:"servers"`
	}
	if err := json.Unmarshal(trim, &wrap); err != nil {
		return nil, err
	}
	return wrap.Servers, nil
}

func findCodexAlias(list []codexServer, exe string) (codexServer, bool) {
	var named []codexServer
	for _, s := range list {
		if isAlias(s.Name) {
			named = append(named, s)
		}
	}
	for _, s := range named {
		if s.Transport != nil && isGrokMcpCommand(s.Transport.Command, exe) {
			return s, true
		}
	}
	if len(named) > 0 {
		return named[0], true
	}
	for _, s := range list {
		if s.Transport != nil && isGrokMcpCommand(s.Transport.Command, exe) {
			return s, true
		}
	}
	return codexServer{}, false
}

func (s *Service) AddMCPToCodex(ctx context.Context) (protocol.MCPApplyResult, error) {
	bundle, err := s.Config(ctx)
	if err != nil {
		return protocol.MCPApplyResult{OK: false, Action: protocol.MCPActionFailed, Target: protocol.MCPTargetCodex, Message: err.Error()}, nil
	}
	bin, err := s.lookPath("codex")
	if err != nil || bin == "" {
		return protocol.MCPApplyResult{
			OK: false, Action: protocol.MCPActionFailed, Target: protocol.MCPTargetCodex,
			Message: "未找到 Codex CLI", NextStep: "安装 Codex 后重试，或复制命令手动执行。",
		}, nil
	}
	stdout, stderr, err := s.run(ctx, bin, "mcp", "list", "--json")
	if err != nil {
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			msg = err.Error()
		}
		return protocol.MCPApplyResult{OK: false, Action: protocol.MCPActionFailed, Target: protocol.MCPTargetCodex, Message: msg}, nil
	}
	list, err := parseCodexList(stdout)
	if err != nil {
		return protocol.MCPApplyResult{OK: false, Action: protocol.MCPActionFailed, Target: protocol.MCPTargetCodex, Message: "无法解析 codex mcp list"}, nil
	}
	if srv, ok := findCodexAlias(list, bundle.Exe); ok {
		return s.handleExistingCodex(ctx, bin, bundle, srv)
	}
	addArgs := []string{"mcp", "add", protocol.MCPServerID, "--", bundle.Exe, "mcp"}
	_, stderr, err = s.run(ctx, bin, addArgs...)
	if err != nil {
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			msg = err.Error()
		}
		return protocol.MCPApplyResult{OK: false, Action: protocol.MCPActionFailed, Target: protocol.MCPTargetCodex, Message: msg}, nil
	}
	live := s.probeCodex(ctx, bundle.Exe)
	res := protocol.MCPApplyResult{
		OK: true, Action: protocol.MCPActionCreated, Target: protocol.MCPTargetCodex,
		LiveEffective: live.LiveVisible && live.CommandMatch && live.Enabled,
		Message:       "已调用 codex mcp add grok_supervisor",
	}
	if !live.TimeoutsPresent {
		res.NextStep = "CLI 不会写入 timeout 字段。可复制 TOML 由你自行补充；不要同时用 CC-Switch 再添加一项。"
	}
	return res, nil
}

func (s *Service) handleExistingCodex(ctx context.Context, bin string, bundle protocol.MCPConfigBundle, srv codexServer) (protocol.MCPApplyResult, error) {
	cmd := ""
	args := []string(nil)
	if srv.Transport != nil {
		cmd = srv.Transport.Command
		args = srv.Transport.Args
	}
	commandMatch := filepath.Clean(cmd) == filepath.Clean(bundle.Exe) && len(args) == 1 && args[0] == "mcp"
	if !isStableID(srv.Name) {
		return protocol.MCPApplyResult{
			OK: true, Action: protocol.MCPActionNeedsManual, Target: protocol.MCPTargetCodex, LiveEffective: false,
			Message:  fmt.Sprintf("旧 MCP 名称无效（%s）：Codex 桌面不接受空格", srv.Name),
			NextStep: "请移除旧的 Grok Supervisor，并用复制的 TOML 或 CC-Switch deep link 新增 grok_supervisor；然后在 Codex 设置的 MCP Servers 中 Restart。不会自动 remove，以免丢失 timeout/env。",
		}, nil
	}
	if commandMatch && !srv.Enabled {
		return protocol.MCPApplyResult{
			OK: true, Action: protocol.MCPActionNeedsManual, Target: protocol.MCPTargetCodex, LiveEffective: false,
			Message:  fmt.Sprintf("该 MCP 已存在但被禁用（%s）", srv.Name),
			NextStep: "请在 Codex 配置中启用，或通过 Codex 的 MCP 管理入口启用。不会自动 remove+add，也不会重复添加。",
		}, nil
	}
	if commandMatch && srv.Enabled {
		res := protocol.MCPApplyResult{
			OK: true, Action: protocol.MCPActionUnchanged, Target: protocol.MCPTargetCodex,
			LiveEffective: true, Message: fmt.Sprintf("Codex 已有 %s 且已启用，未重复添加", srv.Name),
		}
		if !timeoutPresent(srv.StartupTimeoutSec, protocol.MCPStartupTimeoutSec) || !timeoutPresent(srv.ToolTimeoutSec, protocol.MCPToolTimeoutSec) {
			res.NextStep = "超时字段缺失或不同。CLI 无法写入 timeout，请复制 TOML 自行处理，不要 remove 已有项。"
		}
		return res, nil
	}
	if !canRestoreViaAdd(srv) {
		return protocol.MCPApplyResult{
			OK: true, Action: protocol.MCPActionNeedsManual, Target: protocol.MCPTargetCodex,
			Message:  fmt.Sprintf("已捕获 %s，但 remove+add 无法无损恢复 env/timeout", srv.Name),
			NextStep: "请复制 add 命令或 TOML，在 Codex 中自行更新。不会自动 remove。",
		}, nil
	}
	if _, stderr, err := s.run(ctx, bin, "mcp", "remove", srv.Name); err != nil {
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			msg = err.Error()
		}
		return protocol.MCPApplyResult{
			OK: false, Action: protocol.MCPActionFailed, Target: protocol.MCPTargetCodex,
			Message: fmt.Sprintf("remove %s 失败：%s（未继续 add）", srv.Name, msg),
		}, nil
	}
	if _, stderr, err := s.run(ctx, bin, "mcp", "add", srv.Name, "--", bundle.Exe, "mcp"); err != nil {
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			msg = err.Error()
		}
		return protocol.MCPApplyResult{
			OK: false, Action: protocol.MCPActionFailed, Target: protocol.MCPTargetCodex,
			Message:  fmt.Sprintf("已 remove %s 但 add 失败：%s", srv.Name, msg),
			NextStep: "请用复制的 add 命令恢复该 MCP。",
		}, nil
	}
	live := s.probeCodex(ctx, bundle.Exe)
	return protocol.MCPApplyResult{
		OK: true, Action: protocol.MCPActionUpdated, Target: protocol.MCPTargetCodex,
		LiveEffective: live.LiveVisible && live.CommandMatch && live.Enabled,
		Message:       fmt.Sprintf("已更新 Codex MCP %s", srv.Name),
	}, nil
}

func canRestoreViaAdd(srv codexServer) bool {
	if timeoutPresent(srv.StartupTimeoutSec, protocol.MCPStartupTimeoutSec) || timeoutPresent(srv.ToolTimeoutSec, protocol.MCPToolTimeoutSec) {
		return false
	}
	if srv.StartupTimeoutSec != nil && *srv.StartupTimeoutSec != 0 {
		return false
	}
	if srv.ToolTimeoutSec != nil && *srv.ToolTimeoutSec != 0 {
		return false
	}
	if srv.Transport == nil {
		return true
	}
	if len(srv.Transport.Env) > 0 {
		return false
	}
	for _, v := range srv.Transport.EnvVars {
		if strings.TrimSpace(v) != "" {
			return false
		}
	}
	return true
}

func (s *Service) lookPath(name string) (string, error) {
	if s.opts.LookPath != nil {
		return s.opts.LookPath(name)
	}
	return exec.LookPath(name)
}

func (s *Service) run(ctx context.Context, name string, args ...string) (string, string, error) {
	if s.opts.Run != nil {
		return s.opts.Run(ctx, name, args...)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}
