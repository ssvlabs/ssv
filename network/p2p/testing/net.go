package testing

import (
	"fmt"
	"math/rand"
	"net"
	"time"
)

// RandomTCPPort returns a new random tcp port
func RandomTCPPort(from, to int) int {
	for {
		port := random(from, to)
		if checkTCPPort(port) == nil {
			// port is taken
			continue
		}
		return port
	}
}

// checkTCPPort checks that the given port is not taken
func checkTCPPort(port int) error {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf(":%d", port), 3*time.Second)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

// UDPPortsRandomizer helps to generate random, available udp ports
type UDPPortsRandomizer map[int]bool

// Next generates a new random port that is available
func (up UDPPortsRandomizer) Next(from, to int) int {
	udpPort := random(from, to)
udpPortLoop:
	for {
		if !up[udpPort] {
			up[udpPort] = true
			break udpPortLoop
		}
		udpPort = random(from, to)
	}
	return udpPort
}

func random(from, to int) int {
	// #nosec G404
	return rand.Intn(to-from) + from
}
