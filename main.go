package main

import (
	_ "embed"
	"flag"
	"fmt"
	"log"
	"os"
	"os/user"
	"path"
	"ssh-tunnel/api/admin"
	"ssh-tunnel/cfg"
	"ssh-tunnel/tunnel"
	"sync"
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

	config := cfg.AppConfig{}

	flag.StringVar(&config.ServerIp, "server.ip", "", "服务器IP地址")
	flag.StringVar(&config.ServerIp, "s", "", "服务器IP地址(短命令)")
	flag.IntVar(&config.ServerSshPort, "server.ssh.port", 22, "服务器SSH端口")
	flag.IntVar(&config.ServerSshPort, "p", 22, "服务器SSH端口(短命令)")
	flag.StringVar(&config.SshPrivateKeyPath, "ssh.path.private_key", path.Join(u.HomeDir, ".ssh/id_rsa"), "私钥地址")
	flag.StringVar(&config.SshPrivateKeyPath, "pk", path.Join(u.HomeDir, ".ssh/id_rsa"), "私钥地址(短命令)")
	flag.StringVar(&config.SshKnownHostsPath, "ssh.path.known_hosts", path.Join(u.HomeDir, ".ssh/known_hosts"), "已知主机地址")
	flag.StringVar(&config.SshKnownHostsPath, "pkh", path.Join(u.HomeDir, ".ssh/known_hosts"), "已知主机地址(短命令)")
	flag.StringVar(&config.LoginUser, "user", "root", "用户名")
	flag.StringVar(&config.LoginUser, "u", "root", "用户名(短命令)")
	flag.StringVar(&config.LocalAddress, "local.addr", "0.0.0.0:1081", "本地地址")
	flag.StringVar(&config.LocalAddress, "l", "0.0.0.0:1081", "本地地址(短命令)")
	flag.StringVar(&config.HttpLocalAddress, "http.local.addr", "0.0.0.0:1082", "Http监听地址")
	flag.StringVar(&config.HttpBasicUserName, "http.basic.username", "", "Basic认证, 用户名")
	flag.StringVar(&config.HttpBasicPassword, "http.basic.password", "", "Http Basic认证, 密码")
	flag.BoolVar(&config.EnableHttp, "http.enable", false, "是否开启Http代理")
	flag.BoolVar(&config.EnableSocks5, "socks5.enable", true, "是否开启Socks5代理")
	flag.BoolVar(&config.HttpBasicAuthEnable, "http.basic.enable", false, "是否开启Http的Basic认证")
	flag.BoolVar(&config.EnableHttpOverSSH, "http.over.ssh.enable", false, "是否开启Http Over SSH")
	flag.BoolVar(&config.EnableHttpDomainFilter, "http.filter.domain.enable", false, "是否启用Http域名过滤")
	flag.StringVar(&config.HttpDomainFilterFilePath, "http.filter.domain.file-path", path.Join(u.HomeDir, ".ssh-tunnel/domain.txt"), "过滤http请求")

	flag.BoolVar(&config.EnableAdmin, "admin.enable", false, "是否启用Admin页面")
	flag.StringVar(&config.AdminAddress, "admin.addr", ":1083", "Admin监听地址")
	log.Printf("%v", os.Args)

	flag.Parse()
	var wg sync.WaitGroup
	tunnel.Load(&config, &wg)
	admin.Load(&config, &wg)
	wg.Wait()
}
