module orange/sonic-netconf-server

require (
	github.com/Azure/sonic-mgmt-common v0.0.0-20220120155510-515652700481
	github.com/antchfx/xmlquery v1.3.1
	github.com/clbanning/mxj/v2 v2.3.2
	github.com/gliderlabs/ssh v0.3.3
	github.com/go-redis/redis/v7 v7.0.0-beta.3.0.20190824101152-d19aba07b476
	github.com/golang/glog v0.0.0-20160126235308-23def4e6c14b
	github.com/google/uuid v1.3.0
	golang.org/x/crypto v0.17.0
)

require (
	github.com/Workiva/go-datastructures v1.0.50 // indirect
	github.com/anmitsu/go-shlex v0.0.0-20200514113438-38f4b401e2be // indirect
	github.com/antchfx/jsonquery v1.1.4 // indirect
	github.com/antchfx/xpath v1.1.10 // indirect
	github.com/aymanbagabas/go-udiff v0.1.3 // indirect
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/golang/groupcache v0.0.0-20200121045136-8c9f03a8e57e // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/google/go-cmp v0.5.5 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/maruel/natural v1.1.1 // indirect
	github.com/onsi/ginkgo v1.16.5 // indirect
	github.com/onsi/gomega v1.18.1 // indirect
	github.com/openconfig/gnmi v0.0.0-20200617225440-d2b4e6a45802 // indirect
	github.com/openconfig/goyang v0.0.0-20200309174518-a00bece872fc // indirect
	github.com/openconfig/ygot v0.7.1 // indirect
	github.com/philopon/go-toposort v0.0.0-20170620085441-9be86dbd762f // indirect
	golang.org/x/net v0.10.0 // indirect
	golang.org/x/sys v0.15.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	google.golang.org/genproto v0.0.0-20200319113533-08878b785e9c // indirect
	google.golang.org/grpc v1.28.0 // indirect
	google.golang.org/protobuf v1.26.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/Azure/sonic-mgmt-common => ../sonic-mgmt-common

go 1.19
