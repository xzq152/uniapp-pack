// 托盘宿主与启动弹窗。
//
// 与服务端同属 package main，所以一个进程里既跑 HTTP 又跑托盘——
// 发布版因此只有一个 exe，不需要「托盘再拉起服务」的两段式。
package main

import (
	"context"
	_ "embed"
	"fmt"
	"net"
	"net/http"
	"os"
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
	gdi32    = syscall.NewLazyDLL("gdi32.dll")

	pMessageBoxW      = user32.NewProc("MessageBoxW")
	pRegisterClassExW = user32.NewProc("RegisterClassExW")
	pCreateWindowExW  = user32.NewProc("CreateWindowExW")
	pDefWindowProcW   = user32.NewProc("DefWindowProcW")
	pGetMessageW      = user32.NewProc("GetMessageW")
	pTranslateMessage = user32.NewProc("TranslateMessage")
	pDispatchMessageW = user32.NewProc("DispatchMessageW")
	pPostQuitMessage  = user32.NewProc("PostQuitMessage")
	pDestroyWindow    = user32.NewProc("DestroyWindow")
	pCreatePopupMenu  = user32.NewProc("CreatePopupMenu")
	pAppendMenuW      = user32.NewProc("AppendMenuW")
	pTrackPopupMenu   = user32.NewProc("TrackPopupMenu")
	pSetForegroundWin = user32.NewProc("SetForegroundWindow")
	pGetCursorPos     = user32.NewProc("GetCursorPos")
	pSetTimer         = user32.NewProc("SetTimer")
	pLoadIconW        = user32.NewProc("LoadIconW")
	pShowWindow       = user32.NewProc("ShowWindow")
	pUpdateWindow     = user32.NewProc("UpdateWindow")
	pGetClientRect    = user32.NewProc("GetClientRect")
	pFillRect         = user32.NewProc("FillRect")
	pBeginPaint       = user32.NewProc("BeginPaint")
	pEndPaint         = user32.NewProc("EndPaint")
	pCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	pDeleteObject     = gdi32.NewProc("DeleteObject")
	pSetTextColor     = gdi32.NewProc("SetTextColor")
	pSetBkMode        = gdi32.NewProc("SetBkMode")
	pDrawTextW        = user32.NewProc("DrawTextW")
	pCreateFontW      = gdi32.NewProc("CreateFontW")
	pSelectObject     = gdi32.NewProc("SelectObject")
	pGetStockObject   = gdi32.NewProc("GetStockObject")
	pShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
	pShellExecuteW    = shell32.NewProc("ShellExecuteW")
	pCreateMutexW     = kernel32.NewProc("CreateMutexW")
	pGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	pLoadImageW       = user32.NewProc("LoadImageW")
	pGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	pPostMessageW     = user32.NewProc("PostMessageW")
	pFindWindowW      = user32.NewProc("FindWindowW")
	pIsWindow          = user32.NewProc("IsWindow")
)

