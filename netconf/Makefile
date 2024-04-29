#
# Software Name: sonic-netconf-server
# SPDX-FileCopyrightText: Copyright (c) Orange SA
# SPDX-License-Identifier: Apache 2.0
# 
# This software is distributed under the Apache 2.0 licence,
# the text of which is available at https:#opensource.org/license/apache-2-0/
# or see the "LICENSE" file for more details.
# 
# Authors: hossam4.hassan@orange.com, abdelmuhaimen.seaudi@orange.com
# Software description: RFC compliant NETCONF server implementation for SONiC
#


TOPDIR     := ..
BUILD_DIR  ?= $(TOPDIR)/build

GO     ?= go
GOROOT ?= $(shell $(GO) env GOROOT)

NETCONF_BUILD_DIR := $(BUILD_DIR)/netconf_server
NETCONF_BIN := $(NETCONF_BUILD_DIR)/netconf_server

NETCONF_GO_SRCS   = $(shell find . -name '*.go' -not -name '*_test.go')
NETCONF_TEST_SRCS = $(shell find . -name '*_test.go')

# Source files affecting NETCONF server
NETCONF_SRCS := $(NETCONF_GO_SRCS)
# NETCONF_SRCS += $(shell find $(NETCONF_BUILD_DIR)/dist -name '*.go')
NETCONF_SRCS += $(shell find $(TOPDIR)/vendor/github.com/Azure/sonic-mgmt-common -name '*.go')
# NETCONF_SRCS += $(shell find $(TOPDIR)/models/openapi -name '*.yaml')

DEFAULT_TARGETS = $(NETCONF_BIN)

all: $(DEFAULT_TARGETS)

$(NETCONF_BUILD_DIR)/:
	mkdir -p $@

# NETCONF Server binary
$(NETCONF_BIN): $(NETCONF_SRCS) | $(NETCONF_BUILD_DIR)/
	$(GO) build -mod=vendor -gcflags="all=-N -l" -v -o $@ ../netconf/main/main.go
		
# Gotest binary for NETCONF Server
$(NETCONF_TEST_BIN): $(NETCONF_TEST_SRCS) $(NETCONF_SRCS) | $(NETCONF_BUILD_DIR)/
	$(GO) test -mod=vendor -tags test -cover -c ../netconf/server -o $@

clean:
	$(RM) $(NETCONF_BIN)

