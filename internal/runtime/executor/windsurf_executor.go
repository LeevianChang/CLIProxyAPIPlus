package executor

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	"github.com/tidwall/gjson"
	"golang.org/x/net/http2"
)

const windsurfServicePath = "/exa.language_server_pb.LanguageServerService"

var windsurfClaudeModels = map[string]struct {
	uid       string
	enumValue uint64
}{
	"claude-4.5-sonnet":                   {uid: "MODEL_PRIVATE_2", enumValue: 353},
	"claude-4.5-sonnet-thinking":          {uid: "MODEL_PRIVATE_3", enumValue: 354},
	"claude-sonnet-4.6":                   {uid: "claude-sonnet-4-6"},
	"claude-sonnet-4.6-thinking":          {uid: "claude-sonnet-4-6-thinking"},
	"claude-opus-4.6":                     {uid: "claude-opus-4-6"},
	"claude-opus-4.6-thinking":            {uid: "claude-opus-4-6-thinking"},
	"claude-opus-4-7-medium":              {uid: "claude-opus-4-7-medium"},
	"claude-opus-4-7-medium-thinking":     {uid: "claude-opus-4-7-medium-thinking"},
	"claude-opus-4.7":                     {uid: "claude-opus-4-7-medium"},
	"claude-opus-4.7-thinking":            {uid: "claude-opus-4-7-medium-thinking"},
	"claude-sonnet-4-20250514":            {uid: "MODEL_CLAUDE_4_SONNET", enumValue: 281},
	"claude-sonnet-4-5-20250929":          {uid: "MODEL_PRIVATE_2", enumValue: 353},
	"claude-3-5-sonnet-20241022":          {enumValue: 166},
	"claude-3-7-sonnet-20250219":          {enumValue: 226},
	"claude-3-7-sonnet-20250219-thinking": {enumValue: 227},
}

// WindsurfExecutor exposes Windsurf Cascade as an OpenAI-compatible chat executor.
type WindsurfExecutor struct {
	cfg *config.Config
}

func NewWindsurfExecutor(cfg *config.Config) *WindsurfExecutor { return &WindsurfExecutor{cfg: cfg} }

func (e *WindsurfExecutor) Identifier() string { return "windsurf" }

func (e *WindsurfExecutor) PrepareRequest(_ *http.Request, _ *cliproxyauth.Auth) error { return nil }

func (e *WindsurfExecutor) HttpRequest(ctx context.Context, _ *cliproxyauth.Auth, _ *http.Request) (*http.Response, error) {
	_ = ctx
	return nil, statusErr{code: http.StatusNotImplemented, msg: "windsurf executor does not support raw HTTP forwarding"}
}

func (e *WindsurfExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	text, usage, err := e.runCascade(ctx, auth, req, nil)
	if err != nil {
		return cliproxyexecutor.Response{}, err
	}
	payload := buildWindsurfOpenAIResponse(req.Model, text, usage)
	return cliproxyexecutor.Response{Payload: payload, Headers: windsurfJSONHeaders()}, nil
}

func (e *WindsurfExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, _ cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	out := make(chan cliproxyexecutor.StreamChunk)
	go func() {
		defer close(out)
		_, _, err := e.runCascade(ctx, auth, req, func(delta string) {
			if delta == "" {
				return
			}
			out <- cliproxyexecutor.StreamChunk{Payload: buildWindsurfOpenAIStreamChunk(req.Model, delta)}
		})
		if err != nil {
			out <- cliproxyexecutor.StreamChunk{Err: err}
			return
		}
		out <- cliproxyexecutor.StreamChunk{Payload: []byte("data: [DONE]")}
	}()
	return &cliproxyexecutor.StreamResult{Headers: windsurfSSEHeaders(), Chunks: out}, nil
}

func (e *WindsurfExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	_ = ctx
	return auth, nil
}

func (e *WindsurfExecutor) CountTokens(ctx context.Context, _ *cliproxyauth.Auth, req cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	_ = ctx
	count := len(windsurfOpenAITextPrompt(req.Payload)) / 4
	if count < 1 {
		count = 1
	}
	return cliproxyexecutor.Response{Payload: buildOpenAIUsageJSON(int64(count))}, nil
}

