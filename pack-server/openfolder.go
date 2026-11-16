package main

import (
	"fmt"
	"os/exec"
	"runtime"
)

// ---------------------------------------------------------------------------
// 在本机文件管理器里打开产物目录
//
// 这套服务就是在本机（或局域网内某台固定机器）上跑的，构建产物直接落在
// DATA_DIR/outputs/<jobId>/ 里，所以「打开文件夹」比让浏览器再下载一遍更直接：
// 多个渠道包、build-info.json 都能一次看到。
//
// 注意：目录来自服务端自己算出来的 job 输出目录，不接受任何用户传入的路径。
// ---------------------------------------------------------------------------

// openInFileManager 用系统默认文件管理器打开 dir，立即返回（不阻塞 HTTP 处理）。
func openInFileManager(dir string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// explorer 即使成功也可能返回非 0 退出码，所以只判断能不能启动
		cmd = exec.Command("explorer.exe", dir)
	case "darwin":
		cmd = exec.Command("open", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("调用文件管理器失败: %w", err)
	}
	// 后台回收子进程，避免留下僵尸；打开失败也不影响前端已经收到的响应
	go func() { _ = cmd.Wait() }()
	return nil
}
