// 打包服务托盘宿主：启动服务（无窗口）→ 托盘常驻图标 → 右键菜单控制。
// 构建：go build -ldflags "-H=windowsgui -s -w" -o 启动服务.exe ./cmd/launcher
package main

import (
	_ "embed"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")

	pMessageBoxW       = user32.NewProc("MessageBoxW")
	pRegisterClassExW  = user32.NewProc("RegisterClassExW")
	pCreateWindowExW   = user32.NewProc("CreateWindowExW")
	pDefWindowProcW    = user32.NewProc("DefWindowProcW")
	pGetMessageW       = user32.NewProc("GetMessageW")
	pTranslateMessage  = user32.NewProc("TranslateMessage")
	pDispatchMessageW  = user32.NewProc("DispatchMessageW")
	pPostQuitMessage   = user32.NewProc("PostQuitMessage")
	pDestroyWindow     = user32.NewProc("DestroyWindow")
	pCreatePopupMenu   = user32.NewProc("CreatePopupMenu")
	pAppendMenuW       = user32.NewProc("AppendMenuW")
	pTrackPopupMenu    = user32.NewProc("TrackPopupMenu")
	pSetForegroundWin  = user32.NewProc("SetForegroundWindow")
	pGetCursorPos      = user32.NewProc("GetCursorPos")
	pSetTimer          = user32.NewProc("SetTimer")
	pLoadIconW         = user32.NewProc("LoadIconW")
	pCreateIconFromPNG = user32.NewProc("CreateIconFromResourceEx")
	pShellNotifyIconW  = shell32.NewProc("Shell_NotifyIconW")
	pShellExecuteW     = shell32.NewProc("ShellExecuteW")
	pCreateMutexW      = kernel32.NewProc("CreateMutexW")
	pGetModuleHandleW  = kernel32.NewProc("GetModuleHandleW")
	pLoadImageW        = user32.NewProc("LoadImageW")
	pGetSystemMetrics  = user32.NewProc("GetSystemMetrics")
	pPostMessageW      = user32.NewProc("PostMessageW")
)

const (
	wmApp        = 0x8000
	trayMsg      = wmApp + 0x41
	wmLButtonDbl = 0x0203
	wmRButtonUp  = 0x0205
	wmTimer      = 0x0113
	wmDestroy    = 0x0002
	wmClose      = 0x0010

	nimAdd    = 0
	nimModify = 1
	nimDelete = 2

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004
	nifInfo    = 0x00000010

	mfString       = 0x00000000
	mfSeparator    = 0x00000800
	tpmRightButton = 0x00000002
	tpmReturnCmd   = 0x00000100
	mbOK           = 0x00000000
	mbIconError    = 0x00000010
	mbIconInfo     = 0x00000040
	mbIconWarning  = 0x00000030
	createNoWindow = 0x08000000
	idiApplication = 32512
)

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type point struct {
	x, y int32
}

type msgStruct struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
	private uint32
}

type guid struct {
	data1 uint32
	data2 uint16
	data3 uint16
	data4 [8]byte
}

type notifyIconData struct {
	cbSize           uint32
	hWnd             uintptr
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            uintptr
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         guid
	hBalloonIcon     uintptr
}

// 托盘图标：Android 机器人头（Material Design Icons 的 android，填充 #3DDC84）。
// tray.svg 是源图；tray.ico 由 256px 渲染图降采样生成，含 16/24/32/48/64 五种尺寸。
//
//go:embed tray.ico
var trayICO []byte

var (
	exeDir     string
	serverPath string
	hWndMain   uintptr
	hIconTray  uintptr
	running    bool
	tipText    string
)

func utf16Ptr(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func msgBox(text, caption string, flags uintptr) {
	pMessageBoxW.Call(0, uintptr(unsafe.Pointer(utf16Ptr(text))),
		uintptr(unsafe.Pointer(utf16Ptr(caption))), flags|0x1000)
}

func portOpen() bool {
	c, err := net.DialTimeout("tcp", "127.0.0.1:8080", 250*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

func lanIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1"
	}
	fallback := ""
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil {
				continue
			}
			ip := ipnet.IP.String()
			if strings.HasPrefix(ip, "169.254.") || strings.HasPrefix(ip, "127.") {
				continue
			}
			if strings.HasPrefix(ip, "192.168.") {
				return ip
			}
			if fallback == "" && !strings.HasPrefix(ip, "172.17.") {
				fallback = ip
			}
		}
	}
	if fallback != "" {
		return fallback
	}
	return "127.0.0.1"
}

