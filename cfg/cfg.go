package cfg

type AppConfig struct {
	HomeDir                  string
	ServerIp                 string
	ServerSshPort            int
	SshPrivateKeyPath        string
	SshKnownHostsPath        string
	LoginUser                string
	LocalAddress             string
	HttpLocalAddress         string
	HttpBasicAuthEnable      bool
	HttpBasicUserName        string
	HttpBasicPassword        string
	EnableHttp               bool
	EnableSocks5             bool
	EnableHttpOverSSH        bool
	EnableHttpDomainFilter   bool
	HttpDomainFilterFilePath string
	EnableAdmin              bool
	AdminAddress             string
	RetryIntervalSec         int
}
