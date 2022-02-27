# ssh-tunnel
open ssh tunnel tool

## quick start

```bash
./ssh-tunnel -s xx.xx.xx.xx
```

## 命令列表
```bash
./ssh-tunnel -h
Usage of ./ssh-tunnel:
  -l string
        本地地址(短命令) (default "0.0.0.0:1081")
  -local.addr string
        本地地址 (default "0.0.0.0:1081")
  -p int
        服务器SSH端口(短命令) (default 22)
  -pk string
        私钥地址(短命令) (default "/Users/idefav/.ssh/id_rsa")
  -pkh string
        已知主机地址(短命令) (default "/Users/idefav/.ssh/known_hosts")
  -s string
        服务器IP地址(短命令)
  -server.ip string
        服务器IP地址
  -server.ssh.port int
        服务器SSH端口 (default 22)
  -ssh.path.known_hosts string
        已知主机地址 (default "/Users/idefav/.ssh/known_hosts")
  -ssh.path.private_key string
        私钥地址 (default "/Users/idefav/.ssh/id_rsa")
  -u string
        用户名(短命令) (default "root")
  -user string
        用户名 (default "root")
```
