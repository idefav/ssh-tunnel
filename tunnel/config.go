package tunnel

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"path"
	"ssh-tunnel/cfg"
	"ssh-tunnel/safe"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"golang.org/x/crypto/ssh"
)

var DefaultSshTunnel = Tunnel{}

func Load(config *cfg.AppConfig, wg *sync.WaitGroup) error {
	DefaultSshTunnel.SetAppConfig(config)
	if err := DefaultSshTunnel.RefreshRuntimeConfigFromAppConfig(); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	DefaultSshTunnel.SetTunnelContext(ctx)

	if config.EnableSocks5.GetValue() {
		DefaultSshTunnel.enableSocks5 = config.EnableSocks5.GetValue()
		DefaultSshTunnel.localAddress = config.LocalAddress.GetValue()
	}

	if config.EnableHttp.GetValue() {
		DefaultSshTunnel.enableHttp = config.EnableHttp.GetValue()
		DefaultSshTunnel.httpLocalAddress = config.HttpLocalAddress.GetValue()
		DefaultSshTunnel.httpBasicUserName = config.HttpBasicUserName.GetValue()
		DefaultSshTunnel.httpBasicPassword = config.HttpBasicPassword.GetValue()
		DefaultSshTunnel.enableHttpBasic = config.HttpBasicAuthEnable.GetValue()
		DefaultSshTunnel.enableHttpOverSSH = config.EnableHttpOverSSH.GetValue()
		DefaultSshTunnel.enableHttpDomainFilter = config.EnableHttpDomainFilter.GetValue()

		if config.EnableHttpDomainFilter.GetValue() && config.HttpDomainFilterFilePath.GetValue() != "" {
			safe.GO(func() {
				err2 := domainFilterFileWatcher(config.HttpDomainFilterFilePath.GetValue(), &DefaultSshTunnel)
				if err2 != nil {
					log.Printf("Domain filter file watcher error: %v", err2)
				}
			})
		}
	}

	safe.GO(func() {
		sigc := make(chan os.Signal, 1)
		signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
		log.Printf("received %v - initiating shutdown", <-sigc)
		cancel()
	})

	log.Printf("%s starting", path.Base(os.Args[0]))
	defer log.Printf("%s shutdown", path.Base(os.Args[0]))
	if DefaultSshTunnel.enableSocks5 {
		wg.Add(1)
		safe.GO(func() {
			defer wg.Done()
			DefaultSshTunnel.bindSocks5Tunnel(ctx, wg)
		})
	}

	if DefaultSshTunnel.enableHttp {
		wg.Add(1)
		safe.GO(func() {
			defer wg.Done()
			DefaultSshTunnel.bindHttpTunnel(ctx, wg)
		})
	}

	// need open ssh tunnel
	if DefaultSshTunnel.enableSocks5 || DefaultSshTunnel.enableHttpOverSSH {
		safe.GO(func() {
			connCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			safe.GO(func() {
				<-connCtx.Done()
			})
			for {
				if connCtx.Err() != nil {
					return
				}

				if DefaultSshTunnel.currentSSHClient() != nil {
					time.Sleep(200 * time.Millisecond)
					continue
				}

				DefaultSshTunnel.ReconnectSSHWithSource(connCtx, "bootstrap-loop")

				retryInterval := DefaultSshTunnel.retryInterval
				if retryInterval <= 0 {
					retryInterval = time.Second
				}
				select {
				case <-connCtx.Done():
					return
				case <-time.After(retryInterval):
				}
			}

		})
	}

	return nil
}

func (t *Tunnel) RefreshRuntimeConfigFromAppConfig() error {
	config := t.AppConfig()
	if config == nil {
		return fmt.Errorf("app config is nil")
	}

	t.enableSocks5 = config.EnableSocks5.GetValue()
	t.enableHttp = config.EnableHttp.GetValue()
	t.enableHttpBasic = config.HttpBasicAuthEnable.GetValue()
	t.enableHttpOverSSH = config.EnableHttpOverSSH.GetValue()
	t.enableHttpDomainFilter = config.EnableHttpDomainFilter.GetValue()
	t.httpLocalAddress = config.HttpLocalAddress.GetValue()
	t.httpBasicUserName = config.HttpBasicUserName.GetValue()
	t.httpBasicPassword = config.HttpBasicPassword.GetValue()
	t.serverAddress = config.ServerIp.GetValue() + ":" + strconv.Itoa(config.ServerSshPort.GetValue())
	t.localAddress = config.LocalAddress.GetValue()
	t.user = config.LoginUser.GetValue()
	keepAliveInterval := config.SSHKeepAliveIntervalSec.GetValue()
	if keepAliveInterval <= 0 {
		keepAliveInterval = 2
	}
	keepAliveCountMax := config.SSHKeepAliveCountMax.GetValue()
	if keepAliveCountMax <= 0 {
		keepAliveCountMax = 2
	}
	t.keepAlive = KeepAliveConfig{Interval: uint(keepAliveInterval), CountMax: uint(keepAliveCountMax)}
	t.retryInterval = time.Duration(config.RetryIntervalSec.GetValue()) * time.Second
	t.sshDialTimeout = time.Duration(config.SSHDialTimeoutSec.GetValue()) * time.Second
	t.sshDestTimeout = time.Duration(config.SSHDestDialTimeoutSec.GetValue()) * time.Second
	t.reconnectMaxRetries = config.SSHReconnectMaxRetries.GetValue()
	t.reconnectMaxInterval = time.Duration(config.SSHReconnectMaxIntervalSec.GetValue()) * time.Second
	t.hostKeys = ssh.InsecureIgnoreHostKey()

	if t.enableSocks5 || t.enableHttpOverSSH {
		b, err := ioutil.ReadFile(config.SshPrivateKeyPath.GetValue())
		if err != nil {
			log.Printf("Failed to read private key file: %v", err)
			return err
		}
		k, err := ssh.ParsePrivateKey(b)
		if err != nil {
			log.Printf("Failed to parse private key: %v", err)
			return err
		}
		t.auth = []ssh.AuthMethod{ssh.PublicKeys(k)}
	}

	return nil
}

func domainFilterFileWatcher(filePath string, tunnel *Tunnel) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	configPath := path.Dir(filePath)
	err = watcher.Add(configPath)
	if err != nil {
		return err
	}

	changed := make(chan bool)
	done := make(chan bool)

	safe.GO(func() {
		changed <- true
		defer close(done)
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				if event.Name != filePath {
					continue
				}
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
					log.Println("file modified", event.Name)
					changed <- true
				} else if event.Has(fsnotify.Remove) {
					tunnel.domains = make(map[string]bool)
					tunnel.domainMatchCache = make(map[string]bool)
					continue
				}

			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println(err)
			}
		}
	})

	for {
		select {
		case result := <-changed:
			{
				if result == true {
					file, err2 := os.ReadFile(filePath)
					if err2 != nil {
						log.Printf("Failed to read domain filter file: %v", err2)
						continue
					}
					s := string(file)
					log.Printf("domain list loaded!")
					domains := strings.Split(strings.Trim(strings.Trim(strings.Trim(s, "\r"), " "), "\n"), "\n")
					tmpDomains := make(map[string]bool)
					for _, domain := range domains {
						tmp := strings.Trim(strings.ToLower(domain), " ")
						if tmp != "" {
							tmpDomains[tmp] = true
						}
					}
					tunnel.SetDomains(tmpDomains)
					tunnel.domainMatchCache = make(map[string]bool)
				}
			}

		}
	}
}
