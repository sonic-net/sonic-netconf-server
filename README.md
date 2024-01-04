## SONiC NETCONF Server

### Build Instruction
Please note that the build instruction in this guide has only been tested on Ubuntu 18.04.
#### Pre-rerequisites
##### User permissions:
	`sudo usermod -aG sudo $USER`
	`sudo usermod -aG docker $USER`

##### Packages to be installed:
	`sudo apt-get install git docker`

#### Steps to build and create an installer
1. git clone https://github.com/sonic-net/sonic-buildimage.git
2. cd sonic-buildimage/
3. sudo modprobe overlay
4. make init
5. make configure PLATFORM=broadcom
6. To build sonic-netconf-server container:   
	`make target/docker-sonic-netconf-server.gz`
7. To build the ONIE installer:   
	`make target/sonic-broadcom.bin`
 


##### Incremental builds 
Just clean up the deb's/gz that require re-build, and build again. Here is an exmple:

##### To build deb file for sonic-netconf-server

	make target/debs/stretch/sonic-netconf-server_1.0-01_amd64.deb-clean
	make target/debs/stretch/sonic-netconf-server_1.0-01_amd64.deb
	
##### To build sonic-netconf-server docker alone

	make target/docker-sonic-netconf-server.gz-clean
	make target/docker-sonic-netconf-server.gz
