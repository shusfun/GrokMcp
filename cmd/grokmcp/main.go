package main

import (
	"context"
	"fmt"
	"os"

	"time"

	"grokmcp/internal/agent"
	"grokmcp/internal/app"
	"grokmcp/internal/foreground"
	"grokmcp/internal/grokbin"
	mcpserver "grokmcp/internal/mcp"
	"grokmcp/internal/protocol"
	appruntime "grokmcp/internal/runtime"
	"grokmcp/internal/terminal"
)

func main() {
	cmd := "desktop"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	ctx := context.Background()
	switch cmd {
	case "terminal-view":
		if err := terminal.RunViewer(ctx, os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "mcp":
		// 先接住 stdio，再在后台拨 Supervisor。窗口创建不能挡住 initialize。
		// Codex 在 initialize 成功后取消 MCP 进程是宿主行为，这里不处理。
		go foreground.YieldLoop(4 * time.Second)
		backend, cleanup, err := appruntime.Attach(ctx, appruntime.ConnectOptions{})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer cleanup()
		if err := mcpserver.Run(ctx, backend); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "mcp-config":
		exe, err := appruntime.MCPExecutable()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Print(appruntime.FormatMCPConfig(exe))
	case "doctor":
		exe, err := appruntime.MCPExecutable()
		if err != nil {
			exe = "(unknown)"
		}
		fmt.Printf("executable\t%s\n", exe)
		fmt.Printf("mcp_command\t%s mcp\n", exe)
		fmt.Printf("supervisor_ipc\t%v\n", appruntime.Probe(ctx))
		if addr, err := appruntime.IPCAddress(); err == nil {
			fmt.Printf("supervisor_ipc_addr\t%s\n", addr)
		}
		finder := grokbin.New()
		d := finder.Diagnose(ctx, "")
		g := agent.NewGrok(d.GrokPath, finder)
		// doctor 只执行显式诊断，不启动 leader 或恢复任务。
		_ = g.Close()
		printDiagnose(d)
		if d.Error != "" {
			os.Exit(1)
		}
	case "desktop", "app":
		background := backgroundDesktop(tailArgs())
		if background {
			go foreground.YieldLoop(4 * time.Second)
		}
		backend, cleanup, err := appruntime.Open(ctx, appruntime.Options{})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer cleanup()
		if err := app.Run(backend, app.RunOptions{Background: background}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "usage: %s [desktop|mcp|mcp-config|doctor]\n", os.Args[0])
		os.Exit(2)
	}
}

func tailArgs() []string {
	if len(os.Args) < 3 {
		return nil
	}
	return os.Args[2:]
}

func backgroundDesktop(args []string) bool {
	for _, arg := range args {
		if arg == app.BackgroundArg {
			return true
		}
	}
	return false
}

func printDiagnose(d protocol.DiagnoseResult) {
	fmt.Printf("grok_path\t%s\n", d.GrokPath)
	fmt.Printf("grok_version\t%s\n", d.GrokVersion)
	fmt.Printf("compatible\t%v\n", d.Compatible)
	fmt.Printf("logged_in\t%v\n", d.LoggedIn)
	fmt.Printf("leader_running\t%v\n", d.LeaderRunning)
	fmt.Printf("leader_socket\t%s\n", d.LeaderSocket)
	fmt.Printf("attach_mode\t%s\n", d.AttachMode)
	if d.Error != "" {
		fmt.Printf("error\t%s\n", d.Error)
	}
}
