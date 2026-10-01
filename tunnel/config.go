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
	"ssh-tunnel/dnsproxy"
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

const trafficFlushInterval = 5 * time.Second

// Shutdown cancels service work and waits for DNS sockets and channels to close.
// Service managers do not necessarily deliver the Unix signals handled by Load.
func (t *Tunnel) Shutdown() {
	t.lifecycleMu.Lock()
	cancel, done := t.shutdown, t.dnsStopped
	t.lifecycleMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func runTrafficStoreLoop(ctx context.Context, store *TrafficStore, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			if err := store.Close(); err != nil {
				log.Printf("关闭流量数据库失败: %v", err)
			}
			return
		case <-ticker.C:
			if err := store.Flush(); err != nil {
				log.Printf("流量统计刷盘失败: %v", err)
			}
		}
	}
}

func Load(config *cfg.AppConfig, wg *sync.WaitGroup) error {
	return loadWithContext(context.Background(), config, wg)
}

func loadWithContext(parent context.Context, config *cfg.AppConfig, wg *sync.WaitGroup) error {
	// Global DNS settings are a startup snapshot; profile changes cannot enable
	// listeners or silently apply settings advertised as requiring a restart.
	DefaultSshTunnel.enableDNS = config.EnableDNS.GetValue()
	if DefaultSshTunnel.enableDNS {
		if err := cfg.ValidateDNSListenAddress(config.DNSLocalAddress.GetValue()); err != nil {
			return err
		}
		servers, err := cfg.ParseDNSUpstreams(config.DNSUpstreams.GetValue())
		if err != nil {
			return err
		}
		DefaultSshTunnel.dnsUpstreams = servers
	}
	DefaultSshTunnel.SetAppConfig(config)
	if err := DefaultSshTunnel.RefreshRuntimeConfigFromAppConfig(); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(parent)
	DefaultSshTunnel.SetTunnelContext(ctx)
	trafficStore, err := OpenTrafficStore()
	if err != nil {
		cancel()
		return err
	}
	DefaultSshTunnel.trafficStore = trafficStore
	// Publish routes before any listener accepts work, and bind both DNS sockets
	// before starting background workers so a bind failure leaves no listeners.
	if err := initProfileRouting(ctx, config); err != nil {
		cancel()
		DefaultSshTunnel.profileTunnelMgr.StopAll()
		_ = trafficStore.Close()
		return err
	}
	var dnsServer *dnsproxy.Server
	dnsStopped := make(chan struct{})
	if DefaultSshTunnel.enableDNS {
		dnsServer, err = dnsproxy.Listen(config.DNSLocalAddress.GetValue(), DefaultSshTunnel.queryDNS)
		if err != nil {
			cancel()
			DefaultSshTunnel.profileTunnelMgr.StopAll()
			_ = trafficStore.Close()
			return fmt.Errorf("DNS监听失败: %w", err)
		}
		wg.Add(1)
		safe.GO(func() {
			defer wg.Done()
			defer close(dnsStopped)
			dnsServer.Serve(ctx)
			if ctx.Err() == nil {
				log.Printf("DNS listener stopped unexpectedly")
				cancel()
			}
		})
		log.Printf("DNS listening on %s (UDP/TCP)", dnsServer.Addr())
	} else {
		close(dnsStopped)
	}
	DefaultSshTunnel.lifecycleMu.Lock()
	DefaultSshTunnel.shutdown, DefaultSshTunnel.dnsStopped = cancel, dnsStopped
	DefaultSshTunnel.lifecycleMu.Unlock()
	wg.Add(1)
	safe.GO(func() {
		defer wg.Done()
		<-ctx.Done()
		DefaultSshTunnel.profileTunnelMgr.StopAll()
		DefaultSshTunnel.DisconnectSSHPool("shutdown")
	})
	wg.Add(1)
	safe.GO(func() {
		defer wg.Done()
		runTrafficStoreLoop(ctx, trafficStore, trafficFlushInterval)
	})

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
		sigc := make(chan os.Signal, 2)
		signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
		defer signal.Stop(sigc)

		var firstSignal os.Signal
		select {
		case firstSignal = <-sigc:
		case <-ctx.Done():
			return
		}
		log.Printf("received %v - initiating shutdown", firstSignal)
		cancel()

		forcedSignal := <-sigc
		log.Printf("received %v during shutdown - forcing exit", forcedSignal)
		os.Exit(1)
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

	// maintain SSH pool
	if DefaultSshTunnel.enableSocks5 || DefaultSshTunnel.enableHttpOverSSH || DefaultSshTunnel.enableDNS {
		wg.Add(1)
		safe.GO(func() {
			defer wg.Done()
			connCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			safe.GO(func() {
				<-connCtx.Done()
			})
			for {
				if connCtx.Err() != nil {
					return
				}

				select {
				case <-connCtx.Done():
					return
				default:
				}
				DefaultSshTunnel.ensurePool(connCtx, "bootstrap-loop")
				DefaultSshTunnel.runtimeMu.RLock()
				interval := DefaultSshTunnel.configuredReplenishInterval()
				DefaultSshTunnel.runtimeMu.RUnlock()
				select {
				case <-connCtx.Done():
					return
				case <-time.After(interval):
				}
			}

		})
	}

	return nil
}

