package server

import (
	"bufio"
	"encoding/xml"
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"orange/sonic-netconf-server/lib"

	"github.com/antchfx/xmlquery"
	"github.com/gliderlabs/ssh"
	"github.com/golang/glog"
)

var sessionID uint64

const (
	delimeter   = "]]>]]>"
	declaration = "<?xml version=\"1.0\" encoding=\"utf-8\"?>"
)

func SessionHandler(s ssh.Session) {
	setSessionWriteMu(s)
	defer cancelActiveSubscription(s)

	scanner := bufio.NewScanner(s)
	scanner.Split(SplitAt)

	// Send server capablities
	s.Write([]byte(capabilitesXML()))

	// Read client capablities
	scanner.Scan()
	err := readCapabilities(scanner.Text())
	if err != nil {
		writeResponse(s, createErrorResponse("1", err))
		s.Close()
		return
	}

	// Start main loop for incoming rpc calls
	for scanner.Scan() {
		requestStr := scanner.Text()
		fmt.Printf("\nReceving request <<< %s >>> \n %s \n\n", time.Now().Local().String(), requestStr)
		response := process(s, requestStr)
		fmt.Printf("\nSending response <<< %s >>> \n %s \n\n", time.Now().Local().String(), response)
		if response != notificationHandledSentinel {
			writeResponse(s, response)
		}
	}
}

func capabilitesXML() string {

	var serverHello Hello

	serverHello.SessionID = nextSessionID()
	serverHello.Capabilities = append(serverHello.Capabilities, CapNetconf10)
	serverHello.Capabilities = append(serverHello.Capabilities, CapNetconf11)

	serverHello.Capabilities = append(serverHello.Capabilities, CapWritableRunning)
	serverHello.Capabilities = append(serverHello.Capabilities, CapXPath)
	serverHello.Capabilities = append(serverHello.Capabilities, CapMonitoring)
	serverHello.Capabilities = append(serverHello.Capabilities, CapStartup)
	serverHello.Capabilities = append(serverHello.Capabilities, CapNotification)

	if !read {
		readYangModules()
	}

	capYangLib := "urn:ietf:params:netconf:capability:yang-library:1.0?module-set-id=" + *YangModules.ModuleSetId
	serverHello.Capabilities = append(serverHello.Capabilities, capYangLib)

	for _, module := range YangModules.Modules {
		supportedCap := *module.Namespace + "?module=" + *module.Name + "&revision=" + *module.Revision
		serverHello.Capabilities = append(serverHello.Capabilities, supportedCap)
	}

	output, _ := xml.Marshal(serverHello)

	return string(output) + delimeter
}

func readCapabilities(clientCaps string) error {
	caps, err := parseClientHelloCapabilities(clientCaps)
	if err != nil {
		return err
	}

	if !hasCapability(caps, CapNetconf11) && !hasCapability(caps, CapNetconf10) {
		return errors.New("client hello missing compatible NETCONF base capability")
	}

	return nil
}

func nextSessionID() uint64 {
	return atomic.AddUint64(&sessionID, 1)
}

func parseClientHelloCapabilities(clientCaps string) ([]string, error) {
	mainNode, err := xmlquery.Parse(strings.NewReader(clientCaps))
	if err != nil {
		return nil, err
	}

	helloNode := xmlquery.FindOne(mainNode, "/*[local-name() = 'hello']")
	if helloNode == nil {
		return nil, errors.New("invalid client hello")
	}

	if xmlquery.FindOne(helloNode, "./*[local-name() = 'session-id']") != nil {
		return nil, errors.New("client hello must not include session-id")
	}

	capNodes := xmlquery.Find(helloNode, "./*[local-name() = 'capabilities']/*[local-name() = 'capability']/text()")
	if len(capNodes) == 0 {
		return nil, errors.New("client hello missing capabilities")
	}

	caps := make([]string, 0, len(capNodes))
	for _, capNode := range capNodes {
		capability := strings.TrimSpace(capNode.Data)
		if capability == "" {
			continue
		}
		caps = append(caps, capability)
	}
	if len(caps) == 0 {
		return nil, errors.New("client hello missing capabilities")
	}

	return caps, nil
}

