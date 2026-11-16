package main

// Docker Remote API 客户端
//
// 为什么需要这一层：早期实现完全依赖 `docker` 命令行（exec.Command("docker", ...)），
// 于是「服务跑在 A 机、Docker 在 B 机」这种部署就要求 A 机也装 docker CLI。
// 但 DOCKER_HOST 只是让 CLI 连远端，并不能替代 CLI 本身 —— 本机没有 docker 命令时，
// 状态检测和打包流程会全部失败（实测报「本机找不到 docker 命令」）。
//
// 这里直接用 Docker Engine HTTP API（默认 tcp://host:2375），只依赖 net/http，
// 同时兼容：
//   · DOCKER_HOST=tcp://192.168.1.23:2375
//   · DOCKER_HOST=http://192.168.1.23:2375
//   · DOCKER_HOST=unix:///var/run/docker.sock（Linux/macOS 本地）
//   · 未设置 → 回退本机 unix socket / npipe
//
// 说明：只实现本项目用到的接口（inspect/cp/exec/start/stop/rm/image/info），
// 不追求完整 SDK。

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// dockerClient Docker Engine API 客户端
type dockerClient struct {
	// baseURL 形如 http://192.168.1.23:2375
	baseURL string
	http    *http.Client
	// via 用于日志/错误提示，如 "tcp://192.168.1.23:2375"
	via string
	// host 原始 DOCKER_HOST 写法（空 = 本机 socket）
	host string
}

var (
	dockerMu     sync.RWMutex
	dockerActive *dockerClient
)

// getDockerClient 取当前生效的客户端。
//
// 注意：这里不再用 sync.Once 缓存——页面可以在运行时切换 Docker 端点，
// 切换后必须立刻换成新客户端，否则停止/重建会打到旧机器上。
func getDockerClient() *dockerClient {
	dockerMu.RLock()
	c := dockerActive
	dockerMu.RUnlock()
	if c != nil {
		return c
	}
	dockerMu.Lock()
	defer dockerMu.Unlock()
	if dockerActive == nil {
		dockerActive = newDockerClient(os.Getenv("DOCKER_HOST"))
	}
	return dockerActive
}

// setDockerHost 运行时切换 Docker 端点；传空表示回到本机 socket。
func setDockerHost(host string) *dockerClient {
	c := newDockerClient(host)
	dockerMu.Lock()
	dockerActive = c
	dockerMu.Unlock()
	return c
}

