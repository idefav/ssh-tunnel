package handler

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"html/template"
	"io/fs"
	"net"
	"net/http"
	tunnel2 "ssh-tunnel/tunnel"
	"ssh-tunnel/views"
)

type Data struct {
	Domains                map[string]bool
	DomainMatchResultCache map[string]bool
}

type SSHClientState struct {
	Version    string
	LocalAddr  net.Addr
	RemoteAddr net.Addr
	SessionID  string
	User       string
}

func ListStaticFiles(w http.ResponseWriter, r *http.Request) {
	// 明确指定 charset=utf-8
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// 添加完整的 HTML 结构并确保 UTF-8 编码
	fmt.Fprintln(w, "<!DOCTYPE html>")
	fmt.Fprintln(w, "<html>")
	fmt.Fprintln(w, "<head>")
	fmt.Fprintln(w, "<meta charset=\"utf-8\">")
	fmt.Fprintln(w, "<title>StaticFs中的文件列表</title>")
	fmt.Fprintln(w, "</head>")
	fmt.Fprintln(w, "<body>")

	fmt.Fprintln(w, "<h1>StaticFs中的文件列表</h1>")
	fmt.Fprintln(w, "<ul>")

	fs.WalkDir(views.StaticFs, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() {
			// 将每个文件名转换为可点击的链接
			fmt.Fprintf(w, "<li><a href=\"/%s\">%s</a></li>\n", path, path)
		}
		return nil
	})

	fmt.Fprintln(w, "</ul>")
	fmt.Fprintln(w, "</body>")
	fmt.Fprintln(w, "</html>")
}

// ViewStaticFile 显示特定静态文件的内容
func ViewStaticFile(w http.ResponseWriter, r *http.Request) {
	// 从URL中提取文件路径
	vars := mux.Vars(r)
	filePath := "resources/" + vars["filepath"]

	// 读取文件内容
	content, err := fs.ReadFile(views.StaticFs, filePath)
	if err != nil {
		http.Error(w, "文件不存在或无法读取: "+err.Error(), http.StatusNotFound)
		return
	}

	// 设置适当的Content-Type
	contentType := http.DetectContentType(content)
	w.Header().Set("Content-Type", contentType)

	// 输出文件内容
	w.Write(content)
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

func ShowSSHClientStateView(response http.ResponseWriter, request *http.Request) {
	var tunnel = tunnel2.DefaultSshTunnel
	client := tunnel.GetSSHClient()
	version := client.ClientVersion()
	addr := client.LocalAddr()
	remoteAddr := client.RemoteAddr()
	id := client.SessionID()
	user := client.User()

	marshal, _ := json.Marshal(id)

	var data = SSHClientState{
		Version:    string(version),
		LocalAddr:  addr,
		RemoteAddr: remoteAddr,
		SessionID:  string(marshal),
		User:       user,
	}

	tmpl, err := template.ParseFS(views.HtmlFs, "layout.gohtml",
		"nav.gohtml",
		"ssh_state.gohtml")

	if err != nil {
		fmt.Println("Error " + err.Error())
	}
	tmpl.Execute(response, data)
}

func ShowAppConfigView(response http.ResponseWriter, request *http.Request) {
	var tunnel = tunnel2.DefaultSshTunnel
	tmpl, err := template.ParseFS(views.HtmlFs, "layout.gohtml",
		"nav.gohtml",
		"app_config.gohtml")

	if err != nil {
		fmt.Println("Error " + err.Error())
	}
	marshal, _ := json.MarshalIndent(tunnel.AppConfig(), "", "    ")
	// 把 marshal 转换为 map[string]interface{}
	var data map[string]interface{}
	if err := json.Unmarshal(marshal, &data); err != nil {
		http.Error(response, "Error parsing JSON: "+err.Error(), http.StatusInternalServerError)
		return
	}
	tmpl.Execute(response, data)
}

func ShowLogsView(response http.ResponseWriter, request *http.Request) {
	tmpl, err := template.ParseFS(views.HtmlFs, "layout.gohtml",
		"nav.gohtml",
		"logs.gohtml")

	if err != nil {
		fmt.Println("Error " + err.Error())
	}
	tmpl.Execute(response, nil)
}
