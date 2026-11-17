package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

//go:embed all:web/dist
var webFS embed.FS

type Config struct {
	Port string
	// OutputDir 构建产物（APK、build-info.json），默认 exe 同级 output/
	OutputDir string
	// CacheDir 可再生的临时数据（上传包、容器 config 镜像、回收站），默认 exe 同级 cache/
	CacheDir string
	// DataDir 需要长期保留的状态（证书、jobs.json、docker.json、图标、启动图），默认 exe 同级 data/
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

// defaultConfig 以 exe 所在目录为根布置三个同级目录。
//
// 之所以要分开（而不是都塞进 data/）：
//   - output/ 是用户真正要拿的东西，点「打开文件夹」时一眼能看到 APK
//   - cache/ 可以整个删掉而不会丢配置，出问题让用户删它最省事
//   - data/  存证书和任务记录，删了要重新配，不能混在缓存里被误删
func defaultConfig(base string) Config {
	cfg := Config{
		Port: envOr("PORT", "8080"),
		// 三个目录都允许用环境变量覆盖，方便老用户沿用原来的 DATA_DIR
		OutputDir:   envOr("OUTPUT_DIR", filepath.Join(base, "output")),
		CacheDir:    envOr("CACHE_DIR", filepath.Join(base, "cache")),
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
	// 老版本把证书/记录放在 exe 上一级的 data/（当时 exe 在子目录里跑）。
	// 没显式指定 DATA_DIR 时，如果新位置还是空的、而旧位置有数据，就沿用旧的，
	// 避免升级后"证书和任务记录凭空消失"。
	if os.Getenv("DATA_DIR") == "" {
		cfg.DataDir = adoptLegacyDataDir(base, cfg.DataDir)
	}
	return cfg
}

// adoptLegacyDataDir 找一个含真实数据的旧 data/ 目录。
// 返回空字符串表示新位置可以直接用。
func adoptLegacyDataDir(base, current string) string {
	// 新位置已经有数据 → 用它，不要乱动
	if hasUserData(current) {
		return current
	}
	// 候选：exe 上一级的 data/（旧版结构的典型位置）
	parent := filepath.Dir(base)
	if parent == base || parent == "." || parent == string(filepath.Separator) {
		return current
	}
	legacy := filepath.Join(parent, "data")
	if legacy == current {
		return current
	}
	if hasUserData(legacy) {
		log.Printf("检测到旧版数据目录 %s（含证书/记录），已沿用它", legacy)
		return legacy
	}
	return current
}

// hasUserData 判断目录里是否有值得保留的东西（证书或任务记录）
func hasUserData(dir string) bool {
	if dir == "" {
		return false
	}
	for _, p := range []string{
		filepath.Join(dir, "keystores", "index.json"),
		filepath.Join(dir, "jobs.json"),
		filepath.Join(dir, "appkeys.json"),
	} {
		if st, err := os.Stat(p); err == nil && st.Size() > 2 {
			return true
		}
	}
	return false
}

// newServer 组装好完整的 HTTP 服务（路由、静态资源、清理）。
//
// 抽成独立函数是为了能被托盘宿主进程内直接调用——发布版把服务和托盘合成
// 一个 exe，不再需要拉起子进程，也就不会有「找不到服务程序」这类问题。
func newServer(cfg Config) (*http.Server, error) {
	store := NewStore(cfg)
	if err := store.ensureDirs(); err != nil {
		return nil, fmt.Errorf("初始化数据目录失败: %w", err)
	}
	// 页面切换过的 Docker 端点优先于环境变量（换机器不用改 start.bat 重启）
	if saved, found := store.loadDockerPrefs(); found {
		if saved == "" {
			log.Printf("按保存的配置使用本机 Docker")
		} else {
			log.Printf("已加载保存的 Docker 地址: %s", saved)
		}
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
	mux.HandleFunc("GET /api/android/defaults", api.androidDefaults)
	mux.HandleFunc("GET /api/keystores", api.listKeystores)
	mux.HandleFunc("PUT /api/keystore", api.putKeystore)
	mux.HandleFunc("DELETE /api/keystores/{id}", api.delKeystore)

	mux.HandleFunc("GET /api/runtime", api.runtimeStatus)
	mux.HandleFunc("POST /api/runtime/{action}", api.runtimeAction)

	mux.HandleFunc("GET /api/docker/host", api.dockerHostGet)
	mux.HandleFunc("POST /api/docker/host", api.dockerHostSet)
	mux.HandleFunc("POST /api/docker/test", api.dockerHostTest)

	mux.HandleFunc("POST /api/build", api.createJob)
	mux.HandleFunc("GET /api/jobs", api.listJobs)
	mux.HandleFunc("GET /api/jobs/{id}/events", api.jobEvents)
	mux.HandleFunc("DELETE /api/jobs/{id}", api.delJob)
	mux.HandleFunc("POST /api/jobs/{id}/retry", api.retryJob)
	mux.HandleFunc("GET /api/jobs/{id}/log", api.jobLog)
	mux.HandleFunc("GET /api/jobs/{id}/zip", api.downloadAll)
	mux.HandleFunc("POST /api/jobs/{id}/reveal", api.revealOutput)

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
	return srv, nil
}

func mustSub() fs.FS {
	sub, err := fs.Sub(webFS, "web/dist")
	if err != nil {
		// 嵌入资源缺失是打包错误，属于不可恢复；但这里仍 return nil 让调用方崩在明确的位置
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
	fmt.Printf("    APK 产物 : %s\n", cfg.OutputDir)
	fmt.Printf("    缓存目录 : %s\n", cfg.CacheDir)
	fmt.Printf("    数据目录 : %s\n", cfg.DataDir)
	fmt.Printf("    Docker   : %s\n", dockerBanner())
	fmt.Println("   ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("    提示：其他电脑打不开时，检查 Windows 防火墙是否放行该端口")
	fmt.Println()
}

// dockerBanner 说明当前 docker 命令连的是本机还是远端 daemon。
// 用 dockerEndpoint() 而不是直接读环境变量：页面切换过的地址也存在 data/docker.json 里，
// 只读 env 会在横幅上显示与实际不符的地址。
func dockerBanner() string {
	h := dockerEndpoint()
	if h == "" || h == "local" || strings.HasPrefix(h, "local（") {
		return "本地 daemon"
	}
	return h + "（远程）"
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
