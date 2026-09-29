//go:build darwin

package macos

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const tunSocket = tunInstallDir + "/service.sock"

type tunRequest struct {
	DataDir    string `json:"dataDir"`
	ConfigPath string `json:"configPath"`
	StopPath   string `json:"stopPath"`
}

func serviceAvailable() bool {
	conn, err := net.DialTimeout("unix", tunSocket, 300*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// RunTunClient keeps one connection to the privileged service for a TUN session.
func RunTunClient(args []string) error {
	return runTunClient(args, tunSocket)
}

func runTunClient(args []string, socket string) error {
	if len(args) != 3 {
		return errors.New("Tun 客户端参数无效")
	}
	var conn net.Conn
	var err error
	for i := 0; i < 30; i++ {
		conn, err = net.DialTimeout("unix", socket, 300*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		return fmt.Errorf("Tun 服务未就绪: %w", err)
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(tunRequest{args[0], args[1], args[2]}); err != nil {
		return err
	}
	stopWatch := make(chan struct{})
	defer close(stopWatch)
	defer os.Remove(args[2])
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopWatch:
				return
			case <-ticker.C:
				if _, err := os.Stat(args[2]); err == nil {
					if unixConn, ok := conn.(*net.UnixConn); ok {
						_ = unixConn.CloseWrite()
					}
					return
				}
			}
		}
	}()
	reader := bufio.NewReader(conn)
	first, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("Tun 服务未响应: %w", err)
	}
	if first != "READY\n" {
		return errors.New(strings.TrimSpace(first))
	}
	result, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("Tun 服务连接中断: %w", err)
	}
	if result != "STOP\n" {
		return errors.New(strings.TrimSpace(result))
	}
	return nil
}

// RunTunService is the launchd-owned root process. The socket is limited to
// the user who authorized installation, and every request checks peer credentials.
func RunTunService() error {
	if os.Geteuid() != 0 || os.Getuid() != 0 {
		return errors.New("Tun 服务必须由 launchd 以 root 启动")
	}
	self, err := os.Executable()
	if err != nil || filepath.Clean(self) != filepath.Join(tunInstallDir, "service") {
		return errors.New("Tun 服务安装位置无效")
	}
	ownerBytes, err := os.ReadFile(filepath.Join(tunInstallDir, "owner-uid"))
	if err != nil {
		return err
	}
	uid, err := strconv.Atoi(strings.TrimSpace(string(ownerBytes)))
	if err != nil || uid <= 0 {
		return errors.New("Tun 服务授权用户无效")
	}
	if info, err := os.Lstat(tunSocket); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return errors.New("Tun 服务套接字路径被占用")
		}
		if err := os.Remove(tunSocket); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	listener, err := net.Listen("unix", tunSocket)
	if err != nil {
		return err
	}
	defer listener.Close()
	defer os.Remove(tunSocket)
	if err := os.Chown(tunSocket, uid, 0); err != nil {
		return err
	}
	if err := os.Chmod(tunSocket, 0600); err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go func() {
		<-ctx.Done()
		listener.Close()
	}()
	active := make(chan struct{}, 1)
	var sessions sync.WaitGroup
	for {
		conn, err := listener.Accept()
		if err != nil {
			stopping := ctx.Err() != nil
			cancel()
			sessions.Wait()
			if stopping {
				return nil
			}
			return err
		}
		sessions.Add(1)
		go func() {
			defer sessions.Done()
			defer conn.Close()
			peer, err := tunPeerUID(conn)
			if err != nil || peer != uid {
				fmt.Fprintln(conn, "Tun 服务拒绝未授权用户")
				return
			}
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			var request tunRequest
			if err := json.NewDecoder(io.LimitReader(conn, 8192)).Decode(&request); err != nil {
				return // A connection-only health probe.
			}
			conn.SetReadDeadline(time.Time{})
			select {
			case active <- struct{}{}:
				defer func() { <-active }()
			default:
				fmt.Fprintln(conn, "已有 Tun 会话正在运行")
				return
			}
			if err := runTunSession(ctx, conn, uid, request); err != nil {
				fmt.Fprintln(conn, "Tun 服务错误:", err)
			}
		}()
	}
}

