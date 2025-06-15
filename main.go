package main

import (
	_ "embed"
	"flag"
	"fmt"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"io"
	"log"
	"os"
	"os/user"
	"path"
	"runtime"
	"ssh-tunnel/api/admin"
	"ssh-tunnel/cfg"
	"ssh-tunnel/tunnel"
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

	config := cfg.AppConfig{}

	// 先读取配置文件, 然后用命令行覆盖
	vConfig := viper.New()
	vConfig.AddConfigPath(".")
	vConfig.AddConfigPath(path.Join(u.HomeDir, ".ssh-tunnel"))
	// 判断操作系统类型
	osType := runtime.GOOS
	switch osType {
	case "windows":
		// Windows 系统特定配置
		vConfig.AddConfigPath(path.Join("C:\\ssh-tunnel", ".ssh-tunnel"))
	case "darwin":
		// macOS 系统特定配置
		vConfig.AddConfigPath(path.Join("/Library", "Application Support", "ssh-tunnel"))
	case "linux", "freebsd", "openbsd":
		// Linux/BSD 系统特定配置
		vConfig.AddConfigPath(path.Join("/etc", "config", "ssh-tunnel"))
	default:
		// 其他操作系统使用通用配置
		log.Printf("未知操作系统类型: %s, 使用默认配置", osType)
	}

	vConfig.SetConfigName("config")
	vConfig.SetConfigType("properties")

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

	// 命令行处理
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.CommandLine.SetNormalizeFunc(wordSepNormailzeFunc)
	pflag.String("home.dir", "", "配置文件目录; --home.dir=/path/to/dir")
	pflag.StringP("server.ip", "s", "", "服务器IP地址; --server.ip=")
	pflag.IntP("server.ssh.port", "p", 22, "服务器IP地址; --server.ssh.port=your_server_port, -s=your_server_port")
	pflag.String("ssh.path.private_key", "", "私钥地址; --ssh.path.private_key=/path/to/private_key, -pk=/path/to/private_key")
	pflag.String("ssh.path.known_hosts", "", "已知主机地址; --ssh.path.known_hosts=/path/to/known_hosts, -pkh=/path/to/known_hosts")
	pflag.StringP("user", "u", "", "用户名; --user=your_username, -u=your_username")
	pflag.StringP("local.addr", "l", "", "本地地址; --local.addr=xx.xx.xx.xx:port, -l=xx.xx.xx.xx:port")
	pflag.String("http.local.addr", "", "Http监听地址; --http.local.addr=xx.xx.xx.xx:port")
	pflag.String("http.basic.username", "", "Basic认证, 用户名; --http.basic.username=your_username")
	pflag.String("http.basic.password", "", "Http Basic认证, 密码; --http.basic.password=your_password")
	pflag.Bool("http.enable", false, "是否开启Http代理; --http.enable=true")
	pflag.Bool("socks5.enable", false, "是否开启Socks5代理; --socks5.enable=true")
	pflag.Bool("http.basic.enable", false, "是否开启Http的Basic认证; --http.basic.enable=true")
	pflag.Bool("http.over.ssh.enable", false, "是否开启Http Over SSH; --http.over.ssh.enable=true")
	pflag.Bool("http.filter.domain.enable", false, "是否启用Http域名过滤; --http.filter.domain.enable=true")
	pflag.String("http.filter.domain.file-path", "", "过滤http请求; --http.filter.domain.file-path=/path/to/domain.txt")
	pflag.Bool("admin.enable", false, "是否启用Admin页面; --admin.enable=true")
	pflag.String("admin.addr", "", "Admin监听地址; --admin.addr=xx.xx.xx.xx:port")
	pflag.Int("retry.interval.sec", 0, "重试间隔时间(秒); --retry.interval.sec=3")

	pflag.Parse()

	vConfig.BindPFlags(pflag.CommandLine)

	config.HomeDir = vConfig.GetString("home.dir")
	config.ServerIp = vConfig.GetString("server.ip")
	config.ServerSshPort = vConfig.GetInt("server.ssh.port")
	config.SshPrivateKeyPath = vConfig.GetString("ssh.path.private_key")
	config.SshKnownHostsPath = vConfig.GetString("ssh.path.known_hosts")
	config.LoginUser = vConfig.GetString("user")
	config.LocalAddress = vConfig.GetString("local.addr")
	config.HttpLocalAddress = vConfig.GetString("http.local.addr")
	config.HttpBasicUserName = vConfig.GetString("http.basic.username")
	config.HttpBasicPassword = vConfig.GetString("http.basic.password")
	config.EnableHttp = vConfig.GetBool("http.enable")
	config.EnableSocks5 = vConfig.GetBool("socks5.enable")
	config.HttpBasicAuthEnable = vConfig.GetBool("http.basic.enable")
	config.EnableHttpOverSSH = vConfig.GetBool("http.over.ssh.enable")
	config.EnableHttpDomainFilter = vConfig.GetBool("http.filter.domain.enable")
	config.HttpDomainFilterFilePath = vConfig.GetString("http.filter.domain.file-path")
	config.EnableAdmin = vConfig.GetBool("admin.enable")
	config.AdminAddress = vConfig.GetString("admin.addr")
	config.RetryIntervalSec = vConfig.GetInt("retry.interval.sec")

	u, err = user.Current()
	if err != nil {
		log.Fatal(err)
	}

	var userHomeDir string
	userHomeDir, err = os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}

	config.HomeDir = u.HomeDir

	if strings.Contains(u.HomeDir, "system32") {
		config.HomeDir = "C:\\ssh-tunnel"
		os.MkdirAll(config.HomeDir, 0755)
	}
	userHomeDir = config.HomeDir
	config.LogFilePath = path.Join(userHomeDir, ".ssh-tunnel", "console.log")

	if !strings.Contains(u.HomeDir, "system32") {

		logFile, err := os.OpenFile(config.LogFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			fmt.Println("open log file failed, err:", err)
			return
		}

		// 替换原来的log.SetOutput(logFile)为：
		mw := io.MultiWriter(logFile, os.Stdout)
		log.SetOutput(mw)
		log.SetFlags(log.Llongfile | log.Lmicroseconds | log.Ldate)
	}

	log.Println("starting ..., userHomeDir: ", userHomeDir)
	log.Println("current user: ", u.Username)
	log.Println("userHome: ", u.HomeDir)

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

func wordSepNormailzeFunc(f *pflag.FlagSet, name string) pflag.NormalizedName {
	from := []string{"-", "_"}
	to := "."
	for _, sep := range from {
		name = strings.Replace(name, sep, to, -1)
	}
	return pflag.NormalizedName(name)
}
