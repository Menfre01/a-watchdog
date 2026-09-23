package admin

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
)

// distFS 内嵌控制台前端构建产物目录;目录需存在(仓库内保留占位文件),
// 否则 go:embed 在编译期即失败。真实产物由 make build 生成。
//
//go:embed all:dist
var distFS embed.FS

// staticHandler 提供内嵌前端:命中文件则返回;其余路径回退 index.html(SPA);
// 产物未构建(无 index.html)时返回可读提示页。
func (a *Admin) staticHandler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	}
	return newStaticHandler(sub)
}

// newStaticHandler 在给定的只读文件系统上提供控制台静态服务(可注入,便于覆盖各分支)。
func newStaticHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(r.URL.Path, "/console")
		if rel == "" || rel == "/" || !fileExists(fsys, rel) {
			if !fileExists(fsys, "/index.html") {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprintln(w, "控制台前端产物未构建:请运行 make build(或在 web/ 下执行 pnpm build)")
				return
			}
			// SPA 路由回退到 index.html。
			clone := r.Clone(r.Context())
			clone.URL.Path = "/"
			fileServer.ServeHTTP(w, clone)
			return
		}
		clone := r.Clone(r.Context())
		clone.URL.Path = rel
		fileServer.ServeHTTP(w, clone)
	})
}

// fileExists 判断内嵌文件系统中是否存在该文件(目录不算)。
func fileExists(fsys fs.FS, name string) bool {
	f, err := fsys.Open(strings.TrimPrefix(name, "/"))
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	return err == nil && !info.IsDir()
}
