//
// Software Name: sonic-netconf-server
// SPDX-FileCopyrightText: Copyright (c) Orange SA
// SPDX-License-Identifier: Apache 2.0
//
// This software is distributed under the Apache 2.0 licence,
// the text of which is available at https://opensource.org/license/apache-2-0/
// or see the "LICENSE" file for more details.
//
// Authors: hossam4.hassan@orange.com, abdelmuhaimen.seaudi@orange.com
// Software description: RFC compliant NETCONF server implementation for SONiC
//

package server

import (
	"fmt"
	"strings"
	"testing"

	"github.com/antchfx/xmlquery"
)

func init(){
	fmt.Println("+++++ init parser_test +++++")	
}

// TODO
func TestParseGetRequest(t *testing.T){
	
	requestXML := "<get><filter type=\"subtree\"><sonic-vlan><VLAN/></sonic-vlan></filter></get>"

	requestNode, _ := xmlquery.Parse(strings.NewReader(requestXML))

	results, _ := ParseGetRequest(requestNode)

	if len(results) != 1 {
		t.Errorf("Result length was incorrect, got: %d, want: %d.", len(results), 1)
		return
	}

	if results[0].path != "/sonic-vlan:sonic-vlan/VLAN" {
		t.Errorf("Result was incorrect, got: %s, want: %s.", results[0].path, "/sonic-vlan:sonic-vlan/VLAN")
	}
}