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

all: yangs
%:
	pip3 install chevron==0.14.0
	pip install chevron==0.14.0
	pip3 install mmh3
	pip install mmh3
	$(MAKE) -f netconf_codegen.mk $@

yangs:
	ln -s ../../sonic-mgmt-common/models/yang $@