func runHidden(file string, args ...string) error {
	cmd := exec.Command(file, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return cmd.Run()
}

func startServer() error {
	cmd := exec.Command(serverPath)
	cmd.Dir = exeDir
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()
	return nil
}

func stopServer() {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	_ = runHidden(filepath.Join(root, "System32", "taskkill.exe"), "/F", "/IM", "uniapp-pack-server.exe")
}

func waitPort(d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if portOpen() {
			return true
		}
		time.Sleep(300 * time.Millisecond)
	}
	return false
}

// probeService 确认 8080 上是"我们的服务"（而不是被别的程序占用）
func probeService() bool {
	c := &http.Client{Timeout: 800 * time.Millisecond}
	resp, err := c.Get("http://127.0.0.1:8080/api/env")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

// stopContainer 停掉打包用的 Docker 容器（释放内存），不影响服务本体
func stopContainer() {
	cmd := exec.Command("docker", "stop", "uniapp-pack-runtime")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	_ = cmd.Run()
}

// trayNotify 弹一个气泡提示
func trayNotify(title, text string) {
	n := trayData(nifTip|nifInfo, title, text)
	pShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(n)))
}

func openPage() {
	url := "http://127.0.0.1:8080"
	pShellExecuteW.Call(0,
		uintptr(unsafe.Pointer(utf16Ptr("open"))),
		uintptr(unsafe.Pointer(utf16Ptr(url))),
		0, 0, 1)
}

// ---------------------------------------------------------------- 托盘图标

// createTrayIcon 把内置的 .ico 落到临时文件，用 LoadImage 取图标。
// 注意：必须走 LoadImage / CreateIconFromResourceEx 的"单张位图"格式，
// 直接丢整个 .ico 文件给 CreateIconFromResourceEx 会失败并静默退回系统默认图标。
func createTrayIcon() uintptr {
	if len(trayICO) > 0 {
		tmp := filepath.Join(os.TempDir(), fmt.Sprintf("uniapp-tray-%d.ico", os.Getpid()))
		if err := os.WriteFile(tmp, trayICO, 0o644); err == nil {
			defer os.Remove(tmp)
			size, _, _ := pGetSystemMetrics.Call(49) // SM_CXSMICON
			if size == 0 {
				size = 16
			}
			p, _ := syscall.UTF16PtrFromString(tmp)
			h, _, _ := pLoadImageW.Call(0, uintptr(unsafe.Pointer(p)), 1, size, size, 0x10) // IMAGE_ICON | LR_LOADFROMFILE
			if h != 0 {
				return h
			}
		}
	}
	// 兜底：系统默认图标（同时留痕，避免又静默变成默认图标）
	_ = os.WriteFile(filepath.Join(exeDir, "tray-error.txt"),
		[]byte(fmt.Sprintf("LoadImage 失败，已退回系统默认图标。exeDir=%s\r\n", exeDir)), 0o644)
	fallback, _, _ := pLoadIconW.Call(0, idiApplication)
	return fallback
}

func trayData(flags uint32, balloontitle, balloonText string) *notifyIconData {
	n := &notifyIconData{
		hWnd:             hWndMain,
		uID:              1,
		uFlags:           flags,
		uCallbackMessage: trayMsg,
		hIcon:            hIconTray,
	}
	copy(n.szTip[:], syscall.StringToUTF16(tipText))
	if balloontitle != "" || balloonText != "" {
		n.uFlags |= nifInfo
		copy(n.szInfoTitle[:], syscall.StringToUTF16(balloontitle))
		copy(n.szInfo[:], syscall.StringToUTF16(balloonText))
		n.dwInfoFlags = 0x1 // NIIF_INFO
	}
	n.cbSize = uint32(unsafe.Sizeof(*n))
	return n
}

