package mihomo

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mingo-liu/vela/internal/profile"
)

func TestStartupRejectsOccupiedMixedPortBeforeLaunching(t *testing.T) {
	for _, network := range []string{"tcp", "udp"} {
		for _, tun := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/tun=%t", network, tun), func(t *testing.T) {
				var port int
				if network == "tcp" {
					listener, err := net.Listen("tcp", "127.0.0.1:0")
					if err != nil {
						t.Fatal(err)
					}
					defer listener.Close()
					port = listener.Addr().(*net.TCPAddr).Port
				} else {
					listener, err := net.ListenPacket("udp", "127.0.0.1:0")
					if err != nil {
						t.Fatal(err)
					}
					defer listener.Close()
					port = listener.LocalAddr().(*net.UDPAddr).Port
				}
				dir := t.TempDir()
				store := profile.NewStore(dir)
				if err := store.Import("rules: ['MATCH,DIRECT']\n"); err != nil {
					t.Fatal(err)
				}
				binary := filepath.Join(dir, "fake-core")
				if err := os.WriteFile(binary, []byte("#!/bin/sh\ntouch \"$2/launched\"\n"), 0700); err != nil {
					t.Fatal(err)
				}
				proxy := &testSystemProxy{}
				runner := NewRunner(store, profile.NewSubscriptions(store, &testURLStore{}), dir, binary, port, proxy, nil)
				t.Cleanup(runner.Close)
				launcherCalled := false
				runner.SetTunLauncher(func(context.Context, string, string) (*exec.Cmd, error) {
					launcherCalled = true
					return exec.Command(binary), nil
				})
				started := time.Now()
				var state State
				var err error
				if tun {
					state, err = runner.SetTun(true)
				} else {
					state, err = runner.SetSystemProxy(true)
				}
				if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("本地代理端口 %d 不可用", port)) || strings.Contains(err.Error(), "超时") {
					t.Fatalf("unexpected port conflict: %+v, %v", state, err)
				}
				if time.Since(started) > time.Second || runner.cmd != nil || launcherCalled || proxy.enabled || state.Status != "failed" || state.SystemProxyEnabled || state.TunEnabled {
					t.Fatalf("occupied port started a connection: %+v", state)
				}
				for _, name := range []string{"runtime.yaml", "launched"} {
					if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
						t.Fatalf("startup wrote %s before port validation: %v", name, err)
					}
				}
			})
		}
	}
}

func TestStartupLogDetailKeepsListenerFailureBeforeProviderInfo(t *testing.T) {
	const failure = `level=error msg="Start Mixed(http+socks) server error: listen tcp 127.0.0.1:7890: bind: address already in use"`
	output := "level=info msg=starting\n" + failure + "\n" + strings.Repeat("level=info msg=provider-initialized\n", 25)
	if detail := startupLogDetail(output); detail != failure {
		t.Fatalf("listener failure hidden by info logs: %q", detail)
	}
	if detail := startupLogDetail("plain configuration error\n"); detail != "plain configuration error" {
		t.Fatalf("plain error: %q", detail)
	}
	if detail := startupLogDetail(strings.Repeat("x", 600)); len(detail) != 500 {
		t.Fatalf("unbounded detail: %d bytes", len(detail))
	}
}
