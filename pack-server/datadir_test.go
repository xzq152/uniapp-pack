package main

import (
	"os"
	"path/filepath"
	"testing"
)

// 老版本的数据在 exe 上一级 data/，新版默认在 exe 同级 data/。
// 升级时必须自动沿用旧目录，否则用户会发现证书和任务记录凭空消失。
func TestAdoptLegacyDataDir(t *testing.T) {
	tmp := t.TempDir()
	// 模拟旧版结构：exe 在子目录，数据在上一级
	exeDir := filepath.Join(tmp, "pack-server")
	legacyData := filepath.Join(tmp, "data")
	if err := os.MkdirAll(filepath.Join(legacyData, "keystores"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(exeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 旧目录里有真实数据
	writeFile(t, filepath.Join(legacyData, "keystores", "index.json"), `[{"id":"k1","alias":"a"}]`)

	current := filepath.Join(exeDir, "data")
	got := adoptLegacyDataDir(exeDir, current)
	if got != legacyData {
		t.Fatalf("应沿用旧目录 %q，实际 %q", legacyData, got)
	}
}

// 新位置已经有数据时不能被旧目录抢走
func TestAdoptLegacyDataDirPrefersCurrent(t *testing.T) {
	tmp := t.TempDir()
	exeDir := filepath.Join(tmp, "pack-server")
	current := filepath.Join(exeDir, "data")
	legacyData := filepath.Join(tmp, "data")

	writeFile(t, filepath.Join(current, "jobs.json"), `[{"id":"new"}]`)
	writeFile(t, filepath.Join(legacyData, "jobs.json"), `[{"id":"old"}]`)

	got := adoptLegacyDataDir(exeDir, current)
	if got != current {
		t.Fatalf("新位置有数据时应保持 %q，实际 %q", current, got)
	}
}

// 哪里都没有数据时，用新位置（会由 ensureDirs 创建）
func TestAdoptLegacyDataDirFreshInstall(t *testing.T) {
	tmp := t.TempDir()
	exeDir := filepath.Join(tmp, "pack-server")
	if err := os.MkdirAll(exeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(exeDir, "data")
	got := adoptLegacyDataDir(exeDir, current)
	if got != current {
		t.Fatalf("全新安装应使用 %q，实际 %q", current, got)
	}
}

// 空文件不算"有数据"，避免误判
func TestHasUserDataIgnoresEmptyFiles(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, filepath.Join(tmp, "jobs.json"), `[]`)
	if hasUserData(tmp) {
		t.Fatal("只有空数组的 jobs.json 不应算有数据")
	}
	writeFile(t, filepath.Join(tmp, "jobs.json"), `[{"id":"j1"}]`)
	if !hasUserData(tmp) {
		t.Fatal("有记录时应算有数据")
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
