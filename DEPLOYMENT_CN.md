# 远程部署指南

[English](DEPLOYMENT.md) | 中文

本指南说明如何将 CLIProxyAPI Plus 部署到远程服务器，并从本地客户端访问。

## 服务器部署

### 1. 服务器配置

创建生产环境配置文件 `config-production.yaml`：

```yaml
# 绑定到所有接口以接受远程连接
host: '0.0.0.0'
port: 8317

# 启用 TLS 以实现安全连接（生产环境推荐）
tls:
  enable: true
  cert: '/path/to/your/cert.pem'
  key: '/path/to/your/key.pem'

# 管理 API 设置
remote-management:
  # 允许远程管理访问
  allow-remote: true
  # 设置强密钥（启动时会被哈希）
  secret-key: 'your-strong-secret-key-here'
  disable-control-panel: false

# 认证文件目录
auth-dir: '/opt/cliproxyapi/auths'

# 客户端认证的 API 密钥
api-keys:
  - 'client-key-1'
  - 'client-key-2'
  - 'client-key-3'

# 启用使用统计以便监控
usage-statistics-enabled: true

# 生产环境日志
logging-to-file: true
logs-max-total-size-mb: 1024
debug: false
```

### 2. 导入 Kiro Tokens

如果你有 Kiro tokens 需要导入：

```bash
# 编译导入工具
go build -o kiro-import ./cmd/kiro-import

# 导入 tokens
./kiro-import -input tokens.json -output /opt/cliproxyapi/auths
```

### 3. 启动服务器

```bash
# 编译服务器
go build -o cliproxyapi-server ./cmd/server

# 使用生产配置启动
./cliproxyapi-server --config config-production.yaml
```

### 4. 使用 systemd（推荐）

创建 `/etc/systemd/system/cliproxyapi.service`：

```ini
[Unit]
Description=CLIProxyAPI Plus Server
After=network.target

[Service]
Type=simple
User=cliproxyapi
WorkingDirectory=/opt/cliproxyapi
ExecStart=/opt/cliproxyapi/cliproxyapi-server --config /opt/cliproxyapi/config-production.yaml
Restart=on-failure
RestartSec=5s

# 安全加固
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/opt/cliproxyapi/auths /opt/cliproxyapi/logs

[Install]
WantedBy=multi-user.target
```

启用并启动服务：

```bash
sudo systemctl daemon-reload
sudo systemctl enable cliproxyapi
sudo systemctl start cliproxyapi
sudo systemctl status cliproxyapi
```

## 客户端配置

### OpenAI 兼容客户端

配置你的客户端使用远程服务器：

```bash
# 设置环境变量
export OPENAI_API_BASE="https://your-server.com:8317/v1"
export OPENAI_API_KEY="client-key-1"
```

Python 示例：

```python
from openai import OpenAI

client = OpenAI(
    base_url="https://your-server.com:8317/v1",
    api_key="client-key-1"
)

response = client.chat.completions.create(
    model="claude-sonnet-4-5",
    messages=[{"role": "user", "content": "你好！"}]
)
```

### Claude Desktop

编辑 `~/Library/Application Support/Claude/claude_desktop_config.json`：

```json
{
  "mcpServers": {
    "cliproxyapi": {
      "command": "curl",
      "args": [
        "-X", "POST",
        "-H", "Authorization: Bearer client-key-1",
        "-H", "Content-Type: application/json",
        "https://your-server.com:8317/v1/chat/completions"
      ]
    }
  }
}
```

### Cursor IDE

1. 打开设置 → Models
2. 添加自定义模型：
   - Base URL: `https://your-server.com:8317/v1`
   - API Key: `client-key-1`

### Continue.dev

编辑 `~/.continue/config.json`：

```json
{
  "models": [
    {
      "title": "Remote Claude",
      "provider": "openai",
      "model": "claude-sonnet-4-5",
      "apiBase": "https://your-server.com:8317/v1",
      "apiKey": "client-key-1"
    }
  ]
}
```

