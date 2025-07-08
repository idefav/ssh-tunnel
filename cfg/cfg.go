package cfg

import (
	"fmt"
	"github.com/spf13/viper"
	"log"
	"os"
	"os/user"
	"path"
	"runtime"
	"sync"
)

var (
	configInstance    *viper.Viper
	configOnce        sync.Once
	appConfigInstance *AppConfig
	appConfigOnce     sync.Once
)

// GetConfigInstance 获取全局配置实例
func GetConfigInstance() *viper.Viper {
	return configInstance
}

// SetConfigInstance 设置全局配置实例
func SetConfigInstance(v *viper.Viper) {
	configOnce.Do(func() {
		configInstance = v
	})
}

// SaveConfig 保存配置到文件
func SaveConfig() error {
	if configInstance == nil {
		return fmt.Errorf("配置实例未初始化")
	}
	return configInstance.WriteConfig()
}

// UpdateConfigValue 更新配置项
func UpdateConfigValue(key string, value interface{}) error {
	if configInstance == nil {
		return fmt.Errorf("配置实例未初始化")
	}

	configInstance.Set(key, value)
	return SaveConfig()
}

func NewAppConfig() *AppConfig {
	appConfigOnce.Do(func() {
		u, err := user.Current()
		if err != nil {
			log.Printf("Failed to get current user: %v", err)
			// 使用默认路径作为fallback
			defaultHomeDir := ""
			if runtime.GOOS == "windows" {
				defaultHomeDir = os.Getenv("USERPROFILE")
			} else {
				defaultHomeDir = os.Getenv("HOME")
			}
			if defaultHomeDir == "" {
				defaultHomeDir = "."
			}
			appConfigInstance = &AppConfig{
				HomeDir:                  NewConfigItem(HOME_DIR_KEY, "", path.Join(defaultHomeDir, APP_NAME_HIDE), "配置文件存储目录", ""),
				ServerIp:                 NewConfigItem(SERVER_IP_KEY, "s", "", "服务器IP地址", ""),
				ServerSshPort:            NewConfigItem(SERVER_SSH_PORT_KEY, "p", 22, "SSH服务器端口", 22),
				SshPrivateKeyPath:        NewConfigItem(SSH_PRIVATE_KEY_PATH_KEY, "", path.Join(defaultHomeDir, ".ssh/id_rsa"), "SSH私钥文件路径", ""),
				SshKnownHostsPath:        NewConfigItem(SSH_KNOWN_HOSTS_PATH_KEY, "", path.Join(defaultHomeDir, ".ssh/known_hosts"), "SSH已知主机文件路径", ""),
				LoginUser:                NewConfigItem(LOGIN_USER_KEY, "u", "root", "SSH登录用户名", ""),
				LocalAddress:             NewConfigItem(LOCAL_ADDRESS_KEY, "l", "0.0.0.0:1081", "本地地址", ""),
				HttpLocalAddress:         NewConfigItem(HTTP_LOCAL_ADDRESS_KEY, "", "0.0.0.0:1082", "HTTP本地地址", ""),
				HttpBasicAuthEnable:      NewConfigItem(HTTP_BASIC_AUTH_ENABLE_KEY, "", false, "是否启用HTTP基本认证", false),
				HttpBasicUserName:        NewConfigItem(HTTP_BASIC_USER_NAME_KEY, "", "", "HTTP基本认证用户名", ""),
				HttpBasicPassword:        NewConfigItem(HTTP_BASIC_PASSWORD_KEY, "", "", "HTTP基本认证密码", ""),
				EnableHttp:               NewConfigItem(ENABLE_HTTP_KEY, "", false, "开启Http代理", false),
				EnableSocks5:             NewConfigItem(ENABLE_SOCKS5_KEY, "", true, "开启Socks5代理", false),
				EnableHttpOverSSH:        NewConfigItem(ENABLE_HTTP_OVER_SSH_KEY, "", false, "开启HTTP Over SSH", false),
				EnableHttpDomainFilter:   NewConfigItem(ENABLE_HTTP_DOMAIN_FILTER_KEY, "", false, "启用HTTP域名过滤", false),
				HttpDomainFilterFilePath: NewConfigItem(HTTP_DOMAIN_FILTER_FILE_PATH_KEY, "", path.Join(defaultHomeDir, APP_NAME_HIDE, "domain.txt"), "HTTP域名过滤文件路径", ""),
				EnableAdmin:              NewConfigItem(ENABLE_ADMIN_KEY, "", true, "开启管理页面", true),
				AdminAddress:             NewConfigItem(ADMIN_ADDRESS_KEY, "", ":1083", "管理页面监听地址", ""),
				RetryIntervalSec:         NewConfigItem(RETRY_INTERVAL_SEC_KEY, "", 3, "重试间隔时间(秒)", 3),
				LogFilePath:              NewConfigItem(LOG_FILE_PATH_KEY, "", path.Join(defaultHomeDir, APP_NAME_HIDE, "console.log"), "日志文件路径", ""),

				// 自动更新配置
				AutoUpdateEnabled:        NewConfigItem(AUTO_UPDATE_ENABLED_KEY, "", false, "是否启用自动更新", false),
				AutoUpdateOwner:          NewConfigItem(AUTO_UPDATE_OWNER_KEY, "", "idefav", "GitHub仓库所有者", ""),
				AutoUpdateRepo:           NewConfigItem(AUTO_UPDATE_REPO_KEY, "", "ssh-tunnel", "GitHub仓库名称", ""),
				AutoUpdateCurrentVersion: NewConfigItem(AUTO_UPDATE_CURRENT_VERSION_KEY, "", "v1.0.0", "当前版本号", ""),
				AutoUpdateCheckInterval:  NewConfigItem(AUTO_UPDATE_CHECK_INTERVAL_KEY, "", 3600, "检查更新间隔(秒)", 3600),
			}
		} else {
			appConfigInstance = &AppConfig{
				HomeDir:                  NewConfigItem(HOME_DIR_KEY, "", path.Join(u.HomeDir, APP_NAME_HIDE), "配置文件存储目录", ""),
				ServerIp:                 NewConfigItem(SERVER_IP_KEY, "s", "", "服务器IP地址", ""),
				ServerSshPort:            NewConfigItem(SERVER_SSH_PORT_KEY, "p", 22, "SSH服务器端口", 22),
				SshPrivateKeyPath:        NewConfigItem(SSH_PRIVATE_KEY_PATH_KEY, "", path.Join(u.HomeDir, ".ssh/id_rsa"), "SSH私钥文件路径", ""),
				SshKnownHostsPath:        NewConfigItem(SSH_KNOWN_HOSTS_PATH_KEY, "", path.Join(u.HomeDir, ".ssh/known_hosts"), "SSH已知主机文件路径", ""),
				LoginUser:                NewConfigItem(LOGIN_USER_KEY, "u", "root", "SSH登录用户名", ""),
				LocalAddress:             NewConfigItem(LOCAL_ADDRESS_KEY, "l", "0.0.0.0:1081", "本地地址", ""),
				HttpLocalAddress:         NewConfigItem(HTTP_LOCAL_ADDRESS_KEY, "", "0.0.0.0:1082", "HTTP本地地址", ""),
				HttpBasicAuthEnable:      NewConfigItem(HTTP_BASIC_AUTH_ENABLE_KEY, "", false, "是否启用HTTP基本认证", false),
				HttpBasicUserName:        NewConfigItem(HTTP_BASIC_USER_NAME_KEY, "", "", "HTTP基本认证用户名", ""),
				HttpBasicPassword:        NewConfigItem(HTTP_BASIC_PASSWORD_KEY, "", "", "HTTP基本认证密码", ""),
				EnableHttp:               NewConfigItem(ENABLE_HTTP_KEY, "", false, "开启Http代理", false),
				EnableSocks5:             NewConfigItem(ENABLE_SOCKS5_KEY, "", true, "开启Socks5代理", false),
				EnableHttpOverSSH:        NewConfigItem(ENABLE_HTTP_OVER_SSH_KEY, "", false, "开启HTTP Over SSH", false),
				EnableHttpDomainFilter:   NewConfigItem(ENABLE_HTTP_DOMAIN_FILTER_KEY, "", false, "启用HTTP域名过滤", false),
				HttpDomainFilterFilePath: NewConfigItem(HTTP_DOMAIN_FILTER_FILE_PATH_KEY, "", path.Join(u.HomeDir, APP_NAME_HIDE, "domain.txt"), "HTTP域名过滤文件路径", ""),
				EnableAdmin:              NewConfigItem(ENABLE_ADMIN_KEY, "", true, "开启管理页面", true),
				AdminAddress:             NewConfigItem(ADMIN_ADDRESS_KEY, "", ":1083", "管理页面监听地址", ""),
				RetryIntervalSec:         NewConfigItem(RETRY_INTERVAL_SEC_KEY, "", 3, "重试间隔时间(秒)", 3),
				LogFilePath:              NewConfigItem(LOG_FILE_PATH_KEY, "", path.Join(u.HomeDir, APP_NAME_HIDE, "console.log"), "日志文件路径", ""),

				// 自动更新配置
				AutoUpdateEnabled:        NewConfigItem(AUTO_UPDATE_ENABLED_KEY, "", false, "是否启用自动更新", false),
				AutoUpdateOwner:          NewConfigItem(AUTO_UPDATE_OWNER_KEY, "", "idefav", "GitHub仓库所有者", ""),
				AutoUpdateRepo:           NewConfigItem(AUTO_UPDATE_REPO_KEY, "", "ssh-tunnel", "GitHub仓库名称", ""),
				AutoUpdateCurrentVersion: NewConfigItem(AUTO_UPDATE_CURRENT_VERSION_KEY, "", "v1.0.0", "当前版本号", ""),
				AutoUpdateCheckInterval:  NewConfigItem(AUTO_UPDATE_CHECK_INTERVAL_KEY, "", 3600, "检查更新间隔(秒)", 3600),
			
				// 下载代理配置
				DownloadProxyEnabled:     NewConfigItem(DOWNLOAD_PROXY_ENABLED_KEY, "", false, "是否启用下载代理", false),
				DownloadProxyURL:         NewConfigItem(DOWNLOAD_PROXY_URL_KEY, "", "", "下载代理地址", ""),
				DownloadProxyUsername:    NewConfigItem(DOWNLOAD_PROXY_USERNAME_KEY, "", "", "代理用户名", ""),
				DownloadProxyPassword:    NewConfigItem(DOWNLOAD_PROXY_PASSWORD_KEY, "", "", "代理密码", ""),
			}
		}
	})
	return appConfigInstance
}