const (
	appTitle = "uniapp 安卓本地打包"

	wmApp        = 0x8000
	trayMsg      = wmApp + 0x41
	wmLButtonDbl = 0x0203
	wmRButtonUp  = 0x0205
	wmTimer      = 0x0113
	wmDestroy    = 0x0002
	wmClose      = 0x0010
	wmPaint      = 0x000F
	wmEraseBg    = 0x0014
	wmLButtonUp  = 0x0202

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
	swShow         = 5
	swHide         = 0
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

type point struct{ x, y int32 }

type rect struct{ left, top, right, bottom int32 }

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

//go:embed tray.ico
var trayICO []byte

var (
	exeDir    string
	server    *http.Server
	port      string
	outputDir string

	hWndTray   uintptr
	hWndWelcome uintptr
	hIconTray  uintptr
	welcomeOpen bool
)

func utf16Ptr(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func msgBox(text, caption string, flags uintptr) {
	pMessageBoxW.Call(0, uintptr(unsafe.Pointer(utf16Ptr(text))),
		uintptr(unsafe.Pointer(utf16Ptr(caption))), flags|0x1000)
}

func localURL() string { return "http://127.0.0.1:" + port }

// lanIP 取一个最像样的局域网 IP（优先 192.168），没有就 127.0.0.1
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

// portInUse 只用来判「端口是否被占」，不用于判断「是否已有本程序在跑」——
// 后者必须靠互斥体，否则端口被别的程序占时会误判成"已在运行"。
func portInUse(p string) bool {
	c, err := net.DialTimeout("tcp", "127.0.0.1:"+p, 300*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

func waitListening(p string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if portInUse(p) {
			return true
		}
		time.Sleep(120 * time.Millisecond)
	}
	return false
}

func openURL(url string) {
	pShellExecuteW.Call(0,
		uintptr(unsafe.Pointer(utf16Ptr("open"))),
		uintptr(unsafe.Pointer(utf16Ptr(url))),
		0, 0, 1)
}

// notifyExistingInstance 已有实例在跑时的处理：把它的窗口叫到前面，或直接开页面
func notifyExistingInstance() {
	// 先试着找欢迎窗口并显示到前台
	h, _, _ := pFindWindowW.Call(
		uintptr(unsafe.Pointer(utf16Ptr(welcomeClass))),
		uintptr(unsafe.Pointer(utf16Ptr(appTitle))))
	if r, _, _ := pIsWindow.Call(h); r != 0 {
		pShowWindow.Call(h, swShow)
		pSetForegroundWin.Call(h)
		return
	}
	// 没有窗口（已经关掉），就直接打开页面
	if p := portFromExisting(); p != "" {
		openURL("http://127.0.0.1:" + p)
		return
	}
	msgBox("打包服务已经在运行。\r\n\r\n右下角托盘有图标：双击打开页面，右键菜单可退出。",
		appTitle, mbOK|mbIconInfo)
}

// portFromExisting 从 aenv 端口猜不出来，就用默认 8080；
// 用户改过 PORT 的话，靠托盘图标去操作即可。
func portFromExisting() string {
	if p := strings.TrimSpace(os.Getenv("PORT")); p != "" {
		if portInUse(p) {
			return p
		}
	}
	if portInUse("8080") {
		return "8080"
	}
	return ""
}

// ---------------------------------------------------------------- 托盘

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
			h, _, _ := pLoadImageW.Call(0, uintptr(unsafe.Pointer(p)), 1, size, size, 0x10)
			if h != 0 {
				return h
			}
		}
	}
	if exeDir != "" {
		_ = os.WriteFile(filepath.Join(exeDir, "tray-error.txt"),
			[]byte("LoadImage 失败，已退回系统默认图标\r\n"), 0o644)
	}
	fallback, _, _ := pLoadIconW.Call(0, idiApplication)
	return fallback
}

func trayData(flags uint32, balloonTitle, balloonText string) *notifyIconData {
	n := &notifyIconData{
		hWnd:             hWndTray,
		uID:              1,
		uFlags:           flags,
		uCallbackMessage: trayMsg,
		hIcon:            hIconTray,
	}
	tip := "uniapp 安卓本地打包 · 运行中\r\n" + localURL() + "\r\n双击打开页面，右键更多"
	copy(n.szTip[:], syscall.StringToUTF16(tip))
	if balloonTitle != "" || balloonText != "" {
		n.uFlags |= nifInfo
		copy(n.szInfoTitle[:], syscall.StringToUTF16(balloonTitle))
		copy(n.szInfo[:], syscall.StringToUTF16(balloonText))
		n.dwInfoFlags = 0x1
	}
	n.cbSize = uint32(unsafe.Sizeof(*n))
	return n
}

func initTray() error {
	hInst, _, _ := pGetModuleHandleW.Call(0)
	className := utf16Ptr("uniappPackTrayWnd")
	var wc wndClassEx
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	wc.lpfnWndProc = syscall.NewCallback(trayWndProc)
	wc.hInstance = hInst
	wc.lpszClassName = className
	if r, _, _ := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		debugLog("RegisterClassExW(tray) failed")
		return fmt.Errorf("注册窗口类失败")
	}
	hwnd, _, _ := pCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(utf16Ptr(appTitle))),
		0, 0, 0, 0, 0, 0, 0, hInst, 0)
	if hwnd == 0 {
		debugLog("CreateWindowExW(tray) failed")
		return fmt.Errorf("创建隐藏窗口失败")
	}
	hWndTray = hwnd
	hIconTray = createTrayIcon()

	n := trayData(nifMessage|nifIcon|nifTip|nifInfo, "打包服务已启动", lanURL())
	if r, _, _ := pShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(n))); r == 0 {
		_ = os.WriteFile(filepath.Join(exeDir, "tray-error.txt"),
			[]byte(fmt.Sprintf("Shell_NotifyIcon NIM_ADD failed, cbSize=%d\r\n", n.cbSize)), 0o644)
		debugLog(fmt.Sprintf("Shell_NotifyIcon failed cbSize=%d", n.cbSize))
		return fmt.Errorf("托盘图标注册失败")
	}
	debugLog("tray icon added")
	// 每秒刷新 tip（端口/地址变化时能跟上）
	pSetTimer.Call(hwnd, 1, 3000, 0)
	return nil
}