func (t *Tunnel) RefreshRuntimeConfigFromAppConfig() error {
	t.runtimeMu.Lock()
	defer t.runtimeMu.Unlock()
	t.dnsPolicy = nil
	config := t.AppConfig()
	if config == nil {
		return fmt.Errorf("app config is nil")
	}
	identity := fmt.Sprintf("%s\x00%d\x00%s\x00%s", config.ServerIp.GetValue(), config.ServerSshPort.GetValue(), config.LoginUser.GetValue(), config.SshPrivateKeyPath.GetValue())
	if t.sshRuntimeIdentity != "" && t.sshRuntimeIdentity != identity {
		t.DisconnectSSHPool("SSH configuration changed")
	}
	t.sshRuntimeIdentity = identity

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
	t.sshPoolSize = config.SSHPoolSize.GetValue()
	t.sshPoolReplenishInterval = time.Duration(config.SSHPoolReplenishIntervalSec.GetValue()) * time.Second
	t.sshPoolBalanceStrategy = config.SSHPoolBalanceStrategy.GetValue()
	t.sshProbeURL = config.SSHProbeURL.GetValue()
	t.sshProbeURLs = config.SSHProbeURLs.GetValue()
	t.sshProbeTimeout = time.Duration(config.SSHProbeTimeoutSec.GetValue()) * time.Second
	t.sshProbeFailureThreshold = uint64(config.SSHProbeFailureThreshold.GetValue())
	t.sshSuspectCooldown = time.Duration(config.SSHSuspectCooldownSec.GetValue()) * time.Second
	t.proxyRetryMaxAttempts = config.ProxyRetryMaxAttempts.GetValue()
	t.proxyRetryInitialBufferBytes = config.ProxyRetryInitialBufferBytes.GetValue()
	t.hostKeys = ssh.InsecureIgnoreHostKey()

	if t.enableSocks5 || t.enableHttpOverSSH || t.enableDNS {
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
					tunnel.SetDomains(make(map[string]bool))
					tunnel.SetDomainMatchCache(make(map[string]bool))
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
					tunnel.SetDomainMatchCache(make(map[string]bool))
				}
			}

		}
	}
}

// initProfileRouting sets up the route matcher and starts profile tunnels targeted by enabled rules.
func initProfileRouting(ctx context.Context, config *cfg.AppConfig) error {
	matcher := NewRouteMatcher()
	mgr := NewProfileTunnelManager(matcher, DefaultSshTunnel.trafficStore)
	DefaultSshTunnel.routeMatcher = matcher
	DefaultSshTunnel.profileTunnelMgr = mgr

	store, err := cfg.ListProfiles(config)
	if err != nil {
		return fmt.Errorf("加载profiles失败: %w", err)
	}
	routeStore, err := cfg.ListRoutes(config)
	if err != nil {
		return fmt.Errorf("加载独立路由失败: %w", err)
	}
	policy, err := newDNSPolicy(store, cfg.ResolveEffectiveRoutes(routeStore), DefaultSshTunnel.dnsUpstreams)
	if err != nil {
		return err
	}
	DefaultSshTunnel.dnsPolicy = policy
	activeID := store.ActiveProfileID
	if activeID == "" {
		activeID = cfg.DEFAULT_PROFILE_ID
	}
	DefaultSshTunnel.profileID = activeID
	if activeProfile, ok := store.Profiles[activeID]; ok {
		DefaultSshTunnel.profileIdentity = cfg.SSHProfileConnectionFingerprint(activeProfile)
	}
	for profileID := range store.Profiles {
		DefaultSshTunnel.trafficStore.EnsureProfile(profileID)
	}
	mgr.ReloadProfiles(ctx, store.Profiles, activeID, cfg.ResolveEffectiveRoutes(routeStore))
	return nil
}

// ReloadProfileRouting reloads routing rules and restarts profile tunnels as needed.
// Called when profiles are updated via admin API.
func (t *Tunnel) ReloadProfileRouting(config *cfg.AppConfig) error {
	t.runtimeMu.Lock()
	defer t.runtimeMu.Unlock()
	if t.routeMatcher == nil || t.profileTunnelMgr == nil {
		return nil
	}

	store, err := cfg.ListProfiles(config)
	if err != nil {
		return fmt.Errorf("重载Profile路由失败: %w", err)
	}
	routeStore, err := cfg.ListRoutes(config)
	if err != nil {
		return fmt.Errorf("重载独立路由失败: %w", err)
	}
	policy, err := newDNSPolicy(store, cfg.ResolveEffectiveRoutes(routeStore), t.dnsUpstreams)
	if err != nil {
		return err
	}
	activeID := store.ActiveProfileID
	if activeID == "" {
		activeID = cfg.DEFAULT_PROFILE_ID
	}
	t.profileID = activeID
	if activeProfile, ok := store.Profiles[activeID]; ok {
		t.profileIdentity = cfg.SSHProfileConnectionFingerprint(activeProfile)
	}
	if t.trafficStore != nil {
		for profileID := range store.Profiles {
			t.trafficStore.EnsureProfile(profileID)
		}
	}

	ctx := t.reconnectContext(context.Background())
	t.profileTunnelMgr.ReloadProfiles(ctx, store.Profiles, activeID, cfg.ResolveEffectiveRoutes(routeStore))
	t.dnsPolicy = policy

	// Clear domain match cache on the main tunnel
	t.SetDomainMatchCache(make(map[string]bool))
	return nil
}
