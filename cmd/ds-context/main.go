// Command ds-context 是 ds-context 网关与其内嵌控制台的单二进制入口。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"ds-context/pkg/admin"
	"ds-context/pkg/config"
	"ds-context/pkg/server"
)

// shutdownTimeout 是优雅退出时等待在途请求完成的上限。
const shutdownTimeout = 10 * time.Second

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ds-context:", err)
		os.Exit(1)
	}
}

// run 完成装配并阻塞至收到退出信号。
func run() error {
	listen := flag.String("listen", "127.0.0.1:8080", "监听地址(默认仅本机)")
	dataDir := flag.String("data-dir", defaultDataDir(), "数据目录(SQLite 等)")
	password := flag.String("admin-password", os.Getenv("DS_CONTEXT_ADMIN_PASSWORD"),
		"控制台管理员口令(或环境变量 DS_CONTEXT_ADMIN_PASSWORD)")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	store, err := config.Open(filepath.Join(*dataDir, "ds-context.db"))
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
		Addr:              *listen,
		Handler:           server.New(chainHandler, logger).WithConsole(console.Handler()).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		logger.Info("ds-context 网关已启动", "listen", *listen, "data_dir", *dataDir)
		if serr := srv.ListenAndServe(); serr != nil && !errors.Is(serr, http.ErrServerClosed) {
			errCh <- serr
		}
	}()

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
	logger.Info("ds-context 网关已停止")
	return nil
}

// defaultDataDir 返回默认数据目录(~/.ds-context)。
func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".ds-context"
	}
	return filepath.Join(home, ".ds-context")
}
