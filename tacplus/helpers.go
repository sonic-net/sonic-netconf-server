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

package tacplus

import (
	"container/heap"
	"context"
	"errors"
	"math/rand"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v7"
	"github.com/golang/glog"
)

type TacacsInfo struct {
	IP       string
	Port     int
	Priority int
	Password string
	Timeout  int
	AuthType string
	index    int
}

//Single redis client for tacacs communication
var redisClient *redis.Client

func init() {
	redisClient = redis.NewClient(&redis.Options{
		Network:  "unix",
		Addr:     "/var/run/redis/redis.sock",
		Password: "",
		DB:       4,
	})
}

func IsTacacsAAAEnabled() bool {
	aaaAuth, err := redisClient.HGet("AAA|authentication", "login").Result()

	if err != nil || len(aaaAuth) == 0 {
		glog.Info("[AAA] No AAA Authentication login data found")
		return false
	}

	if strings.Contains(aaaAuth, "tacacs+") {
		glog.Info("[AAA] TACACS+ found in AAA login info")
		return true
	}

	glog.Info("[AAA] TACACS+ not found in AAA login info")
	return false
}

func IsTacacsEnabled() bool {
	tacKeys, err := redisClient.Keys("TACPLUS_SERVER|*").Result()
	if err != nil {
		glog.Infof("isTacacsEnabled - Can't read tacacs info")
		return false
	}
	return len(tacKeys) != 0
}

// Returns an array with Tacacs servers on the switch, the array is sorted in ascending order by priority
func GetTacacsInfo() (PriorityQueue, error) {

	globalTacacsTimeout := 5
	globalTacacsPass := ""
	globalTacacsAuthType := "pap"

	tacacsGlobal, err := redisClient.HGetAll("TACPLUS|global").Result()

	if err == nil {

		if pass, ok := tacacsGlobal["passkey"]; ok {

			if encrypted, ok := tacacsGlobal["is_key_encrypted"]; ok && encrypted == "True" {

				glog.Info("Tacacs key is encrypted, decrypting ...")

				decrypted, err := exec.Command("bash", "-c" , "echo " + pass + " | " + "openssl enc -d -aes-256-cbc -base64 -pbkdf2 -pass file:/etc/sonic/enc_master_key").Output()

				if err != nil {
					glog.Infof("Unable execute decrypt openssl command")
					return nil, errors.New("Cannot get tacacs server info")
				}

				glog.Infof("Decryption success")

				globalTacacsPass = string(decrypted)

			}else{
				globalTacacsPass = pass
			}

		}

		if authType, ok := tacacsGlobal["auth_type"]; ok {
			globalTacacsAuthType = authType
		}

		if timeout, ok := tacacsGlobal["timeout"]; ok {
			globalTacacsTimeout, _ = strconv.Atoi(timeout)
		}
	}

	tacKeys, err := redisClient.Keys("TACPLUS_SERVER|*").Result()

	if err != nil || len(tacKeys) == 0 {
		return nil, errors.New("Cannot find any valid tacplus server configuration")
	}

	pq := make(PriorityQueue, len(tacKeys))

	for i, key := range tacKeys {

		serverData, err := redisClient.HGetAll(key).Result()

		if err != nil {
			return nil, errors.New("Unable to read server information")
		}

		tacIp := strings.Split(key, "|")[1]
		tacPort, _ := strconv.Atoi(serverData["tcp_port"])
		tacPriority, _ := strconv.Atoi(serverData["priority"])
		tacTimeout := globalTacacsTimeout
		tacPassword := globalTacacsPass
		tacAuthType := globalTacacsAuthType

		if timeout, ok := serverData["timeout"]; ok {
			tacTimeout, _ = strconv.Atoi(timeout)
		}

		if pass, ok := serverData["passkey"]; ok {
			if encrypted, ok := serverData["is_key_encrypted"]; ok && encrypted == "True" {

				glog.Info("Tacacs key is encrypted, decrypting ...")

				decrypted, err := exec.Command("bash", "-c" , "echo " + pass + " | " + "openssl enc -d -aes-256-cbc -base64 -pbkdf2 -pass file:/etc/sonic/enc_master_key").Output()

				if err != nil {
					glog.Infof("Unable execute decrypt openssl command")
					return nil, errors.New("Cannot get tacacs server info")
				}

				glog.Infof("Decryption success")

				tacPassword = string(decrypted)

			}else{
				tacPassword = pass
			}
		}

		if authType, ok := serverData["auth_type"]; ok {
			tacAuthType = authType
		}

		if tacAuthType == "login" {
			tacAuthType = "ascii"
		}

		pq[i] = &TacacsInfo{
			IP:       tacIp,
			Port:     tacPort,
			Priority: tacPriority,
			Timeout:  tacTimeout,
			Password: tacPassword,
			AuthType: tacAuthType,
		}
	}

	// Sort by priority
	heap.Init(&pq)

	return pq, nil
}

func GenerateRandomInt() int {
	rand.Seed(time.Now().UnixNano())
	return rand.Intn(1000000-1000+1) + 1000 // Generate random number between 1000 - 1000000
}

func GetLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, address := range addrs {
		// check the address type and if it is not a loopback the display it
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}

// Creates a connection from a specific configuration
func CreateClientFromInfo(info *TacacsInfo) *Client {
	return &Client{
		Addr: info.IP + ":" + strconv.Itoa(info.Port),
		ConnConfig: ConnConfig{
			Secret: []byte(info.Password),
			Mux:    false,
		},
	}
}

/*Creates a client by reading the conifg db for tacacs configurations
Returns a reference to a newly created client with the highest priority and an established connection
*/
func CreateClient(context context.Context) (*Client, TacacsInfo, error) {

	queue, err := GetTacacsInfo()

	if err != nil {
		return nil, TacacsInfo{}, errors.New("No tacacs configuration found")
	}

	for queue.Len() > 0 {

		info := heap.Pop(&queue).(*TacacsInfo)

		client := CreateClientFromInfo(info)

		glog.Infof("Found server (%s) in db, testing connection", info.IP)

		connected := client.TestConnection(context)

		if connected {
			glog.Infof("Successful connection to server (%s)", info.IP)
			return client, *info, nil
		}

		glog.Warningf("Unable to connect to server (%s) - skipping", info.IP)

	}

	return nil, TacacsInfo{}, errors.New("Unable to connect to any TACACS servers")
}
