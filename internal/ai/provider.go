package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"metis-screen/internal/config"
)

const (
	UserAgentHeader = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Metis/1.0"
	PingTimeout     = 6 * time.Second
)

var (
	// defaultHTTPClient não deve limitar o stream inteiro via Timeout, pois respostas longas
	// seriam canceladas no meio do SSE. O controle de prazo é feito pelo ctx de cada requisição.
	defaultHTTPClient = &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ResponseHeaderTimeout: 30 * time.Second,
		},
	}
	pingHTTPClient = &http.Client{Timeout: PingTimeout}
)

type Provider interface {
	Name() string
	Model() string
	AskStream(ctx context.Context, systemPrompt, userPrompt string, outCh chan<- string) error
	TestConnection(ctx context.Context) (int, error)
}

func NewProvider(cfg *config.Config, providerName string) Provider {
	name := strings.ToLower(strings.TrimSpace(providerName))
	if strings.HasPrefix(name, "custom:") {
		name = strings.TrimPrefix(name, "custom:")
	}
	if name == "" {
		name = strings.ToLower(strings.TrimSpace(cfg.Provider))
		if strings.HasPrefix(name, "custom:") {
			name = strings.TrimPrefix(name, "custom:")
		}
	}

	switch name {
	case "gemini", "google":
		return &GeminiProvider{
			apiKey: cfg.GeminiKey,
			model:  cfg.GeminiModel,
		}
	case "groq":
		return &OpenAICompatibleProvider{
			name:    "Groq",
			baseURL: "https://api.groq.com/openai/v1/chat/completions",
			apiKey:  cfg.GroqKey,
			model:   cfg.GroqModel,
		}
	case "openrouter", "open router", "open_router":
		return &OpenAICompatibleProvider{
			name:       "OpenRouter",
			baseURL:    "https://openrouter.ai/api/v1/chat/completions",
			apiKey:     cfg.OpenRouterKey,
			model:      cfg.OpenRouterModel,
			openRouter: true,
		}
	case "nvidia":
		return &OpenAICompatibleProvider{
			name:    "NVIDIA",
			baseURL: "https://integrate.api.nvidia.com/v1/chat/completions",
			apiKey:  cfg.NvidiaKey,
			model:   cfg.NvidiaModel,
		}
	case "openai":
		return &OpenAICompatibleProvider{
			name:    "OpenAI",
			baseURL: "https://api.openai.com/v1/chat/completions",
			apiKey:  cfg.OpenAIKey,
			model:   cfg.OpenAIModel,
		}
	case "g4f", "web", "web g4f", "web_g4f":
		return &PythonBridgeProvider{
			name:  "Web G4F",
			model: cfg.G4FModel,
		}
	case "ollama", "local", "ollama local", "ollama_local":
		return &OllamaProvider{
			url:   cfg.OllamaURL,
			model: cfg.OllamaModel,
		}
	default:
		// Procura nos servidores customizados do Metis
		if cfg.ModelsConfig != nil {
			for _, srv := range cfg.ModelsConfig.CustomServers {
				srvID := strings.ToLower(strings.TrimPrefix(strings.ToLower(srv.ID), "custom:"))
				srvNome := strings.ToLower(srv.Nome)
				if srvID == name || srvNome == name {
					apiKey := srv.APIKey
					if apiKey == "" && srv.APIKeyEnv != "" {
						apiKey = os.Getenv(srv.APIKeyEnv)
					}
					model := srv.ModeloAtual
					if model == "" && len(srv.Modelos) > 0 {
						model = srv.Modelos[0]
					}
					isOpenRouter := strings.Contains(strings.ToLower(srv.BaseURL), "openrouter.ai") || srvID == "openrouter"
					endpoint := strings.TrimRight(srv.BaseURL, "/")
					if !strings.HasSuffix(endpoint, "/chat/completions") {
						endpoint += "/chat/completions"
					}
					return &OpenAICompatibleProvider{
						name:       srv.Nome,
						baseURL:    endpoint,
						apiKey:     apiKey,
						model:      model,
						openRouter: isOpenRouter,
					}
				}
			}
		}

		return &OllamaProvider{
			url:   cfg.OllamaURL,
			model: cfg.OllamaModel,
		}
	}
}

