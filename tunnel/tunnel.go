package tunnel

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"golang.org/x/crypto/ssh"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"ssh-tunnel/cfg"
	"ssh-tunnel/safe"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	NetworkError = errors.New("network error")
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
	enableSocks5           bool
	enableHttp             bool
	enableHttpBasic        bool
	enableHttpOverSSH      bool
	enableHttpDomainFilter bool
	httpLocalAddress       string
	httpBasicUserName      string
	httpBasicPassword      string
	serverAddress          string
	localAddress           string
	user                   string
	auth                   []ssh.AuthMethod
	hostKeys               ssh.HostKeyCallback
	domains                map[string]bool
	domainMatchCache       map[string]bool
	appConfig              *cfg.AppConfig

	retryInterval  time.Duration
	keepAlive      KeepAliveConfig
	needReBind     bool
	client         *ssh.Client
	reconnectMutex sync.Mutex // 添加重连锁，确保同一时间只有一个重连过程
}

func (t *Tunnel) GetSSHClient() *ssh.Client {
	return t.client
}

func (t *Tunnel) AppConfig() *cfg.AppConfig {
	return t.appConfig
}

func (t *Tunnel) SetAppConfig(appConfig *cfg.AppConfig) {
	t.appConfig = appConfig
}

func (t *Tunnel) Domains() map[string]bool {
	return t.domains
}

func (t *Tunnel) SetDomains(domains map[string]bool) {
	t.domains = domains
}

func (t *Tunnel) DomainMatchCache() map[string]bool {
	return t.domainMatchCache
}

func (t *Tunnel) SetDomainMatchCache(domainMatchCache map[string]bool) {
	t.domainMatchCache = domainMatchCache
}

func (t *Tunnel) bindHttpTunnel(ctx context.Context, wg *sync.WaitGroup) {
	// Accept all incoming connections.
	t.httpProxyStartEx(ctx, wg)

}

func (t Tunnel) bindSocks5Tunnel(ctx context.Context, wg *sync.WaitGroup) {
	// Accept all incoming connections.
	t.socks5ProxyStart(ctx)

}
func (t *Tunnel) socks5ProxyStart(ctx context.Context) {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	server, err := net.Listen("tcp", t.localAddress)
	if err != nil {
		log.Panic(err)
	}

	safe.GO(func() {
		<-connCtx.Done()
		server.Close()
	})
	log.Println("Start accepting connections")
	for {
		conn, err := server.Accept()
		if err != nil {
			log.Println(err)
			return
		}
		safe.GO(func() {
			resolveErr := t.socks5Proxy(ctx, conn)
			if resolveErr != nil && errors.Is(resolveErr, NetworkError) {
				t.needReBind = true
				// 检测到网络错误，尝试重新连接SSH
				if t.client == nil {
					safe.GO(func() {
						t.ReconnectSSH(ctx)
					})
				}
			}
		})
	}
}

func (t *Tunnel) handleHTTP(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()
	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)

}

func (t *Tunnel) getDestConn(host string) (net.Conn, error) {
	if !t.enableHttpOverSSH {
		return net.DialTimeout("tcp", host, 3*time.Second)
	}

	if !t.enableHttpDomainFilter {
		return t.createSSHConn(host)
	}

	if t.domainMatchCache == nil {
		t.domainMatchCache = make(map[string]bool)
	}

	if value, ok := t.domainMatchCache[host]; ok {
		if value {
			return t.createSSHConn(host)
		} else {
			return net.DialTimeout("tcp", host, 3*time.Second)
		}

	}

	if t.domains != nil && len(t.domains) > 0 {
		for domain, _ := range t.domains {
			if domain != "" {
				split := strings.Split(host, ":")
				if split != nil && len(split) > 0 {
					hasSuffix := strings.HasSuffix(strings.ToLower(strings.Trim(split[0], ".")), strings.ToLower(domain))
					if hasSuffix {
						t.domainMatchCache[host] = true
						return t.createSSHConn(host)
					}
				}
			}
		}
	}
	t.domainMatchCache[host] = false
	return net.DialTimeout("tcp", host, 10*time.Second)

}

func (t *Tunnel) createSSHConn(host string) (net.Conn, error) {
	if t.client == nil {
		log.Println("SSH client is not initialized, cannot dial to host:", host)
		t.ReconnectSSH(context.Background())
	}
	if t.client == nil {
		return nil, errors.New("SSH client is not initialized")
	}
	background := context.Background()
	timeoutCtx, _ := context.WithTimeout(background, 3*time.Second)

	return t.client.DialContext(timeoutCtx, "tcp", host)
}

func (t *Tunnel) handleHTTPS(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	destConn, err := t.getDestConn(r.Host)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}
	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
	}

	safe.GO(func() {
		transfer(destConn, clientConn)
	})
	safe.GO(func() {
		transfer(clientConn, destConn)
	})
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func transfer(destination io.WriteCloser, source io.ReadCloser) {
	defer destination.Close()
	defer source.Close()
	io.Copy(destination, source)
}

