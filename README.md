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

编译纯静态版本
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o cert-manager
```

Nginx存在反向代理时
```
location /.well-known/ {
    root /www/wwwroot/mypass.xxxx.cn;
}
```


手动安装acme.sh
```
pkill -f acme.sh
rm -rf /root/.acme.sh

curl https://get.acme.sh | sh
source ~/.bashrc

~/.acme.sh/acme.sh --version
```

添加开机自启动
```
#!/bin/bash

set -e

SERVICE_NAME=cert-manager
BIN_PATH=/www/wwwroot/getall_ssl/cert-manager
WORK_DIR=/www/wwwroot/getall_ssl
SERVICE_FILE=/etc/systemd/system/${SERVICE_NAME}.service

echo "检查程序是否存在..."
if [ ! -f "$BIN_PATH" ]; then
    echo "程序不存在: $BIN_PATH"
    exit 1
fi

echo "设置可执行权限..."
chmod +x "$BIN_PATH"

echo "创建 systemd 服务文件..."
cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=Go cert-manager Service
After=network.target
Wants=network.target
StartLimitIntervalSec=60
StartLimitBurst=10

[Service]
Type=simple
User=root
Group=root
WorkingDirectory=$WORK_DIR
ExecStart=$BIN_PATH
Restart=always
RestartSec=5
KillSignal=SIGTERM
TimeoutStopSec=30
LimitNOFILE=1048576
StandardOutput=null
StandardError=null

[Install]
WantedBy=multi-user.target
EOF

echo "重新加载 systemd..."
systemctl daemon-reexec
systemctl daemon-reload

echo "设置开机自启..."
systemctl enable $SERVICE_NAME

echo "启动服务..."
systemctl restart $SERVICE_NAME

echo "当前服务状态："
systemctl status $SERVICE_NAME --no-pager

echo "完成！"
echo "查看日志: journalctl -u $SERVICE_NAME -f"
```

.
```
systemctl restart cert-manager
systemctl stop cert-manager
systemctl status cert-manager
journalctl -u cert-manager -f
```




