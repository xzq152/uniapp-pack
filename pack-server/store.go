package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// 数据模型
// ---------------------------------------------------------------------------

// APKFile 一个渠道产出的包（无渠道时 Channel 为空）
type APKFile struct {
	Channel string `json:"channel"`
	File    string `json:"file"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256,omitempty"`
	Exists  bool   `json:"exists"`
}

type Job struct {
	ID          string     `json:"id"`
	CreatedAt   time.Time  `json:"createdAt"`
	FinishedAt  *time.Time `json:"finishedAt,omitempty"`
	Status      string     `json:"status"` // queued | running | done | failed
	Progress    int        `json:"progress"`
	Stage       string     `json:"stage"`
	Error       string     `json:"error,omitempty"`
	UploadID    string     `json:"uploadId"`
	Source      string     `json:"source"`
	AppID       string     `json:"appId"`
	AppName     string     `json:"appName"`
	Package     string     `json:"package"`
	VersionName string     `json:"versionName"`
	VersionCode string     `json:"versionCode"`
	Keystore    string     `json:"keystore"`
	AppKey      string     `json:"appKey,omitempty"`
	Icon        string     `json:"icon,omitempty"`
	Splash      string     `json:"splash,omitempty"`
	Channels    []string   `json:"channels,omitempty"`
	APKs        []APKFile  `json:"apks,omitempty"`
	APK         string     `json:"apk,omitempty"`
	APKSize     int64      `json:"apkSize,omitempty"`
	SHA256      string     `json:"sha256,omitempty"`
	MinSdk      int        `json:"minSdk,omitempty"`
	TargetSdk   int        `json:"targetSdk,omitempty"`
	Signature   string     `json:"signature,omitempty"`
	SignerDN    string     `json:"signerDN,omitempty"`
	Verified    bool       `json:"verified,omitempty"`
	TestSigned  bool       `json:"testSigned,omitempty"`
	AppKeySet   bool       `json:"appKeySet,omitempty"`
	Elapsed     string     `json:"elapsed,omitempty"`

	// 内部字段：不序列化给前端
	storePass string
	keyAlias  string
	keyPass   string

	mu      sync.Mutex
	subs    map[uint64]chan []byte
	nextSub uint64
	logs    []string
}

type Keystore struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	File      string `json:"file"`
	Alias     string `json:"alias"`
	StorePass string `json:"storePass"`
	KeyPass   string `json:"keyPass"`
	CreatedAt string `json:"createdAt"`
}

type Store struct {
	cfg       Config
	mu        sync.Mutex
	jobs      map[string]*Job
	queue     chan *Job
	keystores map[string]*Keystore
	appkeys   map[string]string // appid -> DCloud 离线打包 appkey
	trash     map[string]*TrashItem
}

func NewStore(cfg Config) *Store {
	s := &Store{
		cfg:       cfg,
		jobs:      map[string]*Job{},
		queue:     make(chan *Job, 128),
		keystores: map[string]*Keystore{},
		appkeys:   map[string]string{},
		trash:     map[string]*TrashItem{},
	}
	s.loadJobs()
	s.loadKeystores()
	s.loadAppKeys()
	s.loadTrash()
	s.PurgeTrash()
	return s
}

// ---------------------------------------------------------------------------
// 目录
// ---------------------------------------------------------------------------

func (s *Store) uploadsDir() string  { return filepath.Join(s.cfg.DataDir, "uploads") }
func (s *Store) outputsDir() string  { return filepath.Join(s.cfg.DataDir, "outputs") }
func (s *Store) keystoreDir() string { return filepath.Join(s.cfg.DataDir, "keystores") }
func (s *Store) configDir() string   { return filepath.Join(s.cfg.DataDir, "config") }
func (s *Store) iconsDir() string    { return filepath.Join(s.cfg.DataDir, "icons") }
func (s *Store) splashesDir() string { return filepath.Join(s.cfg.DataDir, "splashes") }
func (s *Store) trashDir() string    { return filepath.Join(s.cfg.DataDir, "trash") }

func (s *Store) uploadDir(id string) string { return filepath.Join(s.uploadsDir(), id) }

func (s *Store) uploadExists(id string) bool {
	if id == "" {
		return false
	}
	ents, err := os.ReadDir(s.uploadDir(id))
	return err == nil && len(ents) > 0
}