func hasCapability(caps []string, capability string) bool {
	for _, cap := range caps {
		if cap == capability {
			return true
		}
	}
	return false
}

func process(session ssh.Session, requestStr string) string {

	defer doRecover(session, requestStr)

	rpcNode, err := xmlquery.Parse(strings.NewReader(requestStr))

	if err != nil {
		return createErrorResponse(extractMessageId(requestStr), errors.New("Malformed XML"))
	}

	rootNode := xmlquery.FindOne(rpcNode, "*")

	if rootNode == nil {
		return createErrorResponse(extractMessageId(requestStr), errors.New("Malformed XML"))
	}

	messageId := rootNode.SelectAttr("message-id")

	if messageId == "" {
		return createErrorResponse(extractMessageId(requestStr), errors.New("Unable to read message-id in rpc"))
	}

	response, err := handleRequest(session, rootNode)

	if err != nil {
		if rpcErrorResponse, ok := isRPCErrorResponse(err); ok {
			return rpcErrorResponse
		}
		return createErrorResponse(messageId, err)
	}

	if response == notificationHandledSentinel {
		return notificationHandledSentinel
	}

	return CreateResponse(messageId, []byte(response))
}

func handleRequest(session ssh.Session, requestNode *xmlquery.Node) (string, error) {

	var response string
	var err error

	typeNode := xmlquery.FindOne(requestNode, "//*[local-name() = 'rpc']/*") // Get request type (get or edit-config)
	if typeNode == nil {
		return "", errors.New("Unsupported command")
	}

	context := session.Context().(ssh.Context)
	if sub, ok := context.Value(ctxKeySubscription).(*subscriptionState); ok && sub.isActive() {
		switch typeNode.Data {
		case "close-session", "kill-session":
		case "create-subscription":
			return "", buildRPCError(requestNode.SelectAttr("message-id"), "protocol", "operation-failed", "error", "")
		default:
			return "", buildRPCError(requestNode.SelectAttr("message-id"), "protocol", "resource-denied", "error", typeNode.Data)
		}
	}

	switch typeNode.Data {
	case "get":
		response, err = GetRequestHandler(context, requestNode)
	case "get-config":
		response, err = GetRequestHandler(context, requestNode)
	case "edit-config":
		response, err = EditRequestHandler(context, requestNode)
	case "get-schema":
		response, err = withAuth(context, "get-schema", func() (string, error) { return FilterSchemaHandler(requestNode) })
	case "commit":
		response, err = commitRequestHandler(context, requestNode)
	case "close-session", "kill-session":
		return withAuth(context, "close-session", func() (string, error) {
			cancelActiveSubscription(session)
			if context.Value("auth-type").(string) == "tacacs" {
				tacConn := context.Value("auth").(lib.TacacsAuthenticator)
				glog.Infof("[TACPLUS] Closing tacacs server connection")
				tacConn.Disconnect()
				context.SetValue("auth", nil) // completely remove tacacs connection reference
				time.AfterFunc(1*time.Second, func() { session.Close() })
			}
			return "ok", nil
		})
	case "lock":
		response, err = lockRequestHandler(context, requestNode)
	case "unlock":
		response, err = unlockRequestHandler(context, requestNode)
	case "sonic-rpc":
		response, err = RpcRequestHandler(context, requestNode)
	case "copy-config":
		response, err = copyConfigRequestHandler(context, requestNode)
	case "create-subscription":
		err = createSubscriptionHandler(session, requestNode)
		if err == nil {
			response = notificationHandledSentinel
		}
	default:
		return "", errors.New("Unsupported command")
	}

	if err != nil {
		return "", err
	}

	return response, nil
}

func CreateResponseFromNode(request *xmlquery.Node, responsePayload []byte) string {
	messageId := request.SelectAttr("message-id")
	return CreateResponse(messageId, responsePayload)
}

