package main

import (
	"net/http"
	"sort"
)

// ---------------------------------------------------------------------------
// 安卓原生配置（SDK 版本 / 权限）
//
// 这些值默认来自 DCloud 离线 SDK 5.34.82692 的模板工程：
//   HBuilder-HelloUniApp/app/build.gradle  → minSdkVersion 21 / targetSdkVersion 33 / compileSdkVersion 35
//   HBuilder-HelloUniApp/app/src/main/AndroidManifest.xml → 32 条基线 uses-permission
//
// 页面里可以改，改完由 entrypoint 的 gen_override / inject_permissions 增量注入，
// 不整份覆盖 AndroidManifest.xml。
// ---------------------------------------------------------------------------

const (
	defaultMinSdk     = 21
	defaultTargetSdk  = 33
	defaultCompileSdk = 35
	sdkVersionLabel   = "5.34.82692"
)

type permInfo struct {
	Name   string `json:"name"`
	Label  string `json:"label"`
	Note   string `json:"note,omitempty"`
	MinSdk int    `json:"minSdk,omitempty"`
}

// baselinePerms 是 DCloud SDK 5.34 aar 合并后真实打进 APK 的权限（不含渠道角标等第三方权限）。
// 实测来源：aapt2 dump permissions 一个真实产物，只有这 7 条 android.permission.*
// 模板工程的 AndroidManifest.xml 里一条权限都没有——权限来自 SDK 的 aar，靠 manifest merger 合并，
// 所以「移除」必须在 Manifest 里写 tools:node="remove"，不能删模板行。
var baselinePerms = []string{
	"INTERNET",
	"ACCESS_NETWORK_STATE",
	"WRITE_EXTERNAL_STORAGE",
	"READ_EXTERNAL_STORAGE",
	"READ_MEDIA_IMAGES",
	"READ_MEDIA_VIDEO",
	"READ_MEDIA_VISUAL_USER_SELECTED",
}

// permLabels 给权限配中文名，页面上一眼能看懂在勾什么
var permLabels = map[string]string{
	"INTERNET":                       "网络访问",
	"ACCESS_NETWORK_STATE":           "读取网络状态",
	"WRITE_EXTERNAL_STORAGE":         "写入外部存储",
	"READ_EXTERNAL_STORAGE":          "读取外部存储",
	"READ_MEDIA_IMAGES":              "读取图片",
	"READ_MEDIA_VIDEO":               "读取视频",
	"READ_MEDIA_VISUAL_USER_SELECTED": "部分照片访问（Android 14+）",
	"ACCESS_COARSE_LOCATION":         "粗略定位（基站/WiFi）",
	"ACCESS_FINE_LOCATION":           "精确定位（GPS）",
	"ACCESS_BACKGROUND_LOCATION":     "后台定位",
	"ACCESS_MEDIA_LOCATION":          "照片 EXIF 位置",
	"CAMERA":                         "相机",
	"RECORD_AUDIO":                   "录音",
	"READ_MEDIA_AUDIO":               "读取音频",
	"POST_NOTIFICATIONS":             "发送通知",
	"READ_PHONE_STATE":               "读取设备信息（IMEI 等）",
	"READ_CONTACTS":                  "读取通讯录",
	"WRITE_CONTACTS":                 "写入通讯录",
	"READ_SMS":                       "读取短信",
	"SEND_SMS":                       "发送短信",
	"CALL_PHONE":                     "拨打电话",
	"READ_CALL_LOG":                  "读取通话记录",
	"ANSWER_PHONE_CALLS":             "接听电话",
	"VIBRATE":                        "振动",
	"FLASHLIGHT":                     "闪光灯",
	"BLUETOOTH":                      "蓝牙",
	"BLUETOOTH_ADMIN":                "蓝牙管理",
	"BLUETOOTH_SCAN":                 "蓝牙扫描",
	"BLUETOOTH_CONNECT":              "蓝牙连接",
	"BLUETOOTH_ADVERTISE":            "蓝牙广播",
	"NFC":                            "NFC",
	"WAKE_LOCK":                      "保持唤醒",
	"RECEIVE_BOOT_COMPLETED":         "开机自启",
	"FOREGROUND_SERVICE":             "前台服务",
	"FOREGROUND_SERVICE_LOCATION":    "前台服务·定位",
	"FOREGROUND_SERVICE_DATA_SYNC":   "前台服务·同步",
	"FOREGROUND_SERVICE_MEDIA_PLAYBACK": "前台服务·播放",
	"SCHEDULE_EXACT_ALARM":           "精确闹钟",
	"USE_EXACT_ALARM":                "精确闹钟（免申请）",
	"REQUEST_INSTALL_PACKAGES":       "应用内安装 APK",
	"SYSTEM_ALERT_WINDOW":            "悬浮窗",
	"QUERY_ALL_PACKAGES":             "查询已装应用",
	"ACTIVITY_RECOGNITION":           "运动识别（计步）",
	"BODY_SENSORS":                   "身体传感器",
	"CHANGE_WIFI_STATE":              "开关 WiFi",
	"ACCESS_WIFI_STATE":              "读取 WiFi 状态",
	"MODIFY_AUDIO_SETTINGS":          "修改音频设置",
	"GET_ACCOUNTS":                   "读取账户列表",
	"DOWNLOAD_WITHOUT_NOTIFICATION":  "静默下载",
	"USE_FINGERPRINT":                "指纹识别",
	"WRITE_SETTINGS":                 "修改系统设置",
	"CAMERA_FLASH":                   "闪光灯（旧写法）",
}

