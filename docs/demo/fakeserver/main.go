// Command fakeserver stands in for the dev servers in peek's README demo.
// record.sh installs copies of it under names like "node" and "python3",
// so peek shows them like the real thing. It ignores its arguments and is
// configured through the environment:
//
//	DEMO_LISTEN   comma-separated addresses to listen on, e.g. 127.0.0.1:3000,[::1]:3000
//	DEMO_CLIENTS  how many connections to open to the first address and keep open
package main

import (
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
)

// open holds every connection so the garbage collector doesn't close them.
var (
	mu   sync.Mutex
	open []net.Conn
)

func keep(c net.Conn) {
	mu.Lock()
	defer mu.Unlock()
	open = append(open, c)
}

func main() {
	addrs := strings.Split(os.Getenv("DEMO_LISTEN"), ",")
	for _, addr := range addrs {
		// "tcp" would turn 0.0.0.0 into a dual-stack [::] socket.
		network := "tcp6"
		if !strings.HasPrefix(addr, "[") {
			network = "tcp4"
		}
		ln, err := net.Listen(network, addr)
		if err != nil {
			log.Fatal(err)
		}
		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				keep(conn)
			}
		}()
	}
	clients, _ := strconv.Atoi(os.Getenv("DEMO_CLIENTS"))
	for range clients {
		conn, err := net.Dial("tcp", addrs[0])
		if err != nil {
			log.Fatal(err)
		}
		keep(conn)
	}
	select {} // run until signalled
}
