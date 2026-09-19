package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"grokmcp/internal/protocol"

	_ "modernc.org/sqlite"
)

func DefaultCCSwitchDB() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cc-switch", "cc-switch.db")
}

func (s *Service) probeCCSwitch(_ context.Context, exe string) protocol.MCPCCSwitchStatus {
	path := s.ccswitchDB()
	if path == "" {
		return protocol.MCPCCSwitchStatus{Detected: protocol.MCPDetectMissing, Message: "未找到 CC-Switch 数据库"}
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return protocol.MCPCCSwitchStatus{Detected: protocol.MCPDetectMissing, Message: "未检测到 CC-Switch 数据库"}
		}
		return protocol.MCPCCSwitchStatus{Detected: protocol.MCPDetectReadError, Message: "无法读取 CC-Switch 数据库"}
	}
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return classifyCCSwitchOpenErr(err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		return classifyCCSwitchOpenErr(err)
	}
	cols, err := mcpServerColumns(db)
	if err != nil {
		return classifyCCSwitchOpenErr(err)
	}
	for _, need := range []string{"id", "name", "server_config", "enabled_codex"} {
		if !cols[need] {
			return protocol.MCPCCSwitchStatus{
				Detected: protocol.MCPDetectUnsupportedSchema,
				Message:  "CC-Switch 数据库结构无法识别，已禁用自动导入探测",
			}
		}
	}
	rows, err := db.Query(`SELECT id, name, server_config, enabled_codex FROM mcp_servers`)
	if err != nil {
		return classifyCCSwitchOpenErr(err)
	}
	defer rows.Close()
	st := protocol.MCPCCSwitchStatus{Detected: protocol.MCPDetectDetected}
	for rows.Next() {
		var id, name, cfg string
		var enabled any
		if err := rows.Scan(&id, &name, &cfg, &enabled); err != nil {
			return classifyCCSwitchOpenErr(err)
		}
		if !isAlias(id) && !isAlias(name) {
			continue
		}
		spec, ok := parseServerConfig(cfg)
		if !ok || !isGrokMcpCommand(spec.Command, exe) {
			continue
		}
		st.Registered = true
		st.MatchedID = id
		if id == protocol.MCPServerLegacyID || name == protocol.MCPServerLegacyID {
			st.LegacyID = protocol.MCPServerLegacyID
		}
		st.EnabledCodex = sqliteBool(enabled)
		st.NeedsUpdate = !specMatches(spec, exe)
		break
	}
	if err := rows.Err(); err != nil {
		return classifyCCSwitchOpenErr(err)
	}
	switch {
	case !st.Registered:
		st.NextStep = "可用 CC-Switch 快速导入（deep link）。导入完成后请重新检测。"
	case st.NeedsUpdate:
		st.NextStep = "deep link 不能更新已有 server_config。请复制更新 JSON，在 CC-Switch MCP 编辑页粘贴。"
	default:
		st.NextStep = "CC-Switch 已登记且配置匹配。可重新检测确认。"
	}
	return st
}

func (s *Service) ccswitchDB() string {
	if s.opts.CCSwitchDB != "" {
		return s.opts.CCSwitchDB
	}
	return DefaultCCSwitchDB()
}

func classifyCCSwitchOpenErr(err error) protocol.MCPCCSwitchStatus {
	msg := strings.ToLower(err.Error())
	switch {
	case os.IsNotExist(err) || strings.Contains(msg, "no such file") || strings.Contains(msg, "unable to open"):
		return protocol.MCPCCSwitchStatus{Detected: protocol.MCPDetectMissing, Message: "未检测到 CC-Switch 数据库"}
	case strings.Contains(msg, "lock") || strings.Contains(msg, "busy"):
		return protocol.MCPCCSwitchStatus{Detected: protocol.MCPDetectLocked, Message: "CC-Switch 数据库正忙，请稍后重新检测"}
	default:
		return protocol.MCPCCSwitchStatus{Detected: protocol.MCPDetectReadError, Message: "无法读取 CC-Switch 数据库"}
	}
}