func (t *Tunnel) basicAuth(w http.ResponseWriter, r *http.Request) bool {
	if !t.enableHttpBasic {
		return true
	}
	var auth = r.Header.Get("Proxy-Authorization")
	if ms := strings.Split(auth, " "); len(ms) == 2 && ms[0] == "Basic" {
		// check user:password
		up, err := base64.StdEncoding.DecodeString(ms[1])
		if err == nil {
			if ms := strings.Split(string(up), ":"); len(ms) == 2 {
				var user, password = ms[0], ms[1]
				var ok = false
				if user == t.httpBasicUserName && password == t.httpBasicPassword {
					ok = true
				}
				if ok {
					return true
				}
			}
		}
		w.WriteHeader(http.StatusProxyAuthRequired)
	} else {

		w.WriteHeader(http.StatusProxyAuthRequired)
		w.Header().Set("Proxy-Authenticate", `Basic realm="Http Proxy"`)
	}
	return false
}

func (t *Tunnel) httpProxyStartEx(ctx context.Context, wg *sync.WaitGroup) {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	l, err := net.Listen("tcp", t.httpLocalAddress)
	if err != nil {
		log.Panic(err)
	}
	safe.GO(func() {
		log.Println("Http Server Started at: " + t.httpLocalAddress)
		for {
			client, err := l.Accept()
			if err != nil {
				log.Panic(err)
			}

			safe.GO(func() {
				t.handleClientRequest(ctx, client)
			})
		}
	})

	<-connCtx.Done()
	ctx, timeOutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer func() {
		// extra handling here
		timeOutCancel()
		log.Println("Server Stopped!")
	}()

}

func (t *Tunnel) handleClientRequest(ctx context.Context, client net.Conn) {
	defer client.Close()
	defer func() {
		if err := recover(); err != nil {
			log.Println("panic occurred:", err)
		}
	}()
	var b [1024]byte
	n, err := client.Read(b[:])
	if err != nil {
		log.Println(err)
		fmt.Fprint(client, "HTTP/1.1 500 "+err.Error()+"\r\n\r\n")
		return
	}
	var method, host, address string
	fmt.Sscanf(string(b[:bytes.IndexByte(b[:], '\n')]), "%s%s", &method, &host)

	if method == http.MethodConnect {
		address = host
	} else {
		hostPortURL, err := url.Parse(host)
		if err != nil {
			log.Println(err)
			fmt.Fprint(client, "HTTP/1.1 500 "+err.Error()+"\r\n\r\n")
			return
		}
		if hostPortURL.Opaque == "443" { //https访问
			address = hostPortURL.Scheme + ":443"
		} else { //http访问
			if strings.Index(hostPortURL.Host, ":") == -1 { //host不带端口， 默认80
				address = hostPortURL.Host + ":80"
			} else {
				address = hostPortURL.Host
			}
		}
	}

	destConn, done := t.getConn(ctx, client, err, address)
	if done {
		log.Println("Get Dest Connection Failed!")
		return
	}

	if method == "CONNECT" {
		fmt.Fprint(client, "HTTP/1.1 200 Connection established\r\n\r\n")
	} else {
		destConn.Write(b[:n])
	}
	//进行转发
	go io.Copy(destConn, client)
	io.Copy(client, destConn)
}

func (t *Tunnel) getConn(ctx context.Context, client net.Conn, err error, address string) (net.Conn, bool) {
	destConn, err := t.getDestConn(address)
	for {
		if err == nil && destConn != nil {
			break
		}

		if strings.Contains(err.Error(), "unexpected packet in response") || strings.Contains(err.Error(), "context deadline exceeded") || strings.Contains(err.Error(), "max packet length exceeded") || t.client == nil {
			t.client = nil
			t.ReconnectSSH(ctx)
			if t.client == nil {
				return nil, true
			}
			destConn, err = t.getDestConn(address)
			return destConn, false
		} else {
			log.Println(err)
			fmt.Fprint(client, "HTTP/1.1 500 "+err.Error()+"\r\n\r\n")
			return nil, true
		}

	}
	return destConn, false
}

func (t *Tunnel) httpProxyStart(ctx context.Context, wg *sync.WaitGroup) {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	server := &http.Server{
		Addr: t.httpLocalAddress,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			result := t.basicAuth(w, r)
			if !result {
				return
			}
			if r.Method == http.MethodConnect {
				t.handleHTTPS(ctx, w, r)
			} else {
				t.handleHTTP(ctx, w, r)
			}
		}),
		// Disable HTTP/2.
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}

	safe.GO(func() {
		err := server.ListenAndServe()
		if err != nil {
			log.Fatalln(err)
		}
	})
	log.Printf("Http Server Started at %s", t.httpLocalAddress)
	<-connCtx.Done()
	ctx, timeOutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer func() {
		// extra handling here
		timeOutCancel()
	}()
	log.Println("Server Stopped!")
	err := server.Shutdown(ctx)

	if err != nil {
		log.Fatalf("Server Shutdown Failed:%+v", err)
	}

	log.Print("Server Exited Properly")

}

