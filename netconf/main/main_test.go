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
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"testing"
)

func init(){
	fmt.Println("+++++ init main_test +++++")	
}

func TestMain(t *testing.T) {
	go main()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGUSR1)
	fmt.Println("Listening on sig kill from TestMain")
	<-sigs
	fmt.Println("Returning from TestMain on sig kill")
}