func lanURL() string { return "http://" + lanIP() + ":" + port }

func trayWndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	switch msg {
	case trayMsg:
		switch uint32(lparam) & 0xFFFF {
		case wmLButtonDbl:
			openURL(localURL())
		case wmRButtonUp:
			showTrayMenu(hwnd)
		}
		return 0
	case wmTimer:
		n := trayData(nifTip, "", "")
		pShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(n)))
		return 0
	case wmClose:
		shutdown()
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

func showTrayMenu(hwnd uintptr) {
	hMenu, _, _ := pCreatePopupMenu.Call()

	status := "状态：服务运行中 · 端口 " + port
	pAppendMenuW.Call(hMenu, mfString|0x00000001, 0,
		uintptr(unsafe.Pointer(utf16Ptr(status))))
	pAppendMenuW.Call(hMenu, mfSeparator, 0, 0)
	pAppendMenuW.Call(hMenu, mfString, 1, uintptr(unsafe.Pointer(utf16Ptr("打开打包页面"))))
	pAppendMenuW.Call(hMenu, mfString, 2, uintptr(unsafe.Pointer(utf16Ptr("打开产物文件夹"))))
	pAppendMenuW.Call(hMenu, mfString, 3, uintptr(unsafe.Pointer(utf16Ptr("显示地址窗口"))))
	pAppendMenuW.Call(hMenu, mfSeparator, 0, 0)
	pAppendMenuW.Call(hMenu, mfString, 4, uintptr(unsafe.Pointer(utf16Ptr("退出（停止服务）"))))

	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWin.Call(hwnd)

	cmd, _, _ := pTrackPopupMenu.Call(hMenu,
		tpmRightButton|tpmReturnCmd|0x0080,
		uintptr(pt.x), uintptr(pt.y), 0, hwnd, 0)

	switch cmd {
	case 1:
		openURL(localURL())
	case 2:
		openOutputDir()
	case 3:
		showWelcomeWindow()
	case 4:
		shutdown()
	}
}

func openOutputDir() {
	if outputDir == "" {
		return
	}
	_ = os.MkdirAll(outputDir, 0o755)
	pShellExecuteW.Call(0,
		uintptr(unsafe.Pointer(utf16Ptr("open"))),
		uintptr(unsafe.Pointer(utf16Ptr(outputDir))),
		0, 0, 1)
}

// shutdown 停服务并退出托盘。
//
// 注意执行时机：这个函数在 wndProc 回调里被调用，而 wndProc 跑在消息循环线程上。
// 如果在这里同步 server.Shutdown（最多等 3 秒），消息循环就被卡住，
// 后续的 DestroyWindow / PostQuitMessage 也就迟迟不执行，表现为「点了退出没反应」。
// 所以先立刻拆窗口、让消息循环尽快退出，服务停止放到后台并给一个短超时。
func shutdown() {
	debugLog("shutdown requested")

	if hWndTray != 0 {
		debugLog("destroying tray window")
		pDestroyWindow.Call(hWndTray) // 会同步触发 wmDestroy → PostQuitMessage
	}
	pPostQuitMessage.Call(0) // 双保险：即使上一步没走通也能退出

	if server != nil {
		srv := server
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = srv.Shutdown(ctx)
			debugLog("http server shut down")
			// 给日志和连接收尾一点时间再强制退出，避免用户看到"已退出"但仍占着端口
			time.Sleep(200 * time.Millisecond)
			os.Exit(0)
		}()
	} else {
		os.Exit(0)
	}
}

// ---------------------------------------------------------------- 启动弹窗

