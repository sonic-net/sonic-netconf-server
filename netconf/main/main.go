package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	"orange/sonic-netconf-server/lib"
	"orange/sonic-netconf-server/netconf/server"

	gliderssh "github.com/gliderlabs/ssh"
	"github.com/go-redis/redis/v7"
	"github.com/golang/glog"
	cryptossh "golang.org/x/crypto/ssh"

	"github.com/google/uuid"
)

// Command line parameters
const (
	netconfKeyDir              = "/etc/sonic/netconf"
	defaultHostKeyAlgorithm    = "ed25519"
	defaultHostKeyRSABits      = 3072
	minimumHostKeyRSABits      = 2048
	hostKeyAlgorithmED25519    = "ed25519"
	hostKeyAlgorithmRSA        = "rsa"
	hostKeyPrivateFileMode     = 0600
	hostKeyPublicFileMode      = 0644
	hostKeyDirectoryFileMode   = 0700
	hostKeyRSAPrivatePEMType   = "RSA PRIVATE KEY"
	hostKeyPKCS8PrivatePEMType = "PRIVATE KEY"
)

var (
	port             int    // Server port
	clientAuth       string // Client auth mode
	hostKeyAlgorithm string
	hostKeyRSABits   int
	redisClient      *redis.Client
	tacplusConfigKey = "TACACS|NETCONF"
	publicKeyPath    = netconfKeyDir + "/netconf-key.pub"
	privateKeyPath   = netconfKeyDir + "/netconf-key"
)

func init() {
	// Parse command line
	flag.IntVar(&port, "port", 830, "Listen port")
	flag.StringVar(&clientAuth, "client_auth", "none", "Client auth mode - none|cert|user|tacacs")
	flag.StringVar(&hostKeyAlgorithm, "host_key_algorithm", defaultHostKeyAlgorithm, "SSH host key algorithm - ed25519|rsa")
	flag.IntVar(&hostKeyRSABits, "host_key_rsa_bits", defaultHostKeyRSABits, "RSA host key size in bits when host_key_algorithm=rsa")
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

	if err := MakeSSHKeyPair(publicKeyPath, privateKeyPath); err != nil {
		glog.Fatalf("Failed to generate SSH key pair: %v", err)
	}

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

	pamAuthenticator := lib.NewPAMAuthenticator(ctx.User(), password)

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

	if err := os.MkdirAll(netconfKeyDir, hostKeyDirectoryFileMode); err != nil {
		return err
	}

	privateKeyPEM, pub, err := generateHostKey(hostKeyAlgorithm, hostKeyRSABits)
	if err != nil {
		return err
	}

	// generate and write private key as PEM
	privateKeyFile, err := os.OpenFile(privateKeyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, hostKeyPrivateFileMode)
	if err != nil {
		return err
	}
	defer privateKeyFile.Close()

	if err := pem.Encode(privateKeyFile, privateKeyPEM); err != nil {
		return err
	}

	return ioutil.WriteFile(pubKeyPath, cryptossh.MarshalAuthorizedKey(pub), hostKeyPublicFileMode)
}

func generateHostKey(algorithm string, rsaBits int) (*pem.Block, cryptossh.PublicKey, error) {
	switch strings.ToLower(strings.TrimSpace(algorithm)) {
	case hostKeyAlgorithmED25519:
		publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, nil, err
		}

		privateKeyBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
		if err != nil {
			return nil, nil, err
		}

		pub, err := cryptossh.NewPublicKey(publicKey)
		if err != nil {
			return nil, nil, err
		}

		return &pem.Block{Type: hostKeyPKCS8PrivatePEMType, Bytes: privateKeyBytes}, pub, nil
	case hostKeyAlgorithmRSA:
		if rsaBits < minimumHostKeyRSABits {
			return nil, nil, fmt.Errorf("RSA host key size must be at least %d bits", minimumHostKeyRSABits)
		}

		privateKey, err := rsa.GenerateKey(rand.Reader, rsaBits)
		if err != nil {
			return nil, nil, err
		}

		pub, err := cryptossh.NewPublicKey(&privateKey.PublicKey)
		if err != nil {
			return nil, nil, err
		}

		return &pem.Block{Type: hostKeyRSAPrivatePEMType, Bytes: x509.MarshalPKCS1PrivateKey(privateKey)}, pub, nil
	default:
		return nil, nil, fmt.Errorf("unsupported host key algorithm %q", algorithm)
	}
}

func fileExists(path string) bool {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return false
	}
	return true
}
