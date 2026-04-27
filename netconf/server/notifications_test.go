package server

import (
	"strings"
	"testing"
	"time"

	"github.com/antchfx/xmlquery"
)

func parseTestRPC(t *testing.T, input string) *xmlquery.Node {
	t.Helper()
	node, err := xmlquery.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("failed to parse XML: %v", err)
	}
	return node
}

func TestParseCreateSubscriptionXPath(t *testing.T) {
	root := parseTestRPC(t, `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <create-subscription xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0">
    <stream>NETCONF</stream>
    <filter type="xpath" select="/sonic-interface:sonic-interface/VLAN"/>
    <startTime>2024-01-02T03:04:05Z</startTime>
    <stopTime>2024-01-02T04:04:05Z</stopTime>
  </create-subscription>
</rpc>`)

	sub, paths, err := ParseCreateSubscription(root)
	if err != nil {
		t.Fatalf("ParseCreateSubscription returned error: %v", err)
	}
	if sub.Stream != DefaultStream {
		t.Fatalf("unexpected stream: got %q want %q", sub.Stream, DefaultStream)
	}
	if sub.StartTime != "2024-01-02T03:04:05Z" || sub.StopTime != "2024-01-02T04:04:05Z" {
		t.Fatalf("unexpected replay window: start=%q stop=%q", sub.StartTime, sub.StopTime)
	}
	if len(paths) != 1 || paths[0] != "/sonic-interface:sonic-interface/VLAN" {
		t.Fatalf("unexpected paths: %#v", paths)
	}
}

func TestParseCreateSubscriptionSubtree(t *testing.T) {
	root := parseTestRPC(t, `<rpc message-id="102" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <create-subscription xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0">
    <filter type="subtree">
      <sonic-interface xmlns="http://github.com/Azure/sonic-interface">
        <VLAN/>
      </sonic-interface>
    </filter>
  </create-subscription>
</rpc>`)

	_, paths, err := ParseCreateSubscription(root)
	if err != nil {
		t.Fatalf("ParseCreateSubscription returned error: %v", err)
	}
	if len(paths) != 1 || paths[0] != "/sonic-interface/VLAN" {
		t.Fatalf("unexpected paths: %#v", paths)
	}
}

func TestParseCreateSubscriptionSubtreeWithListKeyUsesTransformerPath(t *testing.T) {
	root := parseTestRPC(t, `<rpc message-id="103" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <create-subscription xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0">
    <filter type="subtree">
      <sonic-vlan xmlns="http://github.com/Azure/sonic-vlan">
        <VLAN>
          <VLAN_LIST>
            <name>Vlan100</name>
          </VLAN_LIST>
        </VLAN>
      </sonic-vlan>
    </filter>
  </create-subscription>
</rpc>`)

	_, paths, err := ParseCreateSubscription(root)
	if err != nil {
		t.Fatalf("ParseCreateSubscription returned error: %v", err)
	}
	if len(paths) != 1 || paths[0] != "/sonic-vlan/VLAN/VLAN_LIST[name=Vlan100]" {
		t.Fatalf("unexpected paths: %#v", paths)
	}
}

func TestParseCreateSubscriptionSubtreeWithListKeyAndLeaf(t *testing.T) {
	root := parseTestRPC(t, `<rpc message-id="104" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <create-subscription xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0">
    <filter type="subtree">
      <sonic-vlan xmlns="http://github.com/Azure/sonic-vlan">
        <VLAN>
          <VLAN_LIST>
            <name>Vlan100</name>
            <description/>
          </VLAN_LIST>
        </VLAN>
      </sonic-vlan>
    </filter>
  </create-subscription>
</rpc>`)

	_, paths, err := ParseCreateSubscription(root)
	if err != nil {
		t.Fatalf("ParseCreateSubscription returned error: %v", err)
	}
	if len(paths) != 1 || paths[0] != "/sonic-vlan/VLAN/VLAN_LIST[name=Vlan100]/description" {
		t.Fatalf("unexpected paths: %#v", paths)
	}
}

func TestParseCreateSubscriptionRejectsSubtreeValueFilter(t *testing.T) {
	root := parseTestRPC(t, `<rpc message-id="105" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <create-subscription xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0">
    <filter type="subtree">
      <sonic-vlan xmlns="http://github.com/Azure/sonic-vlan">
        <VLAN>
          <VLAN_LIST>
            <name>Vlan100</name>
            <description>not-supported-as-filter-value</description>
          </VLAN_LIST>
        </VLAN>
      </sonic-vlan>
    </filter>
  </create-subscription>
</rpc>`)

	_, _, err := ParseCreateSubscription(root)
	if err == nil {
		t.Fatal("expected unsupported subtree value filter error")
	}
}

func TestParseCreateSubscriptionRejectsComplexXPath(t *testing.T) {
	root := parseTestRPC(t, `<rpc message-id="106" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <create-subscription xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0">
    <filter type="xpath" select="count(/sonic-vlan:sonic-vlan/VLAN)"/>
  </create-subscription>
</rpc>`)

	_, _, err := ParseCreateSubscription(root)
	if err == nil {
		t.Fatal("expected unsupported xpath filter error")
	}
}

