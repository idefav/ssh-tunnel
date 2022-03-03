package main

import (
	"context"
	"flag"
	"fmt"
	_ "github.com/codyguo/godaemon"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"os/user"
	"path"
	"strconv"
	"sync"
	"syscall"
	"time"
)

func main() {
	defer func() { // 必须要先声明defer，否则不能捕获到panic异常
		if err := recover(); err != nil {
			fmt.Println(err) // 这里的err其实就是panic传入的内容
		}
	}()
	u, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}
	var serverIp string
	var serverSshPort int
	var sshPrivateKeyPath string
	var sshKnownHostsPath string
	var loginUser string
	var localAddress string

	tunnel := Tunnel{}
	flag.StringVar(&serverIp, "server.ip", "", "服务器IP地址")
	flag.StringVar(&serverIp, "s", "", "服务器IP地址(短命令)")
	flag.IntVar(&serverSshPort, "server.ssh.port", 22, "服务器SSH端口")
	flag.IntVar(&serverSshPort, "p", 22, "服务器SSH端口(短命令)")
	flag.StringVar(&sshPrivateKeyPath, "ssh.path.private_key", path.Join(u.HomeDir, ".ssh/id_rsa"), "私钥地址")
	flag.StringVar(&sshPrivateKeyPath, "pk", path.Join(u.HomeDir, ".ssh/id_rsa"), "私钥地址(短命令)")
	flag.StringVar(&sshKnownHostsPath, "ssh.path.known_hosts", path.Join(u.HomeDir, ".ssh/known_hosts"), "已知主机地址")
	flag.StringVar(&sshKnownHostsPath, "pkh", path.Join(u.HomeDir, ".ssh/known_hosts"), "已知主机地址(短命令)")
	flag.StringVar(&loginUser, "user", "root", "用户名")
	flag.StringVar(&loginUser, "u", "root", "用户名(短命令)")
	flag.StringVar(&localAddress, "local.addr", "0.0.0.0:1081", "本地地址")
	flag.StringVar(&localAddress, "l", "0.0.0.0:1081", "本地地址(短命令)")

	flag.Parse()

	tunnel.serverAddress = serverIp + ":" + strconv.Itoa(serverSshPort)
	tunnel.localAddress = localAddress
	tunnel.keepAlive = KeepAliveConfig{Interval: 30, CountMax: 3}

	var keys []ssh.Signer
	b, err := ioutil.ReadFile(sshPrivateKeyPath)
	if err != nil {
		log.Fatalf("private key error: %v", err)
	}
	k, err := ssh.ParsePrivateKey(b)
	if err != nil {
		log.Fatalf("private key error: %v", err)
	}
	keys = append(keys, k)
	auth := []ssh.AuthMethod{ssh.PublicKeys(keys...)}

	hostKeys, err := knownhosts.New(sshKnownHostsPath)
	if err != nil {
		log.Fatalf("public key error: %v", err)
	}

	tunnel.auth = auth
	tunnel.hostKeys = hostKeys
	tunnel.user = loginUser
	tunnel.retryInterval = 30 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	GO(func() {
		sigc := make(chan os.Signal, 1)
		signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
		log.Printf("received %v - initiating shutdown", <-sigc)
		cancel()
	})

	var wg sync.WaitGroup
	log.Printf("%s starting", path.Base(os.Args[0]))
	defer log.Printf("%s shutdown", path.Base(os.Args[0]))
	wg.Add(1)
	GO(func() {
		tunnel.bindTunnel(ctx, &wg)
	})
	wg.Wait()
}
