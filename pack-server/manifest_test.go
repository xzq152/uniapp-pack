package main

import (
	"os"
	"strings"
	"testing"
)

// 验证：HBuilderX 导出的打包资源 manifest（version 是对象、没有 distribute）
func TestParseBundleManifest(t *testing.T) {
	p := `D:/uniapp-pack-go/data/uploads/u1794803108110867600/__UNI__6744715/www/manifest.json`
	f, err := os.Open(p)
	if err != nil {
		t.Skip("样本不存在: " + err.Error())
	}
	defer f.Close()
	m := parseManifest(f)
	t.Logf("appId=%q name=%q ver=%q code=%q pkg=%q min=%d target=%d distribute=%v modules=%v",
		m.AppID, m.Name, m.VersionName, m.VersionCode, m.PackageName, m.MinSdk, m.TargetSdk, m.HasDistribute, m.Modules)
	if m.AppID != "__UNI__6744715" {
		t.Errorf("appId 解析错: %q", m.AppID)
	}
	if m.Name != "容通助手" {
		t.Errorf("name 解析错: %q", m.Name)
	}
	if m.VersionName != "2.3.3" || m.VersionCode != "233" {
		t.Errorf("version 对象没解析出来: %q / %q", m.VersionName, m.VersionCode)
	}
	if m.HasDistribute {
		t.Errorf("打包资源不该有 distribute")
	}
	if len(m.Modules) == 0 {
		t.Errorf("模块列表为空，应该读到 Camera/UniNView")
	}
}

// 验证：项目根 manifest.json（version 对象 + app-plus.distribute.android）
func TestParseProjectManifest(t *testing.T) {
	const raw = `{
	  "id": "__UNI__ABCDEF",
	  "name": "测试应用",
	  "version": { "name": "5.2.1", "code": 521 },
	  "permissions": { "Camera": {}, "Geolocation": {}, "Push": {} },
	  "app-plus": {
	    "modules": { "Camera": {}, "Geolocation": {} },
	    "distribute": {
	      "android": {
	        "packagename": "com.demo.app",
	        "minSdkVersion": 23,
	        "targetSdkVersion": 34,
	        "permissions": [
	          "<uses-permission android:name=\"android.permission.CAMERA\"/>",
	          "<uses-permission android:name=\"android.permission.ACCESS_FINE_LOCATION\"/>",
	          "android.permission.POST_NOTIFICATIONS"
	        ]
	      }
	    }
	  }
	}`
	m := parseManifest(strings.NewReader(raw))
	t.Logf("appId=%q ver=%q code=%q pkg=%q min=%d target=%d perms=%v",
		m.AppID, m.VersionName, m.VersionCode, m.PackageName, m.MinSdk, m.TargetSdk, m.Permissions)
	if !m.HasDistribute {
		t.Fatal("distribute 没识别到")
	}
	if m.PackageName != "com.demo.app" || m.MinSdk != 23 || m.TargetSdk != 34 {
		t.Errorf("android 配置解析错: %q %d %d", m.PackageName, m.MinSdk, m.TargetSdk)
	}
	if len(m.Permissions) != 3 || m.Permissions[0] != "CAMERA" || m.Permissions[2] != "POST_NOTIFICATIONS" {
		t.Errorf("权限解析错: %v", m.Permissions)
	}
}

// 验证：权限名白名单能挡住注入
func TestSanitizePerms(t *testing.T) {
	got := sanitizePerms([]string{
		"CAMERA",
		"android.permission.POST_NOTIFICATIONS",
		`<uses-permission android:name="android.permission.READ_SMS"/>`,
		`../../etc/passwd`,
		"CAMERA",
		`CAMERA"/><inject`,
		"",
	})
	t.Logf("结果: %v", got)
	want := []string{"CAMERA", "POST_NOTIFICATIONS", "READ_SMS"}
	if len(got) != len(want) {
		t.Fatalf("期望 %v，实际 %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 项：期望 %q，实际 %q", i, want[i], got[i])
		}
	}
}