func CreateResponse(messageId string, responsePayload []byte) string {
	reply := string(responsePayload)
	switch reply {
	case "{}":
		reply = `<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="` + messageId + `"></rpc-reply>`
	case "ok":
		reply = `<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="` + messageId + `"><ok/></rpc-reply>`
	default:
		reply = `<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="` + messageId + `">` + reply + "</rpc-reply>"
		reply = strings.ReplaceAll(reply, "&amp;", "&")
	}
	return declaration + reply
}

func writeResponse(session ssh.Session, message string) {
	responseString := fmt.Sprintf(ChunkedMessage, len(message), message)
	mu := setSessionWriteMu(session)
	mu.Lock()
	defer mu.Unlock()
	session.Write([]byte(responseString))
}

func writeOkResponse(session ssh.Session, id string) {
	writeResponse(session, CreateResponse(id, []byte("ok")))
}

func createErrorXML(err error) string {
	return fmt.Sprintf("<rpc-error><error-type>rpc</error-type><error-severity>error</error-severity><error-message xml:lang=\"en\">%s</error-message></rpc-error>", err.Error())
}

func createErrorResponse(messageId string, err error) string {
	return CreateResponse(messageId, []byte(createErrorXML(err)))
}

func DefaultHandler(s ssh.Session) {
	fmt.Println("Default ssh is disabled, closing connection")
	s.Close()
}

func SplitAt(data []byte, atEOF bool) (advance int, token []byte, err error) {

	if atEOF && len(data) == 0 || len(trimInput(string(data))) == 0 {
		return 0, nil, nil
	}

	if isChunkedFraming(data) {
		advance, token, err, needMore := parseChunkedToken(data, atEOF)
		if err != nil {
			return 0, nil, err
		}
		if needMore {
			return 0, nil, nil
		}
		return advance, token, nil
	}

	// Find the index of the input of the separator substring
	if i := strings.Index(string(data), RPCDelimiter); i >= 0 {
		return i + len(RPCDelimiter), data[0:i], nil
	}

	if i := strings.Index(string(data), ChunkDelimiter); i >= 0 {
		return i + len(ChunkDelimiter), data[0:i], nil
	}

	// If at end of file with data return the data
	if atEOF {
		return len(data), data, nil
	}

	return 0, nil, nil
}

func isChunkedFraming(data []byte) bool {
	i := 0
	for i < len(data) && (data[i] == '\n' || data[i] == '\r') {
		i++
	}
	return i < len(data) && data[i] == '#'
}

