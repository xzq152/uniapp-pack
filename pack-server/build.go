package main

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// 单 Worker：Docker 打包很吃资源，串行执行
// ---------------------------------------------------------------------------

func (s *Store) worker() {
	for j := range s.queue {
		s.runBuild(j)
	}
}

func (s *Store) runBuild(j *Job) {
	start := time.Now()
	defer s.saveJobs()

	j.SetState("running", "检查打包环境", 3)
	if !s.runtimeRunning() {
		j.FinishErr("打包环境未启动，请先在页面点击「启动环境」")
		return
	}

	// ---- 1. 准备宿主机侧目录 ----
	upDir := s.uploadDir(j.UploadID)
	outDir := s.outputDir(j.ID)
	cfgTmp := filepath.Join(s.cfg.DataDir, "tmp", j.ID, "config")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		j.FinishErr("创建输出目录失败: " + err.Error())
		return
	}
	if err := os.MkdirAll(filepath.Join(cfgTmp, "keystore"), 0o755); err != nil {
		j.FinishErr("创建配置目录失败: " + err.Error())
		return
	}

	// ---- 2. 证书 / 图标 / 原生配置 ----
	j.SetState("running", "准备签名配置", 6)
	if err := s.prepareConfig(j, cfgTmp); err != nil {
		j.FinishErr(err.Error())
		return
	}

	// ---- 3. 找源文件 ----
	srcFile, err := s.pickSource(upDir)
	if err != nil {
		j.AddLog("详细错误: " + err.Error())
		j.FinishErr("上传资源不存在或已失效，请重新上传")
		return
	}

	// ---- 4. 清空容器工作区 + 注入 ----
	j.SetState("running", "清理工作区", 9)
	if err := s.cleanWorkspace(); err != nil {
		j.FinishErr("清理容器工作区失败: " + err.Error())
		return
	}

	j.SetState("running", "上传资源到容器", 13)
	if out, err := exec.Command("docker", "cp", srcFile, runtimeName+":/workspace/src/").CombinedOutput(); err != nil {
		j.FinishErr("注入源码失败: " + err.Error() + "\n" + string(out))
		return
	}
	if err := dockerCpTarIn(runtimeName, "/workspace/config", cfgTmp); err != nil {
		j.FinishErr("注入配置失败: " + err.Error())
		return
	}

	// ---- 5. 编译 ----
	j.SetState("running", "Gradle 编译中", 15)
	ok := s.streamBuild(j)

	// ---- 6. 取回配置和产物（即使失败也取，便于排查）----
	_ = exec.Command("docker", "cp", runtimeName+":/workspace/config/.", s.configDir()).Run()
	// 启动图是临时注入物，不能随配置镜像持久化，否则会覆盖后续构建的选择
	os.Remove(filepath.Join(s.configDir(), "splash.png"))
	os.Remove(filepath.Join(s.configDir(), "splash.9.png"))
	_ = exec.Command("docker", "cp", runtimeName+":/workspace/output/.", outDir).Run()

	if !ok {
		msg := "构建失败"
		if idx := strings.LastIndex(j.Error, "✗"); idx >= 0 {
			msg = j.Error[idx:]
		}
		j.FinishErr(msg)
		return
	}

	// ---- 7. 记录产物 ----
	j.SetState("running", "收集产物", 96)
	if apk, size := firstAPK(outDir); apk != "" {
		j.mu.Lock()
		j.APK = filepath.Base(apk)
		j.APKSize = size
		j.Elapsed = fmt.Sprintf("%.0fs", time.Since(start).Seconds())
		j.mu.Unlock()
		// 策略：构建成功后原包没用了，立刻删掉释放空间（失败则保留，方便一键重试）
		j.AddLog("原包已清理（需要再打请重新上传或点「再打一个」）")
		if err := os.RemoveAll(upDir); err != nil {
			j.AddLog("清理原包失败: " + err.Error())
		}

		if bi, err := os.ReadFile(filepath.Join(outDir, "build-info.json")); err == nil {
			var info BuildInfo
			if json.Unmarshal(bi, &info) == nil {
				j.mu.Lock()
				j.SHA256 = info.Sha256
				j.MinSdk = info.MinSdk
				j.TargetSdk = info.TargetSdk
				j.Signature = info.SignatureSha256
				j.SignerDN = info.SignerDN
				j.Verified = info.SignatureVerified
				j.TestSigned = info.TestSigned
				j.AppKeySet = info.AppKeySet
				if len(info.APKs) > 0 {
					apks := make([]APKFile, 0, len(info.APKs))
					for _, a := range info.APKs {
						apks = append(apks, APKFile{Channel: a.Channel, File: a.File, Size: a.Size, SHA256: a.Sha256})
					}
					j.APKs = apks
					j.APK = apks[0].File
					j.APKSize = apks[0].Size
				}
				j.mu.Unlock()
			}
		}
		// 只保留最近 N 次 APK
		if n, freed := s.PruneOutputs(s.cfg.KeepAPKs); n > 0 {
			j.AddLog(fmt.Sprintf("已清理 %d 个旧 APK，释放 %.1f MB（保留最近 %d 次）", n, float64(freed)/1024/1024, s.cfg.KeepAPKs))
		}
		// 记录只保留最近 N 条，更早的连同产物一起删掉
		j.FinishOK()
		if n := s.PruneJobs(s.cfg.KeepJobs); n > 0 {
			log.Printf("已清理 %d 条旧记录（只保留最近 %d 条，旧记录在回收站保留 7 天）", n, s.cfg.KeepJobs)
		}
	} else {
		j.FinishErr("编译成功但没有找到 APK 产物")
	}
}

