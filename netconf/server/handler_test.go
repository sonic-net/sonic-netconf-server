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
	"testing"
)

func init(){
	fmt.Println("+++++ init handler_test +++++")
	readYangModules()
}

func TestReadCapabilities(t *testing.T) {

	correctHello := "<hello xmlns=\"urn:ietf:params:xml:ns:netconf:base:1.0\"><capabilities><capability>urn:ietf:params:netconf:base:1.1</capability><capability>urn:ietf:params:netconf:base:1.0</capability></capabilities></hello>"

	result := readCapabilities(correctHello)

	if result != nil { 
		t.Errorf("Result was incorrect, Expected client caps to not return an error")
	}

	invalidHello := "<helo xmlns=\"urn:ietf:params:xml:ns:netconf:base:1.0\"><capabilities><capability>urn:ietf:params:netconf:base:1.1</capability><capability>urn:ietf:params:netconf:base:1.0</capability></capabilities></helo>"

	result = readCapabilities(invalidHello)

	if result == nil { 
		t.Errorf("Result was incorrect, Expected to return an error but didn't")
	}
}

func TestCreateResponse(t *testing.T) {
	
	id := "752ab2ee-f662-4ec9-9970-f308a80f18f2"

	result := CreateResponse(id,[]byte("{}"))
	correct := "<?xml version=\"1.0\" encoding=\"utf-8\"?><rpc-reply xmlns=\"urn:ietf:params:xml:ns:netconf:base:1.0\" message-id=\"" + id + "\"></rpc-reply>"
	if result != correct {
		t.Errorf("Result was incorrect, got: %s, want: %s.", result, correct)
	}

	result = CreateResponse(id,[]byte("ok"))
	correct = "<?xml version=\"1.0\" encoding=\"utf-8\"?><rpc-reply xmlns=\"urn:ietf:params:xml:ns:netconf:base:1.0\" message-id=\"" + id + "\"><ok/></rpc-reply>"
	if result != correct {
		t.Errorf("Result was incorrect, got: %s, want: %s.", result, correct)
	}

	result = CreateResponse(id,[]byte("This is a test reply &amp; testing"))
	correct = "<?xml version=\"1.0\" encoding=\"utf-8\"?><rpc-reply xmlns=\"urn:ietf:params:xml:ns:netconf:base:1.0\" message-id=\"" + id + "\">" + "This is a test reply & testing" + "</rpc-reply>"
	if result != correct {
		t.Errorf("Result was incorrect, got: %s, want: %s.", result, correct)
	}
}

func TestProcessRequest(t *testing.T) {

	id := "752ab2ee-f662-4ec9-9970-f308a80f18f2"

	request := SessionRequest {
		xml: "<rpc xmlns=\"urn:ietf:params:xml:ns:netconf:base:1.0\" message-id=\"" + id + "\"><get><filter><sonic-vlan><VLAN><VLAN_LIST><name>Vlan100</name></VLAN_LIST></VLAN></sonic-vlan></filter></get></rpc>",
		authenticator: NewTestAuthenticator(true),
		session: nil,
	}

	// Device specific response, change to your testing device correct response
	correct := "<?xml version=\"1.0\" encoding=\"utf-8\"?><rpc-reply xmlns=\"urn:ietf:params:xml:ns:netconf:base:1.0\" message-id=\"" + id + "\"><data><sonic-vlan xmlns=\"http://github.com/Azure/sonic-vlan\"><VLAN><VLAN_LIST><name>Vlan100</name><description>test vlan100</description><vlanid>100</vlanid></VLAN_LIST></VLAN></sonic-vlan></data></rpc-reply>"

	result := process(request)

	if result != correct {
		t.Errorf("Result was incorrect, got: %s, want: %s.", result, correct)
	}
}

func TestProcessRequestAuthFailed(t *testing.T) {

	id := "752ab2ee-f662-4ec9-9970-f308a80f18f2"

	autt := NewTestAuthenticator(false)

	request := SessionRequest {
		xml: "<rpc xmlns=\"urn:ietf:params:xml:ns:netconf:base:1.0\" message-id=\"" + id + "\"><get><filter><sonic-vlan><VLAN><VLAN_LIST><name>Vlan100</name></VLAN_LIST></VLAN></sonic-vlan></filter></get></rpc>",
		authenticator: autt,
		session: nil,
	}

	// Device specific response, change to your testing device correct response
	correct := "<?xml version=\"1.0\" encoding=\"utf-8\"?><rpc-reply xmlns=\"urn:ietf:params:xml:ns:netconf:base:1.0\" message-id=\"752ab2ee-f662-4ec9-9970-f308a80f18f2\"><rpc-error><error-type>rpc</error-type><error-severity>error</error-severity><error-message xml:lang=\"en\">[AUTH] Unauthorized access /sonic-vlan:sonic-vlan/VLAN/VLAN_LIST[name=Vlan100]</error-message></rpc-error></rpc-reply>"

	result := process(request)

	if result != correct {
		t.Errorf("Result was incorrect, got: %s, want: %s.", result, correct)
	}
}