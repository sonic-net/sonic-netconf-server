package server

import (
	"github.com/golang/glog"
	"golang.org/x/crypto/ssh"
)

type Authenticator interface {
	Authenticate() bool
	Authorize(cmd string, cmdArgs string) bool
	Account(cmd string, cmdArgs string) bool
}


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