func (t *Tunnel) socks5Proxy(ctx context.Context, conn net.Conn) error {
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
		return err
	}

	conn.Write([]byte{0x05, 0x00})

	n, err = conn.Read(b[:])
	if err != nil {
		log.Println(err)
		return err
	}

	var addr string
	switch b[3] {
	case 0x01:
		sip := sockIP{}
		if err := binary.Read(bytes.NewReader(b[4:n]), binary.BigEndian, &sip); err != nil {
			log.Println("Request parsing error")
			return err
		}
		addr = sip.toAddr()
	case 0x03:
		host := string(b[5 : n-2])
		var port uint16
		err = binary.Read(bytes.NewReader(b[n-2:n]), binary.BigEndian, &port)
		if err != nil {
			log.Println(err)
			return err
		}
		addr = fmt.Sprintf("%s:%d", host, port)
	}

	server, err := t.client.Dial("tcp", addr)
	if err != nil {
		log.Println(err)
		return NetworkError
	}
	conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
	safe.GO(func() {
		io.Copy(server, conn)
	})
	io.Copy(conn, server)
	return nil
}

func (t *Tunnel) httpProxy(ctx context.Context, conn net.Conn) error {
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
		return err
	}

	conn.Write([]byte{0x05, 0x00})

	n, err = conn.Read(b[:])
	if err != nil {
		log.Println(err)
		return err
	}

	var addr string
	switch b[3] {
	case 0x01:
		sip := sockIP{}
		if err := binary.Read(bytes.NewReader(b[4:n]), binary.BigEndian, &sip); err != nil {
			log.Println("Request parsing error")
			return err
		}
		addr = sip.toAddr()
	case 0x03:
		host := string(b[5 : n-2])
		var port uint16
		err = binary.Read(bytes.NewReader(b[n-2:n]), binary.BigEndian, &port)
		if err != nil {
			log.Println(err)
			return err
		}
		addr = fmt.Sprintf("%s:%d", host, port)
	}

	server, err := t.client.Dial("tcp", addr)
	if err != nil {
		log.Println(err)
		return NetworkError
	}
	conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
	safe.GO(func() {
		io.Copy(server, conn)
	})
	io.Copy(conn, server)
	return nil
}

type sockIP struct {
	A, B, C, D byte
	PORT       uint16
}

func (ip sockIP) toAddr() string {
	return fmt.Sprintf("%d.%d.%d.%d:%d", ip.A, ip.B, ip.C, ip.D, ip.PORT)
}

func (t Tunnel) dialTunnel(ctx context.Context, wg *sync.WaitGroup, client *ssh.Client, cn1 net.Conn) {
	defer wg.Done()

	// The inbound connection is established. Make sure we close it eventually.
	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	safe.GO(func() {
		<-connCtx.Done()
		cn1.Close()
	})

	// Establish the outbound connection.
	var cn2 net.Conn
	var err error

	cn2, err = client.Dial("tcp", t.serverAddress)
	if err != nil {
		log.Printf("(%v) dial error: %v", t, err)
		return
	}

	safe.GO(func() {
		<-connCtx.Done()
		cn2.Close()
	})

	log.Printf("(%v) connection established", t)
	defer log.Printf("(%v) connection closed", t)

	// Copy bytes from one connection to the other until one side closes.
	var once sync.Once
	var wg2 sync.WaitGroup
	wg2.Add(2)
	safe.GO(func() {
		defer wg2.Done()
		defer cancel()
		if _, err := io.Copy(cn1, cn2); err != nil {
			once.Do(func() { log.Printf("(%v) connection error: %v", t, err) })
		}
		once.Do(func() {}) // Suppress future errors
	})
	safe.GO(func() {
		defer wg2.Done()
		defer cancel()
		if _, err := io.Copy(cn2, cn1); err != nil {
			once.Do(func() { log.Printf("(%v) connection error: %v", t, err) })
		}
		once.Do(func() {}) // Suppress future errors
	})
	wg2.Wait()
}

func (t Tunnel) keepAliveMonitor(ctx context.Context, once *sync.Once, wg *sync.WaitGroup) {
	defer wg.Done()
	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	safe.GO(func() {
		<-connCtx.Done()
	})
	if t.keepAlive.Interval == 0 || t.keepAlive.CountMax == 0 {
		return
	}
	wait := make(chan error, 1)
	wg.Add(1)
	safe.GO(func() {
		defer wg.Done()
		wait <- t.client.Wait()
	})
	var aliveCount int32
	ticker := time.NewTicker(time.Duration(t.keepAlive.Interval) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case err := <-wait:
			if err != nil && err != io.EOF {
				once.Do(func() { log.Printf("(%v) SSH error: %v", t, err) })
			}
			return
		case <-ticker.C:
			if n := atomic.AddInt32(&aliveCount, 1); n > int32(t.keepAlive.CountMax) {
				once.Do(func() { log.Printf("(%v) SSH keep-alive termination", t) })
				if t.client != nil {
					t.client.Close()
					t.client = nil
				}

				return
			}
		}

		wg.Add(1)
		safe.GO(func() {
			defer wg.Done()
			if t.client == nil {
				return
			}
			_, _, err := t.client.SendRequest("keepalive@openssh.com", true, nil)
			if err == nil {
				atomic.StoreInt32(&aliveCount, 0)
			}
		})
	}
}
