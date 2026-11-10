// 停止器：双击停止打包服务（含托盘图标），不显示命令行窗口，只弹一个提示框。
// 构建：go build -ldflags "-H=windowsgui -s -w" -o 停止服务.exe ./cmd/stopper
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32        = syscall.NewLazyDLL("user32.dll")
	pMessageBoxW  = user32.NewProc("MessageBoxW")
	pFindWindowW  = user32.NewProc("FindWindowW")
	pPostMessageW = user32.NewProc("PostMessageW")
)

const (
	mbOK, mbIconInfo, mbTopMost = 0x0, 0x40, 0x1000
	wmClose                     = 0x0010
	createNoWindow              = 0x08000000
)

func utf16Ptr(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func msgBox(text, caption string, flags uintptr) {
	pMessageBoxW.Call(0, uintptr(unsafe.Pointer(utf16Ptr(text))),
		uintptr(unsafe.Pointer(utf16Ptr(caption))), flags|mbTopMost)
}

func runHidden(file string, args ...string) error {
	cmd := exec.Command(file, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return cmd.Run()
}

func main() {
	quiet := false
	withImage := false
	for _, a := range os.Args[1:] {
		switch strings.ToLower(a) {
		case "/quiet", "-quiet":
			quiet = true
		case "/image", "-image":
			withImage = true
		}
	}

	// 先停打包容器（可能要几秒），再退托盘宿主与服务
	if withImage {
		_ = runHidden("docker", "stop", "uniapp-pack-runtime")
	}

	// 1) 先请托盘宿主自己退出（它会带走服务进程、清理托盘图标）
	className := utf16Ptr("uniappPackTrayWnd")
	if hwnd, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(className)), 0); hwnd != 0 {
		pPostMessageW.Call(hwnd, wmClose, 0, 0)
		for i := 0; i < 10; i++ {
			time.Sleep(200 * time.Millisecond)
			if h, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(className)), 0); h == 0 {
				break
			}
		}
	}

	// 2) 兜底：万一托盘宿主没跑（比如被任务管理器杀掉），直接结束进程
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	taskkill := filepath.Join(root, "System32", "taskkill.exe")
	_ = runHidden(taskkill, "/F", "/IM", "uniapp-pack-server.exe")
	_ = runHidden(taskkill, "/F", "/IM", "启动服务.exe")

	if !quiet {
		text := "打包服务已停止，托盘图标已移除。\r\n\r\n需要再启动时，双击「启动服务」。"
		if withImage {
			text = "打包服务与打包镜像（容器）都已停止。\r\n\r\n需要再启动时，双击「启动服务」。"
		}
		msgBox(text, "uniapp 安卓本地打包", mbOK|mbIconInfo)
	}
}