func trayAdd(balloonTitle, balloonText string) bool {
	flags := uint32(nifMessage | nifIcon | nifTip | nifInfo)
	n := trayData(flags, balloonTitle, balloonText)
	r, _, _ := pShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(n)))
	if r == 0 {
		// 注册失败（比如结构体布局不对）留个痕迹，方便排查
		_ = os.WriteFile(filepath.Join(exeDir, "tray-error.txt"),
			[]byte(fmt.Sprintf("Shell_NotifyIcon NIM_ADD failed, cbSize=%d\r\n", n.cbSize)), 0o644)
		return false
	}
	_ = os.Remove(filepath.Join(exeDir, "tray-error.txt"))
	return true
}

func trayUpdateTip() {
	tipText = buildTip()
	n := trayData(nifTip, "", "")
	pShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(n)))
}

func buildTip() string {
	if portOpen() {
		return "uniapp 安卓本地打包 · 运行中\r\nhttp://" + lanIP() + ":8080\r\n双击打开页面，右键更多"
	}
	return "uniapp 安卓本地打包 · 未运行\r\n右键菜单可重新启动"
}

// ---------------------------------------------------------------- 窗口与消息

func wndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	switch msg {
	case trayMsg:
		switch uint32(lparam) & 0xFFFF {
		case wmLButtonDbl:
			openPage()
		case wmRButtonUp:
			showMenu(hwnd)
		}
		return 0

	case wmTimer:
		trayUpdateTip()
		return 0

	case wmClose: // 外部请求关闭（停止服务.exe 会发这个）
		stopServer()
		pDestroyWindow.Call(hwnd)
		return 0

	case wmDestroy:
		n := trayData(nifTip, "", "")
		pShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(n)))
		pPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := pDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return ret
}

func showMenu(hwnd uintptr) {
	hMenu, _, _ := pCreatePopupMenu.Call()

	status := "状态：服务运行中"
	if !portOpen() {
		status = "状态：服务未运行"
	}
	containerText := "打包环境：运行中"
	if !containerRunning() {
		containerText = "打包环境：已停止"
	}
	pAppendMenuW.Call(hMenu, mfString|0x00000001, 0, uintptr(unsafe.Pointer(utf16Ptr(status+" · "+containerText))))
	pAppendMenuW.Call(hMenu, mfSeparator, 0, 0)
	pAppendMenuW.Call(hMenu, mfString, 1, uintptr(unsafe.Pointer(utf16Ptr("打开打包页面"))))
	pAppendMenuW.Call(hMenu, mfString, 2, uintptr(unsafe.Pointer(utf16Ptr("重新启动服务"))))
	pAppendMenuW.Call(hMenu, mfString, 3, uintptr(unsafe.Pointer(utf16Ptr("停止打包环境（仅容器）"))))
	pAppendMenuW.Call(hMenu, mfSeparator, 0, 0)
	pAppendMenuW.Call(hMenu, mfString, 4, uintptr(unsafe.Pointer(utf16Ptr("停止服务并停止镜像"))))
	pAppendMenuW.Call(hMenu, mfString, 5, uintptr(unsafe.Pointer(utf16Ptr("仅停止服务（保留镜像）"))))

	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWin.Call(hwnd)

	cmd, _, _ := pTrackPopupMenu.Call(hMenu,
		tpmRightButton|tpmReturnCmd|0x0080, // TPM_NONOTIFY
		uintptr(pt.x), uintptr(pt.y), 0, hwnd, 0)

	switch cmd {
	case 1:
		openPage()
	case 2:
		restartServer()
	case 3:
		go func() {
			stopContainer()
			trayNotify("打包环境已停止", "容器已关闭，服务仍在运行")
		}()
	case 4:
		// 先停容器（可能要几秒），再退服务、退托盘
		go func() {
			stopContainer()
			pPostMessageW.Call(hwnd, wmClose, 0, 0)
		}()
	case 5:
		stopServer()
		pDestroyWindow.Call(hwnd)
	}
}

// containerRunning 判断打包容器是否在跑（docker 查询，隐藏执行）
func containerRunning() bool {
	cmd := exec.Command("docker", "ps", "--filter", "name=uniapp-pack-runtime", "--format", "{{.Names}}")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "uniapp-pack-runtime")
}

