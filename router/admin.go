package router

import (
	"github.com/gorilla/mux"
	"net/http"
	"ssh-tunnel/tunnel"
	"ssh-tunnel/views/handler"
)

func RegisterRoutes(r *mux.Router, tunnel *tunnel.Tunnel) {

	// 添加根路由重定向到 /view/index
	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/view/index", http.StatusFound)
	})

	// 在路由配置中添加
	r.HandleFunc("/staticfiles", handler.ListStaticFiles)            // 移至viewRouter下
	r.HandleFunc("/resources/{filepath:.*}", handler.ViewStaticFile) // 使用路径参数捕获文件路径

	viewRouter := r.PathPrefix("/view").Subrouter()
	viewRouter.HandleFunc("/index", handler.ShowIndexView)
	viewRouter.HandleFunc("/domains", handler.ShowDomainsView)
	viewRouter.HandleFunc("/caches", handler.ShowCacheView)
	viewRouter.HandleFunc("/ssh/state", handler.ShowSSHClientStateView)
	viewRouter.HandleFunc("/app/config", handler.ShowAppConfigView)
	viewRouter.HandleFunc("/logs", handler.ShowLogsView)
	viewRouter.HandleFunc("/version", handler.ShowVersionView)

	// API路由
	apiRouter := r.PathPrefix("/api").Subrouter()
	apiRouter.HandleFunc("/version/check", handler.CheckForUpdatesHandler)
	apiRouter.HandleFunc("/version/download", handler.DownloadReleaseHandler)
	apiRouter.HandleFunc("/version/update", handler.UpdateToVersionHandler)
	apiRouter.HandleFunc("/version/settings", handler.SaveUpdateSettingsHandler)
	apiRouter.HandleFunc("/version/progress", handler.GetDownloadProgressHandler)

}