func tunPeerUID(conn net.Conn) (int, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, errors.New("Tun 服务连接类型无效")
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *unix.Xucred
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		cred, controlErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	}); err != nil {
		return 0, err
	}
	if controlErr != nil {
		return 0, controlErr
	}
	return int(cred.Uid), nil
}

func runTunSession(ctx context.Context, conn net.Conn, uid int, request tunRequest) error {
	compiled, err := validateTunConfig(uid, request.DataDir, request.ConfigPath, request.StopPath)
	if err != nil {
		return err
	}
	// No root process ever uses a user-writable directory for runtime assets.
	sessionDir, err := os.MkdirTemp(tunInstallDir, ".session-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(sessionDir)
	dataDir, err := privateTunData(uid)
	if err != nil {
		return err
	}
	control, err := newTunControl(compiled, sessionDir)
	if err != nil {
		return err
	}
	defer control.client.CloseIdleConnections()
	privateConfig, err := control.compile(compiled)
	if err != nil {
		return err
	}
	source, err := openTunData(uid)
	if err != nil {
		return err
	}
	privateConfig, err = snapshotTunResources(privateConfig, source, dataDir)
	defer source.Close()
	control.prepare = func(data []byte) ([]byte, error) { return snapshotTunResources(data, source, dataDir) }
	if err != nil {
		return err
	}
	// The database is installed and hashed alongside the helper, never read from
	// a user-controlled path by the privileged core.
	database, err := os.ReadFile(filepath.Join(tunInstallDir, "Country.mmdb"))
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dataDir, "Country.mmdb"), database, 0600); err != nil {
		return err
	}
	geosite, err := os.ReadFile(filepath.Join(tunInstallDir, "geosite.dat"))
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dataDir, "geosite.dat"), geosite, 0600); err != nil {
		return err
	}
	configPath := filepath.Join(sessionDir, "runtime.yaml")
	if err := os.WriteFile(configPath, privateConfig, 0600); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", control.port))
	if err != nil {
		return err
	}
	server := &http.Server{Handler: control, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 25 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 8192}
	defer server.Close()
	go func() { _ = server.Serve(listener) }()
	cmd := exec.Command(filepath.Join(tunInstallDir, "mihomo"), "-d", dataDir, "-f", configPath)
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "HOME=/var/root"}
	logs := &tunLog{}
	cmd.Stdout, cmd.Stderr = logs, logs
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 Tun 内核失败: %w", err)
	}
	fmt.Fprintln(conn, "READY")
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	closed := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, conn)
		close(closed)
	}()
	for {
		select {
		case <-ctx.Done():
			stopTunProcess(cmd, done)
			fmt.Fprintln(conn, "STOP")
			return nil
		case err := <-done:
			if err != nil {
				return fmt.Errorf("Tun 内核退出: %w；%s", err, strings.TrimSpace(logs.String()))
			}
			fmt.Fprintln(conn, "STOP")
			return nil
		case <-closed:
			stopTunProcess(cmd, done)
			fmt.Fprintln(conn, "STOP")
			return nil
		}
	}
}

func stopTunProcess(cmd *exec.Cmd, done <-chan error) {
	_ = cmd.Process.Signal(os.Interrupt)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
}

// Keep provider caches and protocol state across reconnects, under root ownership.
func privateTunData(uid int) (string, error) {
	path := filepath.Join(tunInstallDir, fmt.Sprintf("data-%d", uid))
	if err := os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode().Perm() != 0700 || !ok || stat.Uid != 0 {
		return "", errors.New("Tun 私有数据目录权限无效")
	}
	return path, nil
}
