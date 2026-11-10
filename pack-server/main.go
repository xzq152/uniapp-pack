package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

//go:embed all:web/dist
var webFS embed.FS

type Config struct {
	Port        string
	DataDir     string
	Image       string
	GradleVol   string
	PnpmVol     string
	UseCNMirror string
	GradleArgs  string
	Password    string
	KeepAPKs    int
	KeepJobs    int
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	exeDir, _ := os.Executable()
	base := filepath.Dir(exeDir)

	cfg := Config{
		Port:        envOr("PORT", "8080"),
		DataDir:     envOr("DATA_DIR", filepath.Join(base, "data")),
		Image:       envOr("IMAGE", "uniapp-android-builder:latest"),
		GradleVol:   envOr("GRADLE_VOLUME", "uniapp_gradle_cache"),
		PnpmVol:     envOr("PNPM_VOLUME", "uniapp_pnpm_store"),
		UseCNMirror: envOr("USE_CN_MIRROR", "1"),
		GradleArgs:  envOr("GRADLE_EXTRA_ARGS", ""),
		Password:    envOr("ACCESS_PASSWORD", ""),
		KeepAPKs:    envInt("KEEP_APK", 5),
		KeepJobs:    envInt("KEEP_JOBS", 20),
	}

	store := NewStore(cfg)
	if err := store.ensureDirs(); err != nil {
		log.Fatalf("初始化数据目录失败: %v", err)
	}
	// 启动时清掉没人用的上传包（构建成功后会立刻删，这里兜底处理历史遗留和孤儿）
	if n, freed := store.PruneUploads(); n > 0 {
		log.Printf("已清理 %d 份未使用的上传包，释放 %.1f MB", n, float64(freed)/1024/1024)
	}
	if n, freed := store.PruneOutputs(cfg.KeepAPKs); n > 0 {
		log.Printf("已清理 %d 个旧 APK，释放 %.1f MB（保留最近 %d 次）", n, float64(freed)/1024/1024, cfg.KeepAPKs)
	}
	if n := store.PruneJobs(cfg.KeepJobs); n > 0 {
		log.Printf("已清理 %d 条旧记录（只保留最近 %d 条，旧记录在回收站保留 7 天）", n, cfg.KeepJobs)
	}

	go store.worker()

	api := &API{cfg: cfg, store: store}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/login", api.login)
	mux.HandleFunc("POST /api/logout", api.logout)

	dist := mustSub()
	files := http.FileServer(http.FS(dist))
	mux.Handle("GET /assets/", cacheImmutable(files))
	mux.HandleFunc("GET /", spaHandler(dist, files))

	mux.HandleFunc("PUT /api/upload", api.upload)
	mux.HandleFunc("PUT /api/upload-folder", api.uploadFolder)
	mux.HandleFunc("PUT /api/icon", api.putIcon)
	mux.HandleFunc("PUT /api/splash", api.putSplash)
	mux.HandleFunc("GET /api/appkey", api.getAppKey)
	mux.HandleFunc("GET /api/keystores", api.listKeystores)
	mux.HandleFunc("PUT /api/keystore", api.putKeystore)
	mux.HandleFunc("DELETE /api/keystores/{id}", api.delKeystore)

	mux.HandleFunc("GET /api/runtime", api.runtimeStatus)
	mux.HandleFunc("POST /api/runtime/{action}", api.runtimeAction)

	mux.HandleFunc("POST /api/build", api.createJob)
	mux.HandleFunc("GET /api/jobs", api.listJobs)
	mux.HandleFunc("GET /api/jobs/{id}/events", api.jobEvents)
	mux.HandleFunc("DELETE /api/jobs/{id}", api.delJob)
	mux.HandleFunc("POST /api/jobs/{id}/retry", api.retryJob)
	mux.HandleFunc("GET /api/jobs/{id}/log", api.jobLog)
	mux.HandleFunc("GET /api/jobs/{id}/zip", api.downloadAll)

	mux.HandleFunc("GET /api/trash", api.listTrash)
	mux.HandleFunc("POST /api/trash/{id}/restore", api.restoreTrash)
	mux.HandleFunc("DELETE /api/trash/{id}", api.purgeTrash)
	mux.HandleFunc("GET /api/env", api.env)

	mux.HandleFunc("GET /d/{jobid}/{name}", api.download)
	mux.HandleFunc("HEAD /d/{jobid}/{name}", api.download)

	srv := &http.Server{
		Addr:         "0.0.0.0:" + cfg.Port,
		Handler:      api.authMiddleware(mux),
		ReadTimeout:  0, // 上传大包不限时
		WriteTimeout: 0, // 下载大包 / SSE 不限时
		IdleTimeout:  120 * time.Second,
	}

	printBanner(cfg)
	log.Fatal(srv.ListenAndServe())
}

func mustSub() fs.FS {
	sub, err := fs.Sub(webFS, "web/dist")
	if err != nil {
		log.Fatalf("嵌入资源错误: %v", err)
	}
	return sub
}

// cacheImmutable 给带 hash 的静态资源加长缓存
func cacheImmutable(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		next.ServeHTTP(w, r)
	})
}

// spaHandler 单页应用：静态文件优先，其余路径回落到 index.html
func spaHandler(dist fs.FS, files http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" && p != "login" {
			if f, err := dist.Open(p); err == nil {
				f.Close()
				files.ServeHTTP(w, r)
				return
			}
		}
		b, err := fs.ReadFile(dist, "index.html")
		if err != nil {
			http.Error(w, "页面资源缺失", 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(b)
	}
}

func printBanner(cfg Config) {
	ips := lanIPs()
	fmt.Println()
	fmt.Println("   ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("    uni-app 安卓打包服务已启动")
	fmt.Println("   ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("    本机访问 : http://127.0.0.1:%s\n", cfg.Port)
	for _, ip := range ips {
		fmt.Printf("    局域网访问: http://%s:%s\n", ip, cfg.Port)
	}
	if len(ips) == 0 {
		fmt.Println("    未检测到局域网 IP，请检查网络连接")
	}
	fmt.Printf("    打包镜像 : %s\n", cfg.Image)
	fmt.Printf("    数据目录 : %s\n", cfg.DataDir)
	fmt.Println("   ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("    提示：其他电脑打不开时，检查 Windows 防火墙是否放行该端口")
	fmt.Println()
}

func lanIPs() []string {
	var out []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		if v4 := ipnet.IP.To4(); v4 != nil {
			out = append(out, v4.String())
		}
	}
	return out
}

func execOut(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.Output()
}