// permCatalog 是「追加」用的常用权限（已排除基线里那 7 条）
var permCatalog = []permInfo{
	{Name: "POST_NOTIFICATIONS", Label: "发送通知", Note: "Android 13+ 需单独申请", MinSdk: 33},
	{Name: "CAMERA", Label: "相机", MinSdk: 1},
	{Name: "RECORD_AUDIO", Label: "录音", MinSdk: 1},
	{Name: "READ_MEDIA_AUDIO", Label: "读取音频", Note: "Android 13+", MinSdk: 33},
	{Name: "ACCESS_FINE_LOCATION", Label: "精确定位（GPS）", MinSdk: 1},
	{Name: "ACCESS_COARSE_LOCATION", Label: "粗略定位", MinSdk: 1},
	{Name: "ACCESS_BACKGROUND_LOCATION", Label: "后台定位", Note: "商店需说明用途", MinSdk: 29},
	{Name: "ACCESS_MEDIA_LOCATION", Label: "照片 EXIF 位置", MinSdk: 29},
	{Name: "READ_PHONE_STATE", Label: "读取设备信息", Note: "IMEI 等，敏感", MinSdk: 1},
	{Name: "READ_CONTACTS", Label: "读取通讯录", MinSdk: 1},
	{Name: "WRITE_CONTACTS", Label: "写入通讯录", MinSdk: 1},
	{Name: "READ_SMS", Label: "读取短信", Note: "敏感，审核严格", MinSdk: 1},
	{Name: "SEND_SMS", Label: "发送短信", Note: "敏感，审核严格", MinSdk: 1},
	{Name: "CALL_PHONE", Label: "拨打电话", MinSdk: 1},
	{Name: "READ_CALL_LOG", Label: "读取通话记录", Note: "敏感，审核严格", MinSdk: 16},
	{Name: "ANSWER_PHONE_CALLS", Label: "接听电话", MinSdk: 26},
	{Name: "VIBRATE", Label: "振动", MinSdk: 1},
	{Name: "FLASHLIGHT", Label: "闪光灯", MinSdk: 1},
	{Name: "BLUETOOTH", Label: "蓝牙", Note: "Android 11 及以下", MinSdk: 1},
	{Name: "BLUETOOTH_ADMIN", Label: "蓝牙管理", Note: "Android 11 及以下", MinSdk: 1},
	{Name: "BLUETOOTH_SCAN", Label: "蓝牙扫描", Note: "Android 12+", MinSdk: 31},
	{Name: "BLUETOOTH_CONNECT", Label: "蓝牙连接", Note: "Android 12+", MinSdk: 31},
	{Name: "BLUETOOTH_ADVERTISE", Label: "蓝牙广播", Note: "Android 12+", MinSdk: 31},
	{Name: "NFC", Label: "NFC", MinSdk: 9},
	{Name: "WAKE_LOCK", Label: "保持唤醒", MinSdk: 1},
	{Name: "RECEIVE_BOOT_COMPLETED", Label: "开机自启", MinSdk: 1},
	{Name: "FOREGROUND_SERVICE", Label: "前台服务", MinSdk: 28},
	{Name: "FOREGROUND_SERVICE_LOCATION", Label: "前台服务·定位", Note: "Android 14 声明类型", MinSdk: 34},
	{Name: "FOREGROUND_SERVICE_DATA_SYNC", Label: "前台服务·同步", Note: "Android 14", MinSdk: 34},
	{Name: "FOREGROUND_SERVICE_MEDIA_PLAYBACK", Label: "前台服务·播放", Note: "Android 14", MinSdk: 34},
	{Name: "SCHEDULE_EXACT_ALARM", Label: "精确闹钟", Note: "需用户手动授予", MinSdk: 31},
	{Name: "USE_EXACT_ALARM", Label: "精确闹钟（免申请）", Note: "仅闹钟类应用", MinSdk: 33},
	{Name: "REQUEST_INSTALL_PACKAGES", Label: "应用内安装 APK", MinSdk: 26},
	{Name: "SYSTEM_ALERT_WINDOW", Label: "悬浮窗", Note: "需用户手动授予", MinSdk: 23},
	{Name: "QUERY_ALL_PACKAGES", Label: "查询已装应用", Note: "商店审核严格", MinSdk: 30},
	{Name: "ACTIVITY_RECOGNITION", Label: "运动识别（计步）", MinSdk: 29},
	{Name: "BODY_SENSORS", Label: "身体传感器", MinSdk: 20},
	{Name: "CHANGE_WIFI_STATE", Label: "开关 WiFi", MinSdk: 1},
	{Name: "ACCESS_WIFI_STATE", Label: "读取 WiFi 状态", MinSdk: 1},
	{Name: "MODIFY_AUDIO_SETTINGS", Label: "修改音频设置", MinSdk: 1},
	{Name: "GET_ACCOUNTS", Label: "读取账户列表", MinSdk: 1},
	{Name: "WRITE_SETTINGS", Label: "修改系统设置", MinSdk: 23},
	{Name: "USE_FINGERPRINT", Label: "指纹识别", Note: "Android 9 及以下", MinSdk: 23},
	{Name: "DOWNLOAD_WITHOUT_NOTIFICATION", Label: "静默下载", MinSdk: 1},
}

