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
	runner := mihomo.NewRunner(store, subs, dataDir, binary, 7890, macos.NewSystemProxy(dataDir), func(state mihomo.State) {
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
	menu.Add("启动本地代理").OnClick(func(_ *application.Context) { _, _ = runner.Start() })
	menu.Add("停止本地代理").OnClick(func(_ *application.Context) { _, _ = runner.Stop() })
	menu.Add("开启系统代理").OnClick(func(_ *application.Context) { _, _ = runner.SetSystemProxy(true) })
	menu.Add("关闭系统代理").OnClick(func(_ *application.Context) { _, _ = runner.SetSystemProxy(false) })
	menu.AddSeparator()
	menu.Add("退出 Vela").OnClick(func(_ *application.Context) { wails.Quit() })
	tray.SetMenu(menu)
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
