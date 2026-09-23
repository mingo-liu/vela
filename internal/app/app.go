package app

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mingo-liu/vela/internal/desktop"
	"github.com/mingo-liu/vela/internal/mihomo"
	"github.com/mingo-liu/vela/internal/platform/macos"
	"github.com/mingo-liu/vela/internal/profile"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

func Run(assets fs.FS) error {
	base, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	dataDir := filepath.Join(base, "Vela")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return err
	}
	store := profile.NewStore(dataDir)
	subs := profile.NewSubscriptions(store, macos.SubscriptionKeychain{})
	binary := findBinary()
	var wails *application.App
	var systemProxyMenuItem *application.MenuItem
	menuStateUpdates := make(chan struct{}, 1)
	runner := mihomo.NewRunner(store, subs, dataDir, binary, 7890, macos.NewSystemProxy(dataDir), func(state mihomo.State) {
		select {
		case menuStateUpdates <- struct{}{}:
		default:
		}
		if wails != nil {
			wails.Event.Emit("runtime-state", state)
		}
	})
	service := desktop.NewRuntimeService(runner)
	wails = application.New(application.Options{
		Name:        "Vela",
		Description: "Vela local proxy",
		Services:    []application.Service{application.NewService(service)},
		Assets:      application.AssetOptions{Handler: application.AssetFileServerFS(assets)},
		Mac:         application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false},
		OnShutdown:  runner.Close,
	})
	window := wails.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "Vela", Width: 860, Height: 620, MinWidth: 640, MinHeight: 480,
		URL: "/", BackgroundColour: application.NewRGB(13, 22, 34),
	})
	window.RegisterHook(events.Mac.WindowShouldClose, func(event *application.WindowEvent) {
		window.Hide()
		event.Cancel()
	})
	tray := wails.SystemTray.New()
	tray.SetLabel("Vela")
	menu := wails.NewMenu()
	menu.Add("打开 Vela").OnClick(func(_ *application.Context) { window.Show(); window.Focus() })
	systemProxyMenuItem = menu.AddCheckbox("系统代理", false)
	systemProxyMenuItem.OnClick(func(_ *application.Context) {
		state, _ := runner.SetSystemProxy(!runner.Snapshot().SystemProxyEnabled)
		systemProxyMenuItem.SetChecked(state.SystemProxyEnabled)
	})
	menu.AddSeparator()
	menu.Add("退出 Vela").OnClick(func(_ *application.Context) { wails.Quit() })
	tray.SetMenu(menu)
	go func() {
		for range menuStateUpdates {
			systemProxyMenuItem.SetChecked(runner.Snapshot().SystemProxyEnabled)
		}
	}()
	return wails.Run()
}

func findBinary() string {
	if path := os.Getenv("VELA_MIHOMO_PATH"); path != "" {
		if validBinary(path) {
			return path
		}
		return ""
	}
	self, err := os.Executable()
	if err == nil {
		path := filepath.Clean(filepath.Join(filepath.Dir(self), "..", "Resources", "mihomo"))
		if validBinary(path) {
			return path
		}
	}
	path := filepath.Join("build", "resources", "mihomo")
	if validBinary(path) {
		return path
	}
	return ""
}

func validBinary(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0111 != 0
}
