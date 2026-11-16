package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Docker 端点配置：显示当前地址 + 运行时切换
//
// 之前的做法是只认启动时的 DOCKER_HOST 环境变量，换机器必须改 start.bat 重启。
// 现在可以在页面上直接切换，并持久化到 data/docker.json，重启后仍然生效。
//
// 优先级：data/docker.json（页面切换过） > 环境变量 DOCKER_HOST > 本机 socket
// ---------------------------------------------------------------------------

// dockerPrefs 持久化文件内容
type dockerPrefs struct {
	Host      string    `json:"host"` // tcp://192.168.1.23:2375；"local" = 明确指定本机
	UpdatedAt time.Time `json:"updatedAt"`
}

// prefsLocal 表示「用户明确选了本机」。
//
// 不能用空字符串表达这个意思：空串在加载时会被当成「没有配置」，
// 于是环境变量 DOCKER_HOST 又赢了——用户切回本机、重启后却发现又连回远程，
// 这种“设置没记住”的行为很难排查。所以磁盘上写一个显式标记。
const prefsLocal = "local"

var dockerPrefsMu sync.Mutex

func (s *Store) dockerPrefsPath() string {
	return filepath.Join(s.cfg.DataDir, "docker.json")
}

// normalizeDockerHost 规范化用户输入：
//   - 空 → 空（本机）
//   - 192.168.1.23:2375 / http://x:2375 → tcp://x:2375（统一走 TCP 分支）
//   - unix:///... 原样保留
func normalizeDockerHost(raw string) string {
	h := strings.TrimSpace(raw)
	if h == "" {
		return ""
	}
	switch {
	case strings.HasPrefix(h, "unix://"):
		return h
	case strings.HasPrefix(h, "tcp://"):
		return h
	case strings.HasPrefix(h, "http://"):
		return "tcp://" + strings.TrimPrefix(h, "http://")
	case strings.HasPrefix(h, "https://"):
		return "tcp://" + strings.TrimPrefix(h, "https://")
	default:
		// host:port 或纯主机名，补上默认端口
		if !strings.Contains(h, ":") {
			h += ":2375"
		}
		return "tcp://" + h
	}
}

// loadDockerPrefs 启动时读取持久化端点并生效。
// 返回 (生效的 host, 是否读到了配置)；host 为空且 ok=true 表示用户明确选了本机。
func (s *Store) loadDockerPrefs() (host string, found bool) {
	b, err := os.ReadFile(s.dockerPrefsPath())
	if err != nil {
		return "", false
	}
	// 手工用记事本/PowerShell 改过这个文件可能会带 BOM，先去掉再解析
	b = bytesTrimBOM(b)
	var p dockerPrefs
	if json.Unmarshal(b, &p) != nil {
		return "", false
	}
	if p.Host == prefsLocal {
		setDockerHost("") // 显式本机：压住环境变量
		return "", true
	}
	h := normalizeDockerHost(p.Host)
	setDockerHost(h)
	return h, true
}

// bytesTrimBOM 去掉 UTF-8 BOM（\uFEFF），避免手工编辑过的配置文件解析失败
func bytesTrimBOM(b []byte) []byte {
	return bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
}

// saveDockerPrefs 持久化当前端点；host 为空表示本机，落到磁盘上写 "local"
func (s *Store) saveDockerPrefs(host string) error {
	dockerPrefsMu.Lock()
	defer dockerPrefsMu.Unlock()
	saved := host
	if saved == "" {
		saved = prefsLocal
	}
	p := dockerPrefs{Host: saved, UpdatedAt: time.Now()}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.dockerPrefsPath(), b, 0o644)
}

// DockerProbe 一次连通性探测的结果
type DockerProbe struct {
	OK      bool   `json:"ok"`
	Host    string `json:"host"`
	Version string `json:"version,omitempty"`
	MemGB   string `json:"memGb,omitempty"`
	Message string `json:"message,omitempty"`
}

// probeDockerHost 用给定端点试连一次（GET /version + /info），不改动当前生效端点。
func probeDockerHost(host string) DockerProbe {
	norm := normalizeDockerHost(host)
	label := norm
	if label == "" {
		label = "local"
	}
	c := newDockerClient(norm)
	if !c.available() {
		return DockerProbe{Host: label, Message: "端点不可用（该地址形式暂不支持）"}
	}

	// Windows 上「本机 Docker」走的是 npipe，本项目只实现了 unix socket / TCP，
	// 直连会报 dial unix ... dead network 这种看不懂的错误，这里提前给明确提示。
	if norm == "" && runtime.GOOS == "windows" {
		return DockerProbe{
			Host:    label,
			Message: "Windows 本机 Docker 走命名管道（npipe），当前只支持 unix socket / TCP 端点；请改填远程地址，如 tcp://192.168.1.23:2375",
		}
	}

	// /version 最轻，先确认能通
	resp, err := c.do("GET", "/version", nil, 8*time.Second)
	if err != nil {
		return DockerProbe{Host: label, Message: "连不上：" + err.Error()}
	}
	// 只看网络是否通还不够：对端可能是个拒绝访问的 401/403 代理，
	// 或者根本不是 Docker（返回 404 的普通网页）。这种必须报失败，
	// 否则用户切过去以后所有打包操作都会莫名失败。
	if resp.StatusCode != 200 {
		status := resp.Status
		resp.Body.Close()
		return DockerProbe{Host: label, Message: "该地址能连通但不是可用的 Docker API（返回 " + status + "）"}
	}
	var ver struct {
		Version    string `json:"Version"`
		APIVersion string `json:"ApiVersion"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&ver)
	resp.Body.Close()

	// 再确认 /info 能读（真正调用容器接口前的基本校验）
	m, err := c.info()
	if err != nil {
		return DockerProbe{Host: label, Version: ver.Version, Message: "能连通但读取 info 失败：" + err.Error()}
	}
	p := DockerProbe{OK: true, Host: label, Version: ver.Version}
	if v, ok := m["MemTotal"].(float64); ok && v > 0 {
		p.MemGB = formatGB(int64(v))
	}
	return p
}

func formatGB(n int64) string {
	return fmt.Sprintf("%.1f GB", float64(n)/1024/1024/1024)
}