func (appConfig *AppConfig) Update() {
	config := GetConfigInstance()
	if config == nil {
		log.Println("配置实例未初始化，无法更新配置项")
		return
	}

	// 更新配置项
	// 打印配置内容
	log.Println("更新配置项...")
	for _, key := range config.AllKeys() {
		log.Println("key: ", key, " value: ", config.Get(key))
	}

	appConfigInstance.HomeDir.SetValue(config.GetString(appConfigInstance.HomeDir.Key))
	appConfigInstance.ServerIp.SetValue(config.GetString(appConfigInstance.ServerIp.Key))
	appConfigInstance.ServerSshPort.SetValue(config.GetInt(appConfigInstance.ServerSshPort.Key))
	appConfigInstance.SshPrivateKeyPath.SetValue(config.GetString(appConfigInstance.SshPrivateKeyPath.Key))
	appConfigInstance.SshKnownHostsPath.SetValue(config.GetString(appConfigInstance.SshKnownHostsPath.Key))
	appConfigInstance.LoginUser.SetValue(config.GetString(appConfigInstance.LoginUser.Key))
	appConfigInstance.LocalAddress.SetValue(config.GetString(appConfigInstance.LocalAddress.Key))
	appConfigInstance.HttpLocalAddress.SetValue(config.GetString(appConfigInstance.HttpLocalAddress.Key))
	appConfigInstance.HttpBasicAuthEnable.SetValue(config.GetBool(appConfigInstance.HttpBasicAuthEnable.Key))
	appConfigInstance.HttpBasicUserName.SetValue(config.GetString(appConfigInstance.HttpBasicUserName.Key))
	appConfigInstance.HttpBasicPassword.SetValue(config.GetString(appConfigInstance.HttpBasicPassword.Key))
	appConfigInstance.EnableHttp.SetValue(config.GetBool(appConfigInstance.EnableHttp.Key))
	appConfigInstance.EnableSocks5.SetValue(config.GetBool(appConfigInstance.EnableSocks5.Key))
	appConfigInstance.EnableHttpOverSSH.SetValue(config.GetBool(appConfigInstance.EnableHttpOverSSH.Key))
	appConfigInstance.EnableHttpDomainFilter.SetValue(config.GetBool(appConfigInstance.EnableHttpDomainFilter.Key))
	appConfigInstance.HttpDomainFilterFilePath.SetValue(config.GetString(appConfigInstance.HttpDomainFilterFilePath.Key))
	appConfigInstance.EnableAdmin.SetValue(config.GetBool(appConfigInstance.EnableAdmin.Key))
	appConfigInstance.AdminAddress.SetValue(config.GetString(appConfigInstance.AdminAddress.Key))
	appConfigInstance.RetryIntervalSec.SetValue(config.GetInt(appConfigInstance.RetryIntervalSec.Key))
	appConfigInstance.LogFilePath.SetValue(config.GetString(appConfigInstance.LogFilePath.Key))

	// 更新自动更新配置
	appConfigInstance.AutoUpdateEnabled.SetValue(config.GetBool(appConfigInstance.AutoUpdateEnabled.Key))
	appConfigInstance.AutoUpdateOwner.SetValue(config.GetString(appConfigInstance.AutoUpdateOwner.Key))
	appConfigInstance.AutoUpdateRepo.SetValue(config.GetString(appConfigInstance.AutoUpdateRepo.Key))
	appConfigInstance.AutoUpdateCurrentVersion.SetValue(config.GetString(appConfigInstance.AutoUpdateCurrentVersion.Key))
	appConfigInstance.AutoUpdateCheckInterval.SetValue(config.GetInt(appConfigInstance.AutoUpdateCheckInterval.Key))

	// 更新下载代理配置
	appConfigInstance.DownloadProxyEnabled.SetValue(config.GetBool(appConfigInstance.DownloadProxyEnabled.Key))
	appConfigInstance.DownloadProxyURL.SetValue(config.GetString(appConfigInstance.DownloadProxyURL.Key))
	appConfigInstance.DownloadProxyUsername.SetValue(config.GetString(appConfigInstance.DownloadProxyUsername.Key))
	appConfigInstance.DownloadProxyPassword.SetValue(config.GetString(appConfigInstance.DownloadProxyPassword.Key))

}

