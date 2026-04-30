# Remote Deployment Guide

English | [中文](DEPLOYMENT_CN.md)

This guide explains how to deploy CLIProxyAPI Plus to a remote server and access it from local clients.

## Server Deployment

### 1. Server Configuration

Create a production configuration file `config-production.yaml`:

```yaml
# Bind to all interfaces to accept remote connections
host: '0.0.0.0'
port: 8317

# Enable TLS for secure connections (recommended for production)
tls:
  enable: true
  cert: '/path/to/your/cert.pem'
  key: '/path/to/your/key.pem'

# Management API settings
remote-management:
  # Allow remote management access
  allow-remote: true
  # Set a strong secret key (will be hashed on startup)
  secret-key: 'your-strong-secret-key-here'
  disable-control-panel: false

# Authentication directory
auth-dir: '/opt/cliproxyapi/auths'

# API keys for client authentication
api-keys:
  - 'client-key-1'
  - 'client-key-2'
  - 'client-key-3'

# Enable usage statistics for monitoring
usage-statistics-enabled: true

# Production logging
logging-to-file: true
logs-max-total-size-mb: 1024
debug: false
```

### 2. Import Kiro Tokens

If you have Kiro tokens to import:

```bash
# Build the import tool
go build -o kiro-import ./cmd/kiro-import

# Import tokens
./kiro-import -input tokens.json -output /opt/cliproxyapi/auths
```

### 3. Start the Server

```bash
# Build the server
go build -o cliproxyapi-server ./cmd/server

# Start with production config
./cliproxyapi-server --config config-production.yaml
```

### 4. Using systemd (Recommended)

Create `/etc/systemd/system/cliproxyapi.service`:

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

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/opt/cliproxyapi/auths /opt/cliproxyapi/logs

[Install]
WantedBy=multi-user.target
```

Enable and start the service:

```bash
sudo systemctl daemon-reload
sudo systemctl enable cliproxyapi
sudo systemctl start cliproxyapi
sudo systemctl status cliproxyapi
```

## Client Configuration

### OpenAI-Compatible Clients

Configure your client to use the remote server:

```bash
# Set environment variables
export OPENAI_API_BASE="https://your-server.com:8317/v1"
export OPENAI_API_KEY="client-key-1"
```

Example with Python:

```python
from openai import OpenAI

client = OpenAI(
    base_url="https://your-server.com:8317/v1",
    api_key="client-key-1"
)

response = client.chat.completions.create(
    model="claude-sonnet-4-5",
    messages=[{"role": "user", "content": "Hello!"}]
)
```

### Claude Desktop

Edit `~/Library/Application Support/Claude/claude_desktop_config.json`:

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

1. Open Settings → Models
2. Add custom model:
   - Base URL: `https://your-server.com:8317/v1`
   - API Key: `client-key-1`

### Continue.dev

Edit `~/.continue/config.json`:

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

## Management Panel Access

Access the web management panel from your browser:

```
https://your-server.com:8317/
```

Login with your management secret key configured in `config-production.yaml`.

## Security Best Practices

### 1. Use TLS/HTTPS

Always enable TLS for production deployments:

```yaml
tls:
  enable: true
  cert: '/path/to/cert.pem'
  key: '/path/to/key.pem'
```

Get free certificates from [Let's Encrypt](https://letsencrypt.org/):

```bash
# Using certbot
sudo certbot certonly --standalone -d your-server.com
```

### 2. Strong API Keys

Generate strong random API keys:

```bash
# Generate a random 32-character key
openssl rand -base64 32
```

### 3. Firewall Configuration

Only expose necessary ports:

```bash
# Allow HTTPS traffic
sudo ufw allow 8317/tcp

# Enable firewall
sudo ufw enable
```

### 4. Rate Limiting

Consider using nginx as a reverse proxy with rate limiting:

```nginx
upstream cliproxyapi {
    server 127.0.0.1:8317;
}

server {
    listen 443 ssl http2;
    server_name your-server.com;

    ssl_certificate /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    # Rate limiting
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

### 5. Monitor Logs

Regularly check logs for suspicious activity:

```bash
# View recent logs
sudo journalctl -u cliproxyapi -n 100 -f

# Check for failed authentication attempts
grep "authentication failed" /opt/cliproxyapi/logs/*.log
```

## Troubleshooting

### Connection Refused

Check if the server is listening on the correct interface:

```bash
# Should show 0.0.0.0:8317 or :::8317
sudo netstat -tlnp | grep 8317
```

### TLS Certificate Errors

Verify certificate validity:

```bash
openssl x509 -in /path/to/cert.pem -text -noout
```

### Authentication Failures

Check the management secret key hash:

```bash
# The server logs the hash on startup
sudo journalctl -u cliproxyapi | grep "secret-key"
```

### High Memory Usage

Enable commercial mode to reduce memory overhead:

```yaml
commercial-mode: true
```

## Performance Tuning

### For High Concurrency

```yaml
# Reduce per-request overhead
commercial-mode: true

# Disable usage statistics if not needed
usage-statistics-enabled: false

# Disable request logging
logging-to-file: false
```

### For Multiple Regions

Deploy multiple instances behind a load balancer:

```nginx
upstream cliproxyapi_cluster {
    least_conn;
    server server1.example.com:8317;
    server server2.example.com:8317;
    server server3.example.com:8317;
}
```

## Monitoring

### Health Check Endpoint

Check server health:

```bash
curl -H "Authorization: Bearer your-management-key" \
  https://your-server.com:8317/v0/management/config
```

### Prometheus Metrics

Enable pprof for metrics collection:

```yaml
pprof:
  enable: true
  addr: '127.0.0.1:8316'
```

Access metrics at `http://localhost:8316/debug/pprof/`.

## Backup and Recovery

### Backup Authentication Files

```bash
# Backup auth directory
tar -czf auths-backup-$(date +%Y%m%d).tar.gz /opt/cliproxyapi/auths

# Restore from backup
tar -xzf auths-backup-20260429.tar.gz -C /opt/cliproxyapi/
```

### Configuration Backup

```bash
# Backup configuration
cp /opt/cliproxyapi/config-production.yaml \
   /opt/cliproxyapi/config-production.yaml.backup
```

## Support

For issues and questions:
- GitHub Issues: https://github.com/HsnSaboor/CLIProxyAPIPlus/issues
- Documentation: https://github.com/HsnSaboor/CLIProxyAPIPlus