func (e *WindsurfExecutor) runCascade(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, onDelta func(string)) (string, *windsurfUsage, error) {
	apiKey := windsurfAPIKey(auth)
	if apiKey == "" {
		return "", nil, statusErr{code: http.StatusUnauthorized, msg: "missing Windsurf token; add an auth json with type=windsurf and api_key/token"}
	}
	model := windsurfResolveModel(req.Model)
	modelInfo, ok := windsurfClaudeModels[model]
	if !ok {
		return "", nil, statusErr{code: http.StatusNotFound, msg: fmt.Sprintf("unsupported Windsurf model: %s", req.Model)}
	}
	ls, err := windsurfEnsureLS(ctx, e.cfg)
	if err != nil {
		return "", nil, err
	}
	client := &windsurfLSClient{port: ls.port, csrf: ls.csrf, httpClient: ls.httpClient}
	sessionID := randomID()
	if err = client.warmup(ctx, apiKey, sessionID, e.workspacePath(apiKey)); err != nil {
		return "", nil, err
	}
	cascadeID, err := client.startCascade(ctx, apiKey, sessionID)
	if err != nil {
		return "", nil, err
	}
	text := windsurfOpenAITextPrompt(req.Payload)
	if text == "" {
		return "", nil, statusErr{code: http.StatusBadRequest, msg: "empty Windsurf prompt"}
	}
	if err = client.sendCascadeMessage(ctx, apiKey, sessionID, cascadeID, text, modelInfo.enumValue, modelInfo.uid); err != nil {
		return "", nil, err
	}
	return client.pollCascade(ctx, cascadeID, onDelta)
}

func (e *WindsurfExecutor) workspacePath(apiKey string) string {
	base := ""
	if e.cfg != nil {
		base = strings.TrimSpace(e.cfg.Windsurf.DataDir)
	}
	if base == "" {
		base = filepath.Join(os.TempDir(), "windsurf-workspace")
	}
	seed := apiKey
	if len(seed) > 12 {
		seed = seed[:12]
	}
	seed = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return 'x'
	}, seed)
	return filepath.Join(base, "workspace-"+seed)
}

type windsurfLS struct {
	port       int
	csrf       string
	cmd        *exec.Cmd
	httpClient *http.Client
}

var windsurfLSState struct {
	sync.Mutex
	ls *windsurfLS
}

func windsurfEnsureLS(ctx context.Context, cfg *config.Config) (*windsurfLS, error) {
	windsurfLSState.Lock()
	defer windsurfLSState.Unlock()
	if windsurfLSState.ls != nil && windsurfLSState.ls.cmd != nil && windsurfLSState.ls.cmd.Process != nil {
		return windsurfLSState.ls, nil
	}
	wcfg := config.WindsurfConfig{}
	if cfg != nil {
		wcfg = cfg.Windsurf
	}
	binaryPath := strings.TrimSpace(wcfg.LSBinaryPath)
	if binaryPath == "" {
		binaryPath = "/opt/windsurf/language_server_linux_x64"
	}
	if runtime.GOOS != "linux" && wcfg.LSBinaryPath == "" {
		return nil, fmt.Errorf("windsurf language server path is required on %s", runtime.GOOS)
	}
	if _, err := os.Stat(binaryPath); err != nil {
		return nil, fmt.Errorf("windsurf language server binary not found at %s; install it or set windsurf.ls-binary-path: %w", binaryPath, err)
	}
	port := wcfg.LSPort
	if port <= 0 {
		port = 42100
	}
	csrf := strings.TrimSpace(wcfg.CSRFToken)
	if csrf == "" {
		csrf = "cliproxy-windsurf-csrf"
	}
	apiServer := strings.TrimSpace(wcfg.APIServerURL)
	if apiServer == "" {
		apiServer = "https://server.self-serve.windsurf.com"
	}
	dataDir := strings.TrimSpace(wcfg.DataDir)
	if dataDir == "" {
		dataDir = filepath.Join(os.TempDir(), "cliproxy-windsurf")
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "db"), 0o700); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, binaryPath,
		"--api_server_url="+apiServer,
		"--server_port="+strconv.Itoa(port),
		"--csrf_token="+csrf,
		"--register_user_url=https://api.codeium.com/register_user/",
		"--codeium_dir="+dataDir,
		"--database_dir="+filepath.Join(dataDir, "db"),
		"--detect_proxy=false",
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start windsurf language server: %w", err)
	}
	ls := &windsurfLS{port: port, csrf: csrf, cmd: cmd, httpClient: windsurfHTTP2Client()}
	if err := windsurfWaitPort(ctx, port, 25*time.Second); err != nil {
		_ = cmd.Process.Kill()
		return nil, err
	}
	windsurfLSState.ls = ls
	go func() {
		_ = cmd.Wait()
		windsurfLSState.Lock()
		if windsurfLSState.ls == ls {
			windsurfLSState.ls = nil
		}
		windsurfLSState.Unlock()
	}()
	return ls, nil
}

