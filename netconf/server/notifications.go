package server

// notifications.go - RFC 5277 / RFC 6241 section 8 subscription implementation.
//
// Responsibilities:
//   - In-memory replay ring buffer (replayBuffer).
//   - createSubscriptionHandler - validates a <create-subscription> RPC, sends
//     <ok>, and spawns runSubscription.
//   - runSubscription goroutine - handles replay phase, live event delivery,
//     stopTime timer, and clean shutdown.
//   - buildConfigChangeEvent - converts a translib.SubscribeResponse to an RFC
//     6470 <netconf-config-change> XML string.

import (
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"orange/sonic-netconf-server/lib"

	"github.com/Azure/sonic-mgmt-common/translib"
	"github.com/Workiva/go-datastructures/queue"
	"github.com/antchfx/xmlquery"
	"github.com/gliderlabs/ssh"
	"github.com/golang/glog"
	gnmipb "github.com/openconfig/gnmi/proto/gnmi"
	"github.com/openconfig/ygot/ygot"
)

// ---------------------------------------------------------------------------
// Replay buffer
// ---------------------------------------------------------------------------

// replayEntry stores a single buffered notification event.
type replayEntry struct {
	eventTime  time.Time
	xmlPayload string // fully-formed inner XML (RFC 6470 content)
}

// replayBuffer is a process-wide, stream-scoped, bounded ring of notification
// events.  It is safe for concurrent use.
type replayBuffer struct {
	mu           sync.RWMutex
	entries      []replayEntry
	head         int // index of oldest entry
	count        int
	cap          int
	creationTime time.Time
	agedTime     time.Time
}

func newReplayBuffer(capacity int) *replayBuffer {
	return &replayBuffer{
		entries:      make([]replayEntry, capacity),
		cap:          capacity,
		creationTime: time.Now().UTC(),
	}
}

// append adds a new event to the ring, evicting the oldest when full.
func (rb *replayBuffer) append(payload string, t time.Time) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	idx := (rb.head + rb.count) % rb.cap
	rb.entries[idx] = replayEntry{eventTime: t, xmlPayload: payload}
	if rb.count < rb.cap {
		rb.count++
	} else {
		// Ring is full, so advance head to evict oldest.
		rb.agedTime = rb.entries[rb.head].eventTime
		rb.head = (rb.head + 1) % rb.cap
	}
}

func (rb *replayBuffer) streamMetadata() NotificationStream {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	stream := NotificationStream{
		Name:                  DefaultStream,
		Description:           "Default NETCONF event stream",
		ReplaySupport:         true,
		ReplayLogCreationTime: rb.creationTime.Format(time.RFC3339),
	}
	if rb.count > 0 {
		oldest := rb.entries[rb.head].eventTime
		if oldest.Before(rb.creationTime) || rb.creationTime.IsZero() {
			stream.ReplayLogCreationTime = oldest.UTC().Format(time.RFC3339)
		}
	}
	if !rb.agedTime.IsZero() {
		stream.ReplayLogAgedTime = rb.agedTime.UTC().Format(time.RFC3339)
	}
	return stream
}

func notificationStreamsXML() (string, error) {
	state := NotificationStreamsState{
		Streams: []NotificationStream{globalReplayBuffer.streamMetadata()},
	}
	xmlBytes, err := xml.Marshal(state)
	if err != nil {
		return "", err
	}
	return string(xmlBytes), nil
}

// replayInto sends buffered events in chronological order that fall within
// [state.startTime, state.stopTime) onto session.  It returns the number of
// events replayed.
func (rb *replayBuffer) replayInto(session ssh.Session, state *subscriptionState) int {
	rb.mu.RLock()
	snapshot := make([]replayEntry, rb.count)
	for i := 0; i < rb.count; i++ {
		snapshot[i] = rb.entries[(rb.head+i)%rb.cap]
	}
	rb.mu.RUnlock()

	sent := 0
	for _, e := range snapshot {
		if e.eventTime.Before(state.startTime) {
			continue
		}
		if !state.stopTime.IsZero() && !e.eventTime.Before(state.stopTime) {
			break
		}
		sendNotification(session, e.eventTime, e.xmlPayload)
		sent++
	}
	return sent
}

// Global replay buffer - one per server process (single "NETCONF" stream).
var globalReplayBuffer = newReplayBuffer(ReplayBufferSize)

// ---------------------------------------------------------------------------
// RFC 6470 config-change event builder
// ---------------------------------------------------------------------------