func parseChunkedToken(data []byte, atEOF bool) (advance int, token []byte, err error, needMore bool) {
	i := 0
	for i < len(data) && (data[i] == '\n' || data[i] == '\r') {
		i++
	}

	payload := make([]byte, 0, len(data))
	for {
		if i >= len(data) {
			if atEOF {
				return 0, nil, errors.New("incomplete NETCONF 1.1 chunked message"), false
			}
			return 0, nil, nil, true
		}
		if data[i] != '#' {
			return 0, nil, errors.New("invalid NETCONF 1.1 chunk framing"), false
		}

		// End marker: ##\n
		if i+1 < len(data) && data[i+1] == '#' {
			i += 2
			if i < len(data) && data[i] == '\r' {
				i++
			}
			if i >= len(data) {
				if atEOF {
					return i, payload, nil, false
				}
				return 0, nil, nil, true
			}
			if data[i] != '\n' {
				return 0, nil, errors.New("invalid NETCONF 1.1 chunk end"), false
			}
			i++
			return i, payload, nil, false
		}

		// Chunk header: #<size>\n
		sizeStart := i + 1
		sizeEnd := sizeStart
		for sizeEnd < len(data) && data[sizeEnd] >= '0' && data[sizeEnd] <= '9' {
			sizeEnd++
		}
		if sizeEnd == sizeStart {
			return 0, nil, errors.New("invalid NETCONF 1.1 chunk size"), false
		}
		if sizeEnd >= len(data) {
			if atEOF {
				return 0, nil, errors.New("incomplete NETCONF 1.1 chunk header"), false
			}
			return 0, nil, nil, true
		}

		lineEnd := sizeEnd
		if data[lineEnd] == '\r' {
			lineEnd++
			if lineEnd >= len(data) {
				if atEOF {
					return 0, nil, errors.New("incomplete NETCONF 1.1 chunk header"), false
				}
				return 0, nil, nil, true
			}
		}
		if data[lineEnd] != '\n' {
			return 0, nil, errors.New("invalid NETCONF 1.1 chunk header"), false
		}

		chunkSize, convErr := strconv.Atoi(string(data[sizeStart:sizeEnd]))
		if convErr != nil || chunkSize < 0 {
			return 0, nil, errors.New("invalid NETCONF 1.1 chunk size"), false
		}

		chunkStart := lineEnd + 1
		chunkEnd := chunkStart + chunkSize
		if chunkEnd > len(data) {
			if atEOF {
				return 0, nil, errors.New("incomplete NETCONF 1.1 chunk payload"), false
			}
			return 0, nil, nil, true
		}
		payload = append(payload, data[chunkStart:chunkEnd]...)

		i = chunkEnd
		if i < len(data) && data[i] == '\r' {
			i++
		}
		if i >= len(data) {
			if atEOF {
				return 0, nil, errors.New("incomplete NETCONF 1.1 chunk payload terminator"), false
			}
			return 0, nil, nil, true
		}
		if data[i] != '\n' {
			return 0, nil, errors.New("invalid NETCONF 1.1 chunk payload terminator"), false
		}
		i++
	}
}
func trimInput(input string) string {
	trimmed := strings.Trim(string(input), "\n")
	trimmed = strings.Trim(string(trimmed), "\r")
	trimmed = strings.Trim(string(trimmed), " ")
	return trimmed
}

func doRecover(session ssh.Session, inputStr string) {
	if err := recover(); err != nil {

		buf := make([]byte, 64<<10)
		buf = buf[:runtime.Stack(buf, false)]

		glog.Errorf("Runtime error: panic serving NETCONF request", inputStr)
		glog.Errorf("Panic data: %v\n%s", err, buf)

		errorXML := createErrorXML(errors.New("Unable to handle request"))
		writeResponse(session, CreateResponse(extractMessageId(inputStr), []byte(errorXML)))
	}
}

func extractMessageId(xmlStr string) string {
	r := regexp.MustCompile("message-id=\"(\\S+)\"")
	matches := r.FindStringSubmatch(xmlStr)
	if len(matches) == 0 {
		return "1"
	}
	return matches[1]
}

func withAuth(context ssh.Context, command string, fn func() (string, error)) (string, error) {

	authenticator := context.Value("auth").(lib.Authenticator)

	if !authenticator.Authorize(command, "") {
		return "", errors.New(fmt.Sprintf("Unauthorized access %s", command))
	}

	glog.Infof("[TACPLUS] authorization passed %s", command)

	response, err := fn()

	if err != nil {
		return response, err
	}

	if !authenticator.Account(command, "") {
		return "", errors.New(fmt.Sprintf("Accounting failed cmd:%s", command))
	}

	glog.Infof("[TACPLUS] accounting passed - %s", command)

	return response, nil
}

func setSessionWriteMu(session ssh.Session) *sync.Mutex {
	context := session.Context().(ssh.Context)
	if mu, ok := context.Value(ctxKeyWriteMu).(*sync.Mutex); ok {
		return mu
	}

	mu := newSessionWriteMu()
	context.SetValue(ctxKeyWriteMu, mu)
	return mu
}

func cancelActiveSubscription(session ssh.Session) {
	context := session.Context().(ssh.Context)
	sub, ok := context.Value(ctxKeySubscription).(*subscriptionState)
	if !ok || !sub.deactivate() {
		return
	}

	if sub.cancelChan != nil {
		safeClose(sub.cancelChan)
	}
	if sub.stopTrans != nil {
		safeClose(sub.stopTrans)
	}
}
