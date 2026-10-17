package app

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"testing/fstest"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestSecondLaunchShowsExistingWindow(t *testing.T) {
	shown := false
	options := desktopApplicationOptions(fstest.MapFS{}, func() {}, func() { shown = true })
	options.SingleInstance.OnSecondInstanceLaunch(application.SecondInstanceData{})
	if !shown {
		t.Fatal("second launch did not invoke window activation")
	}
}

// Exercise the real Wails process lock without starting a window or touching
// application data. Exiting without cleanup also tests lock recovery after a crash.
func TestMain(m *testing.M) {
	if id := os.Getenv("VELA_TEST_INSTANCE_ID"); id != "" {
		options := desktopApplicationOptions(fstest.MapFS{}, func() {}, func() {})
		options.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
		options.SingleInstance.UniqueID = id
		options.SingleInstance.ExitCode = 23
		application.New(options)
		fmt.Println("instance-ready")
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestSingleInstanceProcess(t *testing.T) {
	id := "local.vela.test." + strconv.Itoa(os.Getpid())
	t.Cleanup(func() { _ = os.Remove(filepath.Join(os.TempDir(), id+".lock")) })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	start := func() (*exec.Cmd, io.WriteCloser) {
		t.Helper()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSingleInstanceProcess$")
		cmd.Env = append(os.Environ(), "VELA_TEST_INSTANCE_ID="+id)
		input, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		output, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { input.Close(); _ = cmd.Process.Kill() })
		lines := bufio.NewScanner(output)
		if !lines.Scan() || lines.Text() != "instance-ready" {
			t.Fatalf("first instance failed to claim lock: %s", lines.Text())
		}
		return cmd, input
	}
	first, input := start()
	duplicate := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSingleInstanceProcess$")
	duplicate.Env = append(os.Environ(), "VELA_TEST_INSTANCE_ID="+id)
	output, err := duplicate.CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 23 || len(output) != 0 {
		t.Fatalf("second instance continued initialization: %s, %v", output, err)
	}
	input.Close()
	if err := first.Wait(); err != nil {
		t.Fatal(err)
	}
	restarted, input := start()
	input.Close()
	if err := restarted.Wait(); err != nil {
		t.Fatalf("stale lock prevented restart: %v", err)
	}
}