// buildConfigChangeEvent converts a translib.SubscribeResponse into an RFC 6470
// <netconf-config-change> XML string and the event timestamp.
//
// For updated paths  → <edit><operation>merge</operation>…</edit>
// For deleted paths  → <edit><operation>delete</operation>…</edit>
//
// We use ygot.TogNMINotifications to enumerate the individual leaf/list-entry
// paths within SubscribeResponse.Update, giving finer-grained RFC 6470 <edit>
// targets.  Deleted paths from resp.Delete are used directly.
func buildConfigChangeEvent(resp *translib.SubscribeResponse) (string, time.Time) {
	eventTime := time.Unix(0, resp.Timestamp).UTC()
	if resp.Timestamp == 0 {
		eventTime = time.Now().UTC()
	}

	event := NetconfConfigChange{
		Datastore: "running",
	}
	event.ChangedBy.Server = struct{}{}

	// Update paths: walk the ygot struct to enumerate changed leaves.
	if resp.Update != nil {
		updatePaths := ygotUpdatePaths(resp.Update, resp.Timestamp)
		if len(updatePaths) == 0 && resp.Path != "" {
			// Fall back to the subscription path when the ygot walk yields nothing.
			updatePaths = []string{resp.Path}
		}
		for _, p := range updatePaths {
			event.Edit = append(event.Edit, ConfigChangeEdit{
				Target:    p,
				Operation: "merge",
			})
		}
	}

	// Deleted paths come as plain YANG path strings from translib.
	for _, dp := range resp.Delete {
		event.Edit = append(event.Edit, ConfigChangeEdit{
			Target:    dp,
			Operation: "delete",
		})
	}

	xmlBytes, err := xml.Marshal(event)
	if err != nil {
		glog.Errorf("[NOTIF] buildConfigChangeEvent marshal error: %v", err)
		return "", eventTime
	}
	return string(xmlBytes), eventTime
}

// ygotUpdatePaths extracts a deduplicated list of xpath-style path strings from
// a ygot ValidatedGoStruct using TogNMINotifications (ygot v0.7.1).
//
// TogNMINotifications renders each leaf/list-entry in the struct as a gNMI
// Update message.  We convert each gNMI path to a string with PathToString.
func ygotUpdatePaths(val ygot.ValidatedGoStruct, ts int64) []string {
	notifications, err := ygot.TogNMINotifications(val, ts, ygot.GNMINotificationsConfig{
		UsePathElem: true,
	})
	if err != nil {
		glog.V(2).Infof("[NOTIF] ygotUpdatePaths: TogNMINotifications error: %v", err)
		return nil
	}

	seen := make(map[string]struct{})
	var paths []string
	for _, n := range notifications {
		for _, upd := range n.GetUpdate() {
			fullPath := mergePaths(n.GetPrefix(), upd.GetPath())
			ps, err := ygot.PathToString(fullPath)
			if err != nil {
				continue
			}
			if _, ok := seen[ps]; ok {
				continue
			}
			seen[ps] = struct{}{}
			paths = append(paths, ps)
		}
	}
	return paths
}

// mergePaths joins a gNMI prefix and a relative path into a single absolute path.
func mergePaths(prefix, path *gnmipb.Path) *gnmipb.Path {
	if prefix == nil {
		return path
	}
	if path == nil {
		return prefix
	}
	return &gnmipb.Path{
		Elem: append(prefix.GetElem(), path.GetElem()...),
	}
}

// ---------------------------------------------------------------------------
// Subscription goroutine
// ---------------------------------------------------------------------------

