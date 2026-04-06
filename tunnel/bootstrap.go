package tunnel

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"ssh-tunnel/cfg"
	"ssh-tunnel/constants"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"golang.org/x/crypto/ssh"
)

type SSHBootstrapOptions struct {
	ServerIP       string
	ServerSshPort  int
	LoginUser      string
	Password       string
	PrivateKeyPath string
	DialTimeout    time.Duration
}

type SSHBootstrapDetection struct {
	ServerIP                 string `json:"serverIp"`
	ServerSshPort            int    `json:"serverSshPort"`
	LoginUser                string `json:"loginUser"`
	ConfiguredPrivateKeyPath string `json:"configuredPrivateKeyPath"`
	TargetPrivateKeyPath     string `json:"targetPrivateKeyPath"`
	PublicKeyPath            string `json:"publicKeyPath"`
	PrivateKeyExists         bool   `json:"privateKeyExists"`
	PublicKeyExists          bool   `json:"publicKeyExists"`
	KeyPairReady             bool   `json:"keyPairReady"`
	NeedsBootstrap           bool   `json:"needsBootstrap"`
	Message                  string `json:"message"`
}

type SSHBootstrapResult struct {
	ServerIP               string `json:"serverIp"`
	ServerSshPort          int    `json:"serverSshPort"`
	LoginUser              string `json:"loginUser"`
	PrivateKeyPath         string `json:"privateKeyPath"`
	PublicKeyPath          string `json:"publicKeyPath"`
	KeyGenerated           bool   `json:"keyGenerated"`
	AuthorizedKeyInstalled bool   `json:"authorizedKeyInstalled"`
	VerifiedWithPublicKey  bool   `json:"verifiedWithPublicKey"`
	Message                string `json:"message"`
}

func DefaultBootstrapPrivateKeyPath() string {
	if strings.TrimSpace(constants.ConfigFilePath) != "" {
		return filepath.Join(filepath.Dir(constants.ConfigFilePath), "keys", "id_ed25519")
	}

	if runtime.GOOS == "windows" {
		return filepath.Join(constants.WindowsStateDir, "keys", "id_ed25519")
	}

	return filepath.Join(filepath.Dir(constants.UnixConfigPath), "keys", "id_ed25519")
}

func defaultBootstrapPrivateKeyPathForConfig(configPath string) string {
	trimmed := strings.TrimSpace(configPath)
	if trimmed == "" {
		return DefaultBootstrapPrivateKeyPath()
	}
	return filepath.Join(filepath.Dir(filepath.Clean(trimmed)), "keys", "id_ed25519")
}

func DetectSSHBootstrap(appConfig *cfg.AppConfig, targetPrivateKeyPath string) SSHBootstrapDetection {
	configuredPath := ""
	serverIP := ""
	serverPort := 22
	loginUser := "root"

	if appConfig != nil {
		configuredPath = strings.TrimSpace(appConfig.SshPrivateKeyPath.GetValue())
		serverIP = strings.TrimSpace(appConfig.ServerIp.GetValue())
		if appConfig.ServerSshPort.GetValue() > 0 {
			serverPort = appConfig.ServerSshPort.GetValue()
		}
		if strings.TrimSpace(appConfig.LoginUser.GetValue()) != "" {
			loginUser = strings.TrimSpace(appConfig.LoginUser.GetValue())
		}
	}

	resolvedPath := resolveBootstrapPrivateKeyPath(targetPrivateKeyPath)
	publicKeyPath := resolvedPath + ".pub"
	privateExists := fileExists(resolvedPath)
	publicExists := fileExists(publicKeyPath)
	keyPairReady := privateExists && publicExists

	message := "服务专用密钥不存在，需要初始化"
	if keyPairReady {
		message = "服务专用密钥已就绪，可直接执行免密验证或重建授权"
	} else if privateExists {
		message = "检测到私钥存在但公钥缺失，将在初始化时自动补齐公钥"
	}

	return SSHBootstrapDetection{
		ServerIP:                 serverIP,
		ServerSshPort:            serverPort,
		LoginUser:                loginUser,
		ConfiguredPrivateKeyPath: configuredPath,
		TargetPrivateKeyPath:     resolvedPath,
		PublicKeyPath:            publicKeyPath,
		PrivateKeyExists:         privateExists,
		PublicKeyExists:          publicExists,
		KeyPairReady:             keyPairReady,
		NeedsBootstrap:           true,
		Message:                  message,
	}
}

