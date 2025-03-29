package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
)

var stderr = io.Discard

func main() {
	// Define a verbose flag
	verbose := flag.Bool("v", false, "Enable verbose logging")
	flag.Parse()

	// If not in verbose mode, discard logs
	if *verbose {
		stderr = os.Stderr
	}
	log.SetOutput(stderr)

	// Check if we have the correct number of arguments
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s [protocol]:[host]:[port]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Example: %s tcp:example.com:80\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Example: %s udp:dns.server:53\n", os.Args[0])
		os.Exit(1)
	}

	// Parse the argument
	parts := strings.SplitN(os.Args[1], ":", 3)
	if len(parts) != 3 {
		fmt.Fprintf(os.Stderr, "Invalid argument format. Expected [protocol]:[host]:[port]\n")
		os.Exit(1)
	}

	protocol := parts[0]
	host := parts[1]
	port := parts[2]

	// Create connection based on the specified protocol
	var conn net.Conn
	var err error

	address := net.JoinHostPort(host, port)

	switch strings.ToLower(protocol) {
	case "tcp":
		log.Printf("Connecting to TCP %s...", address)
		conn, err = net.Dial("tcp", address)
	case "udp":
		log.Printf("Connecting to UDP %s...", address)
		conn, err = net.Dial("udp", address)
	default:
		exit(fmt.Errorf("Unsupported protocol: %s. Use 'tcp' or 'udp'.\n", protocol))
	}

	if err != nil {
		exit(fmt.Errorf("Error connecting to %s://%s: %v\n", protocol, address, err))
	}
	defer conn.Close()

	log.Printf("Connected to %s://%s", protocol, address)

	// Set up channel to handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Set up wait group for the goroutines
	var wg sync.WaitGroup
	wg.Add(2)

	// Copy from stdin to connection
	go func() {
		defer wg.Done()
		if _, err := io.Copy(conn, os.Stdin); err != nil {
			log.Printf("Error copying from stdin to connection: %v", err)
		}

		// If we're done reading from stdin, signal that we're done to the connection
		// This is important for TCP connections
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			tcpConn.CloseWrite()
		}
	}()

	// Copy from connection to stdout
	go func() {
		defer wg.Done()
		if _, err := io.Copy(os.Stdout, conn); err != nil {
			log.Printf("Error copying from connection to stdout: %v", err)
		}
	}()

	// Wait for either a signal or for both goroutines to finish
	go func() {
		wg.Wait()
		// If both goroutines are done, we can exit
		sigChan <- syscall.SIGTERM
	}()

	// Wait for termination signal
	<-sigChan
	log.Printf("Shutting down connection to %s://%s", protocol, address)
}

func exit(err error) {
	if err != nil {
		fmt.Fprintln(stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}
