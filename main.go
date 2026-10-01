package main

import (
	_ "embed"
	"flag"
	"io"
	"log"
	"os"
	"os/user"
	"path"
	"ssh-tunnel/api/admin"
	"ssh-tunnel/buildinfo"
	"ssh-tunnel/cfg"
	"ssh-tunnel/constants"
	"ssh-tunnel/safe"
	"ssh-tunnel/service/os_config"
	"ssh-tunnel/tunnel"
	"ssh-tunnel/updater"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/kardianos/service"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

var started atomic.Bool

func applyExplicitFlagOverrides(vConfig *viper.Viper, flagSet *pflag.FlagSet, config *cfg.AppConfig) {
	if vConfig == nil || flagSet == nil || config == nil {
		return
	}

	flagSet.Visit(func(f *pflag.Flag) {
		vConfig.Set(f.Name, f.Value.String())
	})
	config.Update()
}

func main() {
	for {
		err := safe.SafeCallWithReturnRecover(runOnce)
		if err == nil {
			return
		}
		if started.Load() {
			log.Printf("panic/error after startup: %v; keep process alive to avoid double-start", err)
			select {}
		}
		log.Printf("main loop recovered/error: %v; restarting in 2s", err)
		time.Sleep(2 * time.Second)
	}
}

func runOnce() error {
	configPath := ""
	filteredArgs := []string{os.Args[0]}
	for i := 1; i < len(os.Args); i++ {
		if strings.HasPrefix(os.Args[i], "--config=") {
			configPath = strings.TrimPrefix(os.Args[i], "--config=")
			log.Println("从命令行参数获取配置文件路径:", configPath)
			continue
		}
		filteredArgs = append(filteredArgs, os.Args[i])
	}

	u, err := user.Current()
	if err != nil {
		homeDir := os.Getenv("USERPROFILE")
		if homeDir == "" {
			homeDir = os.Getenv("HOME")
		}
		if homeDir == "" {
			homeDir = "."
		}
		log.Printf("Failed to get current user (%v), fallback home: %s", err, homeDir)
		u = &user.User{HomeDir: homeDir}
	}

	config := cfg.NewAppConfig()

	// 先读取配置文件, 然后用命令行覆盖
	vConfig := viper.New()
	vConfig.AddConfigPath(".")
	vConfig.AddConfigPath(path.Join(u.HomeDir, ".ssh-tunnel"))

	// 如果显式指定配置文件，则优先使用该路径
	exists, _ := PathExists(configPath)
	if configPath != "" && exists {
		log.Println("使用指定的配置文件:", configPath)
		vConfig.SetConfigFile(configPath)
		vConfig.SetConfigType("properties")
	} else {
		// 设置操作系统特定的默认配置查找路径
		os_config.SetConfig(vConfig)
		vConfig.SetConfigName("config")
		vConfig.SetConfigType("properties")
	}

	// 默认值设置
	vConfig.SetDefault(config.HomeDir.GetKey(), config.HomeDir.GetDefaultValue())
	vConfig.SetDefault(config.SshPrivateKeyPath.GetKey(), config.SshPrivateKeyPath.GetDefaultValue())
	vConfig.SetDefault(config.LoginUser.GetKey(), config.LoginUser.GetDefaultValue())
	vConfig.SetDefault(config.LocalAddress.GetKey(), config.LocalAddress.GetDefaultValue())
	vConfig.SetDefault(config.HttpLocalAddress.GetKey(), config.HttpLocalAddress.GetDefaultValue())
	vConfig.SetDefault(config.EnableHttp.GetKey(), config.EnableHttp.GetDefaultValue())
	vConfig.SetDefault(config.EnableSocks5.GetKey(), config.EnableSocks5.GetDefaultValue())
	vConfig.SetDefault(config.EnableDNS.GetKey(), config.EnableDNS.GetDefaultValue())
	vConfig.SetDefault(config.DNSLocalAddress.GetKey(), config.DNSLocalAddress.GetDefaultValue())
	vConfig.SetDefault(config.DNSUpstreams.GetKey(), config.DNSUpstreams.GetDefaultValue())
	vConfig.SetDefault(config.HttpBasicAuthEnable.GetKey(), config.HttpBasicAuthEnable.GetDefaultValue())
	vConfig.SetDefault(config.EnableHttpOverSSH.GetKey(), config.EnableHttpOverSSH.GetDefaultValue())
	vConfig.SetDefault(config.EnableHttpDomainFilter.GetKey(), config.EnableHttpDomainFilter.GetDefaultValue())
	vConfig.SetDefault(config.HttpDomainFilterFilePath.GetKey(), config.HttpDomainFilterFilePath.GetDefaultValue())
	vConfig.SetDefault(config.EnableAdmin.GetKey(), config.EnableAdmin.GetDefaultValue())
	vConfig.SetDefault(config.AdminAddress.GetKey(), config.AdminAddress.GetDefaultValue())
	vConfig.SetDefault(config.RetryIntervalSec.GetKey(), config.RetryIntervalSec.GetDefaultValue())
	vConfig.SetDefault(config.SSHDialTimeoutSec.GetKey(), config.SSHDialTimeoutSec.GetDefaultValue())
	vConfig.SetDefault(config.SSHDestDialTimeoutSec.GetKey(), config.SSHDestDialTimeoutSec.GetDefaultValue())
	vConfig.SetDefault(config.SSHKeepAliveIntervalSec.GetKey(), config.SSHKeepAliveIntervalSec.GetDefaultValue())
	vConfig.SetDefault(config.SSHKeepAliveCountMax.GetKey(), config.SSHKeepAliveCountMax.GetDefaultValue())
	vConfig.SetDefault(config.SSHReconnectMaxRetries.GetKey(), config.SSHReconnectMaxRetries.GetDefaultValue())
	vConfig.SetDefault(config.SSHReconnectMaxIntervalSec.GetKey(), config.SSHReconnectMaxIntervalSec.GetDefaultValue())
	vConfig.SetDefault(config.SSHPoolSize.GetKey(), config.SSHPoolSize.GetDefaultValue())
	vConfig.SetDefault(config.SSHPoolReplenishIntervalSec.GetKey(), config.SSHPoolReplenishIntervalSec.GetDefaultValue())
	vConfig.SetDefault(config.SSHPoolBalanceStrategy.GetKey(), config.SSHPoolBalanceStrategy.GetDefaultValue())
	vConfig.SetDefault(config.SSHProbeURL.GetKey(), config.SSHProbeURL.GetDefaultValue())
	vConfig.SetDefault(config.SSHProbeURLs.GetKey(), config.SSHProbeURLs.GetDefaultValue())
	vConfig.SetDefault(config.SSHProbeTimeoutSec.GetKey(), config.SSHProbeTimeoutSec.GetDefaultValue())
	vConfig.SetDefault(config.SSHProbeFailureThreshold.GetKey(), config.SSHProbeFailureThreshold.GetDefaultValue())
	vConfig.SetDefault(config.SSHSuspectCooldownSec.GetKey(), config.SSHSuspectCooldownSec.GetDefaultValue())
	vConfig.SetDefault(config.ProxyRetryMaxAttempts.GetKey(), config.ProxyRetryMaxAttempts.GetDefaultValue())
	vConfig.SetDefault(config.ProxyRetryInitialBufferBytes.GetKey(), config.ProxyRetryInitialBufferBytes.GetDefaultValue())
	vConfig.SetDefault(config.LogFilePath.GetKey(), config.LogFilePath.GetDefaultValue())

	// 自动更新默认值
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
		// 配置文件不存在时，使用默认值继续运行
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			log.Printf("Config file not found, using defaults: %v", err)
		} else {
			return err
		}
	}

	// 设置全局配置实例
	cfg.SetConfigInstance(vConfig)

	// 命令行处理
	goFlagSet := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flagSet := pflag.NewFlagSet(os.Args[0], pflag.ContinueOnError)
	flagSet.AddGoFlagSet(goFlagSet)
	flagSet.SetNormalizeFunc(wordSepNormailzeFunc)
	flagSet.String(config.HomeDir.GetKey(), config.HomeDir.GetDefaultValue(), config.HomeDir.GetDescription())
	flagSet.StringP(config.ServerIp.GetKey(), config.ServerIp.GetShorthand(), config.ServerIp.GetDefaultValue(), config.ServerIp.GetDescription())
	flagSet.IntP(config.ServerSshPort.GetKey(), config.ServerSshPort.GetShorthand(), config.ServerSshPort.GetDefaultValue(), config.ServerSshPort.GetDescription())
	flagSet.String(config.SshPrivateKeyPath.GetKey(), config.SshPrivateKeyPath.GetDefaultValue(), config.SshPrivateKeyPath.GetDescription())
	flagSet.StringP(config.LoginUser.GetKey(), config.LoginUser.GetShorthand(), config.LoginUser.GetDefaultValue(), config.LoginUser.GetDescription())
	flagSet.StringP(config.LocalAddress.GetKey(), config.LocalAddress.GetShorthand(), config.LocalAddress.GetDefaultValue(), config.LocalAddress.GetDescription())
	flagSet.String(config.HttpLocalAddress.GetKey(), config.HttpLocalAddress.GetDefaultValue(), config.HttpLocalAddress.GetDescription())
	flagSet.Bool(config.HttpBasicAuthEnable.GetKey(), config.HttpBasicAuthEnable.GetDefaultValue(), config.HttpBasicAuthEnable.GetDescription())
	flagSet.String(config.HttpBasicUserName.GetKey(), config.HttpBasicUserName.GetDefaultValue(), config.HttpBasicUserName.GetDescription())
	flagSet.String(config.HttpBasicPassword.GetKey(), config.HttpBasicPassword.GetDefaultValue(), config.HttpBasicPassword.GetDescription())
	flagSet.Bool(config.EnableHttp.GetKey(), config.EnableHttp.GetDefaultValue(), config.EnableHttp.GetDescription())
	flagSet.Bool(config.EnableSocks5.GetKey(), config.EnableSocks5.GetDefaultValue(), config.EnableSocks5.GetDescription())
	flagSet.Bool(config.EnableDNS.GetKey(), config.EnableDNS.GetDefaultValue(), config.EnableDNS.GetDescription())
	flagSet.String(config.DNSLocalAddress.GetKey(), config.DNSLocalAddress.GetDefaultValue(), config.DNSLocalAddress.GetDescription())
	flagSet.String(config.DNSUpstreams.GetKey(), config.DNSUpstreams.GetDefaultValue(), config.DNSUpstreams.GetDescription())
	flagSet.Bool(config.EnableHttpOverSSH.GetKey(), config.EnableHttpOverSSH.GetDefaultValue(), config.EnableHttpOverSSH.GetDescription())
	flagSet.Bool(config.EnableHttpDomainFilter.GetKey(), config.EnableHttpDomainFilter.GetDefaultValue(), config.EnableHttpDomainFilter.GetDescription())
	flagSet.String(config.HttpDomainFilterFilePath.GetKey(), config.HttpDomainFilterFilePath.GetDefaultValue(), config.HttpDomainFilterFilePath.GetDescription())
	flagSet.Bool(config.EnableAdmin.GetKey(), config.EnableAdmin.GetDefaultValue(), config.EnableAdmin.GetDescription())
	flagSet.String(config.AdminAddress.GetKey(), config.AdminAddress.GetDefaultValue(), config.AdminAddress.GetDescription())
	flagSet.Int(config.RetryIntervalSec.GetKey(), config.RetryIntervalSec.GetDefaultValue(), config.RetryIntervalSec.GetDescription())
	flagSet.Int(config.SSHDialTimeoutSec.GetKey(), config.SSHDialTimeoutSec.GetDefaultValue(), config.SSHDialTimeoutSec.GetDescription())
	flagSet.Int(config.SSHDestDialTimeoutSec.GetKey(), config.SSHDestDialTimeoutSec.GetDefaultValue(), config.SSHDestDialTimeoutSec.GetDescription())
	flagSet.Int(config.SSHKeepAliveIntervalSec.GetKey(), config.SSHKeepAliveIntervalSec.GetDefaultValue(), config.SSHKeepAliveIntervalSec.GetDescription())
	flagSet.Int(config.SSHKeepAliveCountMax.GetKey(), config.SSHKeepAliveCountMax.GetDefaultValue(), config.SSHKeepAliveCountMax.GetDescription())
	flagSet.Int(config.SSHReconnectMaxRetries.GetKey(), config.SSHReconnectMaxRetries.GetDefaultValue(), config.SSHReconnectMaxRetries.GetDescription())
	flagSet.Int(config.SSHReconnectMaxIntervalSec.GetKey(), config.SSHReconnectMaxIntervalSec.GetDefaultValue(), config.SSHReconnectMaxIntervalSec.GetDescription())
	flagSet.Int(config.SSHPoolSize.GetKey(), config.SSHPoolSize.GetDefaultValue(), config.SSHPoolSize.GetDescription())
	flagSet.Int(config.SSHPoolReplenishIntervalSec.GetKey(), config.SSHPoolReplenishIntervalSec.GetDefaultValue(), config.SSHPoolReplenishIntervalSec.GetDescription())
	flagSet.String(config.SSHPoolBalanceStrategy.GetKey(), config.SSHPoolBalanceStrategy.GetDefaultValue(), config.SSHPoolBalanceStrategy.GetDescription())
	flagSet.String(config.SSHProbeURL.GetKey(), config.SSHProbeURL.GetDefaultValue(), config.SSHProbeURL.GetDescription())
	flagSet.String(config.SSHProbeURLs.GetKey(), config.SSHProbeURLs.GetDefaultValue(), config.SSHProbeURLs.GetDescription())
	flagSet.Int(config.SSHProbeTimeoutSec.GetKey(), config.SSHProbeTimeoutSec.GetDefaultValue(), config.SSHProbeTimeoutSec.GetDescription())
	flagSet.Int(config.SSHProbeFailureThreshold.GetKey(), config.SSHProbeFailureThreshold.GetDefaultValue(), config.SSHProbeFailureThreshold.GetDescription())
	flagSet.Int(config.SSHSuspectCooldownSec.GetKey(), config.SSHSuspectCooldownSec.GetDefaultValue(), config.SSHSuspectCooldownSec.GetDescription())
	flagSet.Int(config.ProxyRetryMaxAttempts.GetKey(), config.ProxyRetryMaxAttempts.GetDefaultValue(), config.ProxyRetryMaxAttempts.GetDescription())
	flagSet.Int(config.ProxyRetryInitialBufferBytes.GetKey(), config.ProxyRetryInitialBufferBytes.GetDefaultValue(), config.ProxyRetryInitialBufferBytes.GetDescription())
	flagSet.String(config.LogFilePath.GetKey(), config.LogFilePath.GetDefaultValue(), config.LogFilePath.GetDescription())

	if err := flagSet.Parse(filteredArgs[1:]); err != nil {
		return err
	}

	if err := vConfig.BindPFlags(flagSet); err != nil {
		return err
	}

	// 保存配置文件路径到常量
	constants.ConfigFilePath = vConfig.ConfigFileUsed()

	// 从viper 更新配置数据
	config.Update()
	config.AutoUpdateCurrentVersion.SetValue(buildinfo.CurrentVersion())
	if err := cfg.EnsureAndApplyActiveProfile(config); err != nil {
		log.Printf("apply active profile failed: %v", err)
	}
	applyExplicitFlagOverrides(vConfig, flagSet, config)
	if _, err := updater.SyncRuntimeState(config.HomeDir.GetValue(), buildinfo.CurrentVersion()); err != nil {
		log.Printf("sync update runtime state failed: %v", err)
	}

	log.Println("成功读取配置文件:", vConfig.ConfigFileUsed())

	vConfig.WatchConfig()
	vConfig.OnConfigChange(func(e fsnotify.Event) {
		log.Println("config file changed:", e.Name)
		if err := vConfig.ReadInConfig(); err != nil {
			log.Printf("Failed to reload config file: %v", err)
			return
		}
		config.Update()
		config.AutoUpdateCurrentVersion.SetValue(buildinfo.CurrentVersion())
		if err := cfg.EnsureAndApplyActiveProfile(config); err != nil {
			log.Printf("apply active profile on reload failed: %v", err)
		}
		applyExplicitFlagOverrides(vConfig, flagSet, config)
	})

	// 非服务管理器模式下，读取配置文件后，覆盖配置项的值
	if service.Interactive() {
		logFile, err := os.OpenFile(config.LogFilePath.GetValue(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			log.Printf("open log file failed, err: %v, path: %s", err, config.LogFilePath.GetValue())
			// 日志文件不可用时，继续输出到stdout
		} else {
			mw := io.MultiWriter(logFile, os.Stdout)
			log.SetOutput(mw)
			log.SetFlags(log.Llongfile | log.Lmicroseconds | log.Ldate)
		}
	}

	log.Println("starting ..., userHomeDir: ", u.HomeDir)
	log.Println("current user: ", u.Username)
	log.Println("userHome: ", u.HomeDir)

	var wg sync.WaitGroup

	// 初始化自动更新器
	updater.InitializeUpdater()

	err = tunnel.Load(config, &wg)
	if err != nil {
		return err
	}
	admin.Load(config, &wg)
	started.Store(true)
	wg.Wait()
	return nil
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
