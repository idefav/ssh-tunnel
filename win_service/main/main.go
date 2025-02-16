//go:build windows
// +build windows

package main

import (
	"fmt"
	"github.com/fsnotify/fsnotify"
	"github.com/kardianos/service"
	"github.com/spf13/viper"
	"log"
	"os"
	"os/user"
	"path"
	"ssh-tunnel/api/admin"
	"ssh-tunnel/cfg"
	"ssh-tunnel/tunnel"
	"strings"
	"sync"
)

const DEFAULT_HOME = "C:\\ssh-tunnel"

func main() {
	srvConfig := &service.Config{
		Name:        "SSHTunnelService",
		DisplayName: "SSHTunnelService",
		Description: "SSHTunnelService",
	}
	prg := &program{}
	s, err := service.New(prg, srvConfig)
	if err != nil {
		fmt.Println(err)
	}
	if len(os.Args) > 1 {
		serviceAction := os.Args[1]
		switch serviceAction {
		case "install":
			err := s.Install()
			if err != nil {
				fmt.Println("安装服务失败: ", err.Error())
			} else {
				fmt.Println("安装服务成功")
			}
			return
		case "uninstall":
			err := s.Uninstall()
			if err != nil {
				fmt.Println("卸载服务失败: ", err.Error())
			} else {
				fmt.Println("卸载服务成功")
			}
			return
		case "start":
			err := s.Start()
			if err != nil {
				fmt.Println("运行服务失败: ", err.Error())
			} else {
				fmt.Println("运行服务成功")
			}
			return
		case "stop":
			err := s.Stop()
			if err != nil {
				fmt.Println("停止服务失败: ", err.Error())
			} else {
				fmt.Println("停止服务成功")
			}
			return
		case "exec":
			innerStart()
		}
	}

	err = s.Run()
	if err != nil {
		fmt.Println(err)
	}

}

type program struct{}

func (p *program) Start(s service.Service) error {
	fmt.Println("服务运行...")
	go p.run()
	return nil
}

func (p *program) run() {
	innerStart()
}

func (p *program) Stop(s service.Service) error {
	return nil
}