// newDockerClient 根据给定 host 构造客户端（host 为空则回退本机 socket）
func newDockerClient(host string) *dockerClient {
	host = strings.TrimSpace(host)
	c := &dockerClient{via: host, host: host}

	if host == "" {
		// 未指定：默认本机 unix socket（Linux/macOS）。
		// Windows 上 Docker Desktop 用的是 npipe，这里给它留出可配置空间。
		if v := strings.TrimSpace(os.Getenv("DOCKER_SOCKET")); v != "" {
			host = v
		} else {
			host = "unix:///var/run/docker.sock"
		}
	}
	if c.via == "" {
		c.via = "local（" + host + "）"
	}

	switch {
	case strings.HasPrefix(host, "unix://"):
		sock := strings.TrimPrefix(host, "unix://")
		c.baseURL = "http://docker"
		c.http = &http.Client{
			Timeout: 0,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", sock)
				},
			},
		}
	case strings.HasPrefix(host, "npipe://") || strings.HasPrefix(host, `\\.\pipe\`):
		// Windows 命名管道交给 docker CLI 场景；API 模式不常见，直接标记不可用
		c.baseURL = ""
		c.http = &http.Client{}
	case strings.HasPrefix(host, "tcp://"):
		c.baseURL = "http://" + strings.TrimPrefix(host, "tcp://")
		c.http = &http.Client{Timeout: 0}
	case strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://"):
		c.baseURL = host
		c.http = &http.Client{Timeout: 0}
	default:
		// host:port 形式
		c.baseURL = "http://" + host
		c.http = &http.Client{Timeout: 0}
	}
	return c
}

// available 端点是否已就绪
func (c *dockerClient) available() bool { return c != nil && c.baseURL != "" }

// do 发起请求；timeout<=0 表示不限时（用于流式接口）
func (c *dockerClient) do(method, path string, body io.Reader, timeout time.Duration) (*http.Response, error) {
	if !c.available() {
		return nil, errors.New("Docker API 端点不可用（请检查 DOCKER_HOST）")
	}
	req, err := http.NewRequest(method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := c.http
	if timeout > 0 {
		client = &http.Client{
			Timeout:   timeout,
			Transport: c.http.Transport,
		}
	}
	return client.Do(req)
}

// decodeErr 把非 2xx 的响应体整理成可读错误
func decodeErr(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	msg := strings.TrimSpace(string(b))
	var e struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(b, &e) == nil && e.Message != "" {
		msg = e.Message
	}
	if msg == "" {
		msg = resp.Status
	}
	return fmt.Errorf("%s", msg)
}

// ---------- 基础信息 ----------

// info 对应 docker info；成功即视为 Docker 可用
func (c *dockerClient) info() (map[string]interface{}, error) {
	resp, err := c.do("GET", "/info", nil, 20*time.Second)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, decodeErr(resp)
	}
	var m map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

// memTotal 容器可用内存（字节）
func (c *dockerClient) memTotal() (int64, error) {
	m, err := c.info()
	if err != nil {
		return 0, err
	}
	if v, ok := m["MemTotal"].(float64); ok {
		return int64(v), nil
	}
	return 0, nil
}

// imageExists 镜像是否存在
func (c *dockerClient) imageExists(image string) bool {
	resp, err := c.do("GET", "/images/"+url.PathEscape(image)+"/json", nil, 20*time.Second)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

// ---------- 容器生命周期 ----------

// containerState 返回 (是否存在, 是否运行中)
func (c *dockerClient) containerState(name string) (exists, running bool) {
	resp, err := c.do("GET", "/containers/"+url.PathEscape(name)+"/json", nil, 20*time.Second)
	if err != nil {
		return false, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return false, false
	}
	var v struct {
		State struct {
			Running bool `json:"Running"`
		} `json:"State"`
	}
	if json.NewDecoder(resp.Body).Decode(&v) != nil {
		return true, false
	}
	return true, v.State.Running
}

func (c *dockerClient) startContainer(name string) error {
	resp, err := c.do("POST", "/containers/"+url.PathEscape(name)+"/start", nil, 120*time.Second)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// 304 = 已在运行，视为成功
	if resp.StatusCode == 204 || resp.StatusCode == 304 {
		return nil
	}
	return decodeErr(resp)
}

func (c *dockerClient) stopContainer(name string, timeoutSec int) error {
	path := fmt.Sprintf("/containers/%s/stop?t=%d", url.PathEscape(name), timeoutSec)
	resp, err := c.do("POST", path, nil, 120*time.Second)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 204 || resp.StatusCode == 304 {
		return nil
	}
	return decodeErr(resp)
}

func (c *dockerClient) removeContainer(name string, force bool) error {
	path := "/containers/" + url.PathEscape(name) + "?force=" + map[bool]string{true: "1", false: "0"}[force]
	resp, err := c.do("DELETE", path, nil, 120*time.Second)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 204 || resp.StatusCode == 404 {
		return nil
	}
	return decodeErr(resp)
}

// ---------- 文件拷贝（docker cp 等价） ----------

// copyToContainer 把本地文件/目录打包成 tar 流写入容器路径（等价 docker cp src ctr:dest）
//
// dest 必须是容器内的绝对路径。目录内容会作为 tar 根写入 dest 目录内。
func (c *dockerClient) copyToContainer(ctr, dest, srcPath string) error {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	st, err := os.Stat(srcPath)
	if err != nil {
		return err
	}

	base := filepath.Base(srcPath)
	if st.IsDir() {
		// 目录：逐项写入，保留 base 作为根目录名（与 docker cp 行为一致）
		err = filepath.Walk(srcPath, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(srcPath, p)
			if err != nil {
				return err
			}
			name := filepath.ToSlash(filepath.Join(base, rel))
			if rel == "." {
				name = base
			}
			hdr, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			hdr.Name = name
			if info.IsDir() {
				hdr.Name = strings.TrimSuffix(name, "/") + "/"
			}
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			if info.Mode().IsRegular() {
				f, err := os.Open(p)
				if err != nil {
					return err
				}
				_, cerr := io.Copy(tw, f)
				f.Close()
				return cerr
			}
			return nil
		})
		if err != nil {
			return err
		}
	} else {
		// 单文件：tar 根直接是该文件名
		hdr, err := tar.FileInfoHeader(st, "")
		if err != nil {
			return err
		}
		hdr.Name = base
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(srcPath)
		if err != nil {
			return err
		}
		_, cerr := io.Copy(tw, f)
		f.Close()
		if cerr != nil {
			return cerr
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}

	// 目标目录要保证存在（docker cp 会创建，API 需要目录父级在）
	q := url.Values{}
	q.Set("path", dest)
	q.Set("noOverwriteDirNonDir", "0")
	resp, err := c.do("PUT", "/containers/"+url.PathEscape(ctr)+"/archive?"+q.Encode(), &buf, 5*time.Minute)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 || resp.StatusCode == 204 {
		return nil
	}
	return decodeErr(resp)
}

// copyDirContentsToContainer 把本地目录的「内容」写入容器 dest（等价 docker cp dir/. ctr:dest）
//
// 与 copyToContainer 的差别：不保留 dir 这个目录名，直接把子条目铺进 dest。
func (c *dockerClient) copyDirContentsToContainer(ctr, dest, dir string) error {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil || rel == "." {
			return nil
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if info.IsDir() {
			hdr.Name = strings.TrimSuffix(hdr.Name, "/") + "/"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			_, cerr := io.Copy(tw, f)
			f.Close()
			return cerr
		}
		return nil
	})
	if err != nil {
		_ = tw.Close()
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}

	q := url.Values{}
	q.Set("path", dest)
	q.Set("noOverwriteDirNonDir", "0")
	resp, err := c.do("PUT", "/containers/"+url.PathEscape(ctr)+"/archive?"+q.Encode(), &buf, 5*time.Minute)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 || resp.StatusCode == 204 {
		return nil
	}
	return decodeErr(resp)
}

// copyFromContainer 把容器内路径以 tar 流取出并解压到本地目录（等价 docker cp ctr:src/. localdir）
//
// stripPrefix 用于去掉 tar 中的首层目录（docker cp 的 `/.` 语义）。
func (c *dockerClient) copyFromContainer(ctr, src, localDir string) error {
	q := url.Values{}
	q.Set("path", src)
	resp, err := c.do("GET", "/containers/"+url.PathEscape(ctr)+"/archive?"+q.Encode(), nil, 5*time.Minute)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return decodeErr(resp)
	}

	if err := os.MkdirAll(localDir, 0o755); err != nil {
		return err
	}
	tr := tar.NewReader(bufio.NewReader(resp.Body))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		// 防目录穿越
		name := filepath.Clean(strings.TrimPrefix(hdr.Name, "./"))
		if name == "." || name == "" {
			continue
		}
		if strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			continue
		}
		target := filepath.Join(localDir, name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
			// 保留可执行位（产物里有 shell 脚本）
			_ = os.Chmod(target, os.FileMode(hdr.Mode)&0o777)
		case tar.TypeSymlink:
			_ = os.MkdirAll(filepath.Dir(target), 0o755)
			_ = os.Remove(target)
			_ = os.Symlink(hdr.Linkname, target)
		}
	}
	return nil
}

// ---------- 在容器内执行（docker exec 等价） ----------

// execCreate 创建 exec 实例，返回 exec ID
func (c *dockerClient) execCreate(ctr string, cmd []string, env []string) (string, error) {
	body := map[string]interface{}{
		"AttachStdout": true,
		"AttachStderr": true,
		"Tty":          false,
		"Cmd":          cmd,
	}
	if len(env) > 0 {
		body["Env"] = env
	}
	b, _ := json.Marshal(body)
	resp, err := c.do("POST", "/containers/"+url.PathEscape(ctr)+"/exec", bytes.NewReader(b), 30*time.Second)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		return "", decodeErr(resp)
	}
	var v struct {
		Id string `json:"Id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", err
	}
	return v.Id, nil
}