const (
	APP_NAME                 = "ssh-tunnel"
	APP_NAME_HIDE            = ".ssh-tunnel"
	HOME_DIR_KEY             = "home.dir"
	SERVER_IP_KEY            = "server.ip"
	SERVER_SSH_PORT_KEY      = "server.ssh.port"
	SSH_PRIVATE_KEY_PATH_KEY = "ssh.private_key_path"
	SSH_KNOWN_HOSTS_PATH_KEY = "ssh.known_hosts_path"
	LOGIN_USER_KEY           = "login.username"
	LOCAL_ADDRESS_KEY        = "local.address"
	HTTP_LOCAL_ADDRESS_KEY   = "http.local.address"

	HTTP_BASIC_AUTH_ENABLE_KEY = "http.basic.enable"
	HTTP_BASIC_USER_NAME_KEY   = "http.basic.username"
	HTTP_BASIC_PASSWORD_KEY    = "http.basic.password"

	ENABLE_HTTP_KEY                  = "http.enable"
	ENABLE_SOCKS5_KEY                = "socks5.enable"
	ENABLE_HTTP_OVER_SSH_KEY         = "http.over-ssh.enable"
	ENABLE_HTTP_DOMAIN_FILTER_KEY    = "http.domain-filter.enable"
	HTTP_DOMAIN_FILTER_FILE_PATH_KEY = "http.domain-filter.file-path"

	ENABLE_ADMIN_KEY  = "admin.enable"
	ADMIN_ADDRESS_KEY = "admin.address"

	RETRY_INTERVAL_SEC_KEY = "retry.interval.sec"
	LOG_FILE_PATH_KEY      = "log.file.path"

	// 自动更新相关配置
	AUTO_UPDATE_ENABLED_KEY         = "auto-update.enabled"
	AUTO_UPDATE_OWNER_KEY           = "auto-update.owner"
	AUTO_UPDATE_REPO_KEY            = "auto-update.repo"
	AUTO_UPDATE_CURRENT_VERSION_KEY = "auto-update.current-version"
	AUTO_UPDATE_CHECK_INTERVAL_KEY  = "auto-update.check-interval"
	
	// 下载代理相关配置
	DOWNLOAD_PROXY_ENABLED_KEY  = "download.proxy.enabled"
	DOWNLOAD_PROXY_URL_KEY      = "download.proxy.url"
	DOWNLOAD_PROXY_USERNAME_KEY = "download.proxy.username"
	DOWNLOAD_PROXY_PASSWORD_KEY = "download.proxy.password"
)

