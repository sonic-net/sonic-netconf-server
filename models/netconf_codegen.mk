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

TOPDIR := ..
BUILD_DIR := $(TOPDIR)/build

CODEGEN_TOOLS_DIR := $(TOPDIR)/tools/netconf_codegen

YANGAPI_DIR                 := $(BUILD_DIR)/netconf_codegen
YANGDIR                     := yangs
YANGDIR_COMMON              := $(YANGDIR)/common
YANGDIR_EXTENSIONS          := $(YANGDIR)/extensions
YANG_MOD_FILES              := $(wildcard $(YANGDIR)/*.yang)
YANG_MOD_FILES              += $(wildcard $(YANGDIR_EXTENSIONS)/*.yang)
YANG_COMMON_FILES           := $(wildcard $(YANGDIR_COMMON)/*.yang)

YANGDIR_SONIC               := $(YANGDIR)/sonic
YANGDIR_SONIC_COMMON        := $(YANGDIR_SONIC)/common
SONIC_YANG_MOD_FILES        := $(wildcard $(YANGDIR_SONIC)/*.yang)
SONIC_YANG_COMMON_FILES     := $(wildcard $(YANGDIR_SONIC_COMMON)/*.yang)

SONIC_YANG_M_FILES        := $(YANGDIR_SONIC)/sonic-vlan.yang

TOOLS_DIR        := $(TOPDIR)/tools
PYANG_PLUGIN_DIR := $(TOOLS_DIR)/pyang/pyang_plugins
PYANG ?= pyang

OPENAPI_GEN_PRE  := $(YANGAPI_DIR)/.

OUT_FOLDER = $(YANGAPI_DIR)

all: $(YANGAPI_DIR)/.done $(YANGAPI_DIR)/.sonic_done $(YANGAPI_DIR)/.rpc_done 

netconf-server-init: $(YANGAPI_DIR)/.init_done

.PRECIOUS: %/.
%/.:
	mkdir -p $@


#======================================================================
# Initialize netconf_codegen package
#======================================================================
$(YANGAPI_DIR)/.init_done: | $(YANGAPI_DIR)/.
	cp -r $(CODEGEN_TOOLS_DIR)/src/* $(@D)/
	touch $@

#======================================================================
# Generate Common map for list keys
#======================================================================
$(YANGAPI_DIR)/.done:  $(YANG_MOD_FILES) $(YANG_COMMON_FILES) | $(OPENAPI_GEN_PRE)
	@echo "+++++ Generating Common map for list keys +++++"
	mkdir -p $(YANGAPI_DIR)
	$(PYANG) \
		-f keys \
		--type Common \
		--outdir $(OUT_FOLDER) \
		--plugindir $(PYANG_PLUGIN_DIR) \
		-p $(YANGDIR_COMMON):$(YANGDIR) \
		$(YANG_MOD_FILES)
	@echo "+++++ Generation of Common map for list keys completed +++++"
	touch $@


#======================================================================
# Generate Sonic map for list keys
#======================================================================
$(YANGAPI_DIR)/.sonic_done: $(SONIC_YANG_MOD_FILES) $(SONIC_YANG_COMMON_FILES) | $(OPENAPI_GEN_PRE)
	@echo "+++++ Generating YSonic map for list keys +++++"
	$(PYANG) \
		-f keys \
		--outdir $(OUT_FOLDER) \
		--type Sonic \
		--plugindir $(PYANG_PLUGIN_DIR) \
		-p $(YANGDIR_SONIC_COMMON):$(YANGDIR_SONIC):$(YANGDIR_COMMON) \
		$(SONIC_YANG_MOD_FILES)
	@echo "+++++ Generation of Sonic map for list keys completed +++++"
	touch $@


#======================================================================
# Generate RPC string array
#======================================================================
$(YANGAPI_DIR)/.rpc_done: $(SONIC_YANG_MOD_FILES) $(SONIC_YANG_COMMON_FILES) | $(OPENAPI_GEN_PRE)
	@echo "+++++ Generating RPC string array +++++"
	$(PYANG) \
		-f rpcs \
		--outdir $(OUT_FOLDER) \
		--plugindir $(PYANG_PLUGIN_DIR) \
		-p $(YANGDIR_SONIC_COMMON):$(YANGDIR_SONIC):$(YANGDIR_COMMON) \
		$(SONIC_YANG_MOD_FILES)
	@echo "+++++ Generation of RPC string array completed +++++"
	touch $@


clean:
	$(RM) -r $(YANGAPI_DIR)
