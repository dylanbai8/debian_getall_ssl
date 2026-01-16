# debian_getall_ssl

debian10 安装go
```
cd /usr/local
wget https://go.dev/dl/go1.20.14.linux-amd64.tar.gz
rm -rf /usr/local/go
tar -xzf go1.20.14.linux-amd64.tar.gz

echo 'export PATH=$PATH:/usr/local/go/bin' >> /etc/profile
source /etc/profile
go version
```

编译软件
```
go mod init cert-manager
go mod tidy
go build -o cert-manager main.go
```

Nginx存在反向代理时
```
location /.well-known/ {
    root /www/wwwroot/mypass.xxxx.cn;
}
```
