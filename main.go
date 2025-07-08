package main

import (
	_ "embed"
	"flag"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"github.com/kardianos/service"
	"github.com/spf13/pflag"
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
	"ssh-tunnel/updater"
	"strings"
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

	config := cfg.NewAppConfig()

	// 先读取配置文件, 然后用命令行覆盖
	vConfig := viper.New()
	vConfig.AddConfigPath(".")
	vConfig.AddConfigPath(path.Join(u.HomeDir, ".ssh-tunnel"))

	// 设置操作系统特定的配置
	os_config.SetConfig(vConfig)

	vConfig.SetConfigName("config")
	vConfig.SetConfigType("properties")

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
	
	// 自动更新默认值
	vConfig.SetDefault(config.AutoUpdateEnabled.GetKey(), config.AutoUpdateEnabled.GetDefaultValue())
	vConfig.SetDefault(config.AutoUpdateOwner.GetKey(), config.AutoUpdateOwner.GetDefaultValue())
	vConfig.SetDefault(config.AutoUpdateRepo.GetKey(), config.AutoUpdateRepo.GetDefaultValue())
	vConfig.SetDefault(config.AutoUpdateCurrentVersion.GetKey(), config.AutoUpdateCurrentVersion.GetDefaultValue())
	vConfig.SetDefault(config.AutoUpdateCheckInterval.GetKey(), config.AutoUpdateCheckInterval.GetDefaultValue())
	
	// 自动更新配置默认值
	vConfig.SetDefault(config.AutoUpdateEnabled.GetKey(), config.AutoUpdateEnabled.GetDefaultValue())
	vConfig.SetDefault(config.AutoUpdateOwner.GetKey(), config.AutoUpdateOwner.GetDefaultValue())
	vConfig.SetDefault(config.AutoUpdateRepo.GetKey(), config.AutoUpdateRepo.GetDefaultValue())
	vConfig.SetDefault(config.AutoUpdateCurrentVersion.GetKey(), config.AutoUpdateCurrentVersion.GetDefaultValue())
	vConfig.SetDefault(config.AutoUpdateCheckInterval.GetKey(), config.AutoUpdateCheckInterval.GetDefaultValue())

	// 环境变量配置
	vConfig.SetEnvPrefix(constants.ENV_PREFIX) // 设置环境变量前缀
	replace := strings.NewReplacer(".", "_")   // 替换点为下划线
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

	// 从viper 更新配置数据
	config.Update()

	log.Println("成功读取配置文件:", vConfig.ConfigFileUsed())

	vConfig.WatchConfig()
	vConfig.OnConfigChange(func(e fsnotify.Event) {
		fmt.Println("config file changed:", e.Name)
		if err := vConfig.ReadInConfig(); err != nil {
			log.Printf("Failed to reload config file: %v", err)
			return
		}
		config.Update()
	})

	// 命令行处理
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.CommandLine.SetNormalizeFunc(wordSepNormailzeFunc)
	pflag.String(config.HomeDir.GetKey(), config.HomeDir.GetDefaultValue(), config.HomeDir.GetDescription())
	pflag.StringP(config.ServerIp.GetKey(), config.ServerIp.GetShorthand(), config.ServerIp.GetDefaultValue(), config.ServerIp.GetDescription())
	pflag.IntP(config.ServerSshPort.GetKey(), config.ServerSshPort.GetShorthand(), config.ServerSshPort.GetDefaultValue(), config.ServerSshPort.GetDescription())
	pflag.String(config.SshPrivateKeyPath.GetKey(), config.SshPrivateKeyPath.GetDefaultValue(), config.SshPrivateKeyPath.GetDescription())
	pflag.String(config.SshKnownHostsPath.GetKey(), config.SshKnownHostsPath.GetDefaultValue(), config.SshKnownHostsPath.GetDescription())
	pflag.StringP(config.LoginUser.GetKey(), config.LoginUser.GetShorthand(), config.LoginUser.GetDefaultValue(), config.LoginUser.GetDescription())
	pflag.StringP(config.LocalAddress.GetKey(), config.LocalAddress.GetShorthand(), config.LocalAddress.GetDefaultValue(), config.LocalAddress.GetDescription())
	pflag.String(config.HttpLocalAddress.GetKey(), config.HttpLocalAddress.GetDefaultValue(), config.HttpLocalAddress.GetDescription())
	pflag.Bool(config.HttpBasicAuthEnable.GetKey(), config.HttpBasicAuthEnable.GetDefaultValue(), config.HttpBasicAuthEnable.GetDescription())
	pflag.String(config.HttpBasicUserName.GetKey(), config.HttpBasicUserName.GetDefaultValue(), config.HttpBasicUserName.GetDescription())
	pflag.String(config.HttpBasicPassword.GetKey(), config.HttpBasicPassword.GetDefaultValue(), config.HttpBasicPassword.GetDescription())
	pflag.Bool(config.EnableHttp.GetKey(), config.EnableHttp.GetDefaultValue(), config.EnableHttp.GetDescription())
	pflag.Bool(config.EnableSocks5.GetKey(), config.EnableSocks5.GetDefaultValue(), config.EnableSocks5.GetDescription())
	pflag.Bool(config.EnableHttpOverSSH.GetKey(), config.EnableHttpOverSSH.GetDefaultValue(), config.EnableHttpOverSSH.GetDescription())
	pflag.Bool(config.EnableHttpDomainFilter.GetKey(), config.EnableHttpDomainFilter.GetDefaultValue(), config.EnableHttpDomainFilter.GetDescription())
	pflag.String(config.HttpDomainFilterFilePath.GetKey(), config.HttpDomainFilterFilePath.GetDefaultValue(), config.HttpDomainFilterFilePath.GetDescription())
	pflag.Bool(config.EnableAdmin.GetKey(), config.EnableAdmin.GetDefaultValue(), config.EnableAdmin.GetDescription())
	pflag.String(config.AdminAddress.GetKey(), config.AdminAddress.GetDefaultValue(), config.AdminAddress.GetDescription())
	pflag.Int(config.RetryIntervalSec.GetKey(), config.RetryIntervalSec.GetDefaultValue(), config.RetryIntervalSec.GetDescription())

	pflag.Parse()

	vConfig.BindPFlags(pflag.CommandLine)

	// 非服务管理器模式下，读取配置文件后，覆盖配置项的值
	if service.Interactive() {
		logFile, err := os.OpenFile(config.LogFilePath.GetValue(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			fmt.Println("open log file failed, err:", err, config.LogFilePath.GetValue())
			return
		}

		// 替换原来的log.SetOutput(logFile)为：
		mw := io.MultiWriter(logFile, os.Stdout)
		log.SetOutput(mw)
		log.SetFlags(log.Llongfile | log.Lmicroseconds | log.Ldate)
	}

	log.Println("starting ..., userHomeDir: ", u.HomeDir)
	log.Println("current user: ", u.Username)
	log.Println("userHome: ", u.HomeDir)

	var wg sync.WaitGroup
	
	// 初始化自动更新器
	updater.InitializeUpdater()
	
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

func wordSepNormailzeFunc(f *pflag.FlagSet, name string) pflag.NormalizedName {
	from := []string{"-", "_"}
	to := "."
	for _, sep := range from {
		name = strings.Replace(name, sep, to, -1)
	}
	return pflag.NormalizedName(name)
}