// PruneJobs 只保留最近 keep 条记录，更早的移入回收站（7 天内可恢复）。
// 排队中 / 构建中的任务受保护，不会被清理。
func (s *Store) PruneJobs(keep int) (removed int) {
	if keep <= 0 {
		return 0
	}
	jobs := s.List()
	if len(jobs) <= keep {
		return 0
	}
	sort.Slice(jobs, func(i, k int) bool { return jobs[i].CreatedAt.After(jobs[k].CreatedAt) })

	var drop []string
	for i, j := range jobs {
		if i < keep {
			continue
		}
		j.mu.Lock()
		st := j.Status
		j.mu.Unlock()
		if st == "running" || st == "queued" {
			continue // 正在跑的留着
		}
		drop = append(drop, j.ID)
	}
	for _, id := range drop {
		// 先移入回收站（7 天内可恢复），失败再彻底删除
		if err := s.TrashJob(id); err != nil {
			s.Remove(id)
		}
	}
	return len(drop)
}

// PruneOutputs 只保留最近 keep 次成功构建的 APK，其余删掉（build-info.json 保留，记录和详情都还在）
func (s *Store) PruneOutputs(keep int) (removed int, freed int64) {
	if keep <= 0 {
		return 0, 0
	}
	var dones []*Job
	for _, j := range s.List() {
		if j.Status == "done" && j.APK != "" {
			dones = append(dones, j)
		}
	}
	sort.Slice(dones, func(i, k int) bool { return dones[i].CreatedAt.After(dones[k].CreatedAt) })

	for i, j := range dones {
		if i < keep {
			continue
		}
		dir := s.outputDir(j.ID)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".apk") {
				continue
			}
			p := filepath.Join(dir, e.Name())
			if info, err := e.Info(); err == nil {
				freed += info.Size()
			}
			if os.Remove(p) == nil {
				removed++
			}
		}
	}
	return removed, freed
}

// PruneUploads 清掉「没有任何任务还在用」的上传包：
// 排队中 / 构建中 / 失败（可重试）的任务需要保留，其余（成功已删、孤儿上传）一律清掉
func (s *Store) PruneUploads() (removed int, freed int64) {
	keep := map[string]bool{}
	for _, j := range s.List() {
		switch j.Status {
		case "queued", "running", "failed":
			keep[j.UploadID] = true
		}
	}

	entries, err := os.ReadDir(s.uploadsDir())
	if err != nil {
		return 0, 0
	}
	for _, e := range entries {
		if !e.IsDir() || keep[e.Name()] {
			continue
		}
		dir := filepath.Join(s.uploadsDir(), e.Name())
		var size int64
		filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
			if err == nil && info != nil && info.Mode().IsRegular() {
				size += info.Size()
			}
			return nil
		})
		if err := os.RemoveAll(dir); err == nil {
			removed++
			freed += size
		}
	}
	return removed, freed
}
func (s *Store) outputDir(id string) string { return filepath.Join(s.outputsDir(), id) }

func (s *Store) ensureDirs() error {
	for _, d := range []string{s.uploadsDir(), s.outputsDir(), s.keystoreDir(), s.configDir(), s.iconsDir(), s.splashesDir(), s.trashDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	// 容器内 /workspace/config/keystore 的持久化镜像目录
	return os.MkdirAll(filepath.Join(s.configDir(), "keystore"), 0o755)
}

// ---------------------------------------------------------------------------
// Job
// ---------------------------------------------------------------------------

func (s *Store) Add(j *Job) {
	s.mu.Lock()
	s.jobs[j.ID] = j
	s.mu.Unlock()
	s.saveJobs()
}

func (s *Store) Get(id string) *Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.jobs[id]
}

func (s *Store) List() []*Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		out = append(out, j)
	}
	return out
}

// Remove 彻底删除（回收站清理时用）
func (s *Store) Remove(id string) {
	s.mu.Lock()
	delete(s.jobs, id)
	s.mu.Unlock()
	s.saveJobs()
	os.RemoveAll(s.outputDir(id))
	os.RemoveAll(s.uploadDir(id))
}

// ---------------------------------------------------------------------------
// 回收站：误删 7 天内可恢复
// ---------------------------------------------------------------------------