func windsurfHTTP2Client() *http.Client {
	tr := &http2.Transport{AllowHTTP: true, DialTLS: func(network, addr string, _ *tls.Config) (net.Conn, error) { return net.Dial(network, addr) }}
	return &http.Client{Transport: tr}
}

func windsurfWaitPort(ctx context.Context, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
	return fmt.Errorf("windsurf language server port %d not ready", port)
}

type windsurfLSClient struct {
	port       int
	csrf       string
	httpClient *http.Client
}

func (c *windsurfLSClient) warmup(ctx context.Context, apiKey, sessionID, workspace string) error {
	_ = os.MkdirAll(workspace, 0o700)
	calls := []struct {
		method string
		body   []byte
	}{
		{"InitializeCascadePanelState", windsurfBuildInitializePanelStateRequest(apiKey, sessionID)},
		{"AddTrackedWorkspace", windsurfStringField(1, workspace)},
		{"UpdateWorkspaceTrust", windsurfBuildUpdateWorkspaceTrustRequest(apiKey, sessionID)},
		{"Heartbeat", windsurfMessageField(1, windsurfMetadata(apiKey, sessionID))},
	}
	for _, call := range calls {
		if _, err := c.unary(ctx, call.method, call.body, 10*time.Second); err != nil {
			return fmt.Errorf("windsurf %s: %w", call.method, err)
		}
	}
	return nil
}

func (c *windsurfLSClient) startCascade(ctx context.Context, apiKey, sessionID string) (string, error) {
	body := bytes.Join([][]byte{windsurfMessageField(1, windsurfMetadata(apiKey, sessionID)), windsurfVarintField(4, 1), windsurfVarintField(5, 1)}, nil)
	resp, err := c.unary(ctx, "StartCascade", body, 30*time.Second)
	if err != nil {
		return "", err
	}
	fields, _ := windsurfParseFields(resp)
	if f := windsurfGetField(fields, 1, 2); f != nil {
		return string(f.value), nil
	}
	return "", errors.New("StartCascade returned empty cascade_id")
}

func (c *windsurfLSClient) sendCascadeMessage(ctx context.Context, apiKey, sessionID, cascadeID, text string, modelEnum uint64, modelUID string) error {
	body := bytes.Join([][]byte{
		windsurfStringField(1, cascadeID),
		windsurfMessageField(2, windsurfStringField(1, text)),
		windsurfMessageField(3, windsurfMetadata(apiKey, sessionID)),
		windsurfMessageField(5, windsurfCascadeConfig(modelEnum, modelUID)),
	}, nil)
	_, err := c.unary(ctx, "SendUserCascadeMessage", body, 60*time.Second)
	return err
}

func (c *windsurfLSClient) pollCascade(ctx context.Context, cascadeID string, onDelta func(string)) (string, *windsurfUsage, error) {
	var builder strings.Builder
	yielded := map[int]int{}
	deadline := time.Now().Add(10 * time.Minute)
	lastGrowth := time.Now()
	idleCount := 0
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return builder.String(), nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
		steps, err := c.trajectorySteps(ctx, cascadeID)
		if err != nil {
			return builder.String(), nil, err
		}
		for i, step := range steps {
			if step.errText != "" {
				return builder.String(), nil, statusErr{code: http.StatusBadGateway, msg: step.errText}
			}
			if step.text == "" {
				continue
			}
			prev := yielded[i]
			if len(step.text) > prev {
				delta := step.text[prev:]
				yielded[i] = len(step.text)
				builder.WriteString(delta)
				lastGrowth = time.Now()
				if onDelta != nil {
					onDelta(delta)
				}
			}
		}
		status, err := c.trajectoryStatus(ctx, cascadeID)
		if err != nil {
			return builder.String(), nil, err
		}
		if status == 1 {
			idleCount++
			if idleCount >= 2 && time.Since(lastGrowth) > time.Second {
				usage, _ := c.generatorUsage(ctx, cascadeID)
				return builder.String(), usage, nil
			}
		} else {
			idleCount = 0
		}
		if builder.Len() > 0 && time.Since(lastGrowth) > 30*time.Second {
			usage, _ := c.generatorUsage(ctx, cascadeID)
			return builder.String(), usage, nil
		}
	}
	return builder.String(), nil, errors.New("windsurf cascade timed out")
}

