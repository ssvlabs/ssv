package main

import (
	"io"
	"log"
	"net"
	"os"
)

func main() {
	if len(os.Args) != 3 {
		log.Fatalf("Usage: %s <listen_address> <forward_address>", os.Args[0])
	}

	listenAddr := os.Args[1]
	forwardAddr := os.Args[2]

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("Error starting TCP listener on %s: %v", listenAddr, err)
	}
	defer listener.Close() // nolint
	log.Printf("Proxy listening on %s and forwarding to %s", listenAddr, forwardAddr)

	for {
		clientConn, err := listener.Accept()
		if err != nil {
			log.Printf("Error accepting connection: %v", err)
			continue
		}
		log.Printf("Accepted connection from %s", clientConn.RemoteAddr())

		go handleConnection(clientConn, forwardAddr)
	}
}

func handleConnection(clientConn net.Conn, forwardAddr string) {
	defer clientConn.Close() // nolint

	serverConn, err := net.Dial("tcp", forwardAddr)
	if err != nil {
		log.Printf("Failed to connect to target %s: %v", forwardAddr, err)
		return
	}
	defer serverConn.Close() // nolint

	done := make(chan struct{})

	go func() {
		_, err := io.Copy(serverConn, clientConn)
		if err != nil {
			log.Printf("Error copying from client %s to server %s: %v", clientConn.RemoteAddr(), forwardAddr, err)
		}
		done <- struct{}{}
	}()

	go func() {
		_, err := io.Copy(clientConn, serverConn)
		if err != nil {
			log.Printf("Error copying from server %s to client %s: %v", forwardAddr, clientConn.RemoteAddr(), err)
		}
		done <- struct{}{}
	}()

	<-done
	log.Printf("Closing connection from %s", clientConn.RemoteAddr())
}
