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

all: yangs
%:
	pip3 install chevron==0.14.0
	pip install chevron==0.14.0
	pip3 install mmh3
	pip install mmh3
	$(MAKE) -f netconf_codegen.mk $@

yangs:
	ln -s ../../sonic-mgmt-common/models/yang $@

