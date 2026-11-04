// Command awatchdog 是 AWatchDog 网关与其内嵌控制台的单二进制入口。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"awatchdog/pkg/admin"
	"awatchdog/pkg/config"
	"awatchdog/pkg/server"
)

// shutdownTimeout 是优雅退出时等待在途请求完成的上限。
const shutdownTimeout = 10 * time.Second

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "awatchdog:", err)
		os.Exit(1)
	}
}

// run 完成装配并阻塞至收到退出信号。
func run() error {
	// 默认端口避开常见开发工具占用的 8080:端口被占时,loopback 的另一侧
	// (或 IPv6 侧)会把请求交给占用者,表现为莫名的 404 而非连接失败。
	listen := flag.String("listen", "127.0.0.1:8399", "监听地址(默认仅本机;loopback 会同时监听 IPv4 与 IPv6)")
	dataDir := flag.String("data-dir", defaultDataDir(), "数据目录(SQLite 等)")
	password := flag.String("admin-password", os.Getenv("AWATCHDOG_ADMIN_PASSWORD"),
		"控制台管理员口令(或环境变量 AWATCHDOG_ADMIN_PASSWORD)")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	store, err := config.Open(filepath.Join(*dataDir, "awatchdog.db"))
	if err != nil {
		return err
	}
	defer func() {
		if cerr := store.Close(); cerr != nil {
			logger.Error("关闭配置存储失败", "err", cerr)
		}
	}()

	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	chainHandler, _, err := server.Wire(rootCtx, store, logger)
	if err != nil {
		return fmt.Errorf("wire gateway: %w", err)
	}

	if *password == "" {
		logger.Warn("未设置控制台管理员口令(--admin-password),控制台登录不可用")
	}
	console, err := admin.New(store, *password, logger)
	if err != nil {
		return fmt.Errorf("admin console: %w", err)
	}

	srv := &http.Server{
		Handler:           server.New(chainHandler, logger).WithConsole(console.Handler()).WithToolEvents(store).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	// 双栈 loopback:浏览器把 localhost 优先解析为 IPv6 ::1,只绑 IPv4 会让
	// http://localhost:<port> 落到占用同一端口的其他服务上。
	listeners, err := listenLoopback(rootCtx, *listen)
	if err != nil {
		return err
	}
	errCh := make(chan error, len(listeners))
	for _, ln := range listeners {
		logger.Info("AWatchDog 网关已启动", "listen", ln.Addr().String(), "data_dir", *dataDir)
		go func(l net.Listener) {
			if serr := srv.Serve(l); serr != nil && !errors.Is(serr, http.ErrServerClosed) {
				errCh <- serr
			}
		}(ln)
	}
	logger.Info("控制台入口", "url", consoleURL(listeners[0].Addr()))

	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case <-rootCtx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	logger.Info("AWatchDog 网关已停止")
	return nil
}

// defaultDataDir 返回默认数据目录(~/.awatchdog)。
func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".awatchdog"
	}
	return filepath.Join(home, ".awatchdog")
}

// listenLoopback 在展开出的地址上逐个监听。
// loopback 写法(127.0.0.1 / ::1 / localhost)会展开为 IPv4 与 IPv6 两侧;
// 系统禁用 IPv6 等情况跳过该侧即可,只要有一侧成功就不算失败。
func listenLoopback(ctx context.Context, addr string) ([]net.Listener, error) {
	var (
		listeners []net.Listener
		lastErr   error
		lc        net.ListenConfig
	)
	for _, target := range expandLoopback(addr) {
		ln, err := lc.Listen(ctx, "tcp", target)
		if err != nil {
			lastErr = err
			continue
		}
		listeners = append(listeners, ln)
	}
	if len(listeners) == 0 {
		return nil, fmt.Errorf("listen %s: %w", addr, lastErr)
	}
	return listeners, nil
}

// expandLoopback 把 loopback 地址展开为 IPv4 与 IPv6 两侧,其余地址原样返回。
// 浏览器把 localhost 优先解析为 ::1,只绑一侧会让另一种写法连不上。
func expandLoopback(addr string) []string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return []string{addr}
	}
	switch host {
	case "127.0.0.1", "localhost", "::1":
		return []string{
			net.JoinHostPort("127.0.0.1", port),
			net.JoinHostPort("::1", port),
		}
	default:
		return []string{addr}
	}
}

// consoleURL 依据实际监听地址给出控制台入口(便于日志里直接点开)。
func consoleURL(addr net.Addr) string {
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return "http://127.0.0.1/console/"
	}
	return fmt.Sprintf("http://%s:%s/console/", host, port)
}
