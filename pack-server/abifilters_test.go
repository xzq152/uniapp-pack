package main

import (
	"strings"
	"testing"
)

// sanitizeAbiFilters 决定打进 APK 的 CPU 架构，写错会导致包体膨胀或缺架构。
// 这个值最终会拼进 gradle 的 abiFilters，必须严格白名单校验。
func TestSanitizeAbiFilters(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "默认两架构",
			in:   []string{"arm64-v8a", "armeabi-v7a"},
			want: []string{"arm64-v8a", "armeabi-v7a"},
		},
		{
			name: "去重保序",
			in:   []string{"armeabi-v7a", "arm64-v8a", "armeabi-v7a"},
			want: []string{"armeabi-v7a", "arm64-v8a"},
		},
		{
			name: "允许显式加 x86",
			in:   []string{"arm64-v8a", "x86"},
			want: []string{"arm64-v8a", "x86"},
		},
		{
			name: "非法架构丢弃",
			in:   []string{"arm64-v8a", "mips", "arm64"},
			want: []string{"arm64-v8a"},
		},
		{
			name: "大小写敏感：ARMEABI-V7A 不合法",
			in:   []string{"ARMEABI-V7A"},
			want: nil,
		},
		{
			name: "全部非法返回 nil（由容器端回落默认）",
			in:   []string{"foo", "bar"},
			want: nil,
		},
		{
			name: "空白忽略",
			in:   []string{"", "  ", "x86_64"},
			want: []string{"x86_64"},
		},
		{
			name: "兼容单格里逗号分隔",
			in:   []string{"arm64-v8a,armeabi-v7a"},
			want: []string{"arm64-v8a", "armeabi-v7a"},
		},
		{
			name: "注入尝试被丢弃",
			in:   []string{`arm64-v8a" }`},
			want: nil,
		},
	}
	for _, c := range cases {
		got := sanitizeAbiFilters(c.in)
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s: 期望 %v，实际 %v", c.name, c.want, got)
		}
	}
}

func TestSanitizeAbiFiltersNil(t *testing.T) {
	if got := sanitizeAbiFilters(nil); got != nil {
		t.Errorf("nil 应返回 nil，实际 %v", got)
	}
	if got := sanitizeAbiFilters([]string{}); got != nil {
		t.Errorf("空切片应返回 nil，实际 %v", got)
	}
}
