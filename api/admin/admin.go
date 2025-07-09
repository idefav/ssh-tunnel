package admin

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/kardianos/service"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"ssh-tunnel/cfg"
	"ssh-tunnel/router"
	"ssh-tunnel/safe"
	"ssh-tunnel/tunnel"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 获取配置键映射（前端配置键 -> 实际配置文件键）
func getConfigKeyMapping() map[string]string {
	appConfig := tunnel.DefaultSshTunnel.AppConfig()
	return map[string]string{
		"ServerIp":                 appConfig.ServerIp.Key,
		"ServerSshPort":            appConfig.ServerSshPort.Key,
		"LoginUser":                appConfig.LoginUser.Key,
		"SshPrivateKeyPath":        appConfig.SshPrivateKeyPath.Key,
		"SshKnownHostsPath":        appConfig.SshKnownHostsPath.Key,
		"LocalAddress":             appConfig.LocalAddress.Key,
		"HttpLocalAddress":         appConfig.HttpLocalAddress.Key,
		"EnableHttp":               appConfig.EnableHttp.Key,
		"EnableSocks5":             appConfig.EnableSocks5.Key,
		"EnableHttpOverSSH":        appConfig.EnableHttpOverSSH.Key,
		"HttpBasicAuthEnable":      appConfig.HttpBasicAuthEnable.Key,
		"HttpBasicUserName":        appConfig.HttpBasicUserName.Key,
		"HttpBasicPassword":        appConfig.HttpBasicPassword.Key,
		"EnableHttpDomainFilter":   appConfig.EnableHttpDomainFilter.Key,
		"HttpDomainFilterFilePath": appConfig.HttpDomainFilterFilePath.Key,
		"EnableAdmin":              appConfig.EnableAdmin.Key,
		"AdminAddress":             appConfig.AdminAddress.Key,
		"RetryIntervalSec":         appConfig.RetryIntervalSec.Key,
		"LogFilePath":              appConfig.LogFilePath.Key,
		"HomeDir":                  appConfig.HomeDir.Key,
	}
}

// 辅助函数：返回JSON格式的错误响应
func respondWithError(writer http.ResponseWriter, message string, statusCode int) {
	response := map[string]interface{}{
		"success": false,
		"message": message,
		"error":   true,
	}
	jsonResponse, _ := json.Marshal(response)
	writer.WriteHeader(statusCode)
	writer.Write(jsonResponse)
}