// ---------------------------------------------------------------------------
// 流式执行 docker start -a，实时解析进度
// ---------------------------------------------------------------------------

func (s *Store) streamBuild(j *Job) bool {
	args := []string{"exec"}
	add := func(k, v string) {
		if v != "" {
			args = append(args, "-e", k+"="+v)
		}
	}
	add("APP_ID", j.AppID)
	add("APP_NAME", j.AppName)
	add("APP_PACKAGE", j.Package)
	add("APP_VERSION_NAME", j.VersionName)
	add("APP_VERSION_CODE", j.VersionCode)
	add("KEYSTORE_FILE", j.keystoreFileInContainer())
	add("STORE_PASSWORD", j.storePass)
	add("KEY_ALIAS", j.keyAlias)
	add("KEY_PASSWORD", j.keyPass)
	add("APP_KEY", j.AppKey)
	add("USE_CN_MIRROR", s.cfg.UseCNMirror)
	add("GRADLE_EXTRA_ARGS", s.gradleArgs())
	add("CHANNELS", strings.Join(j.Channels, ","))

	args = append(args, runtimeName, "/opt/pack/entrypoint.sh", "build")

	cmd := exec.Command("docker", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		j.FinishErr("无法创建输出管道: " + err.Error())
		return false
	}
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		j.FinishErr("启动容器失败: " + err.Error())
		return false
	}

	expected := 45 // 首次按经验值估算，跑多了会自动修正
	tasks := 0
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 128*1024), 2*1024*1024)

	for sc.Scan() {
		line := sc.Text()
		j.AddLog(trimLog(line))

		switch {
		case strings.HasPrefix(line, "> Task "):
			tasks++
			if tasks+10 > expected {
				expected = tasks + 10
			}
			p := 15 + int(70*float64(tasks)/float64(expected))
			if p > 85 {
				p = 85
			}
			j.SetState("running", fmt.Sprintf("Gradle 编译中 (%d 个任务)", tasks), p)
		case strings.Contains(line, "收集产物"):
			j.SetState("running", "收集产物", 90)
		case strings.Contains(line, "BUILD SUCCESSFUL"):
			j.SetState("running", "打包完成", 95)
		case strings.HasPrefix(line, "✗"), strings.Contains(line, "BUILD FAILED"),
			strings.Contains(line, "What went wrong"):
			j.mu.Lock()
			j.Error = j.Error + "\n" + line
			j.mu.Unlock()
		}
	}
	_ = sc.Err()

	err = cmd.Wait()
	return err == nil
}

