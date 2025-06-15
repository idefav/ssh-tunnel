package admin

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"io"
	"log"
	"net/http"
	"os"
	"ssh-tunnel/cfg"
	"ssh-tunnel/router"
	"ssh-tunnel/safe"
	"ssh-tunnel/tunnel"
	"strings"
	"sync"
	"time"
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

		adminRouter.HandleFunc("/admin/logs", func(writer http.ResponseWriter, request *http.Request) {
			// 设置SSE头信息
			writer.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
			writer.Header().Set("Cache-Control", "no-cache")
			writer.Header().Set("Connection", "keep-alive")
			writer.Header().Set("Access-Control-Allow-Origin", "*")

			// 确保可以刷新响应
			flusher, ok := writer.(http.Flusher)
			if !ok {
				http.Error(writer, "不支持流式传输", http.StatusInternalServerError)
				return
			}

			// 获取日志文件路径
			logFilePath := tunnel.AppConfig().LogFilePath
			if logFilePath == "" {
				// 如果没有配置，使用默认路径
				logFilePath = "app.log"
			}

			// 检查文件是否存在
			if _, err := os.Stat(logFilePath); os.IsNotExist(err) {
				fmt.Fprintf(writer, "data: 未找到日志文件: %s\n\n", logFilePath)
				flusher.Flush()
				return
			}

			// 获取客户端请求参数，是否只要最新的日志
			onlyNew := request.URL.Query().Get("only_new") == "true"

			// 连接确认消息
			fmt.Fprintf(writer, "data: 已连接到日志流\n\n")
			flusher.Flush()

			// 打开文件
			file, err := os.Open(logFilePath)
			if err != nil {
				fmt.Fprintf(writer, "data: 无法打开日志文件: %s\n\n", err.Error())
				flusher.Flush()
				return
			}
			defer file.Close()

			// 如果只要最新日志，则设置读取位置为文件末尾
			if onlyNew {
				fileInfo, err := file.Stat()
				if err != nil {
					fmt.Fprintf(writer, "data: 无法获取文件信息: %s\n\n", err.Error())
					flusher.Flush()
					return
				}
				file.Seek(fileInfo.Size(), 0)
			}

			// 创建一个buffered reader
			reader := bufio.NewReader(file)

			// 检测客户端连接是否断开
			ctx := request.Context()

			// 先读取现有内容
			if !onlyNew {
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						if err != io.EOF {
							fmt.Fprintf(writer, "data: 读取日志文件错误: %s\n\n", err.Error())
							flusher.Flush()
						}
						break
					}

					// 去除换行符
					line = strings.TrimRight(line, "\r\n")
					if line != "" {
						fmt.Fprintf(writer, "data: %s\n\n", line)
						flusher.Flush()
					}
				}
			}

			// 持续监控日志文件变化
			ticker := time.NewTicker(1 * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					for {
						line, err := reader.ReadString('\n')
						if err != nil {
							if err == io.EOF {
								break
							}

							fmt.Fprintf(writer, "data: 读取日志文件错误: %s\n\n", err.Error())
							flusher.Flush()
							break
						}

						line = strings.TrimRight(line, "\r\n")
						if line != "" {
							fmt.Fprintf(writer, "data: %s\n\n", line)
							flusher.Flush()
						}
					}

				case <-ctx.Done():
					// 客户端断开连接，结束循环
					return
				}
			}
		})

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
