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
	"github.com/golang/glog"
	"golang.org/x/crypto/ssh"
)

type PAMAuthenticator struct {
	username string
	password string
}

func NewPAMAuthenticator(username string, password string) PAMAuthenticator {
	return PAMAuthenticator{
		username: username,
		password: password,
	}
}

func (p PAMAuthenticator) Authenticate() bool {
	var rc struct{
		ID string
	}

	rc.ID = p.username

	glog.Infof("[%s] Received user=%s", rc.ID, p.username)

	//Use ssh for authentication.
	config := &ssh.ClientConfig{
		User: p.username,
		Auth: []ssh.AuthMethod{
			ssh.Password(p.password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	_, err := ssh.Dial("tcp", "127.0.0.1:22", config)
	if err != nil {
		glog.Infof("[%s] Failed to authenticate; %v", rc.ID, err)
		return false
	}

	glog.Infof("[%s] Authentication passed. user=%s ", rc.ID, p.username)
	return true
}

func (p PAMAuthenticator) Authorize(cmd string, cmdArgs string) bool {
	return true
}

func (p PAMAuthenticator) Account(cmd string, cmdArgs string) bool {
	return true
}