func TestNotificationXML(t *testing.T) {
	eventTime := time.Date(2024, time.January, 2, 3, 4, 5, 0, time.FixedZone("offset", 3*60*60))
	out := notificationXML(eventTime, `<replayComplete xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0"></replayComplete>`)

	for _, want := range []string{
		`<?xml version="1.0" encoding="utf-8"?>`,
		`<notification xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0">`,
		`<eventTime>2024-01-02T00:04:05Z</eventTime>`,
		`<replayComplete xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0"></replayComplete>`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("notification XML missing %q in %s", want, out)
		}
	}
}

func TestNotificationStreamsXML(t *testing.T) {
	out, err := notificationStreamsXML()
	if err != nil {
		t.Fatalf("notificationStreamsXML returned error: %v", err)
	}

	for _, want := range []string{
		`<netconf xmlns="urn:ietf:params:xml:ns:netmod:notification">`,
		`<streams>`,
		`<name>NETCONF</name>`,
		`<replaySupport>true</replaySupport>`,
		`<replayLogCreationTime>`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("streams XML missing %q in %s", want, out)
		}
	}
}

func TestBuildRPCErrorIncludesRFCFields(t *testing.T) {
	err := buildRPCError("17", "protocol", "resource-denied", "error", "edit-config")
	reply, ok := isRPCErrorResponse(err)
	if !ok {
		t.Fatal("expected prebuilt rpc-error response")
	}

	for _, want := range []string{
		`message-id="17"`,
		`<error-type>protocol</error-type>`,
		`<error-tag>resource-denied</error-tag>`,
		`<error-severity>error</error-severity>`,
		`<bad-element>edit-config</bad-element>`,
	} {
		if !strings.Contains(reply, want) {
			t.Fatalf("rpc-error missing %q in %s", want, reply)
		}
	}
}

func TestCreateSubscriptionRejectsStopTimeWithoutStartTime(t *testing.T) {
	root := parseTestRPC(t, `<rpc message-id="107" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <create-subscription xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0">
    <stopTime>2024-01-02T04:04:05Z</stopTime>
  </create-subscription>
</rpc>`)

	err := createSubscriptionHandler(nil, root)
	reply, ok := isRPCErrorResponse(err)
	if !ok {
		t.Fatal("expected prebuilt rpc-error response")
	}
	for _, want := range []string{
		`<error-type>protocol</error-type>`,
		`<error-tag>missing-element</error-tag>`,
		`<bad-element>startTime</bad-element>`,
	} {
		if !strings.Contains(reply, want) {
			t.Fatalf("rpc-error missing %q in %s", want, reply)
		}
	}
}

func TestCreateSubscriptionRejectsStopTimeBeforeStartTime(t *testing.T) {
	root := parseTestRPC(t, `<rpc message-id="108" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <create-subscription xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0">
    <startTime>2024-01-02T04:04:05Z</startTime>
    <stopTime>2024-01-02T03:04:05Z</stopTime>
  </create-subscription>
</rpc>`)

	err := createSubscriptionHandler(nil, root)
	reply, ok := isRPCErrorResponse(err)
	if !ok {
		t.Fatal("expected prebuilt rpc-error response")
	}
	for _, want := range []string{
		`<error-type>protocol</error-type>`,
		`<error-tag>bad-element</error-tag>`,
		`<bad-element>stopTime</bad-element>`,
	} {
		if !strings.Contains(reply, want) {
			t.Fatalf("rpc-error missing %q in %s", want, reply)
		}
	}
}

func TestCreateSubscriptionRejectsUnknownStream(t *testing.T) {
	root := parseTestRPC(t, `<rpc message-id="109" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <create-subscription xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0">
    <stream>SNMP</stream>
  </create-subscription>
</rpc>`)

	err := createSubscriptionHandler(nil, root)
	reply, ok := isRPCErrorResponse(err)
	if !ok {
		t.Fatal("expected prebuilt rpc-error response")
	}
	for _, want := range []string{
		`<error-type>protocol</error-type>`,
		`<error-tag>invalid-value</error-tag>`,
		`<bad-element>stream</bad-element>`,
	} {
		if !strings.Contains(reply, want) {
			t.Fatalf("rpc-error missing %q in %s", want, reply)
		}
	}
}

func TestReplayBufferEvictsOldest(t *testing.T) {
	rb := newReplayBuffer(2)
	t1 := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Second)
	t3 := t2.Add(time.Second)

	rb.append("first", t1)
	rb.append("second", t2)
	rb.append("third", t3)

	if rb.count != 2 {
		t.Fatalf("unexpected replay buffer count: got %d want 2", rb.count)
	}

	oldest := rb.entries[rb.head]
	newest := rb.entries[(rb.head+1)%rb.cap]
	if oldest.xmlPayload != "second" || newest.xmlPayload != "third" {
		t.Fatalf("unexpected replay ordering: oldest=%q newest=%q", oldest.xmlPayload, newest.xmlPayload)
	}

	metadata := rb.streamMetadata()
	if metadata.ReplayLogAgedTime == "" {
		t.Fatal("expected replayLogAgedTime after eviction")
	}
}
