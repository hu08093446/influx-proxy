module github.com/chengshiwen/influx-proxy

go 1.16

require (
	github.com/cilium/ebpf v0.9.0 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.2 // indirect
	github.com/go-delve/delve v1.8.3 // indirect
	github.com/go-delve/liner v1.2.2 // indirect
	github.com/gogo/protobuf v1.3.2
	github.com/golang/snappy v0.0.3
	github.com/influxdata/influxdb1-client v0.0.0-20220302092344-a9ab5670611c
	github.com/json-iterator/go v1.1.12
	github.com/mitchellh/gox v1.0.1 // indirect
	github.com/panjf2000/ants/v2 v2.4.8
	github.com/sirupsen/logrus v1.8.1 // indirect
	github.com/spf13/cobra v1.4.0 // indirect
	github.com/spf13/viper v1.10.1
	go.starlark.net v0.0.0-20220328144851-d1966c6b9fcd // indirect
	golang.org/x/arch v0.0.0-20220412001346-fc48f9fe4c15 // indirect
	golang.org/x/sys v0.0.0-20220520151302-bc2c85ada10a // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.0.0
	stathat.com/c/consistent v1.0.0
)

replace github.com/go-delve/liner => github.com/peterh/liner v1.2.2
