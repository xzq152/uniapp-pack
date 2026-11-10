package main

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type API struct {
	cfg   Config
	store *Store
}

// ---------------------------------------------------------------------------
// 上传：裸流 PUT，不做 multipart / base64，直接落盘（局域网最快路径）
// ---------------------------------------------------------------------------

func (a *API) upload(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		name = "upload.zip"
	}
	name = filepath.Base(name)

	id := fmt.Sprintf("u%d", time.Now().UnixNano())
	dir := a.store.uploadDir(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	dst := filepath.Join(dir, name)

	f, err := os.Create(dst)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	// 1MB 缓冲直写，避免小 buffer 带来的 syscall 开销
	written, err := io.CopyBuffer(f, r.Body, make([]byte, 1024*1024))
	f.Close()
	if err != nil {
		os.RemoveAll(dir)
		http.Error(w, "写入失败: "+err.Error(), 500)
		return
	}

	info := ManifestInfo{}
	iconHint := ""
	if strings.HasSuffix(strings.ToLower(name), ".zip") {
		info = readManifestFromZip(dst)
		iconHint = detectIconHint(dst)
	}

	jsonOK(w, map[string]any{
		"uploadId": id,
		"name":     name,
		"size":     written,
		"manifest": info,
		"iconHint": iconHint,
	})
}

// detectIconHint 看资源包里有没有自带 app_icons，顺便告诉前端最大尺寸那张
func detectIconHint(path string) string {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return ""
	}
	defer zr.Close()
	best, bestSize := "", 0
	for _, f := range zr.File {
		n := f.Name
		if !strings.Contains(n, "static/app_icons/") || !strings.HasSuffix(strings.ToLower(n), ".png") {
			continue
		}
		base := filepath.Base(n)
		dim := 0
		fmt.Sscanf(base, "%dx", &dim)
		if dim > bestSize {
			best, bestSize = base, dim
		}
	}
	return best
}

type ManifestInfo struct {
	AppID       string `json:"appId"`
	Name        string `json:"name"`
	VersionName string `json:"versionName"`
	VersionCode string `json:"versionCode"`
}

func readManifestFromZip(path string) ManifestInfo {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return ManifestInfo{}
	}
	defer zr.Close()

	best := (*zip.File)(nil)
	bestDepth := 99
	for _, f := range zr.File {
		base := filepath.Base(f.Name)
		if base != "manifest.json" {
			continue
		}
		d := strings.Count(strings.Trim(f.Name, "/"), "/")
		if d < bestDepth {
			best, bestDepth = f, d
		}
	}
	if best == nil {
		return ManifestInfo{}
	}
	rc, err := best.Open()
	if err != nil {
		return ManifestInfo{}
	}
	defer rc.Close()

	var raw struct {
		ID          string      `json:"id"`
		Name        string      `json:"name"`
		VersionName string      `json:"versionName"`
		VersionCode interface{} `json:"versionCode"`
	}
	if err := json.NewDecoder(io.LimitReader(rc, 4<<20)).Decode(&raw); err != nil {
		return ManifestInfo{}
	}
	vc := ""
	if raw.VersionCode != nil {
		vc = strings.TrimSpace(fmt.Sprint(raw.VersionCode))
		if vc == "<nil>" {
			vc = ""
		}
	}
	return ManifestInfo{
		AppID:       raw.ID,
		Name:        raw.Name,
		VersionName: raw.VersionName,
		VersionCode: vc,
	}
}

// ---------------------------------------------------------------------------
// 证书
// ---------------------------------------------------------------------------

func (a *API) listKeystores(w http.ResponseWriter, r *http.Request) {
	a.store.mu.Lock()
	out := make([]*Keystore, 0, len(a.store.keystores))
	for _, k := range a.store.keystores {
		out = append(out, k)
	}
	a.store.mu.Unlock()
	jsonOK(w, out)
}