type TrashItem struct {
	JobView
	DeletedAt time.Time `json:"deletedAt"`
}

const trashTTL = 7 * 24 * time.Hour

func (s *Store) trashIndexPath() string { return filepath.Join(s.trashDir(), "index.json") }

func (s *Store) TrashJob(id string) error {
	j := s.Get(id)
	if j == nil {
		return nil
	}
	item := &TrashItem{JobView: j.Snapshot(), DeletedAt: time.Now()}

	dst := filepath.Join(s.trashDir(), id)
	os.RemoveAll(dst)
	if _, err := os.Stat(s.outputDir(id)); err == nil {
		if err := os.Rename(s.outputDir(id), dst); err != nil {
			return err
		}
	}
	os.RemoveAll(s.uploadDir(id)) // 原包较大，不保留

	s.mu.Lock()
	delete(s.jobs, id)
	s.trash[id] = item
	s.mu.Unlock()

	s.saveJobs()
	s.saveTrash()
	return nil
}

func (s *Store) ListTrash() []TrashItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]TrashItem, 0, len(s.trash))
	for _, t := range s.trash {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, k int) bool { return out[i].DeletedAt.After(out[k].DeletedAt) })
	return out
}

func (s *Store) RestoreTrash(id string) error {
	s.mu.Lock()
	item := s.trash[id]
	delete(s.trash, id)
	s.mu.Unlock()
	if item == nil {
		return fmt.Errorf("回收站里没有这条记录")
	}

	src := filepath.Join(s.trashDir(), id)
	if _, err := os.Stat(src); err == nil {
		if err := os.Rename(src, s.outputDir(id)); err != nil {
			return err
		}
	}

	j := &Job{
		ID: item.ID, CreatedAt: item.CreatedAt, FinishedAt: item.FinishedAt,
		Status: "done", Progress: 100, Stage: "完成",
		UploadID: item.UploadID, Source: item.Source, AppID: item.AppID,
		AppName: item.AppName, Package: item.Package,
		VersionName: item.VersionName, VersionCode: item.VersionCode,
		Keystore: item.Keystore, AppKey: item.AppKey,
		APK: item.APK, APKSize: item.APKSize, SHA256: item.SHA256,
		MinSdk: item.MinSdk, TargetSdk: item.TargetSdk,
		Signature: item.Signature, SignerDN: item.SignerDN,
		Verified: item.Verified, TestSigned: item.TestSigned, AppKeySet: item.AppKeySet,
		Elapsed: item.Elapsed,
		subs:    make(map[uint64]chan []byte),
	}
	s.mu.Lock()
	s.jobs[id] = j
	s.mu.Unlock()

	s.saveJobs()
	s.saveTrash()
	return nil
}

func (s *Store) PurgeTrashItem(id string) {
	s.mu.Lock()
	delete(s.trash, id)
	s.mu.Unlock()
	os.RemoveAll(filepath.Join(s.trashDir(), id))
	s.saveTrash()
}

// PurgeTrash 清掉超过 7 天的回收站内容
func (s *Store) PurgeTrash() {
	deadline := time.Now().Add(-trashTTL)
	var expired []string
	s.mu.Lock()
	for id, it := range s.trash {
		if it.DeletedAt.Before(deadline) {
			expired = append(expired, id)
		}
	}
	s.mu.Unlock()
	for _, id := range expired {
		s.PurgeTrashItem(id)
	}
}

func (s *Store) saveTrash() {
	s.mu.Lock()
	b, _ := json.MarshalIndent(s.trash, "", "  ")
	s.mu.Unlock()
	_ = os.WriteFile(s.trashIndexPath(), b, 0o644)
}

func (s *Store) loadTrash() {
	b, err := os.ReadFile(s.trashIndexPath())
	if err != nil {
		return
	}
	m := map[string]*TrashItem{}
	if json.Unmarshal(b, &m) == nil {
		s.mu.Lock()
		s.trash = m
		s.mu.Unlock()
	}
}

