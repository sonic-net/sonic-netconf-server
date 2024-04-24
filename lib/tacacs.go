////////////////////////////////////////////////////////////////////////////////
//                                                                            //
//  Copyright 2024 Orange. The term Orange refers to Orange and/or 			  //
//  its affiliates.                                                           //
//                                                                            //
//  Licensed under the Apache License, Version 2.0 (the "License");           //
//  you may not use this file except in compliance with the License.          //
//  You may obtain a copy of the License at                                   //
//                                                                            //
//     http://www.apache.org/licenses/LICENSE-2.0                             //
//                                                                            //
//  Unless required by applicable law or agreed to in writing, software       //
//  distributed under the License is distributed on an "AS IS" BASIS,         //
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.  //
//  See the License for the specific language governing permissions and       //
//  limitations under the License.                                            //
//                                                                            //
////////////////////////////////////////////////////////////////////////////////

package lib

import (
	"context"
	"errors"
	"strconv"
	"time"

	"orange/sonic-netconf-server/tacplus"

	"github.com/golang/glog"
)

type TacacsAuthenticator struct {
	client        *tacplus.Client
	info          tacplus.TacacsInfo
	context       context.Context
	username      string
	password      string
	protocol      string
	service       string
	remoteAddress string
	authType      uint8
}

// Will create a connection with the highest priority tacacs server
func NewTacacsAuthenticator(context context.Context, protocol string, service string, username string, password string, remoteAddress string) (TacacsAuthenticator, error) {

	client, info, err := tacplus.CreateClient(context)

	if err != nil {
		return TacacsAuthenticator{}, err
	}

	var authenticatorInfo TacacsAuthenticator

	switch info.AuthType {
	case "ascii":
		authenticatorInfo.authType = tacplus.AuthenTypeASCII
	case "pap":
		authenticatorInfo.authType = tacplus.AuthenTypePAP
	case "chap":
		authenticatorInfo.authType = tacplus.AuthenTypeCHAP
	case "mschap":
		authenticatorInfo.authType = tacplus.AuthenTypeMSCHAP
	default:
		return TacacsAuthenticator{}, errors.New("Unkown authentication type")
	}

	authenticatorInfo.client = client
	authenticatorInfo.info = info
	authenticatorInfo.context = context
	authenticatorInfo.username = username
	authenticatorInfo.password = password
	authenticatorInfo.protocol = protocol
	authenticatorInfo.service = service
	authenticatorInfo.remoteAddress = remoteAddress

	return authenticatorInfo, nil
}

func (t TacacsAuthenticator) Authenticate() bool {

	authenReq := &tacplus.AuthenStart{
		Action:        tacplus.AuthenActionLogin,
		AuthenType:    t.authType,
		AuthenService: tacplus.AuthenServicePPP,
		PrivLvl:       0,
		Port:          t.protocol,
		User:          t.username,
		Data:          []byte(t.password),
		RemAddr:       t.remoteAddress,
	}

	authenRep, session, err := t.client.SendAuthenStart(t.context, authenReq)

	if err != nil {
		return false
	}

	if authenRep.Status == tacplus.AuthenStatusGetPass {
		// Get password request
		glog.Infof("Send password packet received, resending password with session: %+v", session)

		if session != nil {
			authenRep, err = session.Continue(t.context, t.password)
		}

		if err != nil {
			return false
		}

	}

	if authenRep.Status != tacplus.AuthenStatusPass{
		glog.Infof("Success packet not received, authentication failed")
		return false
	}

	
	// if err != nil || authenRep.Status != tacplus.AuthenStatusPass {
	// 	return false
	// }

	// glog.Infof("Session from authentication %+v", session)

	// authenRep, err = session.Continue(t.context, t.password)

	return true
}

func (t TacacsAuthenticator) Authorize(cmd string, cmdArgs string) bool {

	authorArgs := []string{}
	authorArgs = append(authorArgs, "protocol="+t.protocol)
	authorArgs = append(authorArgs, "service="+t.service)
	authorArgs = append(authorArgs, "timeout="+strconv.Itoa(t.info.Timeout))

	cmd = "cmd=" + cmd
	if len(cmd) >= 255 {
		cmd = cmd[:251] + "..."
	}

	cmdArgs = "cmd-arg=" + cmdArgs
	if len(cmdArgs) >= 255 {
		cmdArgs = cmdArgs[:251] + "..."
	}

	authorArgs = append(authorArgs, cmd)
	authorArgs = append(authorArgs, cmdArgs)

	authorReq := &tacplus.AuthorRequest{
		AuthenMethod:  tacplus.AuthenMethodTACACSPlus,
		PrivLvl:       0,
		AuthenType:    t.authType,
		AuthenService: tacplus.AuthenServicePPP,
		User:          t.username,
		Arg:           authorArgs,
		RemAddr:       t.remoteAddress,
	}

	authorReply, err := t.client.SendAuthorRequest(t.context, authorReq)

	if err != nil || authorReply.Status != tacplus.AuthorStatusPassAdd {
		return false
	}

	return true
}

func (t TacacsAuthenticator) Account(cmd string, cmdArgs string) bool {

	acctArgs := []string{}
	acctArgs = append(acctArgs, "start_time="+strconv.FormatInt(time.Now().Unix(), 10))
	acctArgs = append(acctArgs, "service="+t.service)

	cmd = "cmd=" + cmd
	if len(cmd) >= 255 {
		cmd = cmd[:254]
	}

	cmdArgs = "cmd-arg=" + cmdArgs
	if len(cmdArgs) >= 255 {
		cmdArgs = cmdArgs[:251] + "..."
	}

	acctArgs = append(acctArgs, cmd)
	acctArgs = append(acctArgs, cmdArgs)

	acctReq := &tacplus.AcctRequest{
		Flags:         tacplus.AcctFlagStart,
		AuthenMethod:  tacplus.AuthenMethodTACACSPlus,
		PrivLvl:       0,
		AuthenType:    t.authType,
		AuthenService: tacplus.AuthenServicePPP,
		User:          t.username,
		Arg:           acctArgs,
		RemAddr:       t.remoteAddress,
	}

	acctRep, err := t.client.SendAcctRequest(t.context, acctReq)

	if err != nil || acctRep.Status != tacplus.AcctStatusSuccess {
		return false
	}

	return true
}

func (t TacacsAuthenticator) Disconnect() {
	t.client.Close()
}