func mcpServerColumns(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query(`PRAGMA table_info(mcp_servers)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

func parseServerConfig(raw string) (stdioServer, bool) {
	var spec stdioServer
	if json.Unmarshal([]byte(raw), &spec) != nil || spec.Command == "" {
		return stdioServer{}, false
	}
	return spec, true
}

func specMatches(spec stdioServer, exe string) bool {
	if filepath.Clean(spec.Command) != filepath.Clean(exe) {
		return false
	}
	if len(spec.Args) != 1 || spec.Args[0] != "mcp" {
		return false
	}
	if spec.StartupTimeoutSec != 0 && spec.StartupTimeoutSec != protocol.MCPStartupTimeoutSec {
		return false
	}
	if spec.ToolTimeoutSec != 0 && spec.ToolTimeoutSec != protocol.MCPToolTimeoutSec {
		return false
	}
	if spec.StartupTimeoutSec == 0 || spec.ToolTimeoutSec == 0 {
		return false
	}
	return true
}

func sqliteBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case int64:
		return t != 0
	case int:
		return t != 0
	case []byte:
		s := strings.ToLower(string(t))
		return s == "1" || s == "true"
	case string:
		s := strings.ToLower(t)
		return s == "1" || s == "true"
	default:
		return false
	}
}

func (s *Service) OpenCCSwitchMCPImport(ctx context.Context) (protocol.MCPApplyResult, error) {
	bundle, err := s.Config(ctx)
	if err != nil {
		return protocol.MCPApplyResult{OK: false, Action: protocol.MCPActionFailed, Target: protocol.MCPTargetCCSwitch, Message: err.Error()}, nil
	}
	st := s.probeCCSwitch(ctx, bundle.Exe)
	if st.Registered && !st.NeedsUpdate {
		return protocol.MCPApplyResult{
			OK: true, Action: protocol.MCPActionUnchanged, Target: protocol.MCPTargetCCSwitch,
			Message: "CC-Switch 已登记且配置匹配", NextStep: "可重新检测确认。不要再用 Direct 添加重复项。",
		}, nil
	}
	if st.Registered && st.NeedsUpdate {
		return protocol.MCPApplyResult{
			OK: true, Action: protocol.MCPActionNeedsManual, Target: protocol.MCPTargetCCSwitch,
			Message:  "已存在别名，deep link 不会更新 server_config",
			NextStep: "复制更新 JSON，打开 CC-Switch，在 MCP 编辑页粘贴后重新检测。",
		}, nil
	}
	if !bundle.DeepLinkSupported {
		return protocol.MCPApplyResult{
			OK: false, Action: protocol.MCPActionFailed, Target: protocol.MCPTargetCCSwitch,
			Message: "当前平台未验证 CC-Switch deep link", NextStep: "请复制 STDIO JSON，在 CC-Switch 中手动导入。",
		}, nil
	}
	if err := s.openURL(ctx, bundle.DeepLink); err != nil {
		return protocol.MCPApplyResult{
			OK: false, Action: protocol.MCPActionFailed, Target: protocol.MCPTargetCCSwitch,
			Message: fmt.Sprintf("无法打开 CC-Switch：%v", err), NextStep: "请复制 deep link 或 JSON，在 CC-Switch 中手动导入。",
		}, nil
	}
	return protocol.MCPApplyResult{
		OK: true, Action: protocol.MCPActionPendingUser, Target: protocol.MCPTargetCCSwitch, LiveEffective: false,
		Message: "已打开 CC-Switch，待你确认导入", NextStep: "在 CC-Switch 完成导入后回到这里重新检测。",
	}, nil
}

func (s *Service) OpenCCSwitchApp(ctx context.Context) (protocol.MCPApplyResult, error) {
	if err := s.openApp(ctx); err != nil {
		return protocol.MCPApplyResult{
			OK: false, Action: protocol.MCPActionFailed, Target: protocol.MCPTargetCCSwitch,
			Message: fmt.Sprintf("无法打开 CC-Switch：%v", err), NextStep: "请手动打开 CC-Switch，在 MCP 编辑页粘贴更新 JSON。",
		}, nil
	}
	return protocol.MCPApplyResult{
		OK: true, Action: protocol.MCPActionPendingUser, Target: protocol.MCPTargetCCSwitch, LiveEffective: false,
		Message: "已请求打开 CC-Switch", NextStep: "在 MCP 编辑页粘贴更新 JSON，保存后重新检测。",
	}, nil
}

func (s *Service) openURL(ctx context.Context, raw string) error {
	if s.opts.OpenURL != nil {
		return s.opts.OpenURL(ctx, raw)
	}
	return defaultOpenURL(ctx, s.goos(), raw)
}

func (s *Service) openApp(ctx context.Context) error {
	if s.opts.OpenApp != nil {
		return s.opts.OpenApp(ctx)
	}
	return defaultOpenApp(ctx, s.goos())
}

var errOpenUnsupported = errors.New("no system opener")
