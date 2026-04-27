package server

import (
	"encoding/xml"
	"strings"
	"sync"
	"time"
)

const (
	NsNetconfNotification = "urn:ietf:params:xml:ns:netconf:notification:1.0"
	NsNetconfConfigChange = "urn:ietf:params:xml:ns:yang:ietf-netconf-notifications"
	NsNetmodNotification  = "urn:ietf:params:xml:ns:netmod:notification"

	DefaultStream    = "NETCONF"
	ReplayBufferSize = 1024

	notificationHandledSentinel = "__NETCONF_NOTIFICATION_RESPONSE_WRITTEN__"
)

type contextKey string

const (
	ctxKeySubscription contextKey = "netconf-subscription"
	ctxKeyWriteMu      contextKey = "netconf-write-mu"
)

type subscriptionState struct {
	mu          sync.Mutex
	active      bool
	cancelChan  chan struct{}
	stopTrans   chan struct{}
	stream      string
	startTime   time.Time
	stopTime    time.Time
	filterPaths []string
}

func (s *subscriptionState) isActive() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

func (s *subscriptionState) deactivate() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		return false
	}
	s.active = false
	return true
}

func (s *subscriptionState) completeByStopTime() bool {
	return s.deactivate()
}

type CreateSubscription struct {
	XMLName   xml.Name `xml:"create-subscription"`
	Stream    string   `xml:"stream"`
	Filter    Filter   `xml:"filter"`
	StartTime string   `xml:"startTime"`
	StopTime  string   `xml:"stopTime"`
	HasFilter bool     `xml:"-"`
}

type NotificationMessage struct {
	XMLName   xml.Name `xml:"urn:ietf:params:xml:ns:netconf:notification:1.0 notification"`
	EventTime string   `xml:"eventTime"`
	Payload   []byte   `xml:",innerxml"`
}

type ReplayComplete struct {
	XMLName xml.Name `xml:"urn:ietf:params:xml:ns:netconf:notification:1.0 replayComplete"`
}

type NotificationComplete struct {
	XMLName xml.Name `xml:"urn:ietf:params:xml:ns:netconf:notification:1.0 notificationComplete"`
}

type NetconfConfigChange struct {
	XMLName   xml.Name `xml:"urn:ietf:params:xml:ns:yang:ietf-netconf-notifications netconf-config-change"`
	ChangedBy struct {
		Server struct{} `xml:"server"`
	} `xml:"changed-by"`
	Datastore string             `xml:"datastore"`
	Edit      []ConfigChangeEdit `xml:"edit,omitempty"`
}

type ConfigChangeEdit struct {
	Target    string `xml:"target"`
	Operation string `xml:"operation"`
}

type NotificationStreamsState struct {
	XMLName xml.Name             `xml:"urn:ietf:params:xml:ns:netmod:notification netconf"`
	Streams []NotificationStream `xml:"streams>stream"`
}

type NotificationStream struct {
	Name                  string `xml:"name"`
	Description           string `xml:"description"`
	ReplaySupport         bool   `xml:"replaySupport"`
	ReplayLogCreationTime string `xml:"replayLogCreationTime,omitempty"`
	ReplayLogAgedTime     string `xml:"replayLogAgedTime,omitempty"`
}

func createRPCError(errType, errTag, severity, badElement string) string {
	rpcErr := RPCError{
		ErrorType:     errType,
		ErrorTag:      errTag,
		ErrorSeverity: severity,
	}
	if badElement != "" {
		rpcErr.ErrorInfo.BadElement = badElement
	}

	xmlBytes, err := xml.Marshal(rpcErr)
	if err != nil {
		return createErrorXML(err)
	}

	out := string(xmlBytes)
	out = strings.ReplaceAll(out, "<error-app-tag></error-app-tag>", "")
	out = strings.ReplaceAll(out, "<error-path></error-path>", "")
	out = strings.ReplaceAll(out, "<error-message></error-message>", "")
	out = strings.ReplaceAll(out, "<bad-element></bad-element>", "")
	out = strings.ReplaceAll(out, "<bad-attribute></bad-attribute>", "")
	out = strings.ReplaceAll(out, "<bad-namespace></bad-namespace>", "")
	out = strings.ReplaceAll(out, "<session-id></session-id>", "")
	out = strings.ReplaceAll(out, "<InnerXML></InnerXML>", "")
	out = strings.ReplaceAll(out, "<error-info></error-info>", "")
	return out
}

func notificationXML(eventTime time.Time, payload string) string {
	msg := NotificationMessage{
		EventTime: eventTime.UTC().Format(time.RFC3339),
		Payload:   []byte(payload),
	}
	xmlBytes, err := xml.Marshal(msg)
	if err != nil {
		return ""
	}
	return declaration + string(xmlBytes)
}

func replayCompletePayload() string {
	payload, _ := xml.Marshal(ReplayComplete{})
	return string(payload)
}

func notificationCompletePayload() string {
	payload, _ := xml.Marshal(NotificationComplete{})
	return string(payload)
}

func newSessionWriteMu() *sync.Mutex {
	return &sync.Mutex{}
}
