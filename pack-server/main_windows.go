//go:build windows

// Windows 发布版入口：服务端（HTTP + 内嵌前端）与托盘合并在同一个进程里。
//
// 和旧版「启动服务.exe 再拉起 uniapp-pack-server.exe」的两段式相比：
//   - 只有一个 exe，用户下载双击即可
//   - 不会出现「找不到服务程序」；退出时也不会留下孤儿进程
package main

import (
	"fmt"
	"os"
	"path/filepath"
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