// -----------------------------------------------------------------------------
// Ollama Provider (Local / Offline)
// -----------------------------------------------------------------------------
type OllamaProvider struct {
	url   string
	model string
}

func (o *OllamaProvider) Name() string  { return "Ollama" }
func (o *OllamaProvider) Model() string { return o.model }

func (o *OllamaProvider) TestConnection(ctx context.Context) (int, error) {
	start := time.Now()
	pingCtx, cancel := context.WithTimeout(ctx, PingTimeout)
	defer cancel()

	endpoint := strings.TrimRight(o.url, "/") + "/api/tags"
	req, err := http.NewRequestWithContext(pingCtx, "GET", endpoint, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", UserAgentHeader)

	resp, err := pingHTTPClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("Ollama offline em %s: %w", o.url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("Ollama HTTP %d", resp.StatusCode)
	}

	latency := int(time.Since(start).Milliseconds())
	return latency, nil
}

func (o *OllamaProvider) AskStream(ctx context.Context, systemPrompt, userPrompt string, outCh chan<- string) error {
	endpoint := strings.TrimRight(o.url, "/") + "/api/chat"

	reqBody := map[string]interface{}{
		"model": o.model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"stream": true,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", UserAgentHeader)

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("falha ao conectar no Ollama local (%s): %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ollama erro HTTP %d: %s", resp.StatusCode, string(body))
	}

	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 32*1024)
	scanner.Buffer(buf, 5*1024*1024)
	receivedAny := false
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var chunk struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Done bool `json:"done"`
		}
		if err := json.Unmarshal(line, &chunk); err == nil {
			if chunk.Message.Content != "" {
				receivedAny = true
				select {
				case outCh <- chunk.Message.Content:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			if chunk.Done {
				break
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}
	if !receivedAny {
		return fmt.Errorf("Ollama não retornou conteúdo para o modelo '%s'", o.model)
	}

	return nil
}

func isLocalEndpoint(urlStr string) bool {
	low := strings.ToLower(urlStr)
	return strings.Contains(low, "localhost") ||
		strings.Contains(low, "127.0.0.1") ||
		strings.Contains(low, "0.0.0.0") ||
		strings.Contains(low, "::1")
}

// -----------------------------------------------------------------------------
// OpenAI-Compatible Provider (Groq, OpenRouter, NVIDIA, OpenAI, Custom)
// -----------------------------------------------------------------------------
type OpenAICompatibleProvider struct {
	name       string
	baseURL    string
	apiKey     string
	model      string
	openRouter bool
}

func (p *OpenAICompatibleProvider) Name() string  { return p.name }
func (p *OpenAICompatibleProvider) Model() string { return p.model }

func formatAPIError(providerName string, statusCode int, body []byte) error {
	raw := strings.TrimSpace(string(body))

	var parsed struct {
		Error struct {
			Message string      `json:"message"`
			Type    string      `json:"type"`
			Code    interface{} `json:"code"`
		} `json:"error"`
		Detail  string `json:"detail"`
		Title   string `json:"title"`
		Message string `json:"message"`
		Type    string `json:"type"`
	}

	msg := ""
	if err := json.Unmarshal(body, &parsed); err == nil {
		if parsed.Error.Message != "" {
			msg = parsed.Error.Message
		} else if parsed.Detail != "" {
			msg = parsed.Detail
		} else if parsed.Message != "" {
			msg = parsed.Message
		} else if parsed.Title != "" {
			msg = parsed.Title
		}
	}

	if msg == "" {
		msg = raw
	}

	// Remove qualquer referência a "about:blank" ou URLs de erro genéricas
	msg = strings.ReplaceAll(msg, "about:blank", "")
	msg = strings.TrimSpace(msg)

	provLower := strings.ToLower(providerName)
	msgLower := strings.ToLower(msg)

	// Tratamento amigável e específico para os erros mais frequentes
	if statusCode == 401 || strings.Contains(msgLower, "user not found") || strings.Contains(msgLower, "invalid api key") || strings.Contains(msgLower, "unauthorized") {
		if strings.Contains(provLower, "openrouter") {
			return fmt.Errorf("Chave de API do OpenRouter inválida ou usuário não encontrado. Verifique a chave OPENROUTER_API_KEY no arquivo ~/.config/metis/.env (HTTP 401)")
		}
		return fmt.Errorf("Chave de API inválida ou não autorizada para %s (HTTP 401): %s", providerName, msg)
	}

	if statusCode == 403 || strings.Contains(msgLower, "permission denied") || strings.Contains(msgLower, "unregistered callers") {
		return fmt.Errorf("Acesso não autorizado para %s (HTTP 403). Verifique se a chave de API está configurada no ~/.config/metis/.env", providerName)
	}

	if statusCode == 404 || strings.Contains(msgLower, "model_not_found") || strings.Contains(msgLower, "does not exist") {
		return fmt.Errorf("O modelo solicitado não existe ou foi descontinuado em %s (HTTP 404): %s", providerName, msg)
	}

	if statusCode == 410 || strings.Contains(msgLower, "end of life") || strings.Contains(msgLower, "no longer available") {
		return fmt.Errorf("O modelo chegou ao fim de vida útil (descontinuado pelo provedor %s - HTTP 410): %s", providerName, msg)
	}

	if statusCode == 429 || strings.Contains(msgLower, "rate_limit") || strings.Contains(msgLower, "quota") {
		return fmt.Errorf("Limite de requisições ou cota excedida em %s (HTTP 429): %s", providerName, msg)
	}

	if msg != "" {
		return fmt.Errorf("%s retornou erro (HTTP %d): %s", providerName, statusCode, msg)
	}
	return fmt.Errorf("%s retornou código de erro HTTP %d", providerName, statusCode)
}

func (p *OpenAICompatibleProvider) TestConnection(ctx context.Context) (int, error) {
	if p.apiKey == "" && !isLocalEndpoint(p.baseURL) {
		return 0, fmt.Errorf("Chave de API não informada para %s", p.name)
	}

	start := time.Now()
	pingCtx, cancel := context.WithTimeout(ctx, PingTimeout)
	defer cancel()

	reqBody := map[string]interface{}{
		"model": p.model,
		"messages": []map[string]string{
			{"role": "user", "content": "ping"},
		},
		"max_tokens": 1,
	}
	payload, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(pingCtx, "POST", p.baseURL, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	req.Header.Set("User-Agent", UserAgentHeader)
	if p.openRouter {
		req.Header.Set("HTTP-Referer", "https://github.com/metis")
		req.Header.Set("X-Title", "Metis Terminal")
	}

	resp, err := pingHTTPClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("timeout ou erro ao conectar em %s: %w", p.name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, formatAPIError(p.name, resp.StatusCode, body)
	}

	latency := int(time.Since(start).Milliseconds())
	return latency, nil
}

func (p *OpenAICompatibleProvider) AskStream(ctx context.Context, systemPrompt, userPrompt string, outCh chan<- string) error {
	if p.apiKey == "" && !isLocalEndpoint(p.baseURL) {
		return fmt.Errorf("Chave de API para %s não informada no .env. Configure a variável correspondente em ~/.config/metis/.env", p.name)
	}

	messages := []map[string]string{}
	if systemPrompt != "" {
		messages = append(messages, map[string]string{"role": "system", "content": systemPrompt})
	}
	messages = append(messages, map[string]string{"role": "user", "content": userPrompt})

	maxTokens := 4096
	// Modelos de raciocínio (gpt-oss, compound, deepseek-r1, LFM, qwen3) consomem
	// tokens com o raciocínio interno; um teto maior evita respostas cortadas.
	lowModel := strings.ToLower(p.model)
	if strings.Contains(lowModel, "gpt-oss") || strings.Contains(lowModel, "compound") ||
		strings.Contains(lowModel, "deepseek-r1") || strings.Contains(lowModel, "reasoning") ||
		strings.Contains(lowModel, "lfm") || strings.Contains(lowModel, "qwen3") ||
		strings.Contains(lowModel, "thinking") {
		maxTokens = 8192
	}

	reqBody := map[string]interface{}{
		"model":      p.model,
		"messages":   messages,
		"stream":     true,
		"max_tokens": maxTokens,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	req.Header.Set("User-Agent", UserAgentHeader)
	req.Header.Set("Accept", "text/event-stream, application/json")

	if p.openRouter {
		req.Header.Set("HTTP-Referer", "https://github.com/metis")
		req.Header.Set("X-Title", "Metis Terminal")
	}

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("falha ao conectar em %s (%s): %w", p.name, p.baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return formatAPIError(p.name, resp.StatusCode, body)
	}

	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 32*1024)
	scanner.Buffer(buf, 5*1024*1024)
	receivedAny := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		dataStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if dataStr == "[DONE]" {
			break
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
					Reasoning        string `json:"reasoning"`
				} `json:"delta"`
			} `json:"choices"`
		}

		if err := json.Unmarshal([]byte(dataStr), &chunk); err == nil {
			if len(chunk.Choices) > 0 {
				c := chunk.Choices[0].Delta.Content
				rc := chunk.Choices[0].Delta.ReasoningContent
				if rc == "" {
					rc = chunk.Choices[0].Delta.Reasoning
				}
				// O raciocínio (chain-of-thought) de modelos reasoning é emitido em inglês,
				// longo e com hipóteses repetidas. Por padrão ele NÃO é exibido para evitar
				// resposta misturada em português/inglês. Habilite com METIS_SHOW_REASONING=1
				// quando quiser inspecionar o pensamento do modelo.
				showReasoning := strings.EqualFold(os.Getenv("METIS_SHOW_REASONING"), "1") ||
					strings.EqualFold(os.Getenv("METIS_SHOW_REASONING"), "true")
				combined := c
				if rc != "" && showReasoning {
					if combined != "" {
						combined += "\n\n" + rc
					} else {
						combined = rc
					}
				}
				if combined != "" {
					receivedAny = true
					select {
					case outCh <- combined:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}
	if !receivedAny {
		return fmt.Errorf("%s não retornou dados de streaming para o modelo '%s'", p.name, p.model)
	}

	return nil
}

// -----------------------------------------------------------------------------
// Google Gemini Provider (Streaming via SSE)
// -----------------------------------------------------------------------------
type GeminiProvider struct {
	apiKey string
	model  string
}

func (g *GeminiProvider) Name() string  { return "Gemini" }
func (g *GeminiProvider) Model() string { return g.model }

func (g *GeminiProvider) TestConnection(ctx context.Context) (int, error) {
	if g.apiKey == "" {
		return 0, fmt.Errorf("GEMINI_API_KEY não configurada no ~/.config/metis/.env")
	}

	start := time.Now()
	pingCtx, cancel := context.WithTimeout(ctx, PingTimeout)
	defer cancel()

	endpoint := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/%s?key=%s",
		g.model, g.apiKey,
	)

	req, err := http.NewRequestWithContext(pingCtx, "GET", endpoint, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", UserAgentHeader)

	resp, err := pingHTTPClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("erro de rede ao consultar Gemini: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, formatAPIError("Gemini", resp.StatusCode, body)
	}

	latency := int(time.Since(start).Milliseconds())
	return latency, nil
}

func (g *GeminiProvider) AskStream(ctx context.Context, systemPrompt, userPrompt string, outCh chan<- string) error {
	if g.apiKey == "" {
		return fmt.Errorf("GEMINI_API_KEY não configurada no .env. Adicione sua chave em ~/.config/metis/.env")
	}

	endpoint := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/%s:streamGenerateContent?alt=sse&key=%s",
		g.model, g.apiKey,
	)

	reqBody := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"role": "user",
				"parts": []map[string]string{
					{"text": userPrompt},
				},
			},
		},
	}
	if systemPrompt != "" {
		reqBody["system_instruction"] = map[string]interface{}{
			"parts": []map[string]string{
				{"text": systemPrompt},
			},
		}
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", UserAgentHeader)

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("falha ao consultar Gemini: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return formatAPIError("Gemini", resp.StatusCode, body)
	}

	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 32*1024)
	scanner.Buffer(buf, 5*1024*1024)
	receivedAny := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		dataStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))

		var chunk struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}

		if err := json.Unmarshal([]byte(dataStr), &chunk); err == nil {
			for _, cand := range chunk.Candidates {
				for _, part := range cand.Content.Parts {
					if part.Text != "" {
						receivedAny = true
						select {
						case outCh <- part.Text:
						case <-ctx.Done():
							return ctx.Err()
						}
					}
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}
	if !receivedAny {
		return fmt.Errorf("Gemini não retornou dados para o modelo '%s'", g.model)
	}

	return nil
}

// -----------------------------------------------------------------------------
// Python Bridge Provider (Fallback para G4F ou scripts legados)
// -----------------------------------------------------------------------------
type PythonBridgeProvider struct {
	name  string
	model string
}

func (p *PythonBridgeProvider) Name() string  { return p.name }
func (p *PythonBridgeProvider) Model() string { return p.model }

func getPythonBin() string {
	home, _ := os.UserHomeDir()
	candidates := []string{
		home + "/Metis/.venv/bin/python",
		home + "/metis-suite/venv/bin/python",
		home + "/.ZSH/ai/venv/bin/python",
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() && fi.Mode()&0111 != 0 {
			return c
		}
	}
	if py, err := exec.LookPath("python3"); err == nil {
		return py
	}
	return "python3"
}

func getG4FScript() string {
	home, _ := os.UserHomeDir()
	candidates := []string{
		home + "/.ZSH/ai/g4f_ask.py",
		home + "/metis-suite/zsh/g4f_ask.py",
		home + "/Metis/g4f_ask.py",
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c
		}
	}
	return ""
}

