//go:build !windows

// 非 Windows 平台没有托盘，保持原来的前台服务行为。
package main

import (
	"log"
	"os"
	"path/filepath"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	exePath, _ := os.Executable()
	base := filepath.Dir(exePath)

	cfg := defaultConfig(base)

	srv, err := newServer(cfg)
	if err != nil {
		log.Fatal(err)
	}

	printBanner(cfg)
	log.Fatal(srv.ListenAndServe())
}
