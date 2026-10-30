package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// TestStaticHandlerCacheHeaders 覆盖缓存策略:
// index.html 不带内容哈希,必须 no-cache(否则浏览器缓存旧的 HTML,升级后仍
// 引用已不存在的旧 JS,界面出现"改了没生效");assets 名含哈希,可长期缓存。
func TestStaticHandlerCacheHeaders(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":             &fstest.MapFile{Data: []byte("<!doctype html><html></html>")},
		"assets/index-abc123.js": &fstest.MapFile{Data: []byte("console.log(1)")},
	}
	h := newStaticHandler(fsys)

	// 首页与 SPA 回退:no-cache。
	for _, path := range []string{"/console/", "/console/", "/console/some/spa/route"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
		if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
			t.Errorf("%s 的 Cache-Control = %q, want no-cache", path, got)
		}
	}

	// 内容哈希资源:immutable。
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/console/assets/index-abc123.js", nil))
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Errorf("assets 的 Cache-Control = %q, want 含 immutable", got)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("assets 状态 = %d, want 200", rec.Code)
	}
}