func Load(config *cfg.AppConfig, wg *sync.WaitGroup) {
	safe.GO(func() {
		var tunnel = &tunnel.DefaultSshTunnel

		if !config.EnableAdmin.GetValue() || config.AdminAddress.GetValue() == "" {
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
			logFilePath := tunnel.AppConfig().LogFilePath.GetValue()
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

		// 添加删除日志文件内容的接口
		adminRouter.HandleFunc("/admin/logs/clear", func(writer http.ResponseWriter, request *http.Request) {
			// 只允许POST方法
			if request.Method != http.MethodPost {
				http.Error(writer, "仅支持POST方法", http.StatusMethodNotAllowed)
				return
			}

			// 获取日志文件路径
			logFilePath := tunnel.AppConfig().LogFilePath.GetValue()
			if logFilePath == "" {
				logFilePath = "app.log"
			}

			// 使用安全调用清理日志文件
			err := safe.SafeCallWithReturn(func() error {
				// 清空日志文件内容
				file, err := os.OpenFile(logFilePath, os.O_WRONLY|os.O_TRUNC, 0644)
				if err != nil {
					return fmt.Errorf("无法打开日志文件: %v", err)
				}
				defer file.Close()

				// 写入一条清理记录
				_, err = file.WriteString(fmt.Sprintf("[%s] 日志文件已清理\n", time.Now().Format("2006-01-02 15:04:05")))
				if err != nil {
					return fmt.Errorf("无法写入清理记录: %v", err)
				}

				return nil
			})

			if err != nil {
				log.Printf("清理日志文件失败: %v", err)
				http.Error(writer, fmt.Sprintf("清理日志文件失败: %v", err), http.StatusInternalServerError)
				return
			}

			// 返回成功响应
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			response := map[string]interface{}{
				"success":   true,
				"message":   "日志文件已成功清理",
				"timestamp": time.Now().Format("2006-01-02 15:04:05"),
			}

			responseBytes, _ := json.Marshal(response)
			writer.Write(responseBytes)
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
			filePath := appConfig.HttpDomainFilterFilePath.GetValue()
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

		// 配置修改接口
		adminRouter.HandleFunc("/admin/config/update", func(writer http.ResponseWriter, request *http.Request) {
			// 设置响应头
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.Header().Set("Access-Control-Allow-Origin", "*")
			writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
			writer.Header().Set("Access-Control-Allow-Headers", "Content-Type")

			// 处理OPTIONS预检请求
			if request.Method == "OPTIONS" {
				writer.WriteHeader(http.StatusOK)
				return
			}

			// 只允许POST请求
			if request.Method != "POST" {
				respondWithError(writer, "只支持POST方法", http.StatusMethodNotAllowed)
				return
			}

			// 解析请求参数
			configKey := strings.TrimSpace(request.PostFormValue("key"))
			configValue := request.PostFormValue("value")
			configType := strings.TrimSpace(request.PostFormValue("type"))

			// 参数验证
			if configKey == "" {
				respondWithError(writer, "配置项key不能为空", http.StatusBadRequest)
				return
			}

			// 配置键映射：将前端配置键映射到实际的配置文件键
			keyMapping := getConfigKeyMapping()
			actualConfigKey := configKey
			if mappedKey, exists := keyMapping[configKey]; exists {
				actualConfigKey = mappedKey
			}

			if configType == "" {
				configType = "string" // 默认类型
			}

			// 值验证和类型转换
			var typedValue interface{}
			var err error

			switch configType {
			case "bool":
				if configValue == "" {
					respondWithError(writer, "布尔值不能为空", http.StatusBadRequest)
					return
				}
				lowerValue := strings.ToLower(configValue)
				if lowerValue == "true" || lowerValue == "1" || lowerValue == "yes" {
					typedValue = true
				} else if lowerValue == "false" || lowerValue == "0" || lowerValue == "no" {
					typedValue = false
				} else {
					respondWithError(writer, fmt.Sprintf("无效的布尔值: %s，请输入 true 或 false", configValue), http.StatusBadRequest)
					return
				}

			case "int":
				if configValue == "" {
					respondWithError(writer, "整数值不能为空", http.StatusBadRequest)
					return
				}
				intVal, parseErr := strconv.Atoi(configValue)
				if parseErr != nil {
					respondWithError(writer, fmt.Sprintf("无效的整数值: %s，错误: %v", configValue, parseErr), http.StatusBadRequest)
					return
				}
				// 可以添加范围验证
				if strings.Contains(configKey, "Port") && (intVal < 1 || intVal > 65535) {
					respondWithError(writer, fmt.Sprintf("端口号必须在1-65535范围内，当前值: %d", intVal), http.StatusBadRequest)
					return
				}
				typedValue = intVal

			case "string":
				// 对某些配置项进行特殊验证
				if strings.Contains(configKey, "Path") && configValue != "" {
					// 简单的路径验证
					if strings.Contains(configValue, "..") {
						respondWithError(writer, "路径不能包含相对路径符号(..)，请使用绝对路径", http.StatusBadRequest)
						return
					}
				}
				if strings.Contains(configKey, "Address") && configValue != "" {
					// 简单的地址格式验证
					if !strings.Contains(configValue, ":") {
						respondWithError(writer, "地址格式错误，应该包含端口号，如: 127.0.0.1:8080", http.StatusBadRequest)
						return
					}
				}
				typedValue = configValue

			default:
				respondWithError(writer, fmt.Sprintf("不支持的数据类型: %s", configType), http.StatusBadRequest)
				return
			}

			// 记录配置更新日志
			log.Printf("配置更新请求: frontend_key=%s -> actual_key=%s, value=%v, type=%s", configKey, actualConfigKey, typedValue, configType)

			// 使用viper更新配置，使用实际的配置键
			if err = cfg.UpdateConfigValue(actualConfigKey, typedValue); err != nil {
				log.Printf("保存配置失败: %v", err)
				respondWithError(writer, fmt.Sprintf("保存配置失败: %v", err), http.StatusInternalServerError)
				return
			}

			// 返回成功响应
			response := map[string]interface{}{
				"success":   true,
				"message":   fmt.Sprintf("配置项 %s 已成功更新为 %v", configKey, typedValue),
				"key":       configKey,
				"actualKey": actualConfigKey,
				"value":     typedValue,
				"type":      configType,
			}

			jsonResponse, _ := json.Marshal(response)
			writer.WriteHeader(http.StatusOK)
			writer.Write(jsonResponse)
		})

		// 获取配置项元数据接口
		adminRouter.HandleFunc("/admin/config/metadata", func(writer http.ResponseWriter, request *http.Request) {
			// 设置响应头
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.Header().Set("Access-Control-Allow-Origin", "*")
			writer.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			writer.Header().Set("Access-Control-Allow-Headers", "Content-Type")

			// 处理OPTIONS预检请求
			if request.Method == "OPTIONS" {
				writer.WriteHeader(http.StatusOK)
				return
			}

			// 只允许GET请求
			if request.Method != "GET" {
				http.Error(writer, "只支持GET方法", http.StatusMethodNotAllowed)
				return
			}

			// 获取viper实例
			vConfig := cfg.GetConfigInstance()
			if vConfig == nil {
				http.Error(writer, "配置实例未初始化", http.StatusInternalServerError)
				return
			}

			// 定义配置项元数据
			configMetadata := []map[string]interface{}{
				{"key": "server.ip", "type": "string", "description": "服务器IP地址", "category": "服务器"},
				{"key": "server.ssh.port", "type": "int", "description": "SSH端口", "category": "服务器"},
				{"key": "user", "type": "string", "description": "登录用户名", "category": "服务器"},
				{"key": "ssh.path.private_key", "type": "string", "description": "私钥文件路径", "category": "SSH"},
				{"key": "ssh.path.known_hosts", "type": "string", "description": "已知主机文件路径", "category": "SSH"},
				{"key": "local.addr", "type": "string", "description": "本地SOCKS5监听地址", "category": "代理"},
				{"key": "http.local.addr", "type": "string", "description": "本地HTTP监听地址", "category": "代理"},
				{"key": "http.enable", "type": "bool", "description": "启用HTTP代理", "category": "代理"},
				{"key": "socks5.enable", "type": "bool", "description": "启用SOCKS5代理", "category": "代理"},
				{"key": "http.basic.enable", "type": "bool", "description": "启用HTTP Basic认证", "category": "认证"},
				{"key": "http.basic.username", "type": "string", "description": "HTTP Basic用户名", "category": "认证"},
				{"key": "http.basic.password", "type": "string", "description": "HTTP Basic密码", "category": "认证"},
				{"key": "http.over.ssh.enable", "type": "bool", "description": "启用HTTP Over SSH", "category": "代理"},
				{"key": "http.filter.domain.enable", "type": "bool", "description": "启用域名过滤", "category": "过滤"},
				{"key": "http.filter.domain.file-path", "type": "string", "description": "域名过滤文件路径", "category": "过滤"},
				{"key": "admin.enable", "type": "bool", "description": "启用管理界面", "category": "管理"},
				{"key": "admin.addr", "type": "string", "description": "管理界面监听地址", "category": "管理"},
				{"key": "retry.interval.sec", "type": "int", "description": "重试间隔(秒)", "category": "高级"},
			}

			// 为每个配置项添加当前值
			for i, meta := range configMetadata {
				key := meta["key"].(string)
				configMetadata[i]["value"] = vConfig.Get(key)
			}

			// 返回配置项元数据
			response := map[string]interface{}{
				"success": true,
				"data":    configMetadata,
			}

			jsonResponse, _ := json.Marshal(response)
			writer.Write(jsonResponse)
		})

		// 配置文件清理接口
		adminRouter.HandleFunc("/admin/config/cleanup", func(writer http.ResponseWriter, request *http.Request) {
			// 设置响应头
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.Header().Set("Access-Control-Allow-Origin", "*")
			writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
			writer.Header().Set("Access-Control-Allow-Headers", "Content-Type")

			// 处理OPTIONS预检请求
			if request.Method == "OPTIONS" {
				writer.WriteHeader(http.StatusOK)
				return
			}

			// 只允许POST请求
			if request.Method != "POST" {
				respondWithError(writer, "只支持POST方法", http.StatusMethodNotAllowed)
				return
			}

			// 清理重复配置
			if err := cleanupDuplicateConfigs(); err != nil {
				log.Printf("清理配置失败: %v", err)
				respondWithError(writer, fmt.Sprintf("清理配置失败: %v", err), http.StatusInternalServerError)
				return
			}

			// 返回成功响应
			response := map[string]interface{}{
				"success": true,
				"message": "配置文件清理完成，已移除重复配置项",
			}

			jsonResponse, _ := json.Marshal(response)
			writer.WriteHeader(http.StatusOK)
			writer.Write(jsonResponse)
		})

		// 服务重启接口
		adminRouter.HandleFunc("/admin/service/restart", func(writer http.ResponseWriter, request *http.Request) {
			// 设置响应头
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.Header().Set("Access-Control-Allow-Origin", "*")
			writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
			writer.Header().Set("Access-Control-Allow-Headers", "Content-Type")

			// 处理OPTIONS预检请求
			if request.Method == "OPTIONS" {
				writer.WriteHeader(http.StatusOK)
				return
			}

			// 只允许POST请求
			if request.Method != "POST" {
				respondWithError(writer, "只支持POST方法", http.StatusMethodNotAllowed)
				return
			}

			log.Println("收到服务重启请求")

			// 返回成功响应
			response := map[string]interface{}{
				"success": true,
				"message": "正在尝试重启服务，请稍候...",
			}

			jsonResponse, _ := json.Marshal(response)
			writer.WriteHeader(http.StatusOK)
			writer.Write(jsonResponse) // 异步执行重启操作，给响应时间返回
			safe.GO(func() {
				log.Println("开始执行服务重启...")
				time.Sleep(2 * time.Second) // 等待2秒让响应返回

				// 检查运行模式
				isService := checkIfRunningAsService()
				log.Printf("运行模式检测: 服务模式=%v", isService)

				if isService {
					// 服务模式：尝试重启服务
					log.Println("使用服务模式重启...")
					restartAsService()
				} else {
					// 直接运行模式：尝试重新加载配置
					log.Println("检测到直接运行模式，尝试重新加载配置...")

					// 先尝试优雅关闭SSH连接，触发重连
					if tunnel != nil && tunnel.GetSSHClient() != nil {
						log.Println("正在关闭SSH连接以触发重连...")
						tunnel.GetSSHClient().Close()
					}

					// 尝试重新加载配置
					err := triggerConfigReload()
					if err != nil {
						log.Printf("配置重载失败: %v", err)
						log.Println("建议手动重启程序以应用新配置")
					} else {
						log.Println("配置重载完成，SSH连接将自动重新建立")
					}
				}
			})
		})

		// 检查运行模式接口
		adminRouter.HandleFunc("/admin/service/mode", func(writer http.ResponseWriter, request *http.Request) {
			// 设置响应头
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.Header().Set("Access-Control-Allow-Origin", "*")
			writer.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			writer.Header().Set("Access-Control-Allow-Headers", "Content-Type")

			// 处理OPTIONS预检请求
			if request.Method == "OPTIONS" {
				writer.WriteHeader(http.StatusOK)
				return
			}

			// 只允许GET请求
			if request.Method != "GET" {
				respondWithError(writer, "只支持GET方法", http.StatusMethodNotAllowed)
				return
			}

			// 检查运行模式
			isService := checkIfRunningAsService()

			response := map[string]interface{}{
				"success":    true,
				"isService":  isService,
				"canRestart": isService, // 只有服务模式才支持重启
				"message":    getRunningModeMessage(isService),
			}

			jsonResponse, _ := json.Marshal(response)
			writer.WriteHeader(http.StatusOK)
			writer.Write(jsonResponse)
		})

		router.RegisterRoutes(adminRouter, tunnel)

		server := http.Server{
			Addr:    config.AdminAddress.GetValue(),
			Handler: adminRouter,
		}

		err := server.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			log.Printf("Admin server error: %v", err)
		}
		defer wg.Done()
		wg.Add(1)
		<-connCtx.Done()
		server.Shutdown(connCtx)
		log.Println("启动Admin Server Success at", ":1083")
	})
}

// 检查是否作为服务运行
func checkIfRunningAsService() bool {
	// 使用service.Interactive()来检测运行模式
	// Interactive() 返回true表示交互模式（直接运行）
	// Interactive() 返回false表示服务模式
	return !service.Interactive()
}

// 获取运行模式消息
func getRunningModeMessage(isService bool) string {
	if isService {
		return "程序作为系统服务运行，支持在线重启"
	} else {
		return "程序直接运行模式，配置更改后需要手动重启程序"
	}
}

// 触发配置重新加载
func triggerConfigReload() error {
	// 获取配置实例
	vConfig := cfg.GetConfigInstance()
	if vConfig == nil {
		return fmt.Errorf("配置实例未初始化")
	}

	// 尝试重新读取配置文件
	if err := vConfig.ReadInConfig(); err != nil {
		log.Printf("重新读取配置文件失败: %v", err)
		return err
	}

	log.Println("配置文件重新加载成功")

	// 获取应用配置并更新
	appConfig := tunnel.DefaultSshTunnel.AppConfig()
	if appConfig != nil {
		appConfig.Update()
		log.Println("应用配置更新完成")
	}

	return nil
}

// 作为服务重启
func restartAsService() {
	defer func() {
		log.Println("服务重启处理完成，退出程序...")
		os.Exit(0)
	}()

	switch runtime.GOOS {
	case "windows":
		restartWindowsService()
	case "darwin":
		restartMacOSService()
	case "linux":
		restartLinuxService()
	default:
		log.Printf("不支持的操作系统: %s，尝试通用重启方法", runtime.GOOS)
		restartGenericService()
	}
}

// Windows服务重启
func restartWindowsService() {
	log.Println("正在重启Windows服务...")

	// 停止服务
	stopCmd := exec.Command("sc", "stop", "SSHTunnelService")
	if err := stopCmd.Run(); err != nil {
		log.Printf("停止服务失败: %v", err)
		// 尝试使用net命令
		stopCmd = exec.Command("net", "stop", "SSHTunnelService")
		if err := stopCmd.Run(); err != nil {
			log.Printf("使用net命令停止服务也失败: %v", err)
		}
	}

	// 等待服务停止
	time.Sleep(3 * time.Second)

	// 启动服务
	startCmd := exec.Command("sc", "start", "SSHTunnelService")
	if err := startCmd.Run(); err != nil {
		log.Printf("启动服务失败: %v", err)
		// 尝试使用net命令
		startCmd = exec.Command("net", "start", "SSHTunnelService")
		if err := startCmd.Run(); err != nil {
			log.Printf("使用net命令启动服务也失败: %v", err)
		} else {
			log.Println("Windows服务重启成功（使用net命令）")
		}
	} else {
		log.Println("Windows服务重启成功（使用sc命令）")
	}
}

// macOS服务重启
func restartMacOSService() {
	log.Println("正在重启macOS服务...")

	// macOS使用launchctl管理服务
	serviceName := "com.idefav.ssh-tunnel"

	// 尝试停止服务
	stopCmd := exec.Command("launchctl", "stop", serviceName)
	if err := stopCmd.Run(); err != nil {
		log.Printf("停止macOS服务失败: %v", err)

		// 尝试卸载并重新加载
		unloadCmd := exec.Command("launchctl", "unload", "/Library/LaunchDaemons/"+serviceName+".plist")
		if err := unloadCmd.Run(); err != nil {
			log.Printf("卸载服务失败: %v", err)
		}
	}

	// 等待服务停止
	time.Sleep(2 * time.Second)

	// 尝试启动服务
	startCmd := exec.Command("launchctl", "start", serviceName)
	if err := startCmd.Run(); err != nil {
		log.Printf("启动macOS服务失败: %v", err)

		// 尝试重新加载服务
		loadCmd := exec.Command("launchctl", "load", "/Library/LaunchDaemons/"+serviceName+".plist")
		if err := loadCmd.Run(); err != nil {
			log.Printf("重新加载服务失败: %v", err)
		} else {
			log.Println("macOS服务重启成功（通过重新加载）")
		}
	} else {
		log.Println("macOS服务重启成功")
	}
}

// Linux服务重启
func restartLinuxService() {
	log.Println("正在重启Linux服务...")

	serviceName := "ssh-tunnel"

	// 优先尝试systemctl（systemd）
	if isSystemdAvailable() {
		restartSystemdService(serviceName)
	} else {
		// 回退到传统的service命令
		restartSysVService(serviceName)
	}
}

// 检查systemd是否可用
func isSystemdAvailable() bool {
	cmd := exec.Command("systemctl", "--version")
	err := cmd.Run()
	return err == nil
}

// 使用systemd重启服务
func restartSystemdService(serviceName string) {
	log.Println("使用systemctl重启服务...")

	// 直接重启服务
	restartCmd := exec.Command("systemctl", "restart", serviceName)
	if err := restartCmd.Run(); err != nil {
		log.Printf("systemctl restart失败: %v", err)

		// 尝试先停止再启动
		stopCmd := exec.Command("systemctl", "stop", serviceName)
		if err := stopCmd.Run(); err != nil {
			log.Printf("systemctl stop失败: %v", err)
		}

		time.Sleep(2 * time.Second)

		startCmd := exec.Command("systemctl", "start", serviceName)
		if err := startCmd.Run(); err != nil {
			log.Printf("systemctl start失败: %v", err)
		} else {
			log.Println("Linux服务重启成功（systemctl）")
		}
	} else {
		log.Println("Linux服务重启成功（systemctl restart）")
	}
}

// 使用SysV init重启服务
func restartSysVService(serviceName string) {
	log.Println("使用service命令重启服务...")

	// 直接重启服务
	restartCmd := exec.Command("service", serviceName, "restart")
	if err := restartCmd.Run(); err != nil {
		log.Printf("service restart失败: %v", err)

		// 尝试先停止再启动
		stopCmd := exec.Command("service", serviceName, "stop")
		if err := stopCmd.Run(); err != nil {
			log.Printf("service stop失败: %v", err)
		}

		time.Sleep(2 * time.Second)

		startCmd := exec.Command("service", serviceName, "start")
		if err := startCmd.Run(); err != nil {
			log.Printf("service start失败: %v", err)
		} else {
			log.Println("Linux服务重启成功（service）")
		}
	} else {
		log.Println("Linux服务重启成功（service restart）")
	}
}

// 通用服务重启方法
func restartGenericService() {
	log.Println("使用通用方法重启服务...")

	// 对于不支持的系统，简单地退出程序
	// 假设有外部监控程序会重新启动
	log.Println("不支持的操作系统，程序将退出，期望外部监控重启")
}

// 清理重复配置的辅助函数
func cleanupDuplicateConfigs() error {
	vConfig := cfg.GetConfigInstance()
	if vConfig == nil {
		return fmt.Errorf("配置实例未初始化")
	}

	// 定义正确的配置键列表
	validKeys := []string{
		"home.dir",
		"server.ip",
		"server.ssh.port",
		"ssh.private.key.path",
		"ssh.known.hosts.path",
		"login.username",
		"local.address",
		"http.local.address",
		"http.enable",
		"socks5.enable",
		"http.over.ssh.enable",
		"http.domain.filter.enable",
		"http.domain.filter.file.path",
		"admin.enable",
		"admin.address",
		"http.basic.enable",
		"http.basic.username",
		"http.basic.password",
		"retry.interval.sec",
		"log.file.path",
	}

	// 定义需要删除的重复或错误配置键
	keysToRemove := []string{
		"adminaddress",                 // 错误的键名
		"http.basic.password",          // 重复
		"http.basic.username",          // 重复
		"http.domain-filter.enable",    // 连字符版本，应该用点号
		"http.domain-filter.file-path", // 连字符版本，应该用点号
		"http.filter.domain.file-path", // 顺序错误的版本
		"http.over-ssh.enable",         // 连字符版本，应该用点号
		"ssh.known.hosts.path",         // 重复（原文件中有两个版本）
		"ssh.private.key.path",         // 重复（原文件中有两个版本）
		"ssh.known_hosts_path",         // 下划线版本，应该用点号
		"ssh.private_key_path",         // 下划线版本，应该用点号
		"http.domain.filter.enable",    // 如果有重复的话
	}

	// 删除重复和错误的配置键
	for _, key := range keysToRemove {
		if vConfig.IsSet(key) {
			// 注意：viper没有直接删除键的方法，我们需要重新设置配置
			log.Printf("检测到需要清理的配置项: %s", key)
		}
	}

	// 创建新的配置，只保留有效的配置项
	newConfig := make(map[string]interface{})

	// 保留有效配置的值
	for _, validKey := range validKeys {
		if vConfig.IsSet(validKey) {
			newConfig[validKey] = vConfig.Get(validKey)
		}
	}

	// 清空当前配置并重新设置
	for _, validKey := range validKeys {
		if value, exists := newConfig[validKey]; exists {
			vConfig.Set(validKey, value)
		}
	}

	// 保存清理后的配置
	return cfg.SaveConfig()
}
