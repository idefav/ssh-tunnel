package main

import (
	"fmt"
	"github.com/fsnotify/fsnotify"
	"github.com/kardianos/service"
	"github.com/spf13/viper"
	"io"
	"log"
	"os"
	"os/user"
	"path"
	"ssh-tunnel/api/admin"
	"ssh-tunnel/cfg"
	"ssh-tunnel/service/os_config"
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
			// 检查是否有提供配置文件路径参数
			configPath := ""
			for i := 2; i < len(os.Args); i++ {
				if strings.HasPrefix(os.Args[i], "--config=") {
					configPath = strings.TrimPrefix(os.Args[i], "--config=")
					fmt.Println("使用配置文件路径:", configPath)
					// 将配置路径添加到服务的启动参数中
					srvConfig.Arguments = []string{"--config=" + configPath}
					break
				}
			}
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

	configPath := ""
	for i := 1; i < len(os.Args); i++ {
		if strings.HasPrefix(os.Args[i], "--config=") {
			configPath = strings.TrimPrefix(os.Args[i], "--config=")
			log.Println("从命令行参数获取配置文件路径:", configPath)
			break
		}
	}

	u, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}

	config := cfg.AppConfig{}
	vConfig := viper.New()

	// 添加配置查找路径
	vConfig.AddConfigPath(".")
	vConfig.AddConfigPath(path.Join(u.HomeDir, ".ssh-tunnel"))

	// 如果命令行参数中有配置文件路径，则直接使用该路径
	exists, _ := PathExists(configPath)
	if configPath != "" && exists {
		log.Println("使用指定的配置文件:", configPath)
		vConfig.SetConfigFile(configPath)
		vConfig.SetConfigType("properties")
	} else {
		// 否则使用默认路径
		log.Println("使用默认配置文件路径")
		vConfig.AddConfigPath(path.Join(u.HomeDir, ".ssh-tunnel"))
		vConfig.SetConfigName("config")
		vConfig.SetConfigType("properties")
	}

	os_config.SetConfig(vConfig)

	// Windows 系统特定配置
	// 是否使用OS 服务管理器运行
	interactive := service.Interactive()
	if !interactive {
		// 通过服务管理器运行时，配置文件路径可能在特定目录下
		vConfig.AddConfigPath(path.Join(DEFAULT_HOME, ".ssh-tunnel"))
	}

	// 默认值设置
	vConfig.SetDefault("home.dir", path.Join(u.HomeDir, ".ssh-tunnel"))
	vConfig.SetDefault("ssh.path.private_key", path.Join(u.HomeDir, ".ssh/id_rsa"))
	vConfig.SetDefault("ssh.path.known_hosts", path.Join(u.HomeDir, ".ssh/known_hosts"))
	vConfig.SetDefault("user", "root")
	vConfig.SetDefault("local.addr", "0.0.0.0:1081")
	vConfig.SetDefault("http.local.addr", "0.0.0.0:1082")
	vConfig.SetDefault("http.enable", false)
	vConfig.SetDefault("socks5.enable", true)
	vConfig.SetDefault("http.basic.enable", false)
	vConfig.SetDefault("http.over.ssh.enable", false)
	vConfig.SetDefault("http.filter.domain.enable", false)
	vConfig.SetDefault("http.filter.domain.file-path", path.Join(u.HomeDir, ".ssh-tunnel/domain.txt"))
	vConfig.SetDefault("admin.enable", false)
	vConfig.SetDefault("admin.addr", ":1083")
	vConfig.SetDefault("retry.interval.sec", 3)

	// 环境变量配置
	vConfig.SetEnvPrefix("SSH_TUNNEL")       // 设置环境变量前缀
	replace := strings.NewReplacer(".", "_") // 替换点为下划线
	vConfig.SetEnvKeyReplacer(replace)
	vConfig.AutomaticEnv()

	if err := vConfig.ReadInConfig(); err != nil {
		panic(err)
	}

	log.Println("成功读取配置文件:", vConfig.ConfigFileUsed())

	vConfig.WatchConfig()
	vConfig.OnConfigChange(func(e fsnotify.Event) {
		fmt.Println("config file changed:", e.Name)
		if err := vConfig.ReadInConfig(); err != nil {
			panic(err)
		}
	})

	config.HomeDir = vConfig.GetString("home.dir")
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
	config.RetryIntervalSec = vConfig.GetInt("retry.interval.sec")

	config.LogFilePath = path.Join(config.HomeDir, ".ssh-tunnel", "console.log")

	logFile, err := os.OpenFile(config.LogFilePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		fmt.Println("open log file failed, err:", err)
		return
	}

	// 替换原来的log.SetOutput(logFile)为：
	mw := io.MultiWriter(logFile)
	log.SetOutput(mw)
	log.SetFlags(log.Llongfile | log.Lmicroseconds | log.Ldate)

	log.Println("starting ..., userHomeDir: ", u.HomeDir)
	log.Println("current user: ", u.Username)
	log.Println("u.homeDir: ", u.HomeDir)
	log.Println("config.HomeDir: ", config.HomeDir)
	log.Println("configPath: ", configPath)

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