func innerStart() {
	defer func() { // 必须要先声明defer，否则不能捕获到panic异常
		if err := recover(); err != nil {
			fmt.Println(err) // 这里的err其实就是panic传入的内容
		}
	}()
	u, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}

	userHomeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}

	defaultHomeExist, _ := PathExists(DEFAULT_HOME)
	if !defaultHomeExist {
		os.MkdirAll(DEFAULT_HOME, os.ModePerm)
	}

	config := cfg.AppConfig{}
	config.HomeDir = u.HomeDir
	if strings.ContainsAny(u.HomeDir, "system32") {
		config.HomeDir = "C:\\ssh-tunnel"
		os.MkdirAll(config.HomeDir, 0755)
	}

	userHomeDir = config.HomeDir

	logFile, err := os.OpenFile(path.Join(userHomeDir, "console.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Println("open log file failed, err:", err)
		return
	}
	log.SetOutput(logFile)
	log.SetFlags(log.Llongfile | log.Lmicroseconds | log.Ldate)

	log.Println("starting ..., userHomeDir: ", userHomeDir)
	log.Println("current user: ", u.Username)
	log.Println("u.homeDir: ", u.HomeDir)

	vConfig := viper.New()
	vConfig.AddConfigPath(path.Join(userHomeDir, ".ssh-tunnel"))
	vConfig.SetConfigName("config")
	vConfig.SetConfigType("properties")
	if err := vConfig.ReadInConfig(); err != nil {
		panic(err)
	}
	vConfig.WatchConfig()
	vConfig.OnConfigChange(func(e fsnotify.Event) {
		fmt.Println("config file changed:", e.Name)
		if err := vConfig.ReadInConfig(); err != nil {
			panic(err)
		}
	})

	config.ServerIp = vConfig.GetString("server.ip")
	config.ServerSshPort = vConfig.GetInt("server.ssh.port")
	config.SshPrivateKeyPath = vConfig.GetString("server.ssh.private_key_path")
	config.SshKnownHostsPath = vConfig.GetString("server.ssh.known_hosts_path")
	config.LoginUser = vConfig.GetString("login.username")
	config.LocalAddress = vConfig.GetString("local.address")
	config.HttpLocalAddress = vConfig.GetString("http.local.address")
	config.HttpBasicUserName = vConfig.GetString("http.basic.username")
	config.HttpBasicPassword = vConfig.GetString("http.basic.password")
	config.HttpBasicAuthEnable = vConfig.GetBool("http.basic.enable")

	config.EnableHttp = vConfig.GetBool("http.enable")
	config.EnableSocks5 = vConfig.GetBool("socks5.enable")
	config.EnableHttpOverSSH = vConfig.GetBool("http.over-ssh.enable")
	config.EnableHttpDomainFilter = vConfig.GetBool("http.domain-filter.enable")
	config.HttpDomainFilterFilePath = vConfig.GetString("http.filter.domain.file-path")

	config.EnableAdmin = vConfig.GetBool("admin.enable")
	config.AdminAddress = vConfig.GetString("admin.address")

	//flag.StringVar(&config.ServerIp, "server.ip", "", "服务器IP地址")
	//flag.StringVar(&config.ServerIp, "s", "", "服务器IP地址(短命令)")
	//flag.IntVar(&config.ServerSshPort, "server.ssh.port", 22, "服务器SSH端口")
	//flag.IntVar(&config.ServerSshPort, "p", 22, "服务器SSH端口(短命令)")
	//flag.StringVar(&config.SshPrivateKeyPath, "ssh.path.private_key", path.Join(u.HomeDir, ".ssh/id_rsa"), "私钥地址")
	//flag.StringVar(&config.SshPrivateKeyPath, "pk", path.Join(u.HomeDir, ".ssh/id_rsa"), "私钥地址(短命令)")
	//flag.StringVar(&config.SshKnownHostsPath, "ssh.path.known_hosts", path.Join(u.HomeDir, ".ssh/known_hosts"), "已知主机地址")
	//flag.StringVar(&config.SshKnownHostsPath, "pkh", path.Join(u.HomeDir, ".ssh/known_hosts"), "已知主机地址(短命令)")
	//flag.StringVar(&config.LoginUser, "user", "root", "用户名")
	//flag.StringVar(&config.LoginUser, "u", "root", "用户名(短命令)")
	//flag.StringVar(&config.LocalAddress, "local.addr", "0.0.0.0:1081", "本地地址")
	//flag.StringVar(&config.LocalAddress, "l", "0.0.0.0:1081", "本地地址(短命令)")
	//flag.StringVar(&config.HttpLocalAddress, "http.local.addr", "0.0.0.0:1082", "Http监听地址")
	//flag.StringVar(&config.HttpBasicUserName, "http.basic.username", "", "Basic认证, 用户名")
	//flag.StringVar(&config.HttpBasicPassword, "http.basic.password", "", "Http Basic认证, 密码")
	//flag.BoolVar(&config.EnableHttp, "http.enable", false, "是否开启Http代理")
	//flag.BoolVar(&config.EnableSocks5, "socks5.enable", true, "是否开启Socks5代理")
	//flag.BoolVar(&config.HttpBasicAuthEnable, "http.basic.enable", false, "是否开启Http的Basic认证")
	//flag.BoolVar(&config.EnableHttpOverSSH, "http.over.ssh.enable", false, "是否开启Http Over SSH")
	//flag.BoolVar(&config.EnableHttpDomainFilter, "http.filter.domain.enable", false, "是否启用Http域名过滤")
	//flag.StringVar(&config.HttpDomainFilterFilePath, "http.filter.domain.file-path", path.Join(u.HomeDir, ".ssh-tunnel/domain.txt"), "过滤http请求")
	//
	//flag.BoolVar(&config.EnableAdmin, "admin.enable", false, "是否启用Admin页面")
	//flag.StringVar(&config.AdminAddress, "admin.addr", ":1083", "Admin监听地址")
	//log.Printf("%v", os.Args)
	//
	//flag.Parse()
	var wg sync.WaitGroup
	tunnel.Load(&config, &wg)
	admin.Load(&config, &wg)
	wg.Wait()
}

func PathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