// JobView 是不含锁的可序列化视图（Job 内含 mutex，不能直接拷贝）
type JobView struct {
	ID          string     `json:"id"`
	CreatedAt   time.Time  `json:"createdAt"`
	FinishedAt  *time.Time `json:"finishedAt,omitempty"`
	Status      string     `json:"status"`
	Progress    int        `json:"progress"`
	Stage       string     `json:"stage"`
	Error       string     `json:"error,omitempty"`
	UploadID    string     `json:"uploadId"`
	Source      string     `json:"source"`
	AppID       string     `json:"appId"`
	AppName     string     `json:"appName"`
	Package     string     `json:"package"`
	VersionName string     `json:"versionName"`
	VersionCode string     `json:"versionCode"`
	Keystore    string     `json:"keystore"`
	AppKey      string     `json:"appKey,omitempty"`
	Icon        string     `json:"icon,omitempty"`
	Splash      string     `json:"splash,omitempty"`
	Channels    []string   `json:"channels,omitempty"`
	APKs        []APKFile  `json:"apks,omitempty"`
	APK         string     `json:"apk,omitempty"`
	APKSize     int64      `json:"apkSize,omitempty"`
	SHA256      string     `json:"sha256,omitempty"`
	MinSdk      int        `json:"minSdk,omitempty"`
	TargetSdk   int        `json:"targetSdk,omitempty"`
	Signature   string     `json:"signature,omitempty"`
	SignerDN    string     `json:"signerDN,omitempty"`
	Verified    bool       `json:"verified,omitempty"`
	TestSigned  bool       `json:"testSigned,omitempty"`
	AppKeySet   bool       `json:"appKeySet,omitempty"`
	Elapsed     string     `json:"elapsed,omitempty"`
	HasUpload   bool       `json:"hasUpload"`
	APKExists   bool       `json:"apkExists"`
	Logs        []string   `json:"logs,omitempty"`
}

func (j *Job) Snapshot() JobView {
	j.mu.Lock()
	defer j.mu.Unlock()
	return JobView{
		ID:          j.ID,
		CreatedAt:   j.CreatedAt,
		FinishedAt:  j.FinishedAt,
		Status:      j.Status,
		Progress:    j.Progress,
		Stage:       j.Stage,
		Error:       j.Error,
		UploadID:    j.UploadID,
		Source:      j.Source,
		AppID:       j.AppID,
		AppName:     j.AppName,
		Package:     j.Package,
		VersionName: j.VersionName,
		VersionCode: j.VersionCode,
		Keystore:    j.Keystore,
		AppKey:      j.AppKey,
		Icon:        j.Icon,
		Splash:      j.Splash,
		Channels:    append([]string(nil), j.Channels...),
		APKs:        append([]APKFile(nil), j.APKs...),
		APK:         j.APK,
		APKSize:     j.APKSize,
		SHA256:      j.SHA256,
		MinSdk:      j.MinSdk,
		TargetSdk:   j.TargetSdk,
		Signature:   j.Signature,
		SignerDN:    j.SignerDN,
		Verified:    j.Verified,
		TestSigned:  j.TestSigned,
		AppKeySet:   j.AppKeySet,
		Elapsed:     j.Elapsed,
		Logs:        append([]string(nil), j.logs...),
	}
}

func (j *Job) keystoreFileInContainer() string {
	if j.Keystore != "" && j.Keystore != "auto" {
		return "/workspace/config/keystore/app.jks"
	}
	return ""
}

func (j *Job) AddLog(line string) {
	j.mu.Lock()
	j.logs = append(j.logs, line)
	if len(j.logs) > 3000 {
		j.logs = j.logs[len(j.logs)-3000:]
	}
	logs := j.logs
	j.mu.Unlock()
	j.broadcast(event("log", map[string]any{"line": line}))
	_ = logs
}

func (j *Job) SetState(status, stage string, progress int) {
	j.mu.Lock()
	j.Status = status
	j.Stage = stage
	j.Progress = progress
	j.mu.Unlock()
	j.broadcast(event("state", map[string]any{
		"status": status, "stage": stage, "progress": progress,
	}))
}

func (j *Job) FinishOK() {
	now := time.Now()
	j.mu.Lock()
	j.Status = "done"
	j.Progress = 100
	j.Stage = "完成"
	j.FinishedAt = &now
	j.mu.Unlock()
	j.broadcast(event("state", map[string]any{"status": "done", "stage": "完成", "progress": 100}))
}