// runSubscription is the long-lived goroutine for an active subscription.
// It:
//  1. Replays historical events (if startTime is set).
//  2. Calls translib.Subscribe and drains req.Q.
//  3. Forwards each live event as an RFC 5277 <notification>.
//  4. Handles stopTime expiry and cancel/kill signals.
func runSubscription(
	session ssh.Session,
	req translib.SubscribeRequest,
	ss *translib.SubscribeSession,
	state *subscriptionState,
) {
	defer func() {
		ss.Close()
		state.deactivate()
		glog.Infof("[NOTIF] subscription goroutine exiting")
	}()

	// (a) Replay phase: emit historical events from the in-memory ring buffer.
	if !state.startTime.IsZero() {
		n := globalReplayBuffer.replayInto(session, state)
		glog.Infof("[NOTIF] replayed %d events since %v", n, state.startTime)
		sendReplayComplete(session)
		// If a stopTime was set and has already passed, we are done.
		if !state.stopTime.IsZero() && time.Now().After(state.stopTime) {
			if state.completeByStopTime() {
				sendNotificationComplete(session)
			}
			return
		}
	}

	// (b) Live phase: start translib subscription.
	//     translib.Subscribe returns after initial setup (initial sync push onto
	//     req.Q); long-lived events continue flowing via Redis-backed goroutines.
	if err := translib.Subscribe(req); err != nil {
		glog.Errorf("[NOTIF] translib.Subscribe setup error: %v", err)
		return
	}

	// Set up the optional stop timer. It closes the translib stop channel even
	// when this goroutine is blocked waiting on the priority queue.
	var stopTimer *time.Timer
	if !state.stopTime.IsZero() {
		remaining := time.Until(state.stopTime)
		if remaining > 0 {
			stopTimer = time.AfterFunc(remaining, func() {
				if !state.completeByStopTime() {
					return
				}
				safeClose(req.Stop)
				sendNotificationComplete(session)
			})
			defer stopTimer.Stop()
		} else {
			safeClose(req.Stop)
			if state.completeByStopTime() {
				sendNotificationComplete(session)
			}
			return
		}
	}

	for {
		// Check cancellation / timer first (non-blocking).
		select {
		case <-state.cancelChan:
			safeClose(req.Stop)
			return
		default:
		}

		// Block on the priority queue for the next event.
		// queue.PriorityQueue.Get blocks until an item is available or the queue
		// is disposed.
		items, err := req.Q.Get(1)
		if err != nil {
			// Queue disposed; subscription ended.
			return
		}

		for _, item := range items {
			resp, ok := item.(*translib.SubscribeResponse)
			if !ok {
				continue
			}

			if resp.IsTerminated {
				glog.Infof("[NOTIF] translib signalled IsTerminated")
				return
			}

			if resp.SyncComplete {
				// Initial sync flush done; not a data event.
				continue
			}

			xmlContent, eventTime := buildConfigChangeEvent(resp)
			if xmlContent == "" {
				continue
			}
			globalReplayBuffer.append(xmlContent, eventTime)
			sendNotification(session, eventTime, xmlContent)
		}
	}
}

// safeClose closes a channel exactly once, ignoring a double-close.
func safeClose(ch chan struct{}) {
	defer func() { recover() }()
	close(ch)
}

// ---------------------------------------------------------------------------
// create-subscription handler
// ---------------------------------------------------------------------------

