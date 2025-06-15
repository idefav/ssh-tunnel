package admin

import (
	"context"
	"encoding/json"
	"github.com/gorilla/mux"
	"log"
	"net/http"
	"os"
	"ssh-tunnel/cfg"
	"ssh-tunnel/router"
	"ssh-tunnel/safe"
	"ssh-tunnel/tunnel"
	"strings"
	"sync"
)

func Load(config *cfg.AppConfig, wg *sync.WaitGroup) {
	safe.GO(func() {
		var tunnel = &tunnel.DefaultSshTunnel
		if !config.EnableAdmin || config.AdminAddress == "" {
			return
		}
		connCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		adminRouter := mux.NewRouter()

		adminRouter.HandleFunc("/admin/config/list", func(writer http.ResponseWriter, request *http.Request) {
			configBytes, _ := json.Marshal(tunnel.AppConfig())
			writer.Write(configBytes)
		})

		adminRouter.HandleFunc("/admin/ssh/state", func(writer http.ResponseWriter, request *http.Request) {
			client := tunnel.GetSSHClient()
			if client == nil {
				writer.WriteHeader(500)
				writer.Write([]byte("SSH client is not connected"))
				return
			}
			version := client.ClientVersion()
			addr := client.LocalAddr()
			remoteAddr := client.RemoteAddr()
			id := client.SessionID()
			user := client.User()

			m := make(map[string]interface{})
			m["version"] = version
			m["localAddr"] = addr
			m["remoteAddr"] = remoteAddr
			m["sessionId"] = id
			m["user"] = user
			mbytes, _ := json.Marshal(m)
			writer.Write(mbytes)
		})

		adminRouter.HandleFunc("/admin/ssh/reconnect", func(writer http.ResponseWriter, request *http.Request) {
			tunnel.ReconnectSSH(connCtx)
			client := tunnel.GetSSHClient()
			if client == nil {
				writer.WriteHeader(500)
				writer.Write([]byte("SSH client is not connected"))
				return
			}
			version := client.ClientVersion()
			addr := client.LocalAddr()
			remoteAddr := client.RemoteAddr()
			id := client.SessionID()
			user := client.User()

			m := make(map[string]interface{})
			m["version"] = version
			m["localAddr"] = addr
			m["remoteAddr"] = remoteAddr
			m["sessionId"] = id
			m["user"] = user
			mbytes, _ := json.Marshal(m)
			writer.Write(mbytes)
		})

		adminRouter.HandleFunc("/admin/monitor", func(writer http.ResponseWriter, request *http.Request) {
			m := make(map[string]interface{})
			m["matchedDomain"] = tunnel.DomainMatchCache()
			m["domainFilters"] = tunnel.Domains()

			mbytes, _ := json.Marshal(m)
			writer.Write(mbytes)
		})

		adminRouter.HandleFunc("/admin/cache/clean", func(writer http.ResponseWriter, request *http.Request) {

			tunnel.SetDomainMatchCache(make(map[string]bool))
			writer.Write([]byte("success"))
		})

		adminRouter.HandleFunc("/admin/domains/add", func(writer http.ResponseWriter, request *http.Request) {
			domain := request.URL.Query().Get("domain")
			domain = strings.Trim(domain, " ")
			if domain == "" {
				writer.WriteHeader(500)
				writer.Write([]byte("domain is empty"))
				return
			}

			domains := tunnel.Domains()
			domains[strings.ToLower(domain)] = true
			tunnel.SetDomains(domains)
			writer.Write([]byte("success"))
		})

		adminRouter.HandleFunc("/admin/domains/remove", func(writer http.ResponseWriter, request *http.Request) {
			domain := request.URL.Query().Get("domain")
			domain = strings.Trim(domain, " ")
			if domain == "" {
				writer.WriteHeader(500)
				writer.Write([]byte("domain is empty"))
				return
			}
			domains := tunnel.Domains()
			delete(domains, strings.Trim(strings.ToLower(domain), " "))
			tunnel.SetDomains(domains)
			writer.Write([]byte("success"))
		})

		adminRouter.HandleFunc("/admin/domains/flush", func(writer http.ResponseWriter, request *http.Request) {
			appConfig := tunnel.AppConfig()
			filePath := appConfig.HttpDomainFilterFilePath
			file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
			if err != nil {
				writer.Write([]byte(err.Error()))
				return
			}
			defer file.Close()

			for domain, _ := range tunnel.Domains() {
				file.WriteString(domain + "\n")
			}

			writer.Write([]byte("flush to file:" + filePath + " success"))
		})

		router.RegisterRoutes(adminRouter, tunnel)

		server := http.Server{
			Addr:    config.AdminAddress,
			Handler: adminRouter,
		}

		err := server.ListenAndServe()
		if err != nil {
			log.Panic(err)
		}
		defer wg.Done()
		wg.Add(1)
		<-connCtx.Done()
		server.Shutdown(connCtx)
		log.Println("启动Admin Server Success at", ":1083")
	})
}