func (c *windsurfLSClient) trajectorySteps(ctx context.Context, cascadeID string) ([]windsurfStep, error) {
	resp, err := c.unary(ctx, "GetCascadeTrajectorySteps", windsurfStringField(1, cascadeID), 30*time.Second)
	if err != nil {
		return nil, err
	}
	return windsurfParseTrajectorySteps(resp)
}

func (c *windsurfLSClient) trajectoryStatus(ctx context.Context, cascadeID string) (uint64, error) {
	resp, err := c.unary(ctx, "GetCascadeTrajectory", windsurfStringField(1, cascadeID), 10*time.Second)
	if err != nil {
		return 0, err
	}
	fields, _ := windsurfParseFields(resp)
	if f := windsurfGetField(fields, 2, 0); f != nil {
		return f.varint, nil
	}
	return 0, nil
}

func (c *windsurfLSClient) generatorUsage(ctx context.Context, cascadeID string) (*windsurfUsage, error) {
	resp, err := c.unary(ctx, "GetCascadeTrajectoryGeneratorMetadata", windsurfStringField(1, cascadeID), 10*time.Second)
	if err != nil {
		return nil, err
	}
	return windsurfParseUsage(resp), nil
}

func (c *windsurfLSClient) unary(ctx context.Context, method string, proto []byte, timeout time.Duration) ([]byte, error) {
	body := windsurfGrpcFrame(proto)
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, fmt.Sprintf("http://localhost:%d%s/%s", c.port, windsurfServicePath, method), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/grpc")
	req.Header.Set("TE", "trailers")
	req.Header.Set("User-Agent", "grpc-go/cliproxy-windsurf")
	req.Header.Set("x-codeium-csrf-token", c.csrf)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gRPC HTTP %d: %s", resp.StatusCode, string(raw))
	}
	return windsurfStripGrpc(raw), nil
}

func windsurfAPIKey(auth *cliproxyauth.Auth) string {
	if auth == nil {
		return ""
	}
	if auth.Attributes != nil {
		for _, key := range []string{"api_key", "token", "auth_token"} {
			if v := strings.TrimSpace(auth.Attributes[key]); v != "" {
				return v
			}
		}
	}
	if auth.Metadata != nil {
		for _, key := range []string{"api_key", "token", "auth_token", "access_token"} {
			if v, ok := auth.Metadata[key].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

func windsurfResolveModel(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	switch m {
	case "claude-4.6", "claude-sonnet-4-6":
		return "claude-sonnet-4.6"
	case "claude-4.6-thinking", "claude-sonnet-4-6-thinking":
		return "claude-sonnet-4.6-thinking"
	case "claude-opus-4-6":
		return "claude-opus-4.6"
	case "claude-opus-4-6-thinking":
		return "claude-opus-4.6-thinking"
	case "claude-opus-4-7", "claude-opus-4.7", "opus-4.7":
		return "claude-opus-4-7-medium"
	case "claude-opus-4-7-thinking", "claude-opus-4.7-thinking", "opus-4.7-thinking":
		return "claude-opus-4-7-medium-thinking"
	}
	return m
}

func windsurfOpenAITextPrompt(payload []byte) string {
	root := gjson.ParseBytes(payload)
	var system []string
	var turns []string
	for _, msg := range root.Get("messages").Array() {
		role := msg.Get("role").String()
		content := windsurfContentString(msg.Get("content"))
		if content == "" {
			continue
		}
		if role == "system" {
			system = append(system, content)
			continue
		}
		label := role
		if label == "" {
			label = "user"
		}
		turns = append(turns, "<"+label+">\n"+content+"\n</"+label+">")
	}
	parts := []string{}
	if len(system) > 0 {
		parts = append(parts, strings.Join(system, "\n"))
	}
	parts = append(parts, turns...)
	return strings.Join(parts, "\n\n")
}

func windsurfContentString(v gjson.Result) string {
	if v.Type == gjson.String {
		return v.String()
	}
	if v.IsArray() {
		var parts []string
		for _, item := range v.Array() {
			if item.Get("type").String() == "text" || item.Get("text").Exists() {
				parts = append(parts, item.Get("text").String())
			}
		}
		return strings.Join(parts, "")
	}
	return v.String()
}

func windsurfJSONHeaders() http.Header {
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	return h
}
func windsurfSSEHeaders() http.Header {
	h := http.Header{}
	h.Set("Content-Type", "text/event-stream")
	return h
}

type windsurfUsage struct{ inputTokens, outputTokens int }

func buildWindsurfOpenAIResponse(model, text string, usage *windsurfUsage) []byte {
	if usage == nil {
		usage = &windsurfUsage{inputTokens: len(text) / 4, outputTokens: len(text) / 4}
	}
	obj := map[string]any{"id": "chatcmpl-" + randomID(), "object": "chat.completion", "created": time.Now().Unix(), "model": model, "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": text}, "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": usage.inputTokens, "completion_tokens": usage.outputTokens, "total_tokens": usage.inputTokens + usage.outputTokens}}
	out, _ := json.Marshal(obj)
	return out
}

func buildWindsurfOpenAIStreamChunk(model, delta string) []byte {
	obj := map[string]any{"id": "chatcmpl-" + randomID(), "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": model, "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": delta}, "finish_reason": nil}}}
	out, _ := json.Marshal(obj)
	return append([]byte("data: "), out...)
}

