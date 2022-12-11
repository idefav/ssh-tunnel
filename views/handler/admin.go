package handler

import (
	_ "embed"
	"fmt"
	"html/template"
	"net/http"
	tunnel2 "ssh-tunnel/tunnel"
	"ssh-tunnel/views"
)

type Data struct {
	Domains                map[string]bool
	DomainMatchResultCache map[string]bool
}

func ShowIndexView(response http.ResponseWriter, request *http.Request) {

	var tunnel = tunnel2.DefaultSshTunnel

	var data = Data{
		Domains:                tunnel.Domains(),
		DomainMatchResultCache: tunnel.DomainMatchCache(),
	}

	tmpl, err := template.ParseFS(views.HtmlFs, "layout.gohtml",
		"nav.gohtml",
		"home.gohtml")

	if err != nil {
		fmt.Println("Error " + err.Error())
	}
	tmpl.Execute(response, data)
}

func ShowDomainsView(response http.ResponseWriter, request *http.Request) {
	var tunnel = tunnel2.DefaultSshTunnel

	var data = Data{
		Domains:                tunnel.Domains(),
		DomainMatchResultCache: tunnel.DomainMatchCache(),
	}

	tmpl, err := template.ParseFS(views.HtmlFs, "layout.gohtml",
		"nav.gohtml",
		"domains.gohtml")

	if err != nil {
		fmt.Println("Error " + err.Error())
	}
	tmpl.Execute(response, data)
}

func ShowCacheView(response http.ResponseWriter, request *http.Request) {
	var tunnel = tunnel2.DefaultSshTunnel

	var data = Data{
		Domains:                tunnel.Domains(),
		DomainMatchResultCache: tunnel.DomainMatchCache(),
	}

	tmpl, err := template.ParseFS(views.HtmlFs, "layout.gohtml",
		"nav.gohtml",
		"cache.gohtml")

	if err != nil {
		fmt.Println("Error " + err.Error())
	}
	tmpl.Execute(response, data)
}
