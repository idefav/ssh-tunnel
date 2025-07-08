package tunnel

import (
	"context"
	"github.com/fsnotify/fsnotify"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
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
)

var DefaultSshTunnel = Tunnel{}

func Load(config *cfg.AppConfig, wg *sync.WaitGroup) error {
	ctx, cancel := context.WithCancel(context.Background())
	DefaultSshTunnel.SetAppConfig(config)
	if config.EnableSocks5.GetValue() {
		DefaultSshTunnel.enableSocks5 = config.EnableSocks5.GetValue()
		DefaultSshTunnel.serverAddress = config.ServerIp.GetValue() + ":" + strconv.Itoa(config.ServerSshPort.GetValue())
		DefaultSshTunnel.localAddress = config.LocalAddress.GetValue()
		DefaultSshTunnel.keepAlive = KeepAliveConfig{Interval: 30, CountMax: 3}

		var keys []ssh.Signer
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
		keys = append(keys, k)
		auth := []ssh.AuthMethod{ssh.PublicKeys(keys...)}

		hostKeys, err := knownhosts.New(config.SshKnownHostsPath.GetValue())
		if err != nil {
			log.Printf("Failed to load known hosts file: %v", err)
			return err
		}

		DefaultSshTunnel.auth = auth
		DefaultSshTunnel.hostKeys = hostKeys
		DefaultSshTunnel.user = config.LoginUser.GetValue()
		DefaultSshTunnel.retryInterval = time.Duration(config.RetryIntervalSec.GetValue()) * time.Second
	}

	if config.EnableHttp.GetValue() {
		DefaultSshTunnel.enableHttp = config.EnableHttp.GetValue()
		DefaultSshTunnel.httpLocalAddress = config.HttpLocalAddress.GetValue()
		DefaultSshTunnel.httpBasicUserName = config.HttpBasicUserName.GetValue()
		DefaultSshTunnel.httpBasicPassword = config.HttpBasicPassword.GetValue()
		DefaultSshTunnel.enableHttpBasic = config.HttpBasicAuthEnable.GetValue()
		DefaultSshTunnel.enableHttpOverSSH = config.EnableHttpOverSSH.GetValue()
		DefaultSshTunnel.enableHttpDomainFilter = config.EnableHttpDomainFilter.GetValue()

		if config.EnableHttpOverSSH.GetValue() {
			DefaultSshTunnel.serverAddress = config.ServerIp.GetValue() + ":" + strconv.Itoa(config.ServerSshPort.GetValue())
			DefaultSshTunnel.localAddress = config.LocalAddress.GetValue()
			DefaultSshTunnel.keepAlive = KeepAliveConfig{Interval: 30, CountMax: 3}

			var keys []ssh.Signer
			b, err := ioutil.ReadFile(config.SshPrivateKeyPath.GetValue())
			if err != nil {
				log.Printf("Failed to read private key file for HttpOverSSH: %v", err)
				return err
			}
			k, err := ssh.ParsePrivateKey(b)
			if err != nil {
				log.Printf("Failed to parse private key for HttpOverSSH: %v", err)
				return err
			}
			keys = append(keys, k)
			auth := []ssh.AuthMethod{ssh.PublicKeys(keys...)}

			hostKeys, err := knownhosts.New(config.SshKnownHostsPath.GetValue())
			if err != nil {
				log.Printf("Failed to load known hosts file for HttpOverSSH: %v", err)
				return err
			}

			DefaultSshTunnel.auth = auth
			DefaultSshTunnel.hostKeys = hostKeys
			DefaultSshTunnel.user = config.LoginUser.GetValue()
			DefaultSshTunnel.retryInterval = time.Duration(config.RetryIntervalSec.GetValue()) * time.Second
		}

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
			for DefaultSshTunnel.client == nil {
				var once sync.Once
				cl, err := ssh.Dial("tcp", DefaultSshTunnel.serverAddress, &ssh.ClientConfig{
					User:            DefaultSshTunnel.user,
					Auth:            DefaultSshTunnel.auth,
					HostKeyCallback: DefaultSshTunnel.hostKeys,
					Timeout:         5 * time.Second,
				})
				if err != nil {
					once.Do(func() {
						log.Printf("(%v) SSH dial error: %v", DefaultSshTunnel, err)
					})
					continue
				}
				//wg.Add(1)
				DefaultSshTunnel.client = cl
				log.Println("Connected to ssh server")
				// keep alive
				DefaultSshTunnel.keepAliveMonitor(ctx, &once, wg)
				DefaultSshTunnel.client = nil
				log.Printf("SSH Connection Closed!")
				if context.Canceled != nil {
					return
				}
			}

		})
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

	go func() {
		changed <- true
		defer close(done)
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				log.Println(event)
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
	}()

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

	<-done

	return err
}
