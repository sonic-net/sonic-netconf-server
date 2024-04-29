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

package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"io/ioutil"
	"os"
	"strconv"

	"orange/sonic-netconf-server/netconf/server"

	gliderssh "github.com/gliderlabs/ssh"
	"github.com/golang/glog"
	cryptossh "golang.org/x/crypto/ssh"

	"github.com/google/uuid"
)

// Command line parameters
var (
	port             int    // Server port
	clientAuth       string // Client auth mode
	publicKeyPath    = "/etc/sonic/netconf-key.pub"
	privateKeyPath   = "/etc/sonic/netconf-key"
)

func init() {
	// Parse command line
	flag.IntVar(&port, "port", 830, "Listen port")
	// flag.StringVar(&clientAuth, "client_auth", "none", "Client auth mode - none|user")
	flag.Parse()
	// Suppress warning messages related to logging before flag parse
	flag.CommandLine.Parse([]string{})
}

func main() {

	MakeSSHKeyPair(publicKeyPath, privateKeyPath)

	srv := &gliderssh.Server{Addr: ":" + strconv.Itoa(port), Handler: server.DefaultHandler}

	srv.SubsystemHandlers = map[string]gliderssh.SubsystemHandler{}

	srv.SetOption(gliderssh.HostKeyFile(privateKeyPath))
	srv.SetOption(gliderssh.NoPty())
	srv.SetOption(gliderssh.PasswordAuth(authenticate))

	srv.SubsystemHandlers["netconf"] = server.SessionHandler
	srv.ListenAndServe()
}

func authenticate(ctx gliderssh.Context, password string) bool {

	pamAuthenticator := server.NewPAMAuthenticator(ctx.User(), password)

	if !pamAuthenticator.Authenticate() {
		glog.Errorf("[PAM] Authentication failed user:(%s)", ctx.User())
		return false
	}

	ctx.SetValue("auth-type", "local")

	ctx.SetValue("auth", pamAuthenticator)

	ctx.SetValue("uuid", uuid.New().String())

	glog.Infof("Authentication success user:(%s)", ctx.User())

	return true
}

func MakeSSHKeyPair(pubKeyPath, privateKeyPath string) error {

	if fileExists(publicKeyPath) && fileExists(privateKeyPath) {
		glog.Info("SSH key generation skipped, files exists")
		return nil
	}

	glog.Info("SSH keys not found, generating server keys")

	privateKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		return err
	}

	// generate and write private key as PEM
	privateKeyFile, err := os.Create(privateKeyPath)
	defer privateKeyFile.Close()
	if err != nil {
		return err
	}
	privateKeyPEM := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)}
	if err := pem.Encode(privateKeyFile, privateKeyPEM); err != nil {
		return err
	}

	// generate and write public key
	pub, err := cryptossh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(pubKeyPath, cryptossh.MarshalAuthorizedKey(pub), 0655)
}

func fileExists(path string) bool {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return false
	}
	return true
}