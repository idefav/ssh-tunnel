package router

import (
	"github.com/gorilla/mux"
	"ssh-tunnel/tunnel"
	"ssh-tunnel/views/handler"
)

func RegisterRoutes(r *mux.Router, tunnel *tunnel.Tunnel) {

	viewRouter := r.PathPrefix("/view").Subrouter()
	viewRouter.HandleFunc("/index", handler.ShowIndexView)
	viewRouter.HandleFunc("/domains", handler.ShowDomainsView)
	viewRouter.HandleFunc("/caches", handler.ShowCacheView)
}
