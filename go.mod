module orange/sonic-netconf-server

require (
	github.com/Azure/sonic-mgmt-common v0.0.0-20220120155510-515652700481
	github.com/Azure/sonic-mgmt-framework v0.0.0-20230515200928-1303b05a0e64 // indirect
	github.com/antchfx/xmlquery v1.3.1
	github.com/clbanning/mxj/v2 v2.3.2
	github.com/gliderlabs/ssh v0.3.3
	github.com/go-playground/universal-translator v0.17.0 // indirect
	github.com/go-redis/redis/v7 v7.0.0-beta.3.0.20190824101152-d19aba07b476
	github.com/golang/glog v0.0.0-20160126235308-23def4e6c14b
	github.com/google/uuid v1.3.0
	github.com/gorilla/mux v1.7.4 // indirect
	github.com/leodido/go-urn v1.2.0 // indirect
	github.com/onsi/ginkgo v1.16.5 // indirect
	github.com/onsi/gomega v1.18.1 // indirect
	github.com/pkg/profile v1.4.0 // indirect
	golang.org/x/crypto v0.0.0-20210921155107-089bfa567519
	gopkg.in/go-playground/assert.v1 v1.2.1 // indirect
	gopkg.in/go-playground/validator.v9 v9.31.0 // indirect
)

replace github.com/Azure/sonic-mgmt-common => ../sonic-mgmt-common

go 1.13
