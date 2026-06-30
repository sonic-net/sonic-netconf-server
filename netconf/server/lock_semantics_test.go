package server

import (
	"encoding/xml"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// LockDeniedError
// ---------------------------------------------------------------------------

func TestLockDeniedErrorMessage(t *testing.T) {
	err := &LockDeniedError{HolderSessionID: "42"}
	want := "lock denied by session 42"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

// ---------------------------------------------------------------------------
// parseSessionIDFromLockValue
// ---------------------------------------------------------------------------

func TestParseSessionIDFromLockValue_ValidFormat(t *testing.T) {
	got := parseSessionIDFromLockValue("7:some-uuid-string")
	if got != "7" {
		t.Errorf("got %q, want %q", got, "7")
	}
}

func TestParseSessionIDFromLockValue_LegacyUUIDOnly(t *testing.T) {
	// A raw UUID with no colon prefix should return "0" (non-NETCONF holder).
	got := parseSessionIDFromLockValue("no-colon-here")
	if got != "0" {
		t.Errorf("got %q, want %q", got, "0")
	}
}

func TestParseSessionIDFromLockValue_EmptyString(t *testing.T) {
	got := parseSessionIDFromLockValue("")
	if got != "0" {
		t.Errorf("got %q, want %q", got, "0")
	}
}

func TestParseSessionIDFromLockValue_ColonAtStart(t *testing.T) {
	// Colon at position 0 means no session-id prefix.
	got := parseSessionIDFromLockValue(":uuid")
	if got != "0" {
		t.Errorf("got %q, want %q", got, "0")
	}
}

func TestParseSessionIDFromLockValue_MultipleColons(t *testing.T) {
	// Only the first colon is significant; rest is the uuid.
	got := parseSessionIDFromLockValue("3:urn:ietf:extra")
	if got != "3" {
		t.Errorf("got %q, want %q", got, "3")
	}
}

// ---------------------------------------------------------------------------
// createErrorXML with LockDeniedError
// ---------------------------------------------------------------------------

func TestCreateErrorXMLWithLockDeniedError(t *testing.T) {
	err := &LockDeniedError{HolderSessionID: "99"}
	xmlStr := createErrorXML(err)

	// Must contain the RFC-required error-tag.
	if !strings.Contains(xmlStr, "<error-tag>lock-denied</error-tag>") {
		t.Errorf("missing <error-tag>lock-denied</error-tag> in:\n%s", xmlStr)
	}

	// Must carry the holder's session-id in error-info.
	if !strings.Contains(xmlStr, "<session-id>99</session-id>") {
		t.Errorf("missing <session-id>99</session-id> in:\n%s", xmlStr)
	}

	// Must use error-type "protocol" per RFC 6241 §8.3.9.
	if !strings.Contains(xmlStr, "<error-type>protocol</error-type>") {
		t.Errorf("missing <error-type>protocol</error-type> in:\n%s", xmlStr)
	}

	// Must be parseable XML.
	var rpcErr RPCError
	if err := xml.Unmarshal([]byte(xmlStr), &rpcErr); err != nil {
		t.Errorf("xml.Unmarshal failed: %v\nXML: %s", err, xmlStr)
	}
	if rpcErr.ErrorTag != "lock-denied" {
		t.Errorf("ErrorTag = %q, want %q", rpcErr.ErrorTag, "lock-denied")
	}
	if rpcErr.ErrorInfo.SessionID != "99" {
		t.Errorf("ErrorInfo.SessionID = %q, want %q", rpcErr.ErrorInfo.SessionID, "99")
	}
}

func TestCreateErrorXMLWithPlainError(t *testing.T) {
	// Plain errors must still produce a generic rpc-error (not lock-denied).
	xmlStr := createErrorXML(errorf("something went wrong"))
	if strings.Contains(xmlStr, "lock-denied") {
		t.Errorf("plain error must not produce lock-denied, got:\n%s", xmlStr)
	}
	if !strings.Contains(xmlStr, "something went wrong") {
		t.Errorf("expected error message in output, got:\n%s", xmlStr)
	}
}

// errorf is a lightweight helper that avoids importing fmt in tests.
type simpleError struct{ msg string }

func (e *simpleError) Error() string { return e.msg }

func errorf(msg string) error { return &simpleError{msg: msg} }

// ---------------------------------------------------------------------------
// createErrorXML with zero session-id (non-NETCONF holder)
// ---------------------------------------------------------------------------

func TestCreateErrorXMLLockDeniedZeroSessionID(t *testing.T) {
	err := &LockDeniedError{HolderSessionID: "0"}
	xmlStr := createErrorXML(err)

	if !strings.Contains(xmlStr, "<session-id>0</session-id>") {
		t.Errorf("expected <session-id>0</session-id> for non-NETCONF holder, got:\n%s", xmlStr)
	}
}