// modulePermHints 把 manifest.json 里的 uni-app 模块映射成常用的安卓权限，
// 用于「资源包里读不到 distribute 时」给个合理建议
var modulePermHints = map[string][]string{
	"Camera":      {"CAMERA"},
	"Barcode":     {"CAMERA", "FLASHLIGHT"},
	"Geolocation": {"ACCESS_FINE_LOCATION", "ACCESS_COARSE_LOCATION"},
	"Record":      {"RECORD_AUDIO"},
	"Push":        {"POST_NOTIFICATIONS", "VIBRATE", "RECEIVE_BOOT_COMPLETED"},
	"Maps":        {"ACCESS_FINE_LOCATION", "ACCESS_NETWORK_STATE", "ACCESS_WIFI_STATE"},
	"Bluetooth":   {"BLUETOOTH_SCAN", "BLUETOOTH_CONNECT"},
	"Payment":     {"REQUEST_INSTALL_PACKAGES"},
	"Speech":      {"RECORD_AUDIO"},
	"Share":       {"WRITE_EXTERNAL_STORAGE"},
	"OAuth":       {"GET_ACCOUNTS"},
	"Fingerprint": {"USE_FINGERPRINT"},
	"Contacts":    {"READ_CONTACTS", "WRITE_CONTACTS"},
	"Messaging":   {"READ_SMS", "SEND_SMS"},
	"Downloader":  {"WRITE_EXTERNAL_STORAGE", "DOWNLOAD_WITHOUT_NOTIFICATION"},
}

// androidDefaults 给前端渲染「安卓配置」面板用：基线权限 + 可选目录 + 默认 SDK 版本
func (a *API) androidDefaults(w http.ResponseWriter, r *http.Request) {
	base := make([]permInfo, 0, len(baselinePerms))
	for _, n := range baselinePerms {
		label := permLabels[n]
		if label == "" {
			label = n
		}
		base = append(base, permInfo{Name: n, Label: label})
	}
	sort.Slice(base, func(i, j int) bool { return base[i].Name < base[j].Name })

	hints := map[string][]string{}
	for k, v := range modulePermHints {
		hints[k] = v
	}

	jsonOK(w, map[string]any{
		"minSdk":     defaultMinSdk,
		"targetSdk":  defaultTargetSdk,
		"compileSdk": defaultCompileSdk,
		"sdkVersion": sdkVersionLabel,
		"baseline":   base,
		"catalog":    permCatalog,
		"moduleHints": hints,
	})
}
