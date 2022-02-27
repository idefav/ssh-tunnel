package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"golang.org/x/crypto/ssh"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type KeepAliveConfig struct {
	// Interval is the amount of time in seconds to wait before the
	// tunnel client will send a keep-alive message to ensure some minimum
	// traffic on the SSH connection.
	Interval uint

	// CountMax is the maximum number of consecutive failed responses to
	// keep-alive messages the client is willing to tolerate before considering
	// the SSH connection as dead.
	CountMax uint
}

type Tunnel struct {
	serverAddress string
	localAddress  string
	user          string
	auth          []ssh.AuthMethod
	hostKeys      ssh.HostKeyCallback

	retryInterval time.Duration
	keepAlive     KeepAliveConfig
}

func (t Tunnel) bindTunnel(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		var once sync.Once
		func() {
			cl, err := ssh.Dial("tcp", t.serverAddress, &ssh.ClientConfig{
				User:            t.user,
				Auth:            t.auth,
				HostKeyCallback: t.hostKeys,
				Timeout:         5 * time.Second,
			})
			if err != nil {
				once.Do(func() {
					log.Printf("(%v) SSH dial error: %v", t, err)
				})
			}
			wg.Add(1)
			// keep alive
			go t.keepAliveMonitor(&once, wg, cl)
			defer cl.Close()

			log.Println("Connected to ssh server")
			// Accept all incoming connections.
			t.socks5ProxyStart(ctx, cl)
		}()
		select {
		case <-ctx.Done():
			return
		case <-time.After(t.retryInterval):
			log.Printf("(%v) retrying...", t)
		}
	}
}
func (t *Tunnel) socks5Proxy(ctx context.Context, conn net.Conn, client *ssh.Client) {
	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		<-connCtx.Done()
		conn.Close()
	}()

	var b [1024]byte

	n, err := conn.Read(b[:])
	if err != nil {
		log.Println(err)
		return
	}

	conn.Write([]byte{0x05, 0x00})

	n, err = conn.Read(b[:])
	if err != nil {
		log.Println(err)
		return
	}

	var addr string
	switch b[3] {
	case 0x01:
		sip := sockIP{}
		if err := binary.Read(bytes.NewReader(b[4:n]), binary.BigEndian, &sip); err != nil {
			log.Println("Request parsing error")
			return
		}
		addr = sip.toAddr()
	case 0x03:
		host := string(b[5 : n-2])
		var port uint16
		err = binary.Read(bytes.NewReader(b[n-2:n]), binary.BigEndian, &port)
		if err != nil {
			log.Println(err)
			return
		}
		addr = fmt.Sprintf("%s:%d", host, port)
	}

	server, err := client.Dial("tcp", addr)
	if err != nil {
		log.Println(err)
		return
	}
	conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
	go io.Copy(server, conn)
	io.Copy(conn, server)
}

type sockIP struct {
	A, B, C, D byte
	PORT       uint16
}

func (ip sockIP) toAddr() string {
	return fmt.Sprintf("%d.%d.%d.%d:%d", ip.A, ip.B, ip.C, ip.D, ip.PORT)
}

func (t *Tunnel) socks5ProxyStart(ctx context.Context, client *ssh.Client) {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	server, err := net.Listen("tcp", t.localAddress)
	if err != nil {
		log.Panic(err)
	}

	go func() {
		<-connCtx.Done()
		server.Close()
	}()
	log.Println("Start accepting connections")
	for {
		conn, err := server.Accept()
		if err != nil {
			log.Println(err)
			return
		}
		go t.socks5Proxy(ctx, conn, client)
	}
}

func (t Tunnel) dialTunnel(ctx context.Context, wg *sync.WaitGroup, client *ssh.Client, cn1 net.Conn) {
	defer wg.Done()

	// The inbound connection is established. Make sure we close it eventually.
	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		<-connCtx.Done()
		cn1.Close()
	}()

	// Establish the outbound connection.
	var cn2 net.Conn
	var err error

	cn2, err = client.Dial("tcp", t.serverAddress)
	if err != nil {
		log.Printf("(%v) dial error: %v", t, err)
		return
	}

	go func() {
		<-connCtx.Done()
		cn2.Close()
	}()

	log.Printf("(%v) connection established", t)
	defer log.Printf("(%v) connection closed", t)

	// Copy bytes from one connection to the other until one side closes.
	var once sync.Once
	var wg2 sync.WaitGroup
	wg2.Add(2)
	go func() {
		defer wg2.Done()
		defer cancel()
		if _, err := io.Copy(cn1, cn2); err != nil {
			once.Do(func() { log.Printf("(%v) connection error: %v", t, err) })
		}
		once.Do(func() {}) // Suppress future errors
	}()
	go func() {
		defer wg2.Done()
		defer cancel()
		if _, err := io.Copy(cn2, cn1); err != nil {
			once.Do(func() { log.Printf("(%v) connection error: %v", t, err) })
		}
		once.Do(func() {}) // Suppress future errors
	}()
	wg2.Wait()
}

func (t Tunnel) keepAliveMonitor(once *sync.Once, wg *sync.WaitGroup, client *ssh.Client) {
	defer wg.Done()
	if t.keepAlive.Interval == 0 || t.keepAlive.CountMax == 0 {
		return
	}
	wait := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		wait <- client.Wait()
	}()
	var aliveCount int32
	ticker := time.NewTicker(time.Duration(t.keepAlive.Interval) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case err := <-wait:
			if err != nil && err != io.EOF {
				once.Do(func() { log.Printf("(%v) SSH error: %v", t, err) })
			}
			return
		case <-ticker.C:
			if n := atomic.AddInt32(&aliveCount, 1); n > int32(t.keepAlive.CountMax) {
				once.Do(func() { log.Printf("(%v) SSH keep-alive termination", t) })
				client.Close()
				return
			}
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
			if err == nil {
				atomic.StoreInt32(&aliveCount, 0)
			}
		}()
	}
}
