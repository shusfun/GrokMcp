package main

import (
	"context"
	"fmt"
	"os"

	"grokmcp/internal/agent"
	"grokmcp/internal/app"
	"grokmcp/internal/grokbin"
	mcpserver "grokmcp/internal/mcp"
	"grokmcp/internal/protocol"
	appruntime "grokmcp/internal/runtime"
)

func main() {
	cmd := "desktop"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	ctx := context.Background()
	switch cmd {
	case "mcp":
		backend, cleanup, err := appruntime.Connect(ctx, appruntime.ConnectOptions{})
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
		finder := grokbin.New()
		d := finder.Diagnose(ctx, "")
		g := agent.NewGrok(d.GrokPath, finder)
		if err := g.EnsureLeader(ctx); err == nil {
			d = g.Diagnose(ctx)
		}
		_ = g.Close()
		printDiagnose(d)
		if d.Error != "" {
			os.Exit(1)
		}
	case "desktop", "app":
		backend, cleanup, err := appruntime.Open(ctx, appruntime.Options{})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer cleanup()
		if err := app.Run(backend); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "usage: %s [desktop|mcp|mcp-config|doctor]\n", os.Args[0])
		os.Exit(2)
	}
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
