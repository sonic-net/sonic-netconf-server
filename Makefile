################################################################################
#                                                                              #
#  Copyright 2024 Orange.                                                      #
#                                                                              #
#  Licensed under the Apache License, Version 2.0 (the "License");             #
#  you may not use this file except in compliance with the License.            #
#  You may obtain a copy of the License at                                     #
#                                                                              #
#     http://www.apache.org/licenses/LICENSE-2.0                               #
#                                                                              #
#  Unless required by applicable law or agreed to in writing, software         #
#  distributed under the License is distributed on an "AS IS" BASIS,           #
#  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.    #
#  See the License for the specific language governing permissions and         #
#  limitations under the License.                                              #
#                                                                              #
################################################################################

TOPDIR := $(abspath .)
BUILD_DIR := $(TOPDIR)/build
MGMT_COMMON_DIR := $(abspath ../sonic-mgmt-common)

GO      ?= /usr/local/go/bin/go
GOPATH  ?= /tmp/go

GO_MOD   = go.mod
GO_DEPS  = vendor/.done

export TOPDIR MGMT_COMMON_DIR GO GOPATH

.PHONY: all
all: netconf

$(GO_MOD):
	$(GO) mod init orange/sonic-netconf-server

$(GO_DEPS): $(GO_MOD)
	$(MAKE) -C models -f netconf_codegen.mk netconf-server-init
	$(GO) mod vendor
	$(MGMT_COMMON_DIR)/patches/apply.sh vendor
	touch  $@

go-deps: $(GO_DEPS)

go-deps-clean:
	$(RM) -r vendor

.PHONY: netconf
netconf: go-deps-clean $(GO_DEPS) models 
	$(MAKE) -C netconf

# Special target for local compilation of REST server binary.
# Compiles models, translib and cvl schema from sonic-mgmt-common
rest-server: go-deps-clean
	NO_TEST_BINS=1 $(MAKE) -C $(MGMT_COMMON_DIR)
	NO_TEST_BINS=1 $(MAKE) rest

netconf_server: ; @echo from netconf?

.PHONY: models
models:
	$(MAKE) -C models

models-clean:
	$(MAKE) -C models clean

clean: models-clean
	git check-ignore debian/* | xargs -r $(RM) -r
	$(RM) -r debian/.debhelper
	$(RM) -r $(BUILD_DIR)

cleanall: clean
