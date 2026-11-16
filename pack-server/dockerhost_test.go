package main

import "testing"

// normalizeDockerHost 是页面输入的守门人：用户在地址栏里可能写各种形式，
// 这里统一成客户端能吃的写法，避免「明明是同一台机器却连不上」。
func TestNormalizeDockerHost(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},    // 空 = 本机
		{"   ", ""}, // 只有空格也当本机
		{"tcp://192.168.1.23:2375", "tcp://192.168.1.23:2375"},
		{"  tcp://192.168.1.23:2375  ", "tcp://192.168.1.23:2375"},
		{"http://192.168.1.23:2375", "tcp://192.168.1.23:2375"},
		{"https://docker.example.com:2376", "tcp://docker.example.com:2376"},
		{"192.168.1.23:2375", "tcp://192.168.1.23:2375"},
		{"192.168.1.23", "tcp://192.168.1.23:2375"}, // 没写端口补默认
		{"docker.lan", "tcp://docker.lan:2375"},
		{"unix:///var/run/docker.sock", "unix:///var/run/docker.sock"},
	}
	for _, c := range cases {
		if got := normalizeDockerHost(c.in); got != c.want {
			t.Errorf("normalizeDockerHost(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// 同一个 host 写法的不同变体，规范化后必须一致，否则「测试通过但切换后连不上」
func TestNormalizeDockerHostIdempotent(t *testing.T) {
	inputs := []string{"tcp://192.168.1.23:2375", "192.168.1.23", "http://192.168.1.23:2375"}
	want := normalizeDockerHost(inputs[0])
	for _, in := range inputs {
		once := normalizeDockerHost(in)
		if once != want {
			t.Errorf("%q 规范化后是 %q，与 %q 不一致", in, once, want)
		}
		if twice := normalizeDockerHost(once); twice != once {
			t.Errorf("重复规范化改变了结果: %q -> %q", once, twice)
		}
	}
}

// runningJobCount 决定「构建中能不能切地址」，必须只数 running/queued
func TestRunningJobCount(t *testing.T) {
	s := &Store{jobs: map[string]*Job{}}
	add := func(id, status string) {
		s.jobs[id] = &Job{ID: id, Status: status, subs: map[uint64]chan []byte{}}
	}
	add("a", "running")
	add("b", "queued")
	add("c", "done")
	add("d", "failed")

	if got := s.runningJobCount(); got != 2 {
		t.Fatalf("runningJobCount() = %d, want 2", got)
	}

	for _, j := range s.jobs {
		j.Status = "done"
	}
	if got := s.runningJobCount(); got != 0 {
		t.Fatalf("全部完成后 runningJobCount() = %d, want 0", got)
	}
}

// 用户切回「本机」后必须重启也能记住，不能被环境变量 DOCKER_HOST 反压回去
func TestDockerPrefsLocalSticks(t *testing.T) {
	dir := t.TempDir()
	s := &Store{cfg: Config{DataDir: dir}}

	t.Setenv("DOCKER_HOST", "tcp://192.168.1.23:2375")

	// 先存一个远程，再存本机
	if err := s.saveDockerPrefs("tcp://192.168.1.23:2375"); err != nil {
		t.Fatal(err)
	}
	if err := s.saveDockerPrefs(""); err != nil {
		t.Fatal(err)
	}

	host, found := s.loadDockerPrefs()
	if !found {
		t.Fatal("应该读到了配置")
	}
	if host != "" {
		t.Fatalf("切回本机后 host 应为空，实际 %q", host)
	}
	if got := getDockerClient().host; got != "" {
		t.Fatalf("客户端应该指向本机，实际 %q", got)
	}

	// 清理全局客户端，避免污染后续用例
	setDockerHost("tcp://192.168.1.23:2375")
}

// 没有配置文件时应回落到环境变量语义（found=false，不覆盖已有客户端）
func TestDockerPrefsMissing(t *testing.T) {
	s := &Store{cfg: Config{DataDir: t.TempDir()}}
	if _, found := s.loadDockerPrefs(); found {
		t.Fatal("没有配置文件时 found 应为 false")
	}
}
