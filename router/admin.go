package router

import (
	"github.com/gorilla/mux"
	"ssh-tunnel/tunnel"
	"ssh-tunnel/views/handler"
)

func RegisterRoutes(r *mux.Router, tunnel *tunnel.Tunnel) {

	// 在路由配置中添加
	r.HandleFunc("/staticfiles", handler.ListStaticFiles)            // 移至viewRouter下
	r.HandleFunc("/resources/{filepath:.*}", handler.ViewStaticFile) // 使用路径参数捕获文件路径

	viewRouter := r.PathPrefix("/view").Subrouter()
	viewRouter.HandleFunc("/index", handler.ShowIndexView)
	viewRouter.HandleFunc("/domains", handler.ShowDomainsView)
	viewRouter.HandleFunc("/caches", handler.ShowCacheView)

}
