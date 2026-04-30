# CLIProxyAPI Plus

English | [Chinese](README_CN.md)

## Documentation

- [Remote Deployment Guide](DEPLOYMENT.md) - Deploy to production servers and access from local clients
- [Kiro Token Import](#kiro-token-import) - Import existing Kiro tokens

## Quick Install

```bash
# Direct download
curl -fSL https://raw.githubusercontent.com/HsnSaboor/CLIProxyAPIPlus/main/install.sh -o /tmp/cliproxyapi-installer.sh && chmod +x /tmp/cliproxyapi-installer.sh && /tmp/cliproxyapi-installer.sh

# Or via cdnjsdelivr
curl -fSL https://cdn.jsdelivr.net/gh/HsnSaboor/CLIProxyAPIPlus@main/install.sh -o /tmp/cliproxyapi-installer.sh && chmod +x /tmp/cliproxyapi-installer.sh && /tmp/cliproxyapi-installer.sh
```

This is the Plus version of [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI), adding support for third-party providers on top of the mainline project.

All third-party provider support is maintained by community contributors; CLIProxyAPI does not provide technical support. Please contact the corresponding community maintainer if you need assistance.

The Plus release stays in lockstep with the mainline features.

## Supported Providers

| Provider | Flag | Notes |
|---|---|---|
| Cline | `--cline-login` | OAuth device flow via Cline extension |
| CodeBuddy (CN) | `--codebuddy-login` | OAuth via `copilot.tencent.com` (codebuddy.cn) |
| CodeBuddy International | `--codebuddy-intl-login` | OAuth via `www.codebuddy.ai` |

> For the full list of built-in providers (Claude, Codex, Gemini, Cursor, etc.), see the [mainline README](https://github.com/router-for-me/CLIProxyAPI).

### CodeBuddy International

The `--codebuddy-intl-login` flag authenticates against `www.codebuddy.ai` instead of the default `copilot.tencent.com` endpoint. The international variant uses identical API endpoints and response formats — only the base URL and default domain differ. Tokens are stored with `type: "codebuddy-intl"` and `base_url` metadata so the executor routes requests to the correct backend.

## Kiro Token Import

If you have Kiro tokens in the following format (camelCase naming):

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

You can use the `kiro-import` tool to convert and import them:

### 1. Save your tokens

Save your token array to a file (e.g., `tokens.json`)

### 2. Build and run the import tool

```bash
# Build the import tool
go build -o kiro-import ./cmd/kiro-import

# Import tokens
./kiro-import -input tokens.json -output auths
```

### 3. Start the server

```bash
go run ./cmd/server --config config.yaml
```

The server will automatically load all Kiro authentication files from the `auths/` directory.

### What the tool does

- Automatically extracts user email from JWT access token
- Generates unique filenames based on email (e.g., `kiro-social-user@example.com.json`)
- Converts camelCase format to snake_case format required by the system
- Supports batch import of multiple accounts

### Converted format example

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

## Contributing

This project only accepts pull requests that relate to third-party provider support. Any pull requests unrelated to third-party provider support will be rejected.

If you need to submit any non-third-party provider changes, please open them against the [mainline](https://github.com/router-for-me/CLIProxyAPI) repository.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