// ---------------------------------------------------------------------------
// 辅助
// ---------------------------------------------------------------------------

// pickSource 返回要送进容器的文件：
// 文件夹上传（有 .folder-upload 标记）→ 先压成 packed.zip；zip 上传 → 直接用
func (s *Store) pickSource(upDir string) (string, error) {
	if _, err := os.Stat(filepath.Join(upDir, ".folder-upload")); err == nil {
		return packUploadDir(upDir)
	}
	entries, err := os.ReadDir(upDir)
	if err != nil {
		return "", fmt.Errorf("找不到上传的资源: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		return filepath.Join(upDir, e.Name()), nil
	}
	return "", fmt.Errorf("上传目录为空: %s", upDir)
}

// packUploadDir 把文件夹形式的上传内容压成 zip，交给容器（容器端已有解压逻辑，不用改）
func packUploadDir(dir string) (string, error) {
	zipPath := filepath.Join(dir, "packed.zip")
	zf, err := os.Create(zipPath)
	if err != nil {
		return "", err
	}
	zw := zip.NewWriter(zf)
	skip := map[string]bool{"packed.zip": true, ".folder-upload": true, "manifest.meta.json": true}
	werr := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if skip[filepath.Base(path)] {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return nil
		}
		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		hdr.Method = zip.Deflate
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(w, src)
		return err
	})
	if err := zw.Close(); err != nil {
		zf.Close()
		return "", err
	}
	if err := zf.Close(); err != nil {
		return "", err
	}
	if werr != nil {
		return "", werr
	}
	return zipPath, nil
}

// prepareConfig 把持久化配置 + 选定证书放进待注入的临时配置目录
func (s *Store) prepareConfig(j *Job, dst string) error {
	// 1) 先继承上次持久化的 /workspace/config（里面可能有自动生成的 auto.jks）
	if err := copyDirContents(s.configDir(), dst); err != nil {
		return fmt.Errorf("复制基础配置失败: %w", err)
	}

	// 1.5) 启动图不继承：每次构建用「本次上传/选择」的，避免上一次的残留覆盖
	os.Remove(filepath.Join(dst, "splash.png"))
	os.Remove(filepath.Join(dst, "splash.9.png"))
	os.Remove(filepath.Join(dst, "branding", "splash.png"))

	// 2) 选了「自动生成测试证书」：清掉持久化目录里可能残留的用户证书，
	//    否则 entrypoint 会优先用 app.jks，等于悄悄换了签名
	if j.Keystore == "" || j.Keystore == "auto" {
		for _, n := range []string{"app.jks", "app.keystore", "keystore.properties"} {
			_ = os.Remove(filepath.Join(dst, "keystore", n))
		}
	}

	// 3) 用户指定了证书 → 放进 keystore/app.jks 并写 properties
	if j.Keystore != "" && j.Keystore != "auto" {
		s.mu.Lock()
		ks := s.keystores[j.Keystore]
		s.mu.Unlock()
		if ks == nil {
			return fmt.Errorf("证书不存在: %s", j.Keystore)
		}
		src := filepath.Join(s.keystoreDir(), ks.File)
		if err := copyFile(src, filepath.Join(dst, "keystore", "app.jks")); err != nil {
			return fmt.Errorf("复制证书失败: %w", err)
		}
		props := fmt.Sprintf("storeFile=app.jks\nstorePassword=%s\nkeyAlias=%s\nkeyPassword=%s\n",
			ks.StorePass, ks.Alias, ks.KeyPass)
		if err := os.WriteFile(filepath.Join(dst, "keystore", "keystore.properties"), []byte(props), 0o644); err != nil {
			return fmt.Errorf("写入证书配置失败: %w", err)
		}
	}

	// 5) 页面上传的启动图（普通 png 或 .9.png，entrypoint 里统一转/透传 .9）
	if j.Splash != "" {
		src := filepath.Join(s.splashesDir(), j.Splash)
		if _, err := os.Stat(src); err == nil {
			name := "splash.png"
			if strings.HasSuffix(strings.ToLower(j.Splash), ".9.png") {
				name = "splash.9.png"
			}
			if err := copyFile(src, filepath.Join(dst, name)); err != nil {
				return fmt.Errorf("复制启动图失败: %w", err)
			}
		}
	}

	// 4) 页面上传的图标（不传就用资源包自带的 app_icons）
	if j.Icon != "" {
		src := filepath.Join(s.iconsDir(), j.Icon)
		if _, err := os.Stat(src); err == nil {
			if err := copyFile(src, filepath.Join(dst, "icon.png")); err != nil {
				return fmt.Errorf("复制图标失败: %w", err)
			}
		}
	}
	return nil
}

