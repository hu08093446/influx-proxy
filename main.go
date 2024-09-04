// Copyright 2021 Shiwen Cheng. All rights reserved.
// Use of this source code is governed by a MIT
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/chengshiwen/influx-proxy/backend"
	"github.com/chengshiwen/influx-proxy/service"
)

var (
	configFile string
	version    bool
)

// 这里使用了flag库解析用户执行程序时在命令行的输入，使用解析的结果来初始化变量
func init() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.Lshortfile)
	log.SetOutput(os.Stdout)
	flag.StringVar(&configFile, "config", "proxy.json", "proxy config file with json/yaml/toml format")
	flag.BoolVar(&version, "version", false, "proxy version")

	// 这里的Parse()函数时必须的，不执行的话，变量设置就不起作用
	flag.Parse()
}

func printVersion() {
	fmt.Printf("Version:    %s\n", backend.Version)
	fmt.Printf("Git commit: %s\n", backend.GitCommit)
	fmt.Printf("Build time: %s\n", backend.BuildTime)
	fmt.Printf("Go version: %s\n", runtime.Version())
	fmt.Printf("OS/Arch:    %s/%s\n", runtime.GOOS, runtime.GOARCH)
}

// 添加源码注释
func main() {
	if version {
		printVersion()
		return
	}

	// 这里的configFile时在命令行通过-config制定的
	cfg, err := backend.NewFileConfig(configFile)
	if err != nil {
		fmt.Printf("illegal config file: %s\n", err)
		return
	}
	log.Printf("version: %s, commit: %s, build: %s", backend.Version, backend.GitCommit, backend.BuildTime)
	cfg.PrintSummary()

	// 下面的操作是启动了一个http服务器
	// 相关内容可以参考：https://www.jianshu.com/p/16210100d43d
	mux := http.NewServeMux()
	service.NewHttpService(cfg).Register(mux)

	server := &http.Server{
		Addr:        cfg.ListenAddr,
		Handler:     mux,
		IdleTimeout: time.Duration(cfg.IdleTimeout) * time.Second,
	}
	if cfg.HTTPSEnabled {
		log.Printf("https service start, listen on %s", server.Addr)
		err = server.ListenAndServeTLS(cfg.HTTPSCert, cfg.HTTPSKey)
	} else {
		log.Printf("http service start, listen on %s", server.Addr)
		err = server.ListenAndServe()
	}
	if err != nil {
		log.Print(err)
		return
	}
}
