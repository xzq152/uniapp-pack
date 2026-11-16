package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// ---------------------------------------------------------------------------
// 打包环境（常驻容器）管理
//
// 设计取舍：镜像本身不占内存，真正占内存的是运行中的容器 + Gradle。
// 这里用「一个常驻的空转容器」代表打包环境：启动后可以进容器反复打包
// （Gradle 缓存 / 守护进程都能复用），不用时 stop 掉即可释放内存。
// 默认不启动，由页面控制。
//
// 支持 Go 服务与 Docker daemon 分离部署：
// 设置 DOCKER_HOST（如 tcp://192.168.1.23:2375）后，所有操作直接走 Docker Engine
// HTTP API（见 dockerapi.go），文件传输走 archive 接口，Gradle/pnpm 用命名卷，
// 不涉及宿主路径绑定，所以跨机不需要改代码。
//
// 为什么不再用 `docker` 命令行：CLI 只是让命令行连远端，本身仍要求在服务所在机器
// 上安装。实测「服务在 A 机、Docker 在 B 机」时，A 机没装 docker 就完全跑不了
// （报「本机找不到 docker 命令」）。改 API 后只依赖 HTTP，A 机无需任何 Docker 组件。
// ---------------------------------------------------------------------------

const runtimeName = "uniapp-pack-runtime"

type RuntimeStatus struct {
	DockerOK   bool   `json:"dockerOk"`
	Exists     bool   `json:"exists"`
	Running    bool   `json:"running"`
	Image      string `json:"image"`
	ImageOK    bool   `json:"imageOk"`
	DockerMem  string `json:"dockerMem"`
	DockerHost string `json:"dockerHost"`
	Containers int    `json:"containers"`
	Message    string `json:"message"`
}

// dockerEndpoint 当前实际使用的 Docker 端点（给前端/日志展示用）
//
// 优先取运行时配置（页面切换过），否则回退环境变量 DOCKER_HOST。
func dockerEndpoint() string {
	dockerMu.RLock()
	c := dockerActive
	dockerMu.RUnlock()
	if c != nil {
		return c.via
	}
	if h := strings.TrimSpace(os.Getenv("DOCKER_HOST")); h != "" {
		return h
	}
	return "local"
}

// dockerHint 把 Docker 连接失败翻译成能直接照着做的提示
//
// 现在全部走 HTTP API，所以不再有「本机找不到 docker 命令」这种情况；
// 失败原因基本只有两类：端点不可达 / 镜像缺失。
func dockerHint(err error) string {
	host := dockerEndpoint()
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	if host != "" && host != "local" && !strings.HasPrefix(host, "local（") {
		if msg != "" {
			return "连不上 " + host + "：" + msg + "（确认对方 Docker 已开启 TCP 2375，且防火墙放行）"
		}
		return "连不上 " + host + "：确认对方 Docker 已开启 TCP 2375，且防火墙放行"
	}
	if msg != "" {
		return "Docker 未连接：" + msg + "（可在顶栏切换到远程 Docker 地址）"
	}
	return "Docker 未连接，可点顶栏地址切换为远程 Docker（如 tcp://192.168.1.23:2375）"
}

func (s *Store) runtimeInspect() (exists, running bool) {
	return getDockerClient().containerState(runtimeName)
}

func (s *Store) runtimeRunning() bool {
	_, running := s.runtimeInspect()
	return running
}

func (s *Store) RuntimeStatus() RuntimeStatus {
	st := RuntimeStatus{Image: s.cfg.Image, DockerHost: dockerEndpoint()}
	dc := getDockerClient()

	if _, err := dc.info(); err != nil {
		st.Message = dockerHint(err)
		return st
	}
	st.DockerOK = true

	if total, err := dc.memTotal(); err == nil && total > 0 {
		st.DockerMem = fmt.Sprintf("%.1f GB", float64(total)/1024/1024/1024)
	}
	if dc.imageExists(s.cfg.Image) {
		st.ImageOK = true
	} else {
		st.Message = "打包镜像不存在：" + s.cfg.Image
	}

	st.Exists, st.Running = s.runtimeInspect()

	if !st.ImageOK {
		return st
	}
	if st.Running {
		st.Message = "打包环境已启动"
	} else if st.Exists {
		st.Message = "打包环境已停止，可随时启动"
	} else {
		st.Message = "打包环境未启动"
	}
	return st
}

func (s *Store) StartRuntime() error {
	dc := getDockerClient()
	if _, err := dc.info(); err != nil {
		return errors.New(dockerHint(err))
	}
	if !dc.imageExists(s.cfg.Image) {
		return fmt.Errorf("打包镜像不存在：%s", s.cfg.Image)
	}

	exists, running := s.runtimeInspect()
	if running {
		return nil
	}
	if exists {
		if err := dc.startContainer(runtimeName); err != nil {
			return fmt.Errorf("启动失败: %v", err)
		}
		return nil
	}

	// 创建常驻容器：entrypoint 用 sleep infinity，后续靠 exec 进去打包
	if err := dc.createContainer(runtimeName, s.cfg.Image, s.cfg.GradleVol, s.cfg.PnpmVol); err != nil {
		return fmt.Errorf("创建环境失败: %v", err)
	}
	return nil
}

func (s *Store) StopRuntime() error {
	exists, running := s.runtimeInspect()
	if !exists || !running {
		return nil
	}
	// stop 就够了：内存立刻释放，下次 start 又是热的
	if err := getDockerClient().stopContainer(runtimeName, 15); err != nil {
		return fmt.Errorf("停止失败: %v", err)
	}
	return nil
}

func (s *Store) RestartRuntime() error {
	// 用「删了重建」而不是 restart：重建才会用上最新构建的镜像
	if exists, _ := s.runtimeInspect(); exists {
		_ = s.StopRuntime()
		_ = getDockerClient().removeContainer(runtimeName, true)
	}
	return s.StartRuntime()
}

// cleanWorkspace 清空容器内的工作目录，避免上一次的产物/资源串味
func (s *Store) cleanWorkspace() error {
	script := "for d in /workspace/src /workspace/output /workspace/config /workspace/work; do " +
		"mkdir -p $d; find $d -mindepth 1 -maxdepth 1 -exec rm -rf {} + ; done"
	out, err := getDockerClient().execRun(runtimeName, []string{"/bin/bash", "-c", script})
	if err != nil {
		return fmt.Errorf("%v %s", err, strings.TrimSpace(out))
	}
	return nil
}
