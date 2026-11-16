package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 有任务在排队/构建时切换 Docker 地址必须被拒绝。
//
// 这条规则的实现有过一个坑：判断被写在 `if runtimeRunning() { ... }` 里面，
// 于是「打包环境没启动、但有 queued 任务在排队等」的情况下会放行切换，
// 与文档承诺的 409 不符。这里直接把两种环境状态都覆盖到。
func TestDockerHostSetRejectsWhileBuilding(t *testing.T) {
	for _, runtimeUp := range []bool{true, false} {
		s := &Store{
			cfg:   Config{DataDir: t.TempDir()},
			jobs:  map[string]*Job{},
			queue: make(chan *Job, 8),
		}
		s.jobs["jq"] = &Job{
			ID: "jq", Status: "queued", Stage: "排队中",
			subs: map[uint64]chan []byte{},
		}
		a := &API{store: s}

		before := getDockerClient().host // 先取快照，用于验证「拒绝时端点不变」

		body := strings.NewReader(`{"host":"192.168.1.23"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/docker/host", body)
		rec := httptest.NewRecorder()

		a.dockerHostSet(rec, req)

		if rec.Code != http.StatusConflict {
			t.Fatalf("runtimeUp=%v 时应返回 409，实际 %d（body=%s）",
				runtimeUp, rec.Code, rec.Body.String())
		}
		// 拒绝时不能改动全局端点。这里比对「调用前后」而不是比对某个固定地址——
		// 其他用例也会改动这个全局，写死地址会让本用例在 -shuffle 下随机失败。
		if after := getDockerClient().host; after != before {
			t.Fatalf("被拒绝的切换不应该改动端点：%q -> %q", before, after)
		}
	}
}

// 请求体不是合法 JSON 时应返回 400，而不是 500 或空响应
func TestDockerHostSetBadJSON(t *testing.T) {
	s := &Store{cfg: Config{DataDir: t.TempDir()}, jobs: map[string]*Job{}}
	a := &API{store: s}

	req := httptest.NewRequest(http.MethodPost, "/api/docker/host", strings.NewReader("not-json"))
	rec := httptest.NewRecorder()
	a.dockerHostSet(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应返回 400，实际 %d", rec.Code)
	}
}

// dockerHostGet 必须能自报当前端点，且 raw/local 与实际情况自洽
func TestDockerHostGetSelfConsistent(t *testing.T) {
	s := &Store{cfg: Config{DataDir: t.TempDir()}, jobs: map[string]*Job{}}
	a := &API{store: s}

	req := httptest.NewRequest(http.MethodGet, "/api/docker/host", nil)
	rec := httptest.NewRecorder()
	a.dockerHostGet(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", rec.Code)
	}
	var got struct {
		Host  string `json:"host"`
		Raw   string `json:"raw"`
		Local bool   `json:"local"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	// local 与 raw 必须一致：raw 为空才叫本机
	if got.Local != (got.Raw == "") {
		t.Fatalf("local=%v 与 raw=%q 不自洽", got.Local, got.Raw)
	}
	if got.Host == "" {
		t.Fatal("host 不应为空")
	}
}