## 管理面板访问

从浏览器访问 Web 管理面板：

```
https://your-server.com:8317/
```

使用 `config-production.yaml` 中配置的管理密钥登录。

## 安全最佳实践

### 1. 使用 TLS/HTTPS

生产环境务必启用 TLS：

```yaml
tls:
  enable: true
  cert: '/path/to/cert.pem'
  key: '/path/to/key.pem'
```

从 [Let's Encrypt](https://letsencrypt.org/) 获取免费证书：

```bash
# 使用 certbot
sudo certbot certonly --standalone -d your-server.com
```

### 2. 强 API 密钥

生成强随机 API 密钥：

```bash
# 生成 32 字符随机密钥
openssl rand -base64 32
```

### 3. 防火墙配置

只开放必要的端口：

```bash
# 允许 HTTPS 流量
sudo ufw allow 8317/tcp

# 启用防火墙
sudo ufw enable
```

### 4. 速率限制

考虑使用 nginx 作为反向代理并配置速率限制：

```nginx
upstream cliproxyapi {
    server 127.0.0.1:8317;
}

server {
    listen 443 ssl http2;
    server_name your-server.com;

    ssl_certificate /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    # 速率限制
    limit_req_zone $binary_remote_addr zone=api:10m rate=10r/s;
    limit_req zone=api burst=20 nodelay;

    location / {
        proxy_pass http://cliproxyapi;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

### 5. 监控日志

定期检查日志以发现可疑活动：

```bash
# 查看最近日志
sudo journalctl -u cliproxyapi -n 100 -f

# 检查认证失败尝试
grep "authentication failed" /opt/cliproxyapi/logs/*.log
```

## 故障排除

### 连接被拒绝

检查服务器是否在正确的接口上监听：

```bash
# 应该显示 0.0.0.0:8317 或 :::8317
sudo netstat -tlnp | grep 8317
```

### TLS 证书错误

验证证书有效性：

```bash
openssl x509 -in /path/to/cert.pem -text -noout
```

### 认证失败

检查管理密钥哈希：

```bash
# 服务器启动时会记录哈希
sudo journalctl -u cliproxyapi | grep "secret-key"
```

### 内存占用高

启用商业模式以减少内存开销：

```yaml
commercial-mode: true
```

## 性能调优

### 高并发场景

```yaml
# 减少每个请求的开销
commercial-mode: true

# 如果不需要，禁用使用统计
usage-statistics-enabled: false

# 禁用请求日志
logging-to-file: false
```

### 多区域部署

在负载均衡器后部署多个实例：

```nginx
upstream cliproxyapi_cluster {
    least_conn;
    server server1.example.com:8317;
    server server2.example.com:8317;
    server server3.example.com:8317;
}
```

## 监控

### 健康检查端点

检查服务器健康状态：

```bash
curl -H "Authorization: Bearer your-management-key" \
  https://your-server.com:8317/v0/management/config
```

### Prometheus 指标

启用 pprof 以收集指标：

```yaml
pprof:
  enable: true
  addr: '127.0.0.1:8316'
```

在 `http://localhost:8316/debug/pprof/` 访问指标。

## 备份与恢复

### 备份认证文件

```bash
# 备份认证目录
tar -czf auths-backup-$(date +%Y%m%d).tar.gz /opt/cliproxyapi/auths

# 从备份恢复
tar -xzf auths-backup-20260429.tar.gz -C /opt/cliproxyapi/
```

### 配置备份

```bash
# 备份配置
cp /opt/cliproxyapi/config-production.yaml \
   /opt/cliproxyapi/config-production.yaml.backup
```

## 支持

如有问题和疑问：
- GitHub Issues: https://github.com/HsnSaboor/CLIProxyAPIPlus/issues
- 文档: https://github.com/HsnSaboor/CLIProxyAPIPlus
