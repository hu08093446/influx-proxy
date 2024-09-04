// Copyright 2021 Shiwen Cheng. All rights reserved.
// Use of this source code is governed by a MIT
// license that can be found in the LICENSE file.

package backend

import (
	"errors"
	"log"

	"github.com/chengshiwen/influx-proxy/util"
	jsoniter "github.com/json-iterator/go"
	"github.com/spf13/viper"
)

// 这里是直接初始化为字符串了， 因为有了var，所以不需要":="这个操作符
var (
	Version   = "1.1.1"
	GitCommit = "not build"
	BuildTime = "not build"
)

// todo errors.New返回应该是指针类型，error是指针类型吗？
var (
	ErrEmptyCircles          = errors.New("circles cannot be empty")
	ErrEmptyBackends         = errors.New("backends cannot be empty")
	ErrEmptyBackendName      = errors.New("backend name cannot be empty")
	ErrDuplicatedBackendName = errors.New("backend name duplicated")
	ErrInvalidHashKey        = errors.New("invalid hash_key, require idx, exi, name or url")
)

type BackendConfig struct { // nolint:golint
	Name        string `mapstructure:"name"`
	Url         string `mapstructure:"url"` // nolint:golint
	Username    string `mapstructure:"username"`
	Password    string `mapstructure:"password"`
	AuthEncrypt bool   `mapstructure:"auth_encrypt"`
	WriteOnly   bool   `mapstructure:"write_only"`
}

type CircleConfig struct {
	Name     string           `mapstructure:"name"`
	Backends []*BackendConfig `mapstructure:"backends"`
}

type ProxyConfig struct {
	// 后面的mapstructure是为了和proxy.json中的字段名进行对应
	// 因为要想进行反序列化，属性名要大写，这样就和proxy.json中的字段不对应了，所以需要这样一个别名
	Circles         []*CircleConfig `mapstructure:"circles"`
	ListenAddr      string          `mapstructure:"listen_addr"`
	DBList          []string        `mapstructure:"db_list"`
	DataDir         string          `mapstructure:"data_dir"`
	TLogDir         string          `mapstructure:"tlog_dir"`
	HashKey         string          `mapstructure:"hash_key"`
	FlushSize       int             `mapstructure:"flush_size"`
	FlushTime       int             `mapstructure:"flush_time"`
	CheckInterval   int             `mapstructure:"check_interval"`
	RewriteInterval int             `mapstructure:"rewrite_interval"`
	ConnPoolSize    int             `mapstructure:"conn_pool_size"`
	WriteTimeout    int             `mapstructure:"write_timeout"`
	IdleTimeout     int             `mapstructure:"idle_timeout"`
	Username        string          `mapstructure:"username"`
	Password        string          `mapstructure:"password"`
	AuthEncrypt     bool            `mapstructure:"auth_encrypt"`
	WriteTracing    bool            `mapstructure:"write_tracing"`
	QueryTracing    bool            `mapstructure:"query_tracing"`
	HTTPSEnabled    bool            `mapstructure:"https_enabled"`
	HTTPSCert       string          `mapstructure:"https_cert"`
	HTTPSKey        string          `mapstructure:"https_key"`
}

// 函数名时大写的，所以可以导出，可以通过包名加“.”在其他包中访问
func NewFileConfig(cfgfile string) (cfg *ProxyConfig, err error) {
	viper.SetConfigFile(cfgfile)
	err = viper.ReadInConfig()
	if err != nil {
		return
	}
	cfg = &ProxyConfig{}
	err = viper.Unmarshal(cfg)
	if err != nil {
		return
	}
	// 如果有些配置用户没有使用，这里设置为默认值
	cfg.setDefault()
	err = cfg.checkConfig()
	return
}

// 这里的setDefault是ProxyConfig结构体的方法
// 因为golang没有类的概念，所以采取这种写法
// 可以认为整个config.go就是一个ProxyConfig类
func (cfg *ProxyConfig) setDefault() {
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":7076"
	}
	if cfg.DataDir == "" {
		cfg.DataDir = "data"
	}
	if cfg.TLogDir == "" {
		cfg.TLogDir = "log"
	}
	if cfg.HashKey == "" {
		cfg.HashKey = "idx"
	}
	if cfg.FlushSize <= 0 {
		cfg.FlushSize = 10000
	}
	if cfg.FlushTime <= 0 {
		cfg.FlushTime = 1
	}
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = 1
	}
	if cfg.RewriteInterval <= 0 {
		cfg.RewriteInterval = 10
	}
	if cfg.ConnPoolSize <= 0 {
		cfg.ConnPoolSize = 20
	}
	if cfg.WriteTimeout <= 0 {
		cfg.WriteTimeout = 10
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 10
	}
}

func (cfg *ProxyConfig) checkConfig() (err error) {
	if len(cfg.Circles) == 0 {
		return ErrEmptyCircles
	}
	set := util.NewSet()
	for _, circle := range cfg.Circles {
		if len(circle.Backends) == 0 {
			return ErrEmptyBackends
		}
		for _, backend := range circle.Backends {
			if backend.Name == "" {
				return ErrEmptyBackendName
			}
			if set[backend.Name] {
				return ErrDuplicatedBackendName
			}
			set.Add(backend.Name)
		}
	}
	if cfg.HashKey != "idx" && cfg.HashKey != "exi" && cfg.HashKey != "name" && cfg.HashKey != "url" {
		return ErrInvalidHashKey
	}
	return
}

func (cfg *ProxyConfig) PrintSummary() {
	log.Printf("%d circles loaded from file", len(cfg.Circles))
	for id, circle := range cfg.Circles {
		log.Printf("circle %d: %d backends loaded", id, len(circle.Backends))
	}
	log.Printf("hash key: %s", cfg.HashKey)
	if len(cfg.DBList) > 0 {
		log.Printf("db list: %v", cfg.DBList)
	}
	log.Printf("auth: %t, encrypt: %t", cfg.Username != "" || cfg.Password != "", cfg.AuthEncrypt)
}

func (cfg *ProxyConfig) String() string {
	json := jsoniter.Config{TagKey: "mapstructure"}.Froze()
	b, _ := json.Marshal(cfg)
	return string(b)
}