func BootstrapPasswordlessSSH(options SSHBootstrapOptions) (SSHBootstrapResult, error) {
	options.ServerIP = strings.TrimSpace(options.ServerIP)
	options.LoginUser = strings.TrimSpace(options.LoginUser)
	options.PrivateKeyPath = resolveBootstrapPrivateKeyPath(options.PrivateKeyPath)

	if options.ServerIP == "" {
		return SSHBootstrapResult{}, fmt.Errorf("server ip 不能为空")
	}
	if options.ServerSshPort <= 0 {
		options.ServerSshPort = 22
	}
	if options.LoginUser == "" {
		return SSHBootstrapResult{}, fmt.Errorf("login user 不能为空")
	}
	if options.Password == "" {
		return SSHBootstrapResult{}, fmt.Errorf("password 不能为空")
	}
	if options.DialTimeout <= 0 {
		options.DialTimeout = 5 * time.Second
	}

	publicKeyPath := options.PrivateKeyPath + ".pub"
	result := SSHBootstrapResult{
		ServerIP:       options.ServerIP,
		ServerSshPort:  options.ServerSshPort,
		LoginUser:      options.LoginUser,
		PrivateKeyPath: options.PrivateKeyPath,
		PublicKeyPath:  publicKeyPath,
	}

	signer, authorizedKey, keyGenerated, err := ensureBootstrapKeyPair(options.PrivateKeyPath)
	if err != nil {
		return result, err
	}
	result.KeyGenerated = keyGenerated

	passwordClient, err := dialSSHWithAuth(
		options.LoginUser,
		fmt.Sprintf("%s:%d", options.ServerIP, options.ServerSshPort),
		[]ssh.AuthMethod{ssh.Password(options.Password)},
		options.DialTimeout,
	)
	if err != nil {
		return result, fmt.Errorf("首次密码登录失败: %w", err)
	}

	if err := installAuthorizedKey(passwordClient, authorizedKey); err != nil {
		_ = passwordClient.Close()
		return result, err
	}
	_ = passwordClient.Close()
	result.AuthorizedKeyInstalled = true

	publicKeyClient, err := dialSSHWithAuth(
		options.LoginUser,
		fmt.Sprintf("%s:%d", options.ServerIP, options.ServerSshPort),
		[]ssh.AuthMethod{ssh.PublicKeys(signer)},
		options.DialTimeout,
	)
	if err != nil {
		return result, fmt.Errorf("公钥免密验证失败: %w", err)
	}
	_ = publicKeyClient.Close()
	result.VerifiedWithPublicKey = true
	result.Message = "SSH 免密初始化完成"

	return result, nil
}

