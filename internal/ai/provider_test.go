package ai

import (
	"context"
	"strings"
	"testing"
	"time"

	"metis-screen/internal/config"
)

func TestProviderInstantiation(t *testing.T) {
	cfg := config.Load()

	// Ollama provider
	ollama := NewProvider(cfg, "ollama")
	if ollama.Name() != "Ollama" {
		t.Errorf("expected Ollama, got %s", ollama.Name())
	}

	// Groq provider
	groq := NewProvider(cfg, "groq")
	if groq.Name() != "Groq" {
		t.Errorf("expected Groq, got %s", groq.Name())
	}
}

func TestOllamaConnection(t *testing.T) {
	cfg := config.Load()
	ollama := NewProvider(cfg, "ollama")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	lat, err := ollama.TestConnection(ctx)
	if err != nil {
		t.Logf("Ollama offline ou não rodando: %v", err)
	} else {
		t.Logf("Ollama online! Latência: %dms", lat)
	}
}

func TestFormatAPIError(t *testing.T) {
	// 1. NVIDIA 410 RFC 7807 with about:blank
	nvidiaBody := []byte(`{"type":"about:blank","title":"Gone","status":410,"detail":"The model 'meta/llama-3.1-70b-instruct' has reached its end of life on 2026-08-26T09:00:00Z and is no longer available."}`)
	err := formatAPIError("NVIDIA", 410, nvidiaBody)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	errStr := err.Error()
	if strings.Contains(errStr, "about:blank") {
		t.Errorf("error contains 'about:blank': %s", errStr)
	}
	if !strings.Contains(errStr, "410") || !strings.Contains(errStr, "fim de vida") {
		t.Errorf("expected 410 and fim de vida message, got: %s", errStr)
	}

	// 2. OpenRouter 401 User not found
	openRouterBody := []byte(`{"error":{"message":"User not found.","code":401}}`)
	err = formatAPIError("OpenRouter", 401, openRouterBody)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	errStr = err.Error()
	if !strings.Contains(errStr, "OPENROUTER_API_KEY") {
		t.Errorf("expected guidance on OPENROUTER_API_KEY, got: %s", errStr)
	}

	// 3. Groq 404 model_not_found
	groqBody := []byte(`{"error":{"message":"The model llama-3.3-70b-versatile does not exist","type":"invalid_request_error","code":"model_not_found"}}`)
	err = formatAPIError("Groq", 404, groqBody)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	errStr = err.Error()
	if !strings.Contains(errStr, "descontinuado") && !strings.Contains(errStr, "não existe") {
		t.Errorf("expected model discontinued/not exist message, got: %s", errStr)
	}
}

func TestLiveAsk(t *testing.T) {
	cfg := config.Load()
	if cfg.GroqKey == "" {
		t.Skip("Groq key not configured")
	}
	p := NewProvider(cfg, "groq")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	outCh := make(chan string, 100)
	doneCh := make(chan error, 1)

	go func() {
		doneCh <- p.AskStream(ctx, "Responda apenas com a palavra OK", "Diga OK", outCh)
	}()

	var sb strings.Builder
loop:
	for {
		select {
		case chunk := <-outCh:
			sb.WriteString(chunk)
		case err := <-doneCh:
			if err != nil {
				t.Fatalf("AskStream failed: %v", err)
			}
			// Drain remaining
			for {
				select {
				case c := <-outCh:
					sb.WriteString(c)
				default:
					break loop
				}
			}
		case <-ctx.Done():
			t.Fatalf("timeout: %v", ctx.Err())
		}
	}

	t.Logf("Resposta recebida da IA com sucesso: %s", strings.TrimSpace(sb.String()))
}
