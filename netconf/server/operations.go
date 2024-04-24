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

import "encoding/xml"

const (
	RPCGetRequest       = "GET"
	RPCGetConfigRequest = "GET-Config"
	RPCGetSchemas       = "/netconf-state:netconf-state/schemas"
	RPCGetYangModules   = "/modules-state:modules-state[xmlns=urn:ietf:params:xml:ns:yang:ietf-yang-library]"

	RPCDelimiter   = "]]>]]>"
	ChunkDelimiter = "\n##\n"

	ChunkedMessage = "\n#%d\n%s\n##\n"

	NsNetconfMonitoring = "urn:ietf:params:xml:ns:yang:ietf-netconf-monitoring"
	NsTailfActions      = "http://tail-f.com/ns/netconf/actions/1.0"

	CapNetconf10       = "urn:ietf:params:netconf:base:1.0"
	CapNetconf11       = "urn:ietf:params:netconf:base:1.1"
	CapConfirmedCommit = "urn:ietf:params:netconf:capability:confirmed-commit:1.1"
	CapValidate        = "urn:ietf:params:netconf:capability:validate:1.1"
	CapWithDefaults    = "urn:ietf:params:netconf:capability:with-defaults:1.0"
	CapNotifiction     = "urn:ietf:params:netconf:capability:notification:1.0"
	CapInterleave      = "urn:ietf:params:netconf:capability:interleave:1.0"
	CapStartup         = "urn:ietf:params:netconf:capability:startup:1.0"
	CapWritableRunning = "urn:ietf:params:netconf:capability:writable-running:1.0"
	CapCandidate       = "urn:ietf:params:netconf:capability:candidate:1.0"
	CapRollbackOnError = "urn:ietf:params:netconf:capability:rollback-on-error:1.0"
	CapURL             = "urn:ietf:params:netconf:capability:url:1.0"
	CapXPath           = "urn:ietf:params:netconf:capability:xpath:1.0"
	CapMonitoring      = NsNetconfMonitoring
	CapTailfActions    = NsTailfActions
)

type RPCError struct {
	XMLName       xml.Name `xml:"rpc-error"`
	ErrorType     string   `xml:"error-type"`
	ErrorTag      string   `xml:"error-tag"`
	ErrorSeverity string   `xml:"error-severity"`
	ErrorAppTag   string   `xml:"error-app-tag"`
	ErrorPath     string   `xml:"error-path"`
	ErrorMessage  string   `xml:"error-message"`
	ErrorInfo     struct {
		BadElement   string `xml:"bad-element"`
		BadAttribute string `xml:"bad-attribute"`
		BadNamespace string `xml:"bad-namespace"`
		SessionID    string `xml:"session-id"`
		InnerXML     []byte `xml:",innerxml"`
	} `xml:"error-info"`
}

type Filter struct {
	Type    string `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 type,attr,omitempty"`
	Select  string `xml:"select,attr,omitempty"`
	Subtree string `xml:",innerxml"`
}

type Get struct {
	XMLName xml.Name `xml:"rpc"`
	// Filter  *Filter  `xml:"filter,omitempty"`
	// WithDefaults DefaultsMode `xml:"urn:ietf:params:xml:ns:yang:ietf-netconf-with-defaults with-defaults,omitempty"`
}

type DefaultsMode string

type Hello struct {
	XMLName      xml.Name `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 hello"`
	Capabilities []string `xml:"capabilities>capability"`
	SessionID    int      `xml:"session-id,omitempty"`
}

type Schema struct {
	XMLName    xml.Name `xml:"schema"`
	Identifier string   `xml:"identifier"`
	Version    string   `xml:"version,omitempty"`
	Format     string   `xml:"format,omitempty"`
	NameSpace  string   `xml:"namespace,omitempty"`
	Location   string   `xml:"location,omitempty"`
	ModelPath  string   `xml:"-"`
}

type ModulesState struct {
	XMLName     xml.Name `xml:"urn:ietf:params:xml:ns:yang:ietf-yang-library modules-state"`
	ModuleSetId *string  `xml:"module-set-id"`
	Modules     []Module `xml:"urn:ietf:params:xml:ns:yang:ietf-yang-library modules"`
}

type Module struct {
	XMLName         xml.Name `xml:"module"`
	Name            *string  `xml:",name"`
	ConformanceType string   `xml:"conformance-type"`
	Feature         []string `xml:"feature"`
	Namespace       *string  `xml:"namespace"`
	Revision        *string  `xml:"revision"`
	Schema          *string  `xml:"schema"`
	// Submodule       Module `xml:"submodule"`
	// Deviation       map[ModuleKey]*ModuleKey `xml:"deviation"`
}

type State struct {
	XMLName xml.Name `xml:"urn:ietf:params:xml:ns:yang:ietf-netconf-monitoring netconf-state"`
	Schemas []Schema `xml:"schemas>schema"`
}

type GetSchema struct {
	XMLName    xml.Name `xml:"urn:ietf:params:xml:ns:yang:ietf-netconf-monitoring get-schema"`
	Identifier string   `xml:"urn:ietf:params:xml:ns:yang:ietf-netconf-monitoring identifier"`
	Version    string   `xml:"urn:ietf:params:xml:ns:yang:ietf-netconf-monitoring version,omitempty"`
	Format     string   `xml:"urn:ietf:params:xml:ns:yang:ietf-netconf-monitoring format,omitempty"`
}
