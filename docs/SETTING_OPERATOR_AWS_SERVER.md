#### Setting AWS server for operator ![ethereum](/docs/resources/blox_logo.png)
##### Create a server of your choice and expose on ports 12000 UDP and 13000 TCP
 - In the search bar search for "ec2"
 - Launch new instance
 - Choose "ubuntu server 20.04"
 - Choose "t2.micro" (free tire)
 - Skip to "security group" section
 - make sure you have 3 rules. UDP, TCP and SSH -
 ![security_permission](/docs/resources/security_permission.png)
 - after launch, add new key pair and download the ssh file 
 - launch instance

##### SHH permissions and login to server-  
```
$ cd ./{path to where the ssh downloaded}

$ chmod 400 {ssh file name}

$ ssh -i {ssh file name} ubuntu@{server public ip}
```

##### Download and run install.sh script 
```
$ sudo su

$ wget https://raw.githubusercontent.com/ethereum/eth2-ssv/stage/install.sh

$ chmod +x install.sh

$ ./install.sh
```