// createSubscriptionHandler implements RFC 5277 §2.1.1.
//
// It validates the request, sends <ok>, stores subscriptionState on the session
// context, and spawns runSubscription.  The caller (handleRequest) must return
// the notificationHandledSentinel string so process() does not write a second
// response.
func createSubscriptionHandler(session ssh.Session, requestNode *xmlquery.Node) error {
	ctx := session.Context().(ssh.Context)
	authenticator := ctx.Value("auth").(lib.Authenticator)

	messageId := requestNode.SelectAttr("message-id")

	// Authorization.
	if !authenticator.Authorize("create-subscription", "") {
		return buildRPCError(messageId, "protocol", "access-denied", "error", "")
	}

	// Duplicate subscription check.
	if sub, ok := ctx.Value(ctxKeySubscription).(*subscriptionState); ok && sub.isActive() {
		return buildRPCError(messageId, "protocol", "operation-failed", "error", "")
	}

	// Parse the request body.
	sub, filterPaths, err := ParseCreateSubscription(requestNode)
	if err != nil {
		return buildRPCError(messageId, "protocol", "invalid-value", "error", "filter")
	}
	if sub.HasFilter && len(filterPaths) == 0 {
		return buildRPCError(messageId, "protocol", "invalid-value", "error", "filter")
	}
	if len(filterPaths) == 0 {
		filterPaths = []string{"/"}
	}

	// Stream validation: only "NETCONF" is supported.
	stream := sub.Stream
	if stream == "" {
		stream = DefaultStream
	}
	if !strings.EqualFold(stream, DefaultStream) {
		return buildRPCError(messageId, "protocol", "invalid-value", "error", "stream")
	}

	// startTime validation.
	var startTime time.Time
	if sub.StartTime != "" {
		startTime, err = time.Parse(time.RFC3339, sub.StartTime)
		if err != nil {
			return buildRPCError(messageId, "protocol", "bad-element", "error", "startTime")
		}
		if startTime.After(time.Now()) {
			return buildRPCError(messageId, "protocol", "bad-element", "error", "startTime")
		}
	}

	// stopTime validation.
	var stopTime time.Time
	if sub.StopTime != "" {
		if sub.StartTime == "" {
			return buildRPCError(messageId, "protocol", "missing-element", "error", "startTime")
		}
		stopTime, err = time.Parse(time.RFC3339, sub.StopTime)
		if err != nil {
			return buildRPCError(messageId, "protocol", "bad-element", "error", "stopTime")
		}
		if !startTime.IsZero() && !stopTime.After(startTime) {
			return buildRPCError(messageId, "protocol", "bad-element", "error", "stopTime")
		}
	}

	// Build translib subscription request.
	cancelChan := make(chan struct{})
	stopTrans := make(chan struct{})
	pq := queue.NewPriorityQueue(64, false)

	// Build User from the SSH session username.  Roles are not populated since
	// the existing NETCONF handlers do not use translib-level auth (auth is
	// enforced at the NETCONF layer via lib.Authenticator above).
	req := translib.SubscribeRequest{
		Paths: filterPaths,
		Q:     pq,
		Stop:  stopTrans,
		User:  translib.UserRoles{Name: ctx.User()},
	}

	// Path support check.
	isReqs := make([]translib.IsSubscribePath, len(filterPaths))
	for i, p := range filterPaths {
		isReqs[i] = translib.IsSubscribePath{Path: p, Mode: translib.OnChange}
	}
	responses, isErr := translib.IsSubscribeSupported(translib.IsSubscribeRequest{
		Paths: isReqs,
		User:  req.User,
	})
	if isErr != nil {
		return buildRPCError(messageId, "application", "operation-failed", "error", "")
	}
	for _, r := range responses {
		if r.Err != nil {
			return buildRPCError(messageId, "application", "operation-not-supported", "error", r.Path)
		}
	}

	// Create translib session for path translation caching.
	ss := translib.NewSubscribeSession()
	req.Session = ss

	// Build and store subscription state.
	state := &subscriptionState{
		active:      true,
		cancelChan:  cancelChan,
		stopTrans:   stopTrans,
		stream:      stream,
		startTime:   startTime,
		stopTime:    stopTime,
		filterPaths: filterPaths,
	}
	ctx.SetValue(ctxKeySubscription, state)

	// Send <ok> reply BEFORE spawning the goroutine (RFC 5277 §2.1.1).
	writeResponse(session, CreateResponse(messageId, []byte("ok")))

	go runSubscription(session, req, ss, state)

	// Accounting.
	if !authenticator.Account("create-subscription", "") {
		glog.Warning("[TACPLUS] accounting failed create-subscription")
	}

	return nil
}

// buildRPCError returns an error whose message is a fully-formed NETCONF
// <rpc-error> XML element.  The caller typically returns this from a handler
// function so process() wraps it in <rpc-reply>.
//
// Since process() calls createErrorResponse(messageId, err) which wraps the
// error message in another <rpc-error> shell, we short-circuit that by having
// process() detect a raw RPCError and use it directly.  For simplicity we
// instead write the error directly here and return a sentinel error to skip
// process()'s own wrapping.
func buildRPCError(messageId, errType, errTag, severity, badElement string) error {
	inner := createRPCError(errType, errTag, severity, badElement)
	resp := CreateResponse(messageId, []byte(inner))
	// We cannot call writeResponse here because we don't have a session reference.
	// Return a structured error that process() will recognise.
	return errors.New(fmt.Sprintf("__RPC_ERROR__%s", resp))
}

// isRPCErrorResponse returns true when err was produced by buildRPCError.
func isRPCErrorResponse(err error) (string, bool) {
	const prefix = "__RPC_ERROR__"
	msg := err.Error()
	if strings.HasPrefix(msg, prefix) {
		return msg[len(prefix):], true
	}
	return "", false
}

func sendNotification(session ssh.Session, eventTime time.Time, payload string) {
	message := notificationXML(eventTime, payload)
	if message == "" {
		return
	}
	writeResponse(session, message)
}

func sendReplayComplete(session ssh.Session) {
	sendNotification(session, time.Now().UTC(), replayCompletePayload())
}

func sendNotificationComplete(session ssh.Session) {
	sendNotification(session, time.Now().UTC(), notificationCompletePayload())
}