func restartServer() {
	stopServer()
	time.Sleep(600 * time.Millisecond)
	if err := startServer(); err != nil {
		msgBox("启动失败："+err.Error(), "uniapp 安卓本地打包", mbOK|mbIconError)
		return
	}
	if waitPort(9 * time.Second) {
		tipText = buildTip()
		n := trayData(nifTip|nifInfo, "服务已重新启动", "http://"+lanIP()+":8080")
		pShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(n)))
	} else {
		msgBox("启动超时，请检查 8080 端口是否被占用。", "启动失败", mbOK|mbIconError)
	}
}

func main() {
	quiet := false
	openAfter := false
	for _, a := range os.Args[1:] {
		switch strings.ToLower(a) {
		case "/quiet", "-quiet":
			quiet = true
		case "/open", "-open":
			openAfter = true
		}
	}

	exe, err := os.Executable()
	if err != nil {
		return
	}
	exeDir = filepath.Dir(exe)
	serverPath = filepath.Join(exeDir, "uniapp-pack-server.exe")

	if _, err := os.Stat(serverPath); err != nil {
		if !quiet {
			msgBox("找不到服务程序：\r\n"+serverPath, "启动失败", mbOK|mbIconError)
		}
		return
	}

	// 单实例：托盘只允许一个
	mutexName := utf16Ptr("Local\\uniapp-pack-tray")
	hMutex, _, lastErr := pCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(mutexName)))
	if hMutex != 0 && lastErr == syscall.ERROR_ALREADY_EXISTS {
		if openAfter {
			// 已经在跑了：等它就绪后直接开页面，不再弹框
			waitPort(6 * time.Second)
			openPage()
			return
		}
		if !quiet {
			msgBox("打包服务已经在运行。\r\n\r\n右下角托盘有图标：双击打开页面，右键菜单可停止。",
				"uniapp 安卓本地打包", mbOK|mbIconInfo)
		}
		return
	}
	_ = hMutex

	// 8080 被别的程序占了：明确报错，避免"以为起来了"
	if portOpen() && !probeService() {
		if !quiet {
			msgBox("8080 端口被其他程序占用（响应不是本服务）。\r\n\r\n请先释放该端口，或用 PORT 环境变量换一个端口。",
				"启动失败", mbOK|mbIconError)
		}
		return
	}

	// 服务没在跑就拉起来
	if !portOpen() {
		if err := startServer(); err != nil {
			if !quiet {
				msgBox("启动失败："+err.Error(), "启动失败", mbOK|mbIconError)
			}
			return
		}
		if !waitPort(9*time.Second) && !quiet {
			msgBox("启动超时，请检查 8080 端口是否被占用。", "启动失败", mbOK|mbIconError)
		}
	}

	// 隐藏窗口，用来接收托盘消息
	hInst, _, _ := pGetModuleHandleW.Call(0)
	className := utf16Ptr("uniappPackTrayWnd")
	var wc wndClassEx
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	wc.lpfnWndProc = syscall.NewCallback(wndProc)
	wc.hInstance = hInst
	wc.lpszClassName = className
	if r, _, _ := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		if !quiet {
			msgBox("注册窗口类失败，托盘图标无法显示。", "启动失败", mbOK|mbIconError)
		}
		return
	}
	hwnd, _, _ := pCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(utf16Ptr("uniapp 安卓本地打包"))),
		0, 0, 0, 0, 0, 0, 0, hInst, 0)
	if hwnd == 0 {
		if !quiet {
			msgBox("创建隐藏窗口失败，托盘图标无法显示。", "启动失败", mbOK|mbIconError)
		}
		return
	}
	hWndMain = hwnd
	hIconTray = createTrayIcon()
	tipText = buildTip()

	var added bool
	if quiet {
		added = trayAdd("", "")
	} else {
		added = trayAdd("打包服务已启动", "http://"+lanIP()+":8080　（双击托盘图标打开页面）")
	}
	if !added && !quiet {
		msgBox("托盘图标注册失败，服务已在后台运行但看不到图标。\r\n\r\n请把 tray-error.txt 发给开发者。",
			"提示", mbOK|mbIconWarning)
	}
	if openAfter {
		waitPort(6 * time.Second)
		openPage()
	}

	pSetTimer.Call(hwnd, 1, 3000, 0)

	// 消息循环
	var m msgStruct
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}
