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
	"bufio"
	"encoding/xml"
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/antchfx/xmlquery"
	"github.com/gliderlabs/ssh"
	"github.com/golang/glog"
)

var sessionID = 0

const (
	delimeter   = "]]>]]>"
	declaration = "<?xml version=\"1.0\" encoding=\"utf-8\"?>"
)

type SessionRequest struct{
	xml string
	authenticator Authenticator
	session ssh.Session
}

func SessionHandler(s ssh.Session) {

	scanner := bufio.NewScanner(s)
	scanner.Split(SplitAt)

	// Send server capablities
	capabilities := string(capabilitesXML())
	s.Write([]byte(capabilities + delimeter))

	// Read client capablities
	scanner.Scan()
	err := readCapabilities(scanner.Text())
	if err != nil {
		writeResponse(s, createErrorResponse("1", err))
		s.Close()
	}

	glog.Info("Capabilities exchange success, starting main loop")

	for scanner.Scan() {
		requestStr := scanner.Text()
		glog.Infof("\nReceving request <<< %s >>> \n %s \n\n", time.Now().Local().String(), requestStr)
		request := SessionRequest{
			xml : requestStr,
			authenticator: s.Context().Value("auth").(Authenticator),
			session: s,
		}
		response := process(request)
		glog.Infof("\nSending response <<< %s >>> \n %s \n\n", time.Now().Local().String(), response)
		writeResponse(s, response)
	}
}

func capabilitesXML() []byte {

	var serverHello Hello

	sessionID += 1 // TODO: handle session id out of bounds
	serverHello.SessionID = sessionID
	serverHello.Capabilities = append(serverHello.Capabilities, CapNetconf10)
	serverHello.Capabilities = append(serverHello.Capabilities, CapNetconf11)

	serverHello.Capabilities = append(serverHello.Capabilities, CapWritableRunning)
	serverHello.Capabilities = append(serverHello.Capabilities, CapXPath)
	serverHello.Capabilities = append(serverHello.Capabilities, CapMonitoring)
	serverHello.Capabilities = append(serverHello.Capabilities, CapStartup)

	if !yangModulesInit {
		readYangModules()
	}

	capYangLib := "urn:ietf:params:netconf:capability:yang-library:1.0?module-set-id=" + *YangModules.ModuleSetId
	serverHello.Capabilities = append(serverHello.Capabilities, capYangLib)

	for _, module := range YangModules.Modules {
		supportedCap := *module.Namespace + "?module=" + *module.Name + "&revision=" + *module.Revision
		serverHello.Capabilities = append(serverHello.Capabilities, supportedCap)
	}

	output, _ := xml.Marshal(serverHello)

	return output
}

func readCapabilities(clientCaps string) error {

	// TODO: Handle client caps

	mainNode, err := xmlquery.Parse(strings.NewReader(clientCaps))

	if err != nil {
		return err
	}

	// Check for the hello node for now, need to add further capabities parsing
	helloNode := xmlquery.FindOne(mainNode, "//*[local-name() = 'hello']/*")

	if helloNode == nil {
		return errors.New("Invalid client capablities, exiting")
	}

	return nil
}

func process(request SessionRequest) string {

	defer doRecover(request.session, request.xml)

	rpcNode, err := xmlquery.Parse(strings.NewReader(request.xml))

	if err != nil {
		return createErrorResponse(extractMessageId(request.xml), errors.New("[Malformed XML] Unable to parser request string"))
	}

	rootNode := xmlquery.FindOne(rpcNode, "*")

	if rootNode == nil {
		return createErrorResponse(extractMessageId(request.xml), errors.New("[Malformed XML] Root node not found"))
	}

	messageId := rootNode.SelectAttr("message-id")

	if messageId == "" {
		return createErrorResponse(extractMessageId(request.xml), errors.New("[Missing data] Unable to read message-id in rpc"))
	}

	response, err := handleRequest(request, rootNode)

	if err != nil {
		return createErrorResponse(messageId, err)
	}

	return CreateResponse(messageId, []byte(response))
}

func handleRequest(request SessionRequest, rpcXML *xmlquery.Node) (string, error) {

	var response string
	var err error

	typeNode := xmlquery.FindOne(rpcXML, "//*[local-name() = 'rpc']/*") // Get request type 

	switch typeNode.Data {
	case "get":
		response, err = GetRequestHandler(request.authenticator, rpcXML)
	case "get-schema":
		response, err = GetSchemaHandler(rpcXML)
	case "close-session":
		time.AfterFunc(1* time.Second, func() {request.session.Close()}) // probably a better way to do this ?
		return "ok", nil
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

		glog.Errorf("Runtime error: panic serving NETCONF request (%s)", inputStr)
		glog.Errorf("Panic data: %v \n\n %s \n\n //Trace end", err, buf)

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