// dockerCpTarIn 把 dir 的内容以 tar 流写进容器的 dest 目录
func dockerCpTarIn(ctr, dest, dir string) error {
	cmd := exec.Command("docker", "cp", "-", ctr+":"+dest)
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	cmd.Stdout = nil
	if err := cmd.Start(); err != nil {
		return err
	}

	tw := tar.NewWriter(in)
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil || rel == "." {
			return nil
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			_, err = io.Copy(tw, f)
			f.Close()
			return err
		}
		return nil
	})
	if err != nil {
		tw.Close()
		in.Close()
		cmd.Wait()
		return err
	}
	if err := tw.Close(); err != nil {
		in.Close()
		cmd.Wait()
		return err
	}
	in.Close()
	return cmd.Wait()
}

func copyDirContents(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyDirContents(s, d); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(s, d); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.CopyBuffer(out, in, make([]byte, 1024*1024))
	return err
}

func firstAPK(dir string) (string, int64) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", 0
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".apk") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		return filepath.Join(dir, e.Name()), info.Size()
	}
	return "", 0
}

// gradleArgs 按 Docker 实际内存给 Gradle 定堆，避免 daemon 被 OOM kill
func (s *Store) gradleArgs() string {
	if s.cfg.GradleArgs != "" {
		return s.cfg.GradleArgs
	}
	out, err := exec.Command("docker", "info", "--format", "{{.MemTotal}}").Output()
	total := 8
	if err == nil {
		if v, e := strconv.ParseFloat(strings.TrimSpace(string(out)), 64); e == nil && v > 0 {
			total = int(v / 1024 / 1024 / 1024)
		}
	}
	mx, workers := 4096, 4
	switch {
	case total >= 8:
		mx, workers = 4096, 4
	case total >= 6:
		mx, workers = 3072, 4
	case total >= 4:
		mx, workers = 2048, 2
	default:
		mx, workers = 1024, 2
	}
	return fmt.Sprintf("-Dorg.gradle.jvmargs=-Xmx%dm -Dorg.gradle.workers.max=%d", mx, workers)
}

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;?]*[a-zA-Z]")

func trimLog(s string) string {
	s = ansiRE.ReplaceAllString(s, "")
	s = strings.TrimRight(s, "\r\n")
	if len(s) > 400 {
		return s[:400] + " …(已截断)"
	}
	return s
}

// APKInfo 描述一个渠道产出的包
type APKInfo struct {
	Channel string `json:"channel"`
	File    string `json:"file"`
	Size    int64  `json:"size"`
	Sha256  string `json:"sha256"`
}

// BuildInfo 对应容器产出的 build-info.json
type BuildInfo struct {
	AppID             string    `json:"appId"`
	AppName           string    `json:"appName"`
	Package           string    `json:"package"`
	VersionName       string    `json:"versionName"`
	VersionCode       int       `json:"versionCode"`
	APK               string    `json:"apk"`
	APKs              []APKInfo `json:"apks"`
	Size              int64     `json:"size"`
	Sha256            string    `json:"sha256"`
	MinSdk            int       `json:"minSdk"`
	TargetSdk         int       `json:"targetSdk"`
	SignatureSha256   string    `json:"signatureSha256"`
	SignerDN          string    `json:"signerDN"`
	SignatureVerified bool      `json:"signatureVerified"`
	TestSigned        bool      `json:"testSigned"`
	AppKeySet         bool      `json:"appKeySet"`
	BuiltAt           string    `json:"builtAt"`
	Elapsed           string    `json:"elapsed"`
}
