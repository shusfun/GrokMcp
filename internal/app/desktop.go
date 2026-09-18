package app

import (
	"context"
	"fmt"
	"runtime"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
	"grokmcp/internal/core"
	"grokmcp/internal/notify"
	"grokmcp/internal/protocol"
	"grokmcp/internal/trace"
	"grokmcp/internal/version"
	"grokmcp/internal/web"
)

func Run(backend core.Backend) error {
	svc := &Service{Backend: backend}
	app := application.New(application.Options{
		Name:        "Grok Supervisor",
		Description: "Codex orchestration · Grok execution",
		Services: []application.Service{
			application.NewService(svc),
		},
		Assets: application.AssetOptions{
			Handler:    application.AssetFileServerFS(web.Assets()),
			Middleware: web.Gzip,
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
	})
	svc.quit = app.Quit
	gh, err := github.New(github.Config{
		Repository:    githubRepo,
		ChecksumAsset: "SHA256SUMS.txt",
		AssetMatcher:  matchReleaseAsset,
	})
	if err != nil {
		return err
	}
	if err := app.Updater.Init(updater.Config{
		CurrentVersion: version.Version,
		Providers:      []updater.Provider{gh},
		Window:         updater.WindowNone,
	}); err != nil {
		return err
	}
	svc.updater = app.Updater

	winOpts := application.WebviewWindowOptions{
		Title:            "Grok Supervisor",
		Width:            1100,
		Height:           720,
		MinWidth:         800,
		MinHeight:        520,
		URL:              "/",
		BackgroundColour: application.NewRGB(22, 22, 22),
		Mac: application.MacWindow{
			TitleBar: application.MacTitleBarHidden,
		},
	}
	if runtime.GOOS == "windows" {
		winOpts.Frameless = true
		winOpts.MinimiseButtonState = application.ButtonHidden
		winOpts.MaximiseButtonState = application.ButtonHidden
		winOpts.CloseButtonState = application.ButtonHidden
	}
	window := app.Window.NewWithOptions(winOpts)
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		window.Hide()
		e.Cancel()
	})

	tray := app.SystemTray.New()
	host := &trayHost{
		app:      app,
		window:   window,
		tray:     tray,
		backend:  backend,
		notifier: notify.OS{},
		last:     map[string]protocol.JobState{},
	}
	host.refresh()
	if runtime.GOOS == "darwin" {
		tray.SetLabel("Grok")
	}
	tray.OnClick(func() {
		window.Show()
		window.Focus()
	})

	backend.Subscribe(func(ev protocol.Event) {
		app.Event.Emit("jobs:changed", ev)
		if ev.Type == "job.deleted" {
			host.forget(ev.JobID)
		} else {
			host.onJob(ev.Job)
		}
		host.refresh()
	})
	if ts, ok := backend.(interface {
		TraceSubscribe(func(trace.Event)) func()
	}); ok {
		ts.TraceSubscribe(func(ev trace.Event) {
			app.Event.Emit("debug:trace", ev)
		})
	}

	return app.Run()
}

type trayHost struct {
	app      *application.App
	window   *application.WebviewWindow
	tray     *application.SystemTray
	backend  core.Backend
	notifier notify.Sender
	mu       sync.Mutex
	last     map[string]protocol.JobState
}

func (h *trayHost) showMain() {
	h.window.Show()
	h.window.Focus()
}

func (h *trayHost) refresh() {
	bar, err := h.backend.StatusBar(context.Background())
	if err != nil {
		bar = protocol.StatusBar{}
	}
	menu := h.app.Menu.New()
	menu.Add("打开主控").OnClick(func(*application.Context) { h.showMain() })
	menu.Add("打开 Grok Dashboard").OnClick(func(*application.Context) {
		_ = h.backend.OpenTerminal(context.Background(), protocol.OpenTerminalRequest{Dashboard: true})
	})
	menu.AddSeparator()
	menu.Add(fmt.Sprintf("工作中 %d", bar.Working)).SetEnabled(false)
	menu.Add(fmt.Sprintf("需要输入 %d", bar.NeedsInput)).SetEnabled(false)
	menu.AddSeparator()
	menu.Add("全部转为无头").OnClick(func(*application.Context) {
		jobs, err := h.backend.ListJobs(context.Background())
		if err != nil {
			return
		}
		for _, j := range jobs {
			if j.ViewMode == protocol.ViewHeaded || j.ViewMode == protocol.ViewAttaching {
				_, _ = h.backend.SetView(context.Background(), protocol.SetViewRequest{JobID: j.JobID, View: protocol.ViewHeadless})
			}
		}
	})
	menu.Add("设置").OnClick(func(*application.Context) {
		h.showMain()
		h.app.Event.Emit("navigate", "#/settings")
	})
	menu.Add("诊断").OnClick(func(*application.Context) {
		h.showMain()
		h.app.Event.Emit("navigate", "#/diagnostics")
	})
	menu.AddSeparator()
	menu.Add("退出").OnClick(func(*application.Context) {
		if bar.Working+bar.NeedsInput > 0 {
			h.app.Event.Emit("quit:warn", bar)
			h.showMain()
			return
		}
		h.app.Quit()
	})
	h.tray.SetMenu(menu)
	switch {
	case bar.Disconnected+bar.Failed > 0:
		h.tray.SetLabel("Grok!")
	case bar.NeedsInput > 0:
		h.tray.SetLabel("Grok?")
	case bar.Working > 0:
		h.tray.SetLabel("Grok…")
	default:
		h.tray.SetLabel("Grok")
	}
}

func (h *trayHost) forget(jobID string) {
	if jobID == "" {
		return
	}
	h.mu.Lock()
	delete(h.last, jobID)
	h.mu.Unlock()
}

func (h *trayHost) onJob(job *protocol.Job) {
	if job == nil {
		return
	}
	h.mu.Lock()
	prev := h.last[job.JobID]
	h.last[job.JobID] = job.State
	h.mu.Unlock()
	if msg, ok := notify.Message(prev, job.State, job.Title); ok {
		h.notifier.Send("Grok Supervisor", msg)
	}
}