const welcomeClass = "uniappPackWelcomeWnd"

// 按钮区域（客户区坐标），用于命中测试
var (
	btnOpen   rect
	btnFolder rect
	btnExit   rect
)

type cred struct{}

func showWelcomeWindow() {
	// 已有窗口就激活它，不要重复创建
	h, _, _ := pFindWindowW.Call(
		uintptr(unsafe.Pointer(utf16Ptr(welcomeClass))),
		uintptr(unsafe.Pointer(utf16Ptr(appTitle))))
	if r, _, _ := pIsWindow.Call(h); r != 0 {
		pShowWindow.Call(h, swShow)
		pSetForegroundWin.Call(h)
		return
	}

	hInst, _, _ := pGetModuleHandleW.Call(0)
	className := utf16Ptr(welcomeClass)
	var wc wndClassEx
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	wc.lpfnWndProc = syscall.NewCallback(welcomeWndProc)
	wc.hInstance = hInst
	wc.lpszClassName = className
	wc.hbrBackground = 6 // COLOR_WINDOW+1
	if r, _, _ := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		debugLog("RegisterClassExW(welcome) failed")
		return
	}

	const w, hgt = 460, 300
	sw, _, _ := pGetSystemMetrics.Call(0) // SM_CXSCREEN
	sh, _, _ := pGetSystemMetrics.Call(1) // SM_CYSCREEN
	x := (int(sw) - w) / 2
	y := (int(sh) - hgt) / 2

	hwnd, _, _ := pCreateWindowExW.Call(
		0x00000008|0x00010000, // WS_EX_TOPMOST | WS_EX_APPWINDOW
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(utf16Ptr(appTitle))),
		0x00C00000|0x80000000|0x04000000, // WS_CAPTION|WS_POPUP|WS_SYSMENU
		uintptr(x), uintptr(y), w, hgt, 0, 0, hInst, 0)
	if hwnd == 0 {
		return
	}
	hWndWelcome = hwnd
	welcomeOpen = true
	pShowWindow.Call(hwnd, swShow)
	pUpdateWindow.Call(hwnd)
	pSetForegroundWin.Call(hwnd)
	// 调试留痕：确认窗口真的建出来了（弹窗是发布版第一眼看到的东西，不能静默失败）
	debugLog(fmt.Sprintf("welcome window hwnd=%d pos=%d,%d size=%dx%d", hwnd, x, y, w, hgt))
}

// debugLog 写一行调试信息到 exe 同级 debug.log（仅用于排查，正常不产生内容以外的影响）
func debugLog(line string) {
	if exeDir == "" {
		return
	}
	f, err := os.OpenFile(filepath.Join(exeDir, "debug.log"),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\r\n", time.Now().Format("15:04:05"), line)
}

func welcomeWndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	switch msg {
	case wmPaint:
		paintWelcome(hwnd)
		return 0
	case wmEraseBg:
		return 1
	case wmLButtonUp:
		x := int32(lparam) & 0xFFFF
		y := (int32(lparam) >> 16) & 0xFFFF
		switch {
		case hit(btnOpen, x, y):
			openURL(localURL())
		case hit(btnFolder, x, y):
			openOutputDir()
		case hit(btnExit, x, y):
			shutdown()
		}
		return 0
	case wmClose:
		// 关窗口不退出程序：服务继续在托盘跑
		pShowWindow.Call(hwnd, swHide)
		welcomeOpen = false
		return 0
	case wmDestroy:
		welcomeOpen = false
		hWndWelcome = 0
		return 0
	}
	ret, _, _ := pDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return ret
}

func hit(r rect, x, y int32) bool {
	return x >= r.left && x <= r.right && y >= r.top && y <= r.bottom
}

