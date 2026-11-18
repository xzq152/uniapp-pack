//go:build windows

// Windows 发布版入口：服务端（HTTP + 内嵌前端）与托盘合并在同一个进程里。
//
// 和旧版「启动服务.exe 再拉起 uniapp-pack-server.exe」的两段式相比：
//   - 只有一个 exe，用户下载双击即可
//   - 不会出现「找不到服务程序」；退出时也不会留下孤儿进程
package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

func main() {
	exePath, err := os.Executable()
	if err != nil {
		return
	}
	exeDir = filepath.Dir(exePath)

	// 应急退出通道：uniapp-pack.exe --quit
	// 用途：界面万一卡死（比如菜单模态阻塞）、托盘也点不动时，
	// 用户还能用命令行把进程结束掉，不用去任务管理器翻。
	// 它按进程名找自己人，不依赖消息循环，所以卡死时也有效。
	for _, a := range os.Args[1:] {
		switch strings.ToLower(strings.TrimSpace(a)) {
		case "--quit", "/quit", "-quit":
			quitRunningInstances()
			return
		}
	}

	// 单实例：命名互斥体。
	// 特意不用「探测端口」来判断——端口被别的程序占用时会误判成
	// 「已有本程序在运行」，反而把真正要报的「端口冲突」盖掉了。
	mutexName := utf16Ptr(`Local\uniapp-pack-single-instance`)
	hMutex, _, lastErr := pCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(mutexName)))
	if hMutex != 0 && lastErr == syscall.ERROR_ALREADY_EXISTS {
		notifyExistingInstance()
		return
	}
	_ = hMutex // 句柄保持到进程结束

	cfg := defaultConfig(exeDir)

	// 端口确实被别的程序占了：明确报错，不要傻等
	if portInUse(cfg.Port) {
		msgBox(fmt.Sprintf("端口 %s 已被其他程序占用。\r\n\r\n"+
			"请关掉占用该端口的程序，或用环境变量 PORT 换一个端口后重新启动。", cfg.Port),
			appTitle, mbOK|mbIconError)
		return
	}

	srv, err := newServer(cfg)
	if err != nil {
		debugLog("newServer failed: " + err.Error())
		msgBox("初始化失败：\r\n"+err.Error(), appTitle, mbOK|mbIconError)
		return
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	if !waitListening(cfg.Port, 8*time.Second) {
		msg := "启动超时"
		select {
		case e := <-errCh:
			msg = e.Error()
		default:
		}
		debugLog("listen failed: " + msg)
		msgBox("服务启动失败：\r\n"+msg, appTitle, mbOK|mbIconError)
		return
	}

	server = srv
	port = cfg.Port
	outputDir = cfg.OutputDir
	debugLog(fmt.Sprintf("server listening on %s, output=%s", port, outputDir))

	// 服务中途挂掉（端口被抢、监听出错）时要让用户知道，
	// 否则托盘还显示"运行中"、点开页面却打不开，很难排查。
	go func() {
		e := <-errCh
		if e == nil {
			return
		}
		// 主动退出时 ListenAndServe 也会返回 http.ErrServerClosed，
		// 这是正常关闭，不能当成崩溃——否则日志里会留一条误导性的"意外停止"。
		if errors.Is(e, http.ErrServerClosed) {
			debugLog("server closed (normal shutdown)")
			return
		}
		debugLog("server stopped unexpectedly: " + e.Error())
		if hWndTray != 0 {
			pPostMessageW.Call(hWndTray, wmClose, 0, 0)
		}
	}()

	// 托盘失败不致命：服务还在跑，只是没有图标
	if err := initTray(); err != nil {
		debugLog("initTray failed: " + err.Error())
		msgBox("托盘初始化失败：\r\n"+err.Error()+"\r\n\r\n服务仍在运行，可用浏览器访问 "+localURL(),
			appTitle, mbOK|mbIconWarning)
	}

	// 启动弹窗：展示端口与 IP，可一键打开浏览器 / 产物目录
	showWelcomeWindow()

	// 消息循环是托盘和窗口的驱动源，没有它窗口不会绘制、菜单点不动
	runMessageLoop()
}

// runMessageLoop 标准 Win32 消息循环
func runMessageLoop() {
	debugLog("entering message loop")
	var m msgStruct
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	debugLog("message loop exited")
}

// quitRunningInstances 结束所有正在运行的 uniapp-pack.exe（含自己之外的那个）。
//
// 为什么需要它：正常情况下点「退出」或托盘菜单就能退。但如果界面卡死
// （例如弹窗菜单处于模态阻塞、消息循环停止响应），用户既点不动菜单、
// 又不想去任务管理器里翻进程。这时 `uniapp-pack.exe --quit` 能救场。
//
// 它不经过消息循环，直接按进程名杀，所以卡死状态下也有效。
func quitRunningInstances() {
	// 先优雅尝试：给正在运行的实例发 WM_CLOSE，让它自己走 shutdown（能停服务和清图标）
	trayH, _, _ := pFindWindowW.Call(
		uintptr(unsafe.Pointer(utf16Ptr("uniappPackTrayWnd"))),
		uintptr(unsafe.Pointer(utf16Ptr(appTitle))))
	if r, _, _ := pIsWindow.Call(trayH); r != 0 {
		pPostMessageW.Call(trayH, wmClose, 0, 0)
		// 等它自己退（正常情况 1 秒内）
		for i := 0; i < 20; i++ {
			time.Sleep(100 * time.Millisecond)
			if r2, _, _ := pIsWindow.Call(trayH); r2 == 0 {
				return
			}
		}
	}

	// 兜底：按进程名强杀。
	// 注意要排除自己：--quit 这个实例本身也叫 uniapp-pack.exe，
	// 直接 taskkill /IM 会把自己也杀掉，命令就永远等不到正常返回了。
	// 用 /FI "PID ne <自己>" 把它排除掉。
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	filter := fmt.Sprintf("PID ne %d", os.Getpid())
	cmd := exec.Command(filepath.Join(root, "System32", "taskkill.exe"),
		"/F", "/IM", "uniapp-pack.exe", "/FI", filter)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	out, err := cmd.CombinedOutput()
	if err != nil {
		debugLog(fmt.Sprintf("taskkill failed: %v (output: %s)", err, strings.TrimSpace(string(out))))
	}
}
