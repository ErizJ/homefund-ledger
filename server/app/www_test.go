package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSpaHosting 覆盖桌面版静态托管路径：文件按路径返回、SPA 回退、注入桌面防护脚本、/api 404 隔离
func TestSpaHosting(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(`<!doctype html><html><head><title>x</title></head><body>app</body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := buildRouter(os.DirFS(dir))

	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	// 根路径返回 index.html 且注入桌面防护脚本（屏蔽 F5/Ctrl+R）
	if w := get("/"); w.Code != 200 || !strings.Contains(w.Body.String(), "__VFUND_DESKTOP__") {
		t.Fatalf("GET / 应返回注入防护脚本的 index.html，got %d %q", w.Code, w.Body.String())
	}
	// 静态资源按路径返回
	if w := get("/assets/app.js"); w.Code != 200 || w.Body.String() != "console.log(1)" {
		t.Fatalf("GET /assets/app.js 应返回文件内容，got %d", w.Code)
	}
	// 前端路由（不存在的路径）回退 index.html
	if w := get("/gl-statements"); w.Code != 200 || !strings.Contains(w.Body.String(), "app") {
		t.Fatalf("前端路由应回退 index.html，got %d", w.Code)
	}
	// 未知 /api 保持 404 JSON（不被 SPA 回退吞掉）
	if w := get("/api/no-such-endpoint"); w.Code != 404 || !strings.Contains(w.Body.String(), "接口不存在") {
		t.Fatalf("未知 /api 应 404 JSON，got %d %q", w.Code, w.Body.String())
	}
	// 非 GET 的未知路径 404
	req := httptest.NewRequest(http.MethodPost, "/whatever", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatalf("非 GET 未知路径应 404，got %d", w.Code)
	}
}

// TestRunWithoutWWW 开发模式（www=nil）不挂 SPA 托管，未知路径仍 404
func TestRunWithoutWWW(t *testing.T) {
	r := buildRouter(nil)
	req := httptest.NewRequest(http.MethodGet, "/gl-statements", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatalf("www=nil 时不应有 SPA 回退，got %d", w.Code)
	}
}