func randomID() string { b := make([]byte, 16); _, _ = rand.Read(b); return hex.EncodeToString(b) }

func windsurfMetadata(apiKey, sessionID string) []byte {
	osName := runtime.GOOS
	if osName == "darwin" {
		osName = "macos"
	}
	hw := "x86_64"
	if runtime.GOARCH == "arm64" {
		hw = "arm64"
	}
	return bytes.Join([][]byte{windsurfStringField(1, "windsurf"), windsurfStringField(2, "2.0.67"), windsurfStringField(3, apiKey), windsurfStringField(4, "en"), windsurfStringField(5, osName), windsurfStringField(7, "2.0.67"), windsurfStringField(8, hw), windsurfVarintField(9, uint64(time.Now().UnixNano())), windsurfStringField(10, sessionID), windsurfStringField(12, "windsurf")}, nil)
}

func windsurfBuildInitializePanelStateRequest(apiKey, sessionID string) []byte {
	return bytes.Join([][]byte{windsurfMessageField(1, windsurfMetadata(apiKey, sessionID)), windsurfVarintField(3, 1)}, nil)
}
func windsurfBuildUpdateWorkspaceTrustRequest(apiKey, sessionID string) []byte {
	return bytes.Join([][]byte{windsurfMessageField(1, windsurfMetadata(apiKey, sessionID)), windsurfVarintField(2, 1)}, nil)
}

func windsurfCascadeConfig(modelEnum uint64, modelUID string) []byte {
	section := bytes.Join([][]byte{windsurfVarintField(1, 1), windsurfStringField(2, "No tools are available. Answer directly as a plain chat API.")}, nil)
	conv := bytes.Join([][]byte{windsurfVarintField(4, 3), windsurfMessageField(10, section), windsurfMessageField(12, section)}, nil)
	plannerParts := [][]byte{windsurfMessageField(2, conv)}
	if modelUID != "" {
		plannerParts = append(plannerParts, windsurfStringField(35, modelUID), windsurfStringField(34, modelUID))
	}
	if modelEnum > 0 {
		plannerParts = append(plannerParts, windsurfMessageField(15, windsurfVarintField(1, modelEnum)), windsurfVarintField(1, modelEnum))
	}
	plannerParts = append(plannerParts, windsurfVarintField(6, 32768))
	planner := bytes.Join(plannerParts, nil)
	memory := windsurfVarintField(1, 0)
	brain := bytes.Join([][]byte{windsurfVarintField(1, 1), windsurfMessageField(6, windsurfMessageField(6, nil))}, nil)
	return bytes.Join([][]byte{windsurfMessageField(1, planner), windsurfMessageField(5, memory), windsurfMessageField(7, brain)}, nil)
}

func windsurfVarintField(field int, value uint64) []byte {
	return append(windsurfEncodeVarint(uint64(field<<3)), windsurfEncodeVarint(value)...)
}
func windsurfStringField(field int, value string) []byte {
	return windsurfBytesField(field, []byte(value))
}
func windsurfMessageField(field int, value []byte) []byte {
	if len(value) == 0 {
		return nil
	}
	return windsurfBytesField(field, value)
}
func windsurfBytesField(field int, value []byte) []byte {
	out := append(windsurfEncodeVarint(uint64(field<<3|2)), windsurfEncodeVarint(uint64(len(value)))...)
	return append(out, value...)
}