// drawText 在指定矩形里画一行文字
func drawText(hdc uintptr, text string, r *rect, size int32, bold bool, color uint32, center bool) {
	weight := int32(400)
	if bold {
		weight = 700
	}
	hFont, _, _ := pCreateFontW.Call(
		uintptr(-size), 0, 0, 0, uintptr(weight), 0, 0, 0,
		134, // GB2312_CHARSET，避免中文变方块
		0, 0, 5, 0,
		uintptr(unsafe.Pointer(utf16Ptr("Microsoft YaHei UI"))))
	old, _, _ := pSelectObject.Call(hdc, hFont)
	pSetTextColor.Call(hdc, uintptr(color))
	pSetBkMode.Call(hdc, 1) // TRANSPARENT
	flags := uint32(0x00000004|0x00000080|0x00000020) // DT_VCENTER|DT_SINGLELINE|DT_CENTER... 见下
	if center {
		flags = 0x00000001 | 0x00000004 | 0x00000020 | 0x00000080
	} else {
		flags = 0x00000004 | 0x00000020 | 0x00000080
	}
	pDrawTextW.Call(hdc,
		uintptr(unsafe.Pointer(utf16Ptr(text))),
		^-uintptr(0),
		uintptr(unsafe.Pointer(r)),
		uintptr(flags))
	pSelectObject.Call(hdc, old)
	pDeleteObject.Call(hFont)
}

func drawButton(hdc uintptr, label string, r rect, primary bool) uint32 {
	var bg uint32 = 0x00F2F3F5
	var fg uint32 = 0x002A2F36
	if primary {
		bg = 0x00D95200 // BGR：0x0052D9 蓝 → 0x00D95200
		fg = 0x00FFFFFF
	}
	brush, _, _ := pCreateSolidBrush.Call(uintptr(bg))
	rr := r
	pFillRect.Call(hdc, uintptr(unsafe.Pointer(&rr)), brush)
	pDeleteObject.Call(brush)
	tr := rect{r.left, r.top + 9, r.right, r.bottom}
	drawText(hdc, label, &tr, 15, true, fg, true)
	return bg
}

func paintWelcome(hwnd uintptr) {
	var ps struct {
		hdc         uintptr
		fErase      int32
		rcPaint     rect
		fRestore    int32
		fIncUpdate  int32
		rgbReserved [32]byte
	}
	hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}

	// 背景
	brush, _, _ := pCreateSolidBrush.Call(uintptr(0x00FFFFFF))
	var full rect
	pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&full)))
	pFillRect.Call(hdc, uintptr(unsafe.Pointer(&full)), brush)
	pDeleteObject.Call(brush)

	// 标题
	t := rect{24, 18, full.right - 24, 52}
	drawText(hdc, "打包服务已启动", &t, 22, true, 0x001C2126, false)

	// 提示
	sub := rect{24, 52, full.right - 24, 78}
	drawText(hdc, "保持这个程序在运行即可；关掉本窗口不影响，服务会留在托盘。", &sub, 13, false, 0x005A6472, false)

	// 地址块
	y := int32(88)
	lbl := rect{24, y, 100, y + 24}
	drawText(hdc, "本机访问", &lbl, 13, false, 0x008A94A1, false)
	v1 := rect{104, y, full.right - 24, y + 24}
	drawText(hdc, localURL(), &v1, 14, true, 0x001C2126, false)

	y += 26
	lbl2 := rect{24, y, 100, y + 24}
	drawText(hdc, "局域网", &lbl2, 13, false, 0x008A94A1, false)
	v2 := rect{104, y, full.right - 24, y + 24}
	drawText(hdc, lanURL(), &v2, 14, true, 0x001C2126, false)

	y += 26
	lbl3 := rect{24, y, 100, y + 24}
	drawText(hdc, "产物目录", &lbl3, 13, false, 0x008A94A1, false)
	v3 := rect{104, y, full.right - 24, y + 24}
	drawText(hdc, outputDir, &v3, 12, false, 0x005A6472, false)

	// 按钮排布：左侧主按钮 + 右侧两个次按钮，各留 10px 间距。
	// 之前用递减 br 的写法算错了一次，导致按钮重叠，这里改成显式的左边界推进。
	const bw, bh, gap = 104, 34, 10
	by := int32(full.bottom) - 22 - bh
	left := int32(24)

	btnOpen = rect{left, by, left + bw, by + bh}
	left += bw + gap
	btnFolder = rect{left, by, left + bw + 24, by + bh}

	// 退出放最右，符合"危险操作靠边"的习惯
	exitRight := int32(full.right) - 24
	btnExit = rect{exitRight - 72, by, exitRight, by + bh}

	drawButton(hdc, "退出", btnExit, false)
	drawButton(hdc, "打开产物目录", btnFolder, false)
	drawButton(hdc, "打开页面", btnOpen, true)

	pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
}