func (j *Job) FinishErr(msg string) {
	now := time.Now()
	j.mu.Lock()
	j.Status = "failed"
	j.Stage = "失败"
	j.Error = msg
	j.FinishedAt = &now
	j.mu.Unlock()
	j.broadcast(event("state", map[string]any{"status": "failed", "stage": "失败", "error": msg}))
}

// ---------------------------------------------------------------------------
// SSE 广播
// ---------------------------------------------------------------------------

func event(typ string, data any) []byte {
	b, _ := json.Marshal(map[string]any{"type": typ, "data": data})
	return b
}

func (j *Job) Subscribe() (uint64, chan []byte) {
	ch := make(chan []byte, 512)
	j.mu.Lock()
	if j.subs == nil {
		j.subs = make(map[uint64]chan []byte)
	}
	j.nextSub++
	id := j.nextSub
	j.subs[id] = ch
	j.mu.Unlock()
	return id, ch
}

func (j *Job) Unsubscribe(id uint64) {
	j.mu.Lock()
	ch, ok := j.subs[id]
	if ok {
		delete(j.subs, id)
	}
	j.mu.Unlock()
	if ok {
		close(ch)
	}
}

func (j *Job) broadcast(b []byte) {
	j.mu.Lock()
	subs := make([]chan []byte, 0, len(j.subs))
	for _, ch := range j.subs {
		subs = append(subs, ch)
	}
	j.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- b:
		default: // 客户端太慢就丢弃，不阻塞构建
		}
	}
}

// ---------------------------------------------------------------------------
// 持久化（任务元信息 / 证书列表）
// ---------------------------------------------------------------------------

func (s *Store) saveJobs() {
	s.mu.Lock()
	list := make([]*Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		list = append(list, j)
	}
	s.mu.Unlock()

	out := make([]JobView, 0, len(list))
	for _, j := range list {
		out = append(out, j.Snapshot())
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(s.cfg.DataDir, "jobs.json"), b, 0o644)
}

func (s *Store) loadJobs() {
	b, err := os.ReadFile(filepath.Join(s.cfg.DataDir, "jobs.json"))
	if err != nil {
		return
	}
	var list []*Job
	if json.Unmarshal(b, &list) != nil {
		return
	}
	for _, j := range list {
		// 重启后中断的任务标记为失败，避免一直转圈
		if j.Status == "running" || j.Status == "queued" {
			j.Status = "failed"
			j.Stage = "中断"
			j.Error = "服务重启导致任务中断"
		}
		j.subs = make(map[uint64]chan []byte)
		s.jobs[j.ID] = j
	}
}

func (s *Store) saveKeystores() {
	s.mu.Lock()
	list := make([]*Keystore, 0, len(s.keystores))
	for _, k := range s.keystores {
		list = append(list, k)
	}
	s.mu.Unlock()
	b, _ := json.MarshalIndent(list, "", "  ")
	_ = os.WriteFile(filepath.Join(s.keystoreDir(), "index.json"), b, 0o644)
}

func (s *Store) loadKeystores() {
	b, err := os.ReadFile(filepath.Join(s.keystoreDir(), "index.json"))
	if err != nil {
		return
	}
	var list []*Keystore
	if json.Unmarshal(b, &list) != nil {
		return
	}
	for _, k := range list {
		s.keystores[k.ID] = k
	}
}

// ---------------------------------------------------------------------------
// appkey（按 appid 记住，下次同应用自动带出）
// ---------------------------------------------------------------------------

func (s *Store) appKeyPath() string { return filepath.Join(s.cfg.DataDir, "appkeys.json") }

func (s *Store) loadAppKeys() {
	b, err := os.ReadFile(s.appKeyPath())
	if err != nil {
		return
	}
	m := map[string]string{}
	if json.Unmarshal(b, &m) == nil {
		s.appkeys = m
	}
}

func (s *Store) saveAppKeys() {
	s.mu.Lock()
	b, _ := json.MarshalIndent(s.appkeys, "", "  ")
	s.mu.Unlock()
	_ = os.WriteFile(s.appKeyPath(), b, 0o644)
}

func (s *Store) GetAppKey(appid string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.appkeys[appid]
}

func (s *Store) SetAppKey(appid, key string) {
	if appid == "" || key == "" {
		return
	}
	s.mu.Lock()
	s.appkeys[appid] = key
	s.mu.Unlock()
	s.saveAppKeys()
}
