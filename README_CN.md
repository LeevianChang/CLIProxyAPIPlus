# CLIProxyAPI Plus

[English](README.md) | 中文

## 文档

- [远程部署指南](DEPLOYMENT_CN.md) - 部署到生产服务器并从本地客户端访问
- [Kiro Token 导入](#kiro-token-导入) - 导入现有的 Kiro tokens

这是 [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) 的 Plus 版本，在原有基础上增加了第三方供应商的支持。

所有的第三方供应商支持都由第三方社区维护者提供，CLIProxyAPI 不提供技术支持。如需取得支持，请与对应的社区维护者联系。

## 支持的供应商

| 供应商 | 标志 | 说明 |
|---|---|---|
| Cline | `--cline-login` | 通过 Cline 扩展的 OAuth 设备流程 |
| CodeBuddy (CN) | `--codebuddy-login` | 通过 `copilot.tencent.com` 的 OAuth (codebuddy.cn) |
| CodeBuddy 国际版 | `--codebuddy-intl-login` | 通过 `www.codebuddy.ai` 的 OAuth |

> 完整的内置供应商列表（Claude、Codex、Gemini、Cursor 等），请参阅[主线 README](https://github.com/router-for-me/CLIProxyAPI)。

### CodeBuddy 国际版

`--codebuddy-intl-login` 标志针对 `www.codebuddy.ai` 进行身份验证，而非默认的 `copilot.tencent.com` 端点。国际版使用相同的 API 端点和响应格式，仅基础 URL 和默认域名不同。令牌以 `type: "codebuddy-intl"` 和 `base_url` 元数据存储，以便执行器将请求路由到正确的后端。

## Kiro Token 导入

如果你有以下格式的 Kiro tokens（驼峰命名）：

```json
[
  {
    "accessToken": "aoaAAAAAGnjvxElKPxBwWvj1UzPFeXga...",
    "expiresAt": "2026-04-18T17:27:45.502321326Z",
    "refreshToken": "aorAAAAAGpaWAEksIImJljyatyIXMKSGV...",
    "provider": "Google",
    "profileArn": "arn:aws:codewhisperer:us-east-1:699475941385:profile/EHGA3GRVQMUK"
  }
]
```

你可以使用 `kiro-import` 工具来转换并导入它们：

### 1. 保存你的 tokens

将你的 token 数组保存到文件（例如 `tokens.json`）

### 2. 编译并运行导入工具

```bash
# 编译导入工具
go build -o kiro-import ./cmd/kiro-import

# 导入 tokens
./kiro-import -input tokens.json -output auths
```

### 3. 启动服务器

```bash
go run ./cmd/server --config config.yaml
```

服务器会自动加载 `auths/` 目录下的所有 Kiro 认证文件。

### 工具功能

- 自动从 JWT access token 中提取用户邮箱
- 基于邮箱生成唯一文件名（例如 `kiro-social-user@example.com.json`）
- 将驼峰命名格式转换为系统所需的下划线命名格式
- 支持批量导入多个账号

### 转换后的格式示例

```json
{
  "type": "kiro",
  "access_token": "aoaAAAAAGnjvxElKPxBwWvj1UzPFeXga...",
  "refresh_token": "aorAAAAAGpaWAEksIImJljyatyIXMKSGV...",
  "profile_arn": "arn:aws:codewhisperer:us-east-1:699475941385:profile/EHGA3GRVQMUK",
  "expires_at": "2026-04-18T17:27:45.502321326Z",
  "auth_method": "social",
  "provider": "Google",
  "email": "user@example.com"
}
```

## 贡献

该项目仅接受第三方供应商支持的 Pull Request。任何非第三方供应商支持的 Pull Request 都将被拒绝。

如果需要提交任何非第三方供应商支持的 Pull Request，请提交到[主线](https://github.com/router-for-me/CLIProxyAPI)版本。

## 许可证

此项目根据 MIT 许可证授权 - 有关详细信息，请参阅 [LICENSE](LICENSE) 文件。