func (a *API) putKeystore(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	name := strings.TrimSpace(q.Get("name"))
	if name == "" {
		name = "证书"
	}
	alias := strings.TrimSpace(q.Get("alias"))
	storePass := q.Get("storePass")
	keyPass := q.Get("keyPass")
	if keyPass == "" {
		keyPass = storePass
	}
	if alias == "" || storePass == "" {
		http.Error(w, "alias 和 storePass 必填", 400)
		return
	}

	id := fmt.Sprintf("k%d", time.Now().UnixNano())
	file := id + ".jks"
	dst := filepath.Join(a.store.keystoreDir(), file)
	f, err := os.Create(dst)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if _, err := io.CopyBuffer(f, r.Body, make([]byte, 1024*1024)); err != nil {
		f.Close()
		http.Error(w, err.Error(), 500)
		return
	}
	f.Close()

	ks := &Keystore{
		ID:        id,
		Name:      name,
		File:      file,
		Alias:     alias,
		StorePass: storePass,
		KeyPass:   keyPass,
		CreatedAt: time.Now().Format("2006-01-02 15:04"),
	}
	a.store.mu.Lock()
	a.store.keystores[id] = ks
	a.store.mu.Unlock()
	a.store.saveKeystores()
	jsonOK(w, ks)
}

func (a *API) delKeystore(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/keystores/")
	a.store.mu.Lock()
	ks := a.store.keystores[id]
	delete(a.store.keystores, id)
	a.store.mu.Unlock()
	if ks != nil {
		os.Remove(filepath.Join(a.store.keystoreDir(), ks.File))
	}
	a.store.saveKeystores()
	jsonOK(w, true)
}

// ---------------------------------------------------------------------------
// 构建任务
// ---------------------------------------------------------------------------

type buildReq struct {
	UploadID    string   `json:"uploadId"`
	AppID       string   `json:"appId"`
	AppName     string   `json:"appName"`
	Package     string   `json:"package"`
	VersionName string   `json:"versionName"`
	VersionCode string   `json:"versionCode"`
	Keystore    string   `json:"keystore"`
	StorePass   string   `json:"storePass"`
	KeyAlias    string   `json:"keyAlias"`
	KeyPass     string   `json:"keyPass"`
	AppKey      string   `json:"appKey"`
	Icon        string   `json:"icon"`
	Splash      string   `json:"splash"`
	Channels    []string `json:"channels"`
}

// sanitizeChannels 规范化渠道名：去空格、只留 [A-Za-z0-9_-]、去重保序，最多 12 个
func sanitizeChannels(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, raw := range in {
		var b strings.Builder
		for _, r := range strings.TrimSpace(raw) {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
				b.WriteRune(r)
			}
		}
		c := b.String()
		if c == "" {
			continue
		}
		key := strings.ToLower(c)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, c)
		if len(out) >= 12 {
			break
		}
	}
	return out
}

func (a *API) createJob(w http.ResponseWriter, r *http.Request) {
	var req buildReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求格式错误: "+err.Error(), 400)
		return
	}
	if req.UploadID == "" {
		http.Error(w, "请先上传资源包", 400)
		return
	}
	if !a.store.runtimeRunning() {
		http.Error(w, "打包环境未启动，请先点击「启动环境」", 409)
		return
	}
	if req.Package == "" {
		http.Error(w, "包名 applicationId 必填", 400)
		return
	}
	if req.VersionName == "" {
		req.VersionName = "1.0.0"
	}
	if req.VersionCode == "" {
		req.VersionCode = "1"
	}
	if req.Keystore == "" {
		req.Keystore = "auto"
	}
	// 渠道名只允许字母数字下划线中划线，去重保序
	channels := sanitizeChannels(req.Channels)

	// 校验证书口令已带齐
	storePass, keyAlias, keyPass := "", "", ""
	if req.Keystore != "auto" {
		a.store.mu.Lock()
		ks := a.store.keystores[req.Keystore]
		a.store.mu.Unlock()
		if ks == nil {
			http.Error(w, "证书不存在", 400)
			return
		}
		storePass, keyAlias, keyPass = ks.StorePass, ks.Alias, ks.KeyPass
	}

	j := &Job{
		ID:          fmt.Sprintf("j%d", time.Now().UnixNano()),
		CreatedAt:   time.Now(),
		Status:      "queued",
		Progress:    0,
		Stage:       "排队中",
		UploadID:    req.UploadID,
		AppID:       req.AppID,
		AppName:     req.AppName,
		Package:     req.Package,
		VersionName: req.VersionName,
		VersionCode: req.VersionCode,
		Keystore:    req.Keystore,
		storePass:   storePass,
		keyAlias:    keyAlias,
		keyPass:     keyPass,
		AppKey:      strings.TrimSpace(req.AppKey),
		Icon:        req.Icon,
		Splash:      req.Splash,
		Channels:    channels,
		subs:        make(map[uint64]chan []byte),
	}
	// 记住 appkey，下次同 appid 自动带出
	a.store.SetAppKey(req.AppID, strings.TrimSpace(req.AppKey))
	if src, err := a.store.pickSource(a.store.uploadDir(req.UploadID)); err == nil {
		j.Source = filepath.Base(src)
	}

	a.store.Add(j)
	a.store.queue <- j
	jsonOK(w, map[string]any{"id": j.ID})
}

