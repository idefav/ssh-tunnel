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
	"ssh-tunnel/constants"
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

	config := cfg.NewAppConfig()
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
		os_config.SetConfig(vConfig)
	}

	// 默认值设置
	vConfig.SetDefault(config.HomeDir.GetKey(), config.HomeDir.GetDefaultValue())
	vConfig.SetDefault(config.SshPrivateKeyPath.GetKey(), config.SshPrivateKeyPath.GetDefaultValue())
	vConfig.SetDefault(config.SshKnownHostsPath.GetKey(), config.SshKnownHostsPath.GetDefaultValue())
	vConfig.SetDefault(config.LoginUser.GetKey(), config.LoginUser.GetDefaultValue())
	vConfig.SetDefault(config.LocalAddress.GetKey(), config.LocalAddress.GetDefaultValue())
	vConfig.SetDefault(config.HttpLocalAddress.GetKey(), config.HttpLocalAddress.GetDefaultValue())
	vConfig.SetDefault(config.EnableHttp.GetKey(), config.EnableHttp.GetDefaultValue())
	vConfig.SetDefault(config.EnableSocks5.GetKey(), config.EnableSocks5.GetDefaultValue())
	vConfig.SetDefault(config.HttpBasicAuthEnable.GetKey(), config.HttpBasicAuthEnable.GetDefaultValue())
	vConfig.SetDefault(config.EnableHttpOverSSH.GetKey(), config.EnableHttpOverSSH.GetDefaultValue())
	vConfig.SetDefault(config.EnableHttpDomainFilter.GetKey(), config.EnableHttpDomainFilter.GetDefaultValue())
	vConfig.SetDefault(config.HttpDomainFilterFilePath.GetKey(), config.HttpDomainFilterFilePath.GetDefaultValue())
	vConfig.SetDefault(config.EnableAdmin.GetKey(), config.EnableAdmin.GetDefaultValue())
	vConfig.SetDefault(config.AdminAddress.GetKey(), config.AdminAddress.GetDefaultValue())
	vConfig.SetDefault(config.RetryIntervalSec.GetKey(), config.RetryIntervalSec.GetDefaultValue())
	vConfig.SetDefault(config.LogFilePath.GetKey(), config.LogFilePath.GetDefaultValue())

	// 环境变量配置
	vConfig.SetEnvPrefix("SSH_TUNNEL")       // 设置环境变量前缀
	replace := strings.NewReplacer(".", "_") // 替换点为下划线
	vConfig.SetEnvKeyReplacer(replace)
	vConfig.AutomaticEnv()

	if err := vConfig.ReadInConfig(); err != nil {
		log.Printf("Failed to read config file: %v", err)
		return
	}

	// 设置全局配置实例
	cfg.SetConfigInstance(vConfig)

	// 保存配置文件路径到常量
	constants.ConfigFilePath = vConfig.ConfigFileUsed()

	config.Update()

	log.Println("成功读取配置文件:", vConfig.ConfigFileUsed())

	// 打印配置内容
	for _, key := range vConfig.AllKeys() {
		value := vConfig.Get(key)
		log.Printf("配置项: %s = %v\n", key, value)
	}
	vConfig.WatchConfig()
	vConfig.OnConfigChange(func(e fsnotify.Event) {
		fmt.Println("config file changed:", e.Name)
		if err := vConfig.ReadInConfig(); err != nil {
			log.Printf("Failed to reload config file: %v", err)
			return
		}
		config.Update()
	})

	logFile, err := os.OpenFile(config.LogFilePath.GetValue(), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		fmt.Println("open log file failed, err:", err, ", log.file.path:", config.LogFilePath.GetValue())
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
	err = tunnel.Load(config, &wg)
	if err != nil {
		log.Printf("Failed to load tunnel configuration: %v", err)
		return
	}
	admin.Load(config, &wg)
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