func TryRunBootstrapCLI(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (bool, int) {
	if len(args) == 0 || args[0] != "bootstrap" {
		return false, 0
	}

	flagSet := pflag.NewFlagSet("bootstrap", pflag.ContinueOnError)
	flagSet.SetOutput(stderr)

	var serverIP string
	var serverSshPort int
	var loginUser string
	var configPath string
	var privateKeyPath string
	var passwordStdin bool
	var jsonOutput bool

	flagSet.StringVar(&serverIP, cfg.SERVER_IP_KEY, "", "SSH服务器IP地址")
	flagSet.IntVar(&serverSshPort, cfg.SERVER_SSH_PORT_KEY, 22, "SSH服务器端口")
	flagSet.StringVar(&loginUser, cfg.LOGIN_USER_KEY, "root", "SSH登录用户名")
	flagSet.StringVar(&configPath, "config", "", "配置文件路径，用于推导默认服务密钥目录")
	flagSet.StringVar(&privateKeyPath, cfg.SSH_PRIVATE_KEY_PATH_KEY, "", "服务专用SSH私钥文件路径")
	flagSet.BoolVar(&passwordStdin, "password-stdin", false, "从标准输入读取SSH登录密码")
	flagSet.BoolVar(&jsonOutput, "json", false, "输出JSON结果")

	if err := flagSet.Parse(args[1:]); err != nil {
		fmt.Fprintf(stderr, "bootstrap 参数解析失败: %v\n", err)
		return true, 2
	}

	if !passwordStdin {
		fmt.Fprintln(stderr, "bootstrap 模式必须显式指定 --password-stdin")
		return true, 2
	}

	if strings.TrimSpace(privateKeyPath) == "" {
		privateKeyPath = defaultBootstrapPrivateKeyPathForConfig(configPath)
	}

	passwordBytes, err := io.ReadAll(stdin)
	if err != nil {
		fmt.Fprintf(stderr, "读取密码失败: %v\n", err)
		return true, 1
	}
	password := strings.TrimRight(string(passwordBytes), "\r\n")

	result, err := BootstrapPasswordlessSSH(SSHBootstrapOptions{
		ServerIP:       serverIP,
		ServerSshPort:  serverSshPort,
		LoginUser:      loginUser,
		Password:       password,
		PrivateKeyPath: privateKeyPath,
	})
	if err != nil {
		fmt.Fprintf(stderr, "SSH bootstrap failed: %v\n", err)
		return true, 1
	}

	if jsonOutput {
		encoded, encodeErr := json.Marshal(result)
		if encodeErr != nil {
			fmt.Fprintf(stderr, "结果序列化失败: %v\n", encodeErr)
			return true, 1
		}
		fmt.Fprintln(stdout, string(encoded))
		return true, 0
	}

	fmt.Fprintf(stdout, "SSH bootstrap completed. privateKeyPath=%s publicKeyPath=%s\n", result.PrivateKeyPath, result.PublicKeyPath)
	return true, 0
}

func resolveBootstrapPrivateKeyPath(pathValue string) string {
	trimmed := strings.TrimSpace(pathValue)
	if trimmed == "" {
		return DefaultBootstrapPrivateKeyPath()
	}
	return filepath.Clean(trimmed)
}

func ensureBootstrapKeyPair(privateKeyPath string) (ssh.Signer, string, bool, error) {
	privateKeyPath = filepath.Clean(strings.TrimSpace(privateKeyPath))
	if privateKeyPath == "" {
		return nil, "", false, fmt.Errorf("private key path 不能为空")
	}

	if err := os.MkdirAll(filepath.Dir(privateKeyPath), 0700); err != nil {
		return nil, "", false, fmt.Errorf("创建私钥目录失败: %w", err)
	}

	publicKeyPath := privateKeyPath + ".pub"
	comment := buildAuthorizedKeyComment()

	if fileExists(privateKeyPath) {
		privateBytes, err := os.ReadFile(privateKeyPath)
		if err != nil {
			return nil, "", false, fmt.Errorf("读取私钥失败: %w", err)
		}
		rawKey, err := ssh.ParseRawPrivateKey(privateBytes)
		if err != nil {
			return nil, "", false, fmt.Errorf("解析现有私钥失败: %w", err)
		}
		signer, err := ssh.NewSignerFromKey(rawKey)
		if err != nil {
			return nil, "", false, fmt.Errorf("创建SSH signer失败: %w", err)
		}
		authorizedKey := formatAuthorizedKey(signer.PublicKey(), comment)
		if !fileExists(publicKeyPath) {
			if err := os.WriteFile(publicKeyPath, []byte(authorizedKey+"\n"), 0644); err != nil {
				return nil, "", false, fmt.Errorf("补写公钥失败: %w", err)
			}
		}
		_ = os.Chmod(privateKeyPath, 0600)
		return signer, authorizedKey, false, nil
	}

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", false, fmt.Errorf("生成ed25519私钥失败: %w", err)
	}

	pkcs8Bytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, "", false, fmt.Errorf("序列化私钥失败: %w", err)
	}

	privatePEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8Bytes})
	if err := os.WriteFile(privateKeyPath, privatePEM, 0600); err != nil {
		return nil, "", false, fmt.Errorf("写入私钥失败: %w", err)
	}

	rawKey, err := ssh.ParseRawPrivateKey(privatePEM)
	if err != nil {
		return nil, "", false, fmt.Errorf("解析新私钥失败: %w", err)
	}
	sshSigner, err := ssh.NewSignerFromKey(rawKey)
	if err != nil {
		return nil, "", false, fmt.Errorf("创建新SSH signer失败: %w", err)
	}

	authorizedKey := formatAuthorizedKey(sshSigner.PublicKey(), comment)
	if err := os.WriteFile(publicKeyPath, []byte(authorizedKey+"\n"), 0644); err != nil {
		return nil, "", false, fmt.Errorf("写入公钥失败: %w", err)
	}

	return sshSigner, authorizedKey, true, nil
}

func installAuthorizedKey(client *ssh.Client, authorizedKey string) error {
	command := fmt.Sprintf(
		"umask 077; mkdir -p ~/.ssh; chmod 700 ~/.ssh; touch ~/.ssh/authorized_keys; chmod 600 ~/.ssh/authorized_keys; grep -qxF %s ~/.ssh/authorized_keys || printf '%%s\\n' %s >> ~/.ssh/authorized_keys",
		shellQuote(authorivedKeyLine(authorizedKey)),
		shellQuote(authorivedKeyLine(authorizedKey)),
	)

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("创建SSH session失败: %w", err)
	}
	defer session.Close()

	output, err := session.CombinedOutput(command)
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message != "" {
			return fmt.Errorf("写入远端authorized_keys失败: %v: %s", err, message)
		}
		return fmt.Errorf("写入远端authorized_keys失败: %w", err)
	}

	return nil
}

func authorivedKeyLine(value string) string {
	return strings.TrimSpace(value)
}

func dialSSHWithAuth(user string, address string, auth []ssh.AuthMethod, timeout time.Duration) (*ssh.Client, error) {
	return ssh.Dial("tcp", address, &ssh.ClientConfig{
		User:            user,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	})
}

func buildAuthorizedKeyComment() string {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		return "ssh-tunnel"
	}
	return fmt.Sprintf("ssh-tunnel@%s", strings.TrimSpace(hostname))
}

func formatAuthorizedKey(publicKey ssh.PublicKey, comment string) string {
	base := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(publicKey)))
	comment = strings.TrimSpace(comment)
	if comment == "" {
		return base
	}
	return base + " " + comment
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func fileExists(pathValue string) bool {
	_, err := os.Stat(pathValue)
	return err == nil
}