func (a *API) listJobs(w http.ResponseWriter, r *http.Request) {
	list := a.store.List()
	// 按创建时间倒序；同一秒内用 ID（含纳秒时间戳）兜底，保证顺序稳定
	sort.Slice(list, func(i, k int) bool {
		if list[i].CreatedAt.Equal(list[k].CreatedAt) {
			return list[i].ID > list[k].ID
		}
		return list[i].CreatedAt.After(list[k].CreatedAt)
	})
	out := make([]JobView, 0, len(list))
	for _, j := range list {
		v := j.Snapshot()
		v.Logs = nil // 列表不带日志，省流量
		v.HasUpload = a.store.uploadExists(v.UploadID)
		if v.APK != "" {
			if _, err := os.Stat(filepath.Join(a.store.outputDir(v.ID), v.APK)); err == nil {
				v.APKExists = true
			}
		}
		for i := range v.APKs {
			if _, err := os.Stat(filepath.Join(a.store.outputDir(v.ID), v.APKs[i].File)); err == nil {
				v.APKs[i].Exists = true
				v.APKExists = true
			}
		}
		out = append(out, v)
	}
	jsonOK(w, out)
}

func (a *API) jobEvents(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/jobs/"), "/events")
	j := a.store.Get(id)
	if j == nil {
		http.NotFound(w, r)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "不支持流式响应", 500)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	flusher.Flush()

	send := func(typ string, data any) {
		b, _ := json.Marshal(map[string]any{"type": typ, "data": data})
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typ, b)
		flusher.Flush()
	}

	snap := j.Snapshot()
	send("snapshot", snap)

	subID, ch := j.Subscribe()
	defer j.Unsubscribe(subID)

	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()

	for {
		select {
		case b := <-ch:
			var ev struct {
				Type string          `json:"type"`
				Data json.RawMessage `json:"data"`
			}
			if json.Unmarshal(b, &ev) == nil {
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, ev.Data)
				flusher.Flush()
			}
		case <-tick.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (a *API) env(w http.ResponseWriter, r *http.Request) {
	total := ""
	if out, err := runOut("docker", "info", "--format", "{{.MemTotal}}"); err == nil {
		if v, e := strconv.ParseFloat(strings.TrimSpace(out), 64); e == nil {
			total = fmt.Sprintf("%.1f GB", v/1024/1024/1024)
		}
	}
	jsonOK(w, map[string]any{
		"image":       a.cfg.Image,
		"dockerMem":   total,
		"gradleArgs":  a.store.gradleArgs(),
		"gradleCache": a.cfg.GradleVol,
		"version":     "1.0.0",
		"auth":        a.cfg.Password != "",
		"keepJobs":    a.cfg.KeepJobs,
		"keepApks":    a.cfg.KeepAPKs,
	})
}

// ---------------------------------------------------------------------------
// 下载：支持 Range（断点续传 / 多线程），1MB 缓冲
// ---------------------------------------------------------------------------

func (a *API) download(w http.ResponseWriter, r *http.Request) {
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/d/"), "/", 2)
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	jobID, name := parts[0], parts[1]
	if strings.Contains(name, "..") || strings.ContainsAny(name, "\\/") {
		http.Error(w, "非法文件名", 400)
		return
	}
	path := filepath.Join(a.store.outputDir(jobID), name)
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	size := st.Size()

	ctype := "application/octet-stream"
	if strings.HasSuffix(strings.ToLower(name), ".apk") {
		ctype = "application/vnd.android.package-archive"
	} else if strings.HasSuffix(strings.ToLower(name), ".json") {
		ctype = "application/json; charset=utf-8"
	}

	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("Content-Disposition", contentDisposition(name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Del("Content-Encoding") // 别让任何中间件压缩大文件

	buf := make([]byte, 1024*1024)
	start, end := int64(0), size-1
	code := http.StatusOK

	if rh := r.Header.Get("Range"); strings.HasPrefix(rh, "bytes=") {
		spec := strings.TrimPrefix(rh, "bytes=")
		if i := strings.Index(spec, "-"); i >= 0 {
			s, e := strings.TrimSpace(spec[:i]), strings.TrimSpace(spec[i+1:])
			if s != "" {
				if v, err := strconv.ParseInt(s, 10, 64); err == nil && v >= 0 && v < size {
					start = v
				}
			}
			if e != "" {
				if v, err := strconv.ParseInt(e, 10, 64); err == nil && v >= start && v < size {
					end = v
				}
			}
			if start > 0 || end < size-1 {
				code = http.StatusPartialContent
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
			}
		}
	}

	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	w.WriteHeader(code)
	if r.Method == http.MethodHead {
		return
	}

	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return
	}
	_, _ = io.CopyBuffer(w, io.LimitReader(f, end-start+1), buf)
}

// downloadAll 把这条记录的所有渠道包打成一个 zip 流式返回（一次点击全下）
func (a *API) downloadAll(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	j := a.store.Get(id)
	if j == nil {
		http.NotFound(w, r)
		return
	}
	v := j.Snapshot()
	dir := a.store.outputDir(id)
	entries, err := os.ReadDir(dir)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".apk") {
			continue
		}
		files = append(files, e.Name())
	}
	if len(files) == 0 {
		http.Error(w, "产物已清理", http.StatusNotFound)
		return
	}
	sort.Strings(files)

	base := strings.TrimSpace(v.AppName)
	if base == "" {
		base = v.Package
	}
	if base == "" {
		base = "apk"
	}
	zipName := fmt.Sprintf("%s_v%s_%s.zip", base, v.VersionName, time.Now().Format("20060102-150405"))

	w.Header().Set("Content-Type", "application/zip")
	esc := strings.ReplaceAll(url.QueryEscape(zipName), "+", "%20")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="apks.zip"; filename*=UTF-8''%s`, esc))
	w.Header().Set("X-Content-Type-Options", "nosniff")

	zw := zip.NewWriter(w)
	buf := make([]byte, 1024*1024)
	for _, name := range files {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		h := &zip.FileHeader{Name: name, Method: zip.Store} // apk 已压缩，存起来即可
		h.Modified = time.Now()
		dst, err := zw.CreateHeader(h)
		if err == nil {
			_, _ = io.CopyBuffer(dst, f, buf)
		}
		f.Close()
	}
	_ = zw.Close()
}

func contentDisposition(name string) string {
	// RFC 5987：兼容中文文件名
	esc := strings.ReplaceAll(url.QueryEscape(name), "+", "%20")
	return fmt.Sprintf(`attachment; filename="fallback.bin"; filename*=UTF-8''%s`, esc)
}

// ---------------------------------------------------------------------------

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// 列表/状态是动态数据，禁止任何一层缓存，避免页面拿到旧顺序
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	_ = json.NewEncoder(w).Encode(v)
}

func jsonField(s, key string) string {
	var m map[string]any
	if json.Unmarshal([]byte(s), &m) != nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func runOut(name string, args ...string) (string, error) {
	b, err := execOut(name, args...)
	return string(b), err
}

// ---------------------------------------------------------------------------
// 启动图上传：普通 png 服务端存原名，.9.png 直接透传（转 9patch 在容器内做）
// ---------------------------------------------------------------------------

func (a *API) putSplash(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	ext := ".png"
	if strings.HasSuffix(strings.ToLower(name), ".9.png") {
		ext = ".9.png"
	}
	id := fmt.Sprintf("s%d", time.Now().UnixNano())
	dst := filepath.Join(a.store.splashesDir(), id+ext)
	f, err := os.Create(dst)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	n, err := io.CopyBuffer(f, r.Body, make([]byte, 1024*1024))
	f.Close()
	if err != nil {
		os.Remove(dst)
		http.Error(w, err.Error(), 500)
		return
	}
	jsonOK(w, map[string]any{"id": id + ext, "size": n})
}

// ---------------------------------------------------------------------------
// 打包环境（容器）状态与控制
// ---------------------------------------------------------------------------

func (a *API) runtimeStatus(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, a.store.RuntimeStatus())
}

func (a *API) runtimeAction(w http.ResponseWriter, r *http.Request) {
	action := r.PathValue("action")
	var err error
	switch action {
	case "start":
		err = a.store.StartRuntime()
	case "stop":
		err = a.store.StopRuntime()
	case "restart":
		err = a.store.RestartRuntime()
	default:
		http.Error(w, "未知操作", 400)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	jsonOK(w, a.store.RuntimeStatus())
}

// ---------------------------------------------------------------------------
// 图标上传：不传就用资源包自带的 app_icons
// ---------------------------------------------------------------------------

func (a *API) putIcon(w http.ResponseWriter, r *http.Request) {
	id := fmt.Sprintf("i%d", time.Now().UnixNano())
	dst := filepath.Join(a.store.iconsDir(), id+".png")
	f, err := os.Create(dst)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	n, err := io.CopyBuffer(f, r.Body, make([]byte, 1024*1024))
	f.Close()
	if err != nil {
		os.Remove(dst)
		http.Error(w, err.Error(), 500)
		return
	}
	jsonOK(w, map[string]any{"id": id + ".png", "size": n})
}

func (a *API) getAppKey(w http.ResponseWriter, r *http.Request) {
	appid := r.URL.Query().Get("appid")
	jsonOK(w, map[string]any{"appid": appid, "appKey": a.store.GetAppKey(appid)})
}

// ---------------------------------------------------------------------------
// 删除 → 回收站 / 回收站管理
// ---------------------------------------------------------------------------

func (a *API) delJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := a.store.TrashJob(id); err != nil {
		http.Error(w, "删除失败: "+err.Error(), 500)
		return
	}
	jsonOK(w, true)
}

func (a *API) listTrash(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, a.store.ListTrash())
}

func (a *API) restoreTrash(w http.ResponseWriter, r *http.Request) {
	if err := a.store.RestoreTrash(r.PathValue("id")); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	jsonOK(w, true)
}

func (a *API) purgeTrash(w http.ResponseWriter, r *http.Request) {
	a.store.PurgeTrashItem(r.PathValue("id"))
	jsonOK(w, true)
}

// ---------------------------------------------------------------------------
// 日志下载 / 一键重试
// ---------------------------------------------------------------------------

func (a *API) jobLog(w http.ResponseWriter, r *http.Request) {
	j := a.store.Get(r.PathValue("id"))
	if j == nil {
		http.NotFound(w, r)
		return
	}
	v := j.Snapshot()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Query().Get("download") == "1" {
		name := fmt.Sprintf("%s_v%s.log", v.AppName, v.VersionName)
		w.Header().Set("Content-Disposition", contentDisposition(name))
	}
	fmt.Fprintf(w, "# %s  %s(%s)  %s\n", v.AppName, v.VersionName, v.VersionCode, v.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(w, "# package=%s  status=%s  elapsed=%s\n\n", v.Package, v.Status, v.Elapsed)
	for _, line := range v.Logs {
		fmt.Fprintln(w, line)
	}
}

func (a *API) retryJob(w http.ResponseWriter, r *http.Request) {
	old := a.store.Get(r.PathValue("id"))
	if old == nil {
		http.Error(w, "记录不存在", 404)
		return
	}
	if !a.store.runtimeRunning() {
		http.Error(w, "打包环境未启动，请先点击「启动环境」", 409)
		return
	}
	if _, err := os.Stat(a.store.uploadDir(old.UploadID)); err != nil {
		http.Error(w, "原始资源包已清理（删除记录后不保留），请重新上传", 400)
		return
	}

	storePass, keyAlias, keyPass := "", "", ""
	if old.Keystore != "" && old.Keystore != "auto" {
		a.store.mu.Lock()
		ks := a.store.keystores[old.Keystore]
		a.store.mu.Unlock()
		if ks == nil {
			http.Error(w, "原证书已被删除，请重新选择证书", 400)
			return
		}
		storePass, keyAlias, keyPass = ks.StorePass, ks.Alias, ks.KeyPass
	}

	j := &Job{
		ID:          fmt.Sprintf("j%d", time.Now().UnixNano()),
		CreatedAt:   time.Now(),
		Status:      "queued",
		Stage:       "排队中",
		UploadID:    old.UploadID,
		Source:      old.Source,
		AppID:       old.AppID,
		AppName:     old.AppName,
		Package:     old.Package,
		VersionName: old.VersionName,
		VersionCode: old.VersionCode,
		Keystore:    old.Keystore,
		AppKey:      old.AppKey,
		Icon:        old.Icon,
		storePass:   storePass,
		keyAlias:    keyAlias,
		keyPass:     keyPass,
		subs:        make(map[uint64]chan []byte),
	}
	a.store.Add(j)
	a.store.queue <- j
	jsonOK(w, map[string]any{"id": j.ID})
}

// ---------------------------------------------------------------------------
// 文件夹上传：HBuilderX 导出的就是目录。
// 浏览器把所有文件按「#F 相对路径\n大小\n + 内容」的流式格式一次性传上来，
// 服务端按相同格式还原成目录树（比逐文件请求快得多）。
// ---------------------------------------------------------------------------

// 清理相对路径：去 ..、去盘符、统一 /
func safeRelPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	var parts []string
	for _, seg := range strings.Split(p, "/") {
		seg = strings.TrimSpace(seg)
		if seg == "" || seg == "." || seg == ".." || strings.Contains(seg, ":") {
			continue
		}
		parts = append(parts, seg)
	}
	return strings.Join(parts, "/")
}

func (a *API) uploadFolder(w http.ResponseWriter, r *http.Request) {
	id := fmt.Sprintf("u%d", time.Now().UnixNano())
	dir := a.store.uploadDir(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	br := bufio.NewReaderSize(r.Body, 1024*1024)
	buf := make([]byte, 1024*1024)

	var total int64
	var count int
	var rootName string
	manifest := ManifestInfo{}
	manifestDepth := 99
	iconBest, iconHint := 0, ""

	for {
		line, err := br.ReadString('\n')
		if err != nil {
			break // 正常结束
		}
		line = strings.TrimRight(line, "\r\n")
		if !strings.HasPrefix(line, "#F ") {
			http.Error(w, "数据格式错误: "+line, 400)
			os.RemoveAll(dir)
			return
		}
		rel := safeRelPath(strings.TrimSpace(line[3:]))
		sizeLine, err := br.ReadString('\n')
		if err != nil {
			break
		}
		size, err := strconv.ParseInt(strings.TrimSpace(sizeLine), 10, 64)
		if err != nil || size < 0 {
			http.Error(w, "文件大小格式错误: "+sizeLine, 400)
			os.RemoveAll(dir)
			return
		}

		dst := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			http.Error(w, err.Error(), 500)
			os.RemoveAll(dir)
			return
		}
		f, err := os.Create(dst)
		if err != nil {
			http.Error(w, err.Error(), 500)
			os.RemoveAll(dir)
			return
		}
		n, cerr := io.CopyBuffer(f, io.LimitReader(br, size), buf)
		f.Close()
		if cerr != nil || n < size {
			http.Error(w, "上传中断", 500)
			os.RemoveAll(dir)
			return
		}
		total += size
		count++

		if rootName == "" {
			if i := strings.Index(rel, "/"); i > 0 {
				rootName = rel[:i]
			}
		}

		// manifest.json（取最浅的那份）
		depth := strings.Count(rel, "/")
		if strings.EqualFold(filepath.Base(rel), "manifest.json") && depth < manifestDepth {
			if m := readManifestFromZipRaw(dst); m != nil {
				manifest = *m
				manifestDepth = depth
			}
		}
		// 图标提示
		if strings.Contains(rel, "static/app_icons/") && strings.HasSuffix(strings.ToLower(rel), ".png") {
			var dim int
			fmt.Sscanf(filepath.Base(rel), "%dx", &dim)
			if dim > iconBest {
				iconBest, iconHint = dim, filepath.Base(rel)
			}
		}
	}

	if count == 0 {
		os.RemoveAll(dir)
		http.Error(w, "没有收到任何文件", 400)
		return
	}
	// 标记这是文件夹模式（打包时按目录压 zip）
	_ = os.WriteFile(filepath.Join(dir, ".folder-upload"), []byte("1"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "manifest.meta.json"),
		[]byte(fmt.Sprintf(`{"files":%d,"size":%d}`, count, total)), 0o644)

	display := rootName
	if display == "" {
		display = "文件夹"
	}
	jsonOK(w, map[string]any{
		"uploadId": id,
		"name":     display + "/",
		"files":    count,
		"size":     total,
		"manifest": manifest,
		"iconHint": iconHint,
	})
}

// readManifestFromZipRaw 直接解析磁盘上的 manifest.json（供文件夹模式用）
func readManifestFromZipRaw(path string) *ManifestInfo {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var raw struct {
		ID          string      `json:"id"`
		Name        string      `json:"name"`
		VersionName string      `json:"versionName"`
		VersionCode interface{} `json:"versionCode"`
	}
	if err := json.NewDecoder(io.LimitReader(f, 4<<20)).Decode(&raw); err != nil {
		return nil
	}
	vc := ""
	if raw.VersionCode != nil {
		vc = strings.TrimSpace(fmt.Sprint(raw.VersionCode))
		if vc == "<nil>" {
			vc = ""
		}
	}
	return &ManifestInfo{AppID: raw.ID, Name: raw.Name, VersionName: raw.VersionName, VersionCode: vc}
}
