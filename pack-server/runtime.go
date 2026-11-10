package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// 打包环境（常驻容器）管理
//
// 设计取舍：镜像本身不占内存，真正占内存的是运行中的容器 + Gradle。
// 这里用「一个常驻的空转容器」代表打包环境：启动后可以 docker exec 进去反复打包
// （Gradle 缓存 / 守护进程都能复用），不用时 stop 掉即可释放内存。
// 默认不启动，由页面控制。
// ---------------------------------------------------------------------------

const runtimeName = "uniapp-pack-runtime"

type RuntimeStatus struct {
	DockerOK   bool   `json:"dockerOk"`
	Exists     bool   `json:"exists"`
	Running    bool   `json:"running"`
	Image      string `json:"image"`
	ImageOK    bool   `json:"imageOk"`
	DockerMem  string `json:"dockerMem"`
	Containers int    `json:"containers"`
	Message    string `json:"message"`
}

func (s *Store) runtimeInspect() (exists, running bool) {
	out, err := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", runtimeName).Output()
	if err != nil {
		return false, false
	}
	return true, strings.TrimSpace(string(out)) == "true"
}

func (s *Store) runtimeRunning() bool {
	_, running := s.runtimeInspect()
	return running
}

func (s *Store) RuntimeStatus() RuntimeStatus {
	st := RuntimeStatus{Image: s.cfg.Image}

	if _, err := exec.Command("docker", "info").Output(); err != nil {
		st.Message = "Docker 未运行，请先启动 Docker Desktop"
		return st
	}
	st.DockerOK = true

	if out, err := exec.Command("docker", "info", "--format", "{{.MemTotal}}").Output(); err == nil {
		if v, e := strconv.ParseFloat(strings.TrimSpace(string(out)), 64); e == nil {
			st.DockerMem = fmt.Sprintf("%.1f GB", v/1024/1024/1024)
		}
	}
	if _, err := exec.Command("docker", "image", "inspect", s.cfg.Image).Output(); err == nil {
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
	if _, err := exec.Command("docker", "info").Output(); err != nil {
		return fmt.Errorf("Docker 未运行，请先启动 Docker Desktop")
	}
	if _, err := exec.Command("docker", "image", "inspect", s.cfg.Image).Output(); err != nil {
		return fmt.Errorf("打包镜像不存在：%s", s.cfg.Image)
	}

	exists, running := s.runtimeInspect()
	if running {
		return nil
	}
	if exists {
		if out, err := exec.Command("docker", "start", runtimeName).CombinedOutput(); err != nil {
			return fmt.Errorf("启动失败: %v %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}

	args := []string{
		"run", "-d", "--name", runtimeName,
		"-e", "TZ=Asia/Shanghai",
		"-v", s.cfg.GradleVol + ":/root/.gradle",
		"-v", s.cfg.PnpmVol + ":/root/.local/share/pnpm/store",
		"--entrypoint", "/bin/sleep",
		s.cfg.Image, "infinity",
	}
	if out, err := exec.Command("docker", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("创建环境失败: %v %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (s *Store) StopRuntime() error {
	exists, running := s.runtimeInspect()
	if !exists || !running {
		return nil
	}
	// stop 就够了：内存立刻释放，下次 start 又是热的
	if out, err := exec.Command("docker", "stop", runtimeName).CombinedOutput(); err != nil {
		return fmt.Errorf("停止失败: %v %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (s *Store) RestartRuntime() error {
	// 用「删了重建」而不是 docker restart：重建才会用上最新构建的镜像
	if exists, _ := s.runtimeInspect(); exists {
		_ = s.StopRuntime()
		exec.Command("docker", "rm", "-f", runtimeName).Run()
	}
	return s.StartRuntime()
}

// cleanWorkspace 清空容器内的工作目录，避免上一次的产物/资源串味
func (s *Store) cleanWorkspace() error {
	script := "for d in /workspace/src /workspace/output /workspace/config /workspace/work; do " +
		"mkdir -p $d; find $d -mindepth 1 -maxdepth 1 -exec rm -rf {} + ; done"
	out, err := exec.Command("docker", "exec", runtimeName, "/bin/bash", "-c", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