type ConfigItem[T any] struct {
	Key          string
	Shorthand    string
	DefaultValue T
	Description  string
	Value        T
}

func (item *ConfigItem[T]) GetValue() T {
	return item.Value
}

func (item *ConfigItem[T]) SetValue(value T) {
	item.Value = value
	if configInstance != nil {
		configInstance.Set(item.Key, value)
	}
}

func (item *ConfigItem[T]) GetKey() string {
	return item.Key
}

func (item *ConfigItem[T]) GetShorthand() string {
	return item.Shorthand
}

func (item *ConfigItem[T]) GetDefaultValue() T {
	return item.DefaultValue
}

func (item *ConfigItem[T]) GetDescription() string {
	return item.Description
}

// NewConfigItem 创建一个新的配置项
func NewConfigItem[T any](key string, shorthand string, defaultValue T, description string, value T) ConfigItem[T] {
	return ConfigItem[T]{
		Key:          key,
		Shorthand:    shorthand,
		DefaultValue: defaultValue,
		Description:  description,
		Value:        value,
	}
}

type AppConfig struct {
	HomeDir                  ConfigItem[string]
	ServerIp                 ConfigItem[string]
	ServerSshPort            ConfigItem[int]
	SshPrivateKeyPath        ConfigItem[string]
	SshKnownHostsPath        ConfigItem[string]
	LoginUser                ConfigItem[string]
	LocalAddress             ConfigItem[string]
	HttpLocalAddress         ConfigItem[string]
	HttpBasicAuthEnable      ConfigItem[bool]
	HttpBasicUserName        ConfigItem[string]
	HttpBasicPassword        ConfigItem[string]
	EnableHttp               ConfigItem[bool]
	EnableSocks5             ConfigItem[bool]
	EnableHttpOverSSH        ConfigItem[bool]
	EnableHttpDomainFilter   ConfigItem[bool]
	HttpDomainFilterFilePath ConfigItem[string]
	EnableAdmin              ConfigItem[bool]
	AdminAddress             ConfigItem[string]
	RetryIntervalSec         ConfigItem[int]
	LogFilePath              ConfigItem[string]

	// 自动更新配置
	AutoUpdateEnabled        ConfigItem[bool]
	AutoUpdateOwner          ConfigItem[string]
	AutoUpdateRepo           ConfigItem[string]
	AutoUpdateCurrentVersion ConfigItem[string]
	AutoUpdateCheckInterval  ConfigItem[int]
	
	// 下载代理配置
	DownloadProxyEnabled     ConfigItem[bool]
	DownloadProxyURL         ConfigItem[string]
	DownloadProxyUsername    ConfigItem[string]
	DownloadProxyPassword    ConfigItem[string]
}