// execAttach 连接 exec 的输出流，逐行回调（用于构建日志实时推送）
//
// 返回容器内命令的退出码。
func (c *dockerClient) execAttach(execID string, onLine func(string)) (int, error) {
	b, _ := json.Marshal(map[string]interface{}{
		"Detach": false,
		"Tty":    false,
	})
	resp, err := c.do("POST", "/exec/"+url.PathEscape(execID)+"/start", bytes.NewReader(b), 0)
	if err != nil {
		return -1, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 101 {
		return -1, decodeErr(resp)
	}

	if err := demuxStream(resp.Body, onLine); err != nil {
		return -1, err
	}

	// 取退出码
	iresp, err := c.do("GET", "/exec/"+url.PathEscape(execID)+"/json", nil, 30*time.Second)
	if err != nil {
		return -1, err
	}
	defer iresp.Body.Close()
	var v struct {
		Running  bool `json:"Running"`
		ExitCode int  `json:"ExitCode"`
	}
	if err := json.NewDecoder(iresp.Body).Decode(&v); err != nil {
		return -1, err
	}
	return v.ExitCode, nil
}

// createContainer 创建常驻打包容器（等价 docker run -d）
//
// entrypoint 用 /bin/sleep infinity：容器常驻不退出，后续靠 exec 进去打包，
// Gradle 守护进程与缓存可在多次构建间复用。
func (c *dockerClient) createContainer(name, image, gradleVol, pnpmVol string) error {
	body := map[string]interface{}{
		"Image":      image,
		"Entrypoint": []string{"/bin/sleep"},
		"Cmd":        []string{"infinity"},
		"Env":        []string{"TZ=Asia/Shanghai"},
		"HostConfig": map[string]interface{}{
			"Binds": []string{
				gradleVol + ":/root/.gradle",
				pnpmVol + ":/root/.local/share/pnpm/store",
			},
			"AutoRemove": false,
		},
	}
	b, _ := json.Marshal(body)
	q := url.Values{}
	q.Set("name", name)
	resp, err := c.do("POST", "/containers/create?"+q.Encode(), bytes.NewReader(b), 60*time.Second)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		return decodeErr(resp)
	}
	var v struct {
		Id string `json:"Id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&v)

	// 立即启动
	return c.startContainer(name)
}

// execRun 创建并同步执行，收集全部输出（用于短命令）
func (c *dockerClient) execRun(ctr string, cmd []string) (string, error) {
	id, err := c.execCreate(ctr, cmd, nil)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	_, err = c.execAttach(id, func(line string) {
		sb.WriteString(line)
		sb.WriteString("\n")
	})
	return sb.String(), err
}

// demuxStream 解析 Docker 的多路复用流（8 字节帧头：streamType(1)+pad(3)+size(4)）
//
// 若对端不是多路复用（Tty=true 时是裸流），则回退为按行读取。
func demuxStream(r io.Reader, onLine func(string)) error {
	br := bufio.NewReaderSize(r, 64*1024)
	// 先探一个字节判断格式
	peek, err := br.Peek(8)
	if err != nil && err != io.EOF {
		return err
	}
	muxed := len(peek) == 8 && peek[0] <= 2 && peek[1] == 0 && peek[2] == 0 && peek[3] == 0

	if !muxed {
		// 裸流：按行切分
		sc := bufio.NewScanner(br)
		sc.Buffer(make([]byte, 0, 128*1024), 4*1024*1024)
		for sc.Scan() {
			onLine(sc.Text())
		}
		return sc.Err()
	}

	var lineBuf bytes.Buffer
	for {
		hdr := make([]byte, 8)
		if _, err := io.ReadFull(br, hdr); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return err
		}
		size := int(hdr[4])<<24 | int(hdr[5])<<16 | int(hdr[6])<<8 | int(hdr[7])
		if size < 0 || size > 64*1024*1024 {
			return fmt.Errorf("非法帧长度: %d", size)
		}
		payload := make([]byte, size)
		if _, err := io.ReadFull(br, payload); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return err
		}
		lineBuf.Write(payload)
		// 按行回调
		for {
			data := lineBuf.Bytes()
			idx := bytes.IndexByte(data, '\n')
			if idx < 0 {
				break
			}
			onLine(strings.TrimRight(string(data[:idx]), "\r"))
			lineBuf.Next(idx + 1)
		}
	}
	if lineBuf.Len() > 0 {
		onLine(strings.TrimRight(lineBuf.String(), "\r"))
	}
	return nil
}
