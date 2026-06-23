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

	"orange/sonic-netconf-server/lib"
	"orange/sonic-netconf-server/netconf/server"
	"orange/sonic-netconf-server/tacplus"

	gliderssh "github.com/gliderlabs/ssh"
	"github.com/go-redis/redis/v7"
	"github.com/golang/glog"
	cryptossh "golang.org/x/crypto/ssh"

	"github.com/google/uuid"
)

// Command line parameters
var (
	port             int    // Server port
	clientAuth       string // Client auth mode
	redisClient      *redis.Client
	tacplusConfigKey = "TACACS|NETCONF"
	publicKeyPath    = "/etc/sonic/netconf-key.pub"
	privateKeyPath   = "/etc/sonic/netconf-key"
)

func init() {
	// Parse command line
	flag.IntVar(&port, "port", 830, "Listen port")
	flag.StringVar(&clientAuth, "client_auth", "none", "Client auth mode - none|cert|user|tacacs")
	flag.Parse()
	// Suppress warning messages related to logging before flag parse
	flag.CommandLine.Parse([]string{})

	redisClient = redis.NewClient(&redis.Options{
		Network:  "unix",
		Addr:     "/var/run/redis/redis.sock",
		Password: "",
		DB:       4,
	})
}

func main() {

	MakeSSHKeyPair(publicKeyPath, privateKeyPath)

	srv := &gliderssh.Server{Addr: ":" + strconv.Itoa(port), Handler: server.DefaultHandler}

	srv.SubsystemHandlers = map[string]gliderssh.SubsystemHandler{}

	srv.SetOption(gliderssh.HostKeyFile(privateKeyPath))
	srv.SetOption(gliderssh.NoPty())
	srv.SetOption(gliderssh.PasswordAuth(authenticate))

	srv.SubsystemHandlers["netconf"] = server.SessionHandler

	glog.Infof("Server start on port %+v", port)
	srv.ListenAndServe()
}

func authenticate(ctx gliderssh.Context, password string) bool {

	if tacplus.IsTacacsAAAEnabled() {

		glog.Infof("TACACS enabled on AAA, creating a connection to tacacs server")

		protocol, service, err := GetNetconfTacacsConfig()
		if err != nil {
			return false
		}

		glog.Infof("[TACPLUS] protocol: %s - service: %s", protocol, service)

		tacAuthenticator, err := lib.NewTacacsAuthenticator(ctx, protocol, service, ctx.User(), password, ctx.RemoteAddr().String())

		if err != nil {
			return false
		}

		glog.Infof("Starting authentication")

		if !tacAuthenticator.Authenticate() {
			glog.Errorf("[TACPLUS] Authentication failed user:(%s)", ctx.User())
			tacAuthenticator.Disconnect()
			return false
		}

		ctx.SetValue("auth-type", "tacacs")
		ctx.SetValue("auth", tacAuthenticator)
	} else {

		// No tacacs, authenticate with local credentials
		glog.Infof("TACACS not enabled on AAA, authenticating with local credentials")

		pamAuthenticator := lib.NewPAMAuthenticator(ctx.User(), password)

		if !pamAuthenticator.Authenticate() {
			glog.Errorf("[PAM] Authentication failed user:(%s)", ctx.User())
			return false
		}

		ctx.SetValue("auth-type", "local")
		ctx.SetValue("auth", pamAuthenticator)
	}

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

func GetNetconfTacacsConfig() (string, string, error) {

	service := "netconf"
	protocol := "ssh"

	protocolConfigExists, err := redisClient.HExists("TACACS|NETCONF", "protocol").Result()
	if err != nil {
		return "", "", err
	}

	if protocolConfigExists {
		protocol, err = redisClient.HGet("TACACS|NETCONF", "protocol").Result()
		if err != nil {
			return "", "", err
		}
		glog.Infof("Custom TACPLUS protocol: %s", protocol)
	}

	serviceConfigExists, err := redisClient.HExists("TACACS|NETCONF", "service").Result()
	if err != nil {
		return "", "", err
	}

	if serviceConfigExists {
		service, err = redisClient.HGet("TACACS|NETCONF", "service").Result()
		if err != nil {
			return "", "", err
		}
		glog.Infof("Custom TACPLUS service: %s", service)
	}

	return protocol, service, nil
}