func windsurfEncodeVarint(v uint64) []byte {
	var out []byte
	for v >= 0x80 {
		out = append(out, byte(v)|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}
func windsurfDecodeVarint(buf []byte, pos int) (uint64, int, error) {
	var x uint64
	var s uint
	start := pos
	for ; pos < len(buf); pos++ {
		b := buf[pos]
		if b < 0x80 {
			if s >= 64 {
				return 0, 0, errors.New("varint overflow")
			}
			return x | uint64(b)<<s, pos - start + 1, nil
		}
		x |= uint64(b&0x7f) << s
		s += 7
	}
	return 0, 0, errors.New("truncated varint")
}

func windsurfGrpcFrame(payload []byte) []byte {
	out := make([]byte, 5+len(payload))
	binary.BigEndian.PutUint32(out[1:5], uint32(len(payload)))
	copy(out[5:], payload)
	return out
}
func windsurfStripGrpc(buf []byte) []byte {
	var out []byte
	for len(buf) >= 5 {
		n := int(binary.BigEndian.Uint32(buf[1:5]))
		if buf[0] != 0 || len(buf) < 5+n {
			break
		}
		out = append(out, buf[5:5+n]...)
		buf = buf[5+n:]
	}
	if len(out) > 0 {
		return out
	}
	return buf
}

type windsurfField struct {
	num    int
	wire   int
	value  []byte
	varint uint64
}

func windsurfParseFields(buf []byte) ([]windsurfField, error) {
	var fields []windsurfField
	for pos := 0; pos < len(buf); {
		tag, n, err := windsurfDecodeVarint(buf, pos)
		if err != nil {
			return fields, err
		}
		pos += n
		f := windsurfField{num: int(tag >> 3), wire: int(tag & 7)}
		switch f.wire {
		case 0:
			v, n, err := windsurfDecodeVarint(buf, pos)
			if err != nil {
				return fields, err
			}
			f.varint = v
			pos += n
		case 2:
			l, n, err := windsurfDecodeVarint(buf, pos)
			if err != nil {
				return fields, err
			}
			pos += n
			if pos+int(l) > len(buf) {
				return fields, errors.New("truncated length-delimited field")
			}
			f.value = buf[pos : pos+int(l)]
			pos += int(l)
		default:
			return fields, fmt.Errorf("unsupported wire type %d", f.wire)
		}
		fields = append(fields, f)
	}
	return fields, nil
}
func windsurfGetField(fields []windsurfField, num, wire int) *windsurfField {
	for i := range fields {
		if fields[i].num == num && fields[i].wire == wire {
			return &fields[i]
		}
	}
	return nil
}

type windsurfStep struct {
	text    string
	errText string
}

func windsurfParseTrajectorySteps(buf []byte) ([]windsurfStep, error) {
	fields, err := windsurfParseFields(buf)
	if err != nil {
		return nil, err
	}
	var out []windsurfStep
	for _, f := range fields {
		if f.num != 1 || f.wire != 2 {
			continue
		}
		sf, _ := windsurfParseFields(f.value)
		step := windsurfStep{}
		if pf := windsurfGetField(sf, 20, 2); pf != nil {
			pff, _ := windsurfParseFields(pf.value)
			if mt := windsurfGetField(pff, 8, 2); mt != nil {
				step.text = string(mt.value)
			} else if rt := windsurfGetField(pff, 1, 2); rt != nil {
				step.text = string(rt.value)
			}
		}
		if ef := windsurfGetField(sf, 24, 2); ef != nil {
			step.errText = string(ef.value)
		}
		if ef := windsurfGetField(sf, 31, 2); step.errText == "" && ef != nil {
			step.errText = string(ef.value)
		}
		out = append(out, step)
	}
	return out, nil
}

func windsurfParseUsage(buf []byte) *windsurfUsage {
	fields, _ := windsurfParseFields(buf)
	usage := &windsurfUsage{}
	for _, entry := range fields {
		if entry.num != 1 || entry.wire != 2 {
			continue
		}
		gm, _ := windsurfParseFields(entry.value)
		cmf := windsurfGetField(gm, 1, 2)
		if cmf == nil {
			continue
		}
		cm, _ := windsurfParseFields(cmf.value)
		uf := windsurfGetField(cm, 4, 2)
		if uf == nil {
			continue
		}
		us, _ := windsurfParseFields(uf.value)
		if f := windsurfGetField(us, 2, 0); f != nil {
			usage.inputTokens += int(f.varint)
		}
		if f := windsurfGetField(us, 3, 0); f != nil {
			usage.outputTokens += int(f.varint)
		}
	}
	if usage.inputTokens == 0 && usage.outputTokens == 0 {
		return nil
	}
	return usage
}