func (p *PythonBridgeProvider) TestConnection(ctx context.Context) (int, error) {
	start := time.Now()
	pyBin := getPythonBin()
	cmd := exec.CommandContext(ctx, pyBin, "-c", "import g4f; print('ok')")
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("módulo Python 'g4f' não encontrado no interpretador %s", pyBin)
	}
	latency := int(time.Since(start).Milliseconds())
	return latency, nil
}

func (p *PythonBridgeProvider) AskStream(ctx context.Context, systemPrompt, userPrompt string, outCh chan<- string) error {
	fullPrompt := userPrompt
	if systemPrompt != "" {
		fullPrompt = systemPrompt + "\n\n" + userPrompt
	}

	model := p.model
	if model == "" {
		model = "gpt-4o"
	}

	pyBin := getPythonBin()
	g4fScript := getG4FScript()

	var cmd *exec.Cmd
	if g4fScript != "" {
		cmd = exec.CommandContext(ctx, pyBin, g4fScript, model)
	} else {
		cmd = exec.CommandContext(ctx, pyBin, "-c", `
import sys
try:
    from g4f.client import Client
    client = Client()
    model = sys.argv[1]
    prompt = sys.stdin.read()
    response = client.chat.completions.create(model=model, messages=[{"role": "user", "content": prompt}])
    print(response.choices[0].message.content)
except Exception as e:
    print(f"Erro G4F: {e}", file=sys.stderr)
    sys.exit(1)
`, model)
	}
	cmd.Stdin = strings.NewReader(fullPrompt)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("falha na ponte Python G4F (%s): %s", model, strings.TrimSpace(string(out)))
	}

	outStr := strings.TrimSpace(string(out))
	if outStr == "" {
		return fmt.Errorf("G4F não retornou resposta para o modelo '%s'", model)
	}

	select {
	case outCh <- outStr:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}
