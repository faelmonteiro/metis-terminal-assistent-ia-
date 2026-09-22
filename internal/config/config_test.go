package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMetisThemes(t *testing.T) {
	expectedThemes := []string{
		"metis_oracle",
		"olympus_greek",
		"valhalla_nordic",
		"dracula_synth",
		"nord_ocean",
		"matrix_emerald",
		"onyx_mono",
	}

	for _, id := range expectedThemes {
		theme, ok := MetisThemes[id]
		if !ok {
			t.Errorf("expected theme %s to exist", id)
			continue
		}
		if theme.BgMain == "" || theme.BorderGlow == "" || theme.AccentGold == "" {
			t.Errorf("theme %s missing critical color properties", id)
		}
	}
}

func TestGetActiveTheme(t *testing.T) {
	cfg := &Config{
		ModelsConfig: &ModelsConfigFile{
			Preferences: Preferences{
				ThemeID: "valhalla_nordic",
			},
		},
	}

	theme := cfg.GetActiveTheme()
	if theme.ID != "valhalla_nordic" {
		t.Errorf("expected valhalla_nordic, got %s", theme.ID)
	}

	// Fallback to default when invalid theme
	cfg.ModelsConfig.Preferences.ThemeID = "non_existent_theme"
	themeFallback := cfg.GetActiveTheme()
	if themeFallback.ID != "metis_oracle" {
		t.Errorf("expected fallback to metis_oracle, got %s", themeFallback.ID)
	}
}

func TestCustomServersInGetProvidersList(t *testing.T) {
	cfg := &Config{
		ModelsConfig: &ModelsConfigFile{
			BuiltinModels: map[string][]string{
				"Groq": {"llama-3.3-70b-versatile"},
			},
			CustomServers: []CustomServer{
				{
					ID:          "deepseek_custom",
					Nome:        "DeepSeek Custom Server",
					BaseURL:     "https://api.deepseek.com/v1/chat/completions",
					APIKeyEnv:   "DEEPSEEK_API_KEY",
					ModeloAtual: "deepseek-chat",
					Modelos:     []string{"deepseek-chat", "deepseek-coder"},
				},
			},
		},
	}

	provs := cfg.GetProvidersList()
	foundCustom := false
	for _, p := range provs {
		if p.ID == "deepseek_custom" {
			foundCustom = true
			if p.Name != "DeepSeek Custom Server" {
				t.Errorf("expected custom server name 'DeepSeek Custom Server', got %s", p.Name)
			}
			if p.ActiveModel != "deepseek-chat" {
				t.Errorf("expected active model 'deepseek-chat', got %s", p.ActiveModel)
			}
			if len(p.Models) != 2 {
				t.Errorf("expected 2 models, got %d", len(p.Models))
			}
		}
	}

	if !foundCustom {
		t.Errorf("expected custom server deepseek_custom to be in providers list")
	}
}

func TestConfigReload(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &Config{
		Provider: "gemini",
		ModelsConfig: &ModelsConfigFile{
			Preferences: Preferences{
				ThemeID: "metis_oracle",
			},
		},
	}

	// Executa Reload sem crash
	cfg.Reload()

	// Verifica se a lista de provedores é recuperada normalmente
	provs := cfg.GetProvidersList()
	if len(provs) == 0 {
		t.Errorf("expected providers list to not be empty after Reload")
	}

	ids := make(map[string]bool)
	for _, p := range provs {
		ids[p.ID] = true
	}
	for _, expected := range []string{"g4f", "ollama", "gemini", "groq", "nvidia", "openrouter"} {
		if !ids[expected] {
			t.Errorf("expected %s to be present in providers list after reload", expected)
		}
	}
}

func TestWindowOpacity(t *testing.T) {
	cfg := &Config{
		ModelsConfig: &ModelsConfigFile{
			Preferences: Preferences{
				WindowOpacity: 60,
			},
		},
	}
	if op := cfg.GetWindowOpacity(); op != 60 {
		t.Errorf("expected opacity 60, got %d", op)
	}

	// Test fallback
	cfgEmpty := &Config{}
	if op := cfgEmpty.GetWindowOpacity(); op != 85 {
		t.Errorf("expected fallback opacity 85, got %d", op)
	}
}

func TestAddModelAndRemoveModel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &Config{
		Provider: "groq",
		ModelsConfig: &ModelsConfigFile{
			BuiltinModels: map[string][]string{
				"Groq": {"llama-3.3-70b-versatile"},
			},
			RemovedModels: map[string][]string{},
		},
	}

	// 1. Testa AddModel
	err := cfg.AddModel("groq", "novo-modelo-teste")
	if err != nil {
		t.Fatalf("unexpected error in AddModel: %v", err)
	}

	provs := cfg.GetProvidersList()
	var groqModels []string
	for _, p := range provs {
		if p.ID == "groq" {
			groqModels = p.Models
		}
	}

	foundNew := false
	for _, m := range groqModels {
		if m == "novo-modelo-teste" {
			foundNew = true
		}
	}
	if !foundNew {
		t.Errorf("expected novo-modelo-teste to be present in Groq models")
	}

	// 2. Testa RemoveModel
	err = cfg.RemoveModel("groq", "novo-modelo-teste")
	if err != nil {
		t.Fatalf("unexpected error in RemoveModel: %v", err)
	}

	provsAfter := cfg.GetProvidersList()
	for _, p := range provsAfter {
		if p.ID == "groq" {
			for _, m := range p.Models {
				if m == "novo-modelo-teste" {
					t.Errorf("expected novo-modelo-teste to be removed from Groq models")
				}
			}
		}
	}
}

func TestSyncMetisModels(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := Load()
	err := cfg.SyncMetisModels()
	if err != nil {
		t.Fatalf("unexpected error in SyncMetisModels: %v", err)
	}
	provs := cfg.GetProvidersList()
	if len(provs) == 0 {
		t.Fatalf("expected providers after sync, got none")
	}
	for _, p := range provs {
		if p.ID == "groq" {
			if len(p.Models) == 0 {
				t.Errorf("expected groq to have models after sync, got 0")
			}
		}
	}
}

func TestRemoveModelsBatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &Config{
		Provider: "groq",
		ModelsConfig: &ModelsConfigFile{
			BuiltinModels: map[string][]string{
				"Groq": {"model-a", "model-b", "model-c", "model-d"},
			},
			RemovedModels: map[string][]string{},
		},
	}

	err := cfg.RemoveModels("groq", []string{"model-b", "model-c"})
	if err != nil {
		t.Fatalf("unexpected error in RemoveModels: %v", err)
	}

	provs := cfg.GetProvidersList()
	for _, p := range provs {
		if p.ID == "groq" {
			for _, m := range p.Models {
				if m == "model-b" || m == "model-c" {
					t.Errorf("model %s was not removed by RemoveModels batch", m)
				}
			}
			foundA := false
			foundD := false
			for _, m := range p.Models {
				if m == "model-a" {
					foundA = true
				}
				if m == "model-d" {
					foundD = true
				}
			}
			if !foundA || !foundD {
				t.Errorf("remaining models model-a and model-d should still be present")
			}
		}
	}
}

func TestEnvMtimePrecedence(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	configMetisDir := filepath.Join(tempHome, ".config", "metis")
	metisDir := filepath.Join(tempHome, "Metis")
	_ = os.MkdirAll(configMetisDir, 0755)
	_ = os.MkdirAll(metisDir, 0755)

	oldEnv := filepath.Join(configMetisDir, ".env")
	newEnv := filepath.Join(metisDir, ".env")

	_ = os.WriteFile(oldEnv, []byte(`GROQ_API_KEY="old-key-123"`+"\n"), 0600)
	// Garante que o arquivo newEnv tenha timestamp de modificação posterior
	pastTime := time.Now().Add(-10 * time.Minute)
	_ = os.Chtimes(oldEnv, pastTime, pastTime)

	_ = os.WriteFile(newEnv, []byte(`GROQ_API_KEY="new-fresh-key-456"`+"\n"), 0600)
	nowTime := time.Now()
	_ = os.Chtimes(newEnv, nowTime, nowTime)

	loadEnvFiles(tempHome, true)

	if got := os.Getenv("GROQ_API_KEY"); got != "new-fresh-key-456" {
		t.Errorf("expected newest .env value 'new-fresh-key-456', got %q", got)
	}
}

func TestCustomServerSyncAndFallback(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	configMetisDir := filepath.Join(tempHome, ".config", "metis")
	_ = os.MkdirAll(configMetisDir, 0755)

	cfgPath := filepath.Join(configMetisDir, "config_models.json")
	initialJSON := `{
		"active_provider": "minha_ia_custom",
		"custom_servers": [
			{
				"id": "minha_ia_custom",
				"nome": "Minha IA Custom",
				"base_url": "http://localhost:8000/v1",
				"modelo_atual": "modelo-x",
				"modelos": ["modelo-x"]
			}
		]
	}`
	_ = os.WriteFile(cfgPath, []byte(initialJSON), 0600)

	cfg := Load()
	provs := cfg.GetProvidersList()
	found := false
	for _, p := range provs {
		if p.ID == "minha_ia_custom" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected custom server minha_ia_custom to be found")
	}

	// Simula remoção do servidor customizado no Metis
	updatedJSON := `{
		"active_provider": "groq",
		"custom_servers": []
	}`
	_ = os.WriteFile(cfgPath, []byte(updatedJSON), 0600)

	err := cfg.SyncMetisModels()
	if err != nil {
		t.Fatalf("unexpected error in SyncMetisModels: %v", err)
	}

	provsAfter := cfg.GetProvidersList()
	for _, p := range provsAfter {
		if p.ID == "minha_ia_custom" {
			t.Errorf("expected minha_ia_custom to be removed, but still found")
		}
	}

	if cfg.Provider == "minha_ia_custom" {
		t.Errorf("expected active provider to fallback, but still %s", cfg.Provider)
	}
}

func TestSyncPreservesActiveProviderAndModel(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	cfg := Load()
	// Salva OpenRouter como ativo
	err := cfg.SaveProviderAndModel("openrouter", "deepseek/deepseek-v4-flash-0731:free")
	if err != nil {
		t.Fatalf("unexpected error saving provider: %v", err)
	}

	if cfg.Provider != "openrouter" {
		t.Fatalf("expected provider openrouter, got %s", cfg.Provider)
	}
	if cfg.OpenRouterModel != "deepseek/deepseek-v4-flash-0731:free" {
		t.Fatalf("expected model deepseek/deepseek-v4-flash-0731:free, got %s", cfg.OpenRouterModel)
	}

	// Executa SyncMetisModels
	err = cfg.SyncMetisModels()
	if err != nil {
		t.Fatalf("unexpected error in SyncMetisModels: %v", err)
	}

	// Verifica se a escolha foi mantida após o sync
	if cfg.Provider != "openrouter" {
		t.Errorf("expected provider openrouter preserved after sync, got %s", cfg.Provider)
	}
	if cfg.OpenRouterModel != "deepseek/deepseek-v4-flash-0731:free" {
		t.Errorf("expected model deepseek/deepseek-v4-flash-0731:free preserved after sync, got %s", cfg.OpenRouterModel)
	}
}

func TestDeletedModelDoesNotReappearAfterSync(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	cfg := Load()
	// Remove um modelo canônico padrão
	err := cfg.RemoveModel("groq", "openai/gpt-oss-120b")
	if err != nil {
		t.Fatalf("unexpected error in RemoveModel: %v", err)
	}

	// Executa sincronização
	err = cfg.SyncMetisModels()
	if err != nil {
		t.Fatalf("unexpected error in SyncMetisModels: %v", err)
	}

	// Verifica se o modelo removido NÃO reapareceu após a sincronização
	for _, p := range cfg.GetProvidersList() {
		if p.ID == "groq" {
			for _, m := range p.Models {
				if m == "openai/gpt-oss-120b" {
					t.Errorf("deleted model openai/gpt-oss-120b reappeared after SyncMetisModels!")
				}
			}
		}
	}
}

func TestProviderAliasesAndKeyConsolidation(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	// Testa variações de nomes de provedores
	if providerToCanonicalKey("web g4f") != "G4F" {
		t.Errorf("expected G4F, got %s", providerToCanonicalKey("web g4f"))
	}
	if providerToCanonicalKey("ollama local") != "Ollama" {
		t.Errorf("expected Ollama, got %s", providerToCanonicalKey("ollama local"))
	}
	if providerToCanonicalKey("open router") != "OpenRouter" {
		t.Errorf("expected OpenRouter, got %s", providerToCanonicalKey("open router"))
	}

	if normalizeProvider("web g4f") != "g4f" {
		t.Errorf("expected g4f, got %s", normalizeProvider("web g4f"))
	}
	if normalizeProvider("ollama local") != "ollama" {
		t.Errorf("expected ollama, got %s", normalizeProvider("ollama local"))
	}
	if normalizeProvider("open router") != "openrouter" {
		t.Errorf("expected openrouter, got %s", normalizeProvider("open router"))
	}

	if providerToNum("web g4f") != "2" {
		t.Errorf("expected 2, got %s", providerToNum("web g4f"))
	}
	if providerToNum("ollama local") != "1" {
		t.Errorf("expected 1, got %s", providerToNum("ollama local"))
	}
	if providerToNum("open router") != "6" {
		t.Errorf("expected 6, got %s", providerToNum("open router"))
	}
}

func TestRemovedModelsPersistAcrossNewLoad(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	// 1. Inicializa config e remove modelos em provedores diferentes
	cfg := Load()
	_ = cfg.RemoveModel("ollama", "llama3.2:3b")
	_ = cfg.RemoveModel("g4f", "gpt-4o")

	// 2. Simula fechar e reabrir o programa (chama Load() do zero lendo do disco)
	cfgReopened := Load()

	for _, p := range cfgReopened.GetProvidersList() {
		if p.ID == "ollama" {
			for _, m := range p.Models {
				if m == "llama3.2:3b" {
					t.Errorf("llama3.2:3b reappeared after program reopened!")
				}
			}
		}
		if p.ID == "g4f" {
			for _, m := range p.Models {
				if m == "gpt-4o" {
					t.Errorf("gpt-4o reappeared after program reopened!")
				}
			}
		}
	}

	// 3. Executa sincronização e verifica se os modelos continuam removidos
	err := cfgReopened.SyncMetisModels()
	if err != nil {
		t.Fatalf("unexpected error in SyncMetisModels: %v", err)
	}

	for _, p := range cfgReopened.GetProvidersList() {
		if p.ID == "ollama" {
			for _, m := range p.Models {
				if m == "llama3.2:3b" {
					t.Errorf("llama3.2:3b reappeared after SyncMetisModels!")
				}
			}
		}
		if p.ID == "g4f" {
			for _, m := range p.Models {
				if m == "gpt-4o" {
					t.Errorf("gpt-4o reappeared after SyncMetisModels!")
				}
			}
		}
	}
}

func TestActiveModelsPerProvider(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	cfg := Load()
	// Configura modelos específicos para dois provedores diferentes
	_ = cfg.SaveProviderAndModel("groq", "openai/gpt-oss-120b")
	_ = cfg.SaveProviderAndModel("openrouter", "liquid/lfm-2.5-2.6b:free")

	// Simula fechar e reabrir o programa
	reopened := Load()

	if reopened.GroqModel != "openai/gpt-oss-120b" {
		t.Errorf("expected groq model 'openai/gpt-oss-120b', got %s", reopened.GroqModel)
	}
	if reopened.OpenRouterModel != "liquid/lfm-2.5-2.6b:free" {
		t.Errorf("expected openrouter model 'liquid/lfm-2.5-2.6b:free', got %s", reopened.OpenRouterModel)
	}
}

func TestCrossProviderModelContaminationPrevented(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	configMetisDir := filepath.Join(tempHome, ".config", "metis")
	_ = os.MkdirAll(configMetisDir, 0755)

	// Simula arquivo onde o groq estava ativo com qwen/qwen3.8-27b
	jsonContent := `{
		"active_provider": "groq",
		"active_model": "qwen/qwen3.8-27b",
		"preferences": {
			"last_active_provider": "groq",
			"last_active_model": "qwen/qwen3.8-27b"
		}
	}`
	_ = os.WriteFile(filepath.Join(configMetisDir, "config_models.json"), []byte(jsonContent), 0600)

	// Mas o arquivo .last_provider indica que o usuário agora escolheu openrouter
	_ = os.WriteFile(filepath.Join(configMetisDir, ".last_provider"), []byte("openrouter\n"), 0644)

	cfg := Load()

	if cfg.Provider != "openrouter" {
		t.Fatalf("expected provider openrouter, got %s", cfg.Provider)
	}

	// O OpenRouter NÃO pode ter recebido qwen/qwen3.8-27b do Groq
	if cfg.OpenRouterModel == "qwen/qwen3.8-27b" {
		t.Errorf("OpenRouter was contaminated with Groq model 'qwen/qwen3.8-27b'")
	}

	// O modelo ativo do OpenRouter deve ser um modelo válido do OpenRouter
	valid := cfg.isModelValidForProviderLocked("openrouter", cfg.OpenRouterModel)
	if !valid {
		t.Errorf("expected valid OpenRouter model, got %s", cfg.OpenRouterModel)
	}
}

func TestCustomServerModelEnvVarDoesNotOverwriteAPIKey(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	configMetisDir := filepath.Join(tempHome, ".config", "metis")
	_ = os.MkdirAll(configMetisDir, 0755)

	envFile := filepath.Join(configMetisDir, ".env")
	_ = os.WriteFile(envFile, []byte("MY_SERVER_API_KEY=\"secret-token-12345\"\n"), 0600)

	cfg := &Config{
		ModelsConfig: &ModelsConfigFile{
			CustomServers: []CustomServer{
				{
					ID:          "my-server",
					Nome:        "My Server",
					BaseURL:     "http://localhost:8080/v1",
					APIKeyEnv:   "MY_SERVER_API_KEY",
					ModeloAtual: "model-1",
					Modelos:     []string{"model-1", "model-2"},
				},
			},
		},
	}

	// Salva novo modelo no servidor customizado
	_ = cfg.SaveProviderAndModel("my-server", "model-2")

	// Verifica se a chave de API permaneceu intacta no .env
	loadEnvFiles(tempHome, true)
	if os.Getenv("MY_SERVER_API_KEY") != "secret-token-12345" {
		t.Errorf("API key was destroyed! Expected 'secret-token-12345', got %q", os.Getenv("MY_SERVER_API_KEY"))
	}
}

func TestOpenRouterModelDeletionAndSyncPersistence(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	configMetisDir := filepath.Join(tempHome, ".config", "metis")
	_ = os.MkdirAll(configMetisDir, 0755)

	initialJSON := `{
  "active_provider": "openrouter",
  "active_model": "qwen/qwen3.8-27b",
  "builtin_models": {
    "OpenRouter": ["qwen/qwen3.8-27b", "google/gemma-2-9b"]
  },
  "custom_servers": [
    {
      "id": "openrouter",
      "nome": "OpenRouter",
      "base_url": "https://openrouter.ai/api/v1/chat/completions",
      "api_key_env": "OPENROUTER_API_KEY",
      "modelo_atual": "qwen/qwen3.8-27b",
      "modelos": ["qwen/qwen3.8-27b", "google/gemma-2-9b"]
    }
  ],
  "preferences": {
    "theme_id": "dracula_synth",
    "window_opacity": 75,
    "last_active_provider": "openrouter",
    "last_active_model": "qwen/qwen3.8-27b",
    "active_models": {
      "openrouter": "qwen/qwen3.8-27b"
    }
  },
  "removed_models": {
    "OpenRouter": []
  }
}`
	_ = os.WriteFile(filepath.Join(configMetisDir, "config_models.json"), []byte(initialJSON), 0600)
	_ = os.WriteFile(filepath.Join(configMetisDir, ".env"), []byte("DEFAULT_PROVIDER=\"6\"\nOPENROUTER_MODEL=\"qwen/qwen3.8-27b\"\n"), 0600)

	cfg := Load()
	if cfg.Provider != "openrouter" {
		t.Fatalf("expected provider openrouter, got %s", cfg.Provider)
	}

	// 1. Remove o modelo qwen/qwen3.8-27b
	err := cfg.RemoveModel("openrouter", "qwen/qwen3.8-27b")
	if err != nil {
		t.Fatalf("unexpected error on RemoveModel: %v", err)
	}

	// 2. Verifica se foi para removed_models e retirado das listas
	if !cfg.isModelRemovedLocked("openrouter", "qwen/qwen3.8-27b") {
		t.Errorf("model should be in removed_models")
	}
	if cfg.isModelValidForProviderLocked("openrouter", "qwen/qwen3.8-27b") {
		t.Errorf("model should NOT be valid for provider after removal")
	}

	// 3. Simula chamada de fechamento de GUI (defer) com o modelo deletado
	_ = cfg.SaveProviderAndModel("OpenRouter", "qwen/qwen3.8-27b")

	// Garante que o modelo deletado NÃO foi ressuscitado
	if !cfg.isModelRemovedLocked("openrouter", "qwen/qwen3.8-27b") {
		t.Errorf("model must remain in removed_models even after SaveProviderAndModel with old model")
	}

	// 4. Executa sincronização
	_ = cfg.SyncMetisModels()

	// Garante que a sincronização NÃO ressuscitou o modelo deletado
	if !cfg.isModelRemovedLocked("openrouter", "qwen/qwen3.8-27b") {
		t.Errorf("model must remain in removed_models even after SyncMetisModels")
	}
	for _, p := range cfg.GetProvidersList() {
		if p.ID == "openrouter" {
			for _, m := range p.Models {
				if m == "qwen/qwen3.8-27b" {
					t.Errorf("qwen/qwen3.8-27b resurrected in providers list!")
				}
			}
		}
	}

	// 5. Simula reabertura do programa a partir do zero
	cfg2 := Load()
	if cfg2.isModelValidForProviderLocked("openrouter", "qwen/qwen3.8-27b") {
		t.Errorf("model must remain invalid after reloading fresh config")
	}
	if cfg2.OpenRouterModel == "qwen/qwen3.8-27b" {
		t.Errorf("active OpenRouter model should have fallen back, but got %s", cfg2.OpenRouterModel)
	}
	if cfg2.GetActiveTheme().ID != "dracula_synth" {
		t.Errorf("expected theme dracula_synth, got %s", cfg2.GetActiveTheme().ID)
	}
	if cfg2.GetWindowOpacity() != 75 {
		t.Errorf("expected opacity 75, got %d", cfg2.GetWindowOpacity())
	}
}

func TestDeleteAllModelsOfProviderFallback(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	configMetisDir := filepath.Join(tempHome, ".config", "metis")
	_ = os.MkdirAll(configMetisDir, 0755)

	initialJSON := `{
  "active_provider": "openrouter",
  "active_model": "only-model",
  "builtin_models": {
    "Groq": ["qwen/qwen3.8-27b"],
    "OpenRouter": ["only-model"]
  },
  "custom_servers": [
    {
      "id": "openrouter",
      "nome": "OpenRouter",
      "base_url": "https://openrouter.ai/api/v1/chat/completions",
      "api_key_env": "OPENROUTER_API_KEY",
      "modelo_atual": "only-model",
      "modelos": ["only-model"]
    }
  ],
  "preferences": {
    "theme_id": "metis_oracle",
    "last_active_provider": "openrouter",
    "last_active_model": "only-model",
    "active_models": {
      "openrouter": "only-model"
    }
  },
  "removed_models": {}
}`
	_ = os.WriteFile(filepath.Join(configMetisDir, "config_models.json"), []byte(initialJSON), 0600)
	_ = os.WriteFile(filepath.Join(configMetisDir, ".env"), []byte("DEFAULT_PROVIDER=\"6\"\nOPENROUTER_MODEL=\"only-model\"\nGROQ_MODEL=\"qwen/qwen3.8-27b\"\n"), 0600)

	cfg := Load()

	// Remove o único modelo existente no OpenRouter
	_ = cfg.RemoveModel("openrouter", "only-model")

	// O modelo removido deve estar em removed_models
	if !cfg.isModelRemovedLocked("openrouter", "only-model") {
		t.Errorf("only-model should be marked as removed")
	}

	// Como o OpenRouter ficou sem modelos e era o provedor ativo, deve ter feito fallback de provedor para Groq
	if cfg.Provider == "openrouter" {
		t.Errorf("provider should have fallen back from openrouter, but remained %s", cfg.Provider)
	}

	// Simula encerramento do app chamando SaveProviderAndModel com o modelo deletado
	_ = cfg.SaveProviderAndModel("openrouter", "only-model")

	// Garante que o modelo não foi ressuscitado
	if !cfg.isModelRemovedLocked("openrouter", "only-model") {
		t.Errorf("only-model should not be resurrected by SaveProviderAndModel")
	}

	// Recarrega do zero
	cfg2 := Load()
	if cfg2.isModelValidForProviderLocked("openrouter", "only-model") {
		t.Errorf("only-model must not be valid after reload")
	}
}

func TestFullAddRemoveSyncRestartLifecycle(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	configMetisDir := filepath.Join(tempHome, ".config", "metis")
	_ = os.MkdirAll(configMetisDir, 0755)

	initialJSON := `{
  "active_provider": "groq",
  "active_model": "qwen/qwen3.8-27b",
  "builtin_models": {
    "Groq": ["qwen/qwen3.8-27b"],
    "OpenRouter": []
  },
  "custom_servers": [
    {
      "id": "openrouter",
      "nome": "OpenRouter",
      "base_url": "https://openrouter.ai/api/v1/chat/completions",
      "api_key_env": "OPENROUTER_API_KEY",
      "modelo_atual": "",
      "modelos": []
    }
  ],
  "preferences": {
    "theme_id": "metis_oracle",
    "window_opacity": 60,
    "last_active_provider": "groq",
    "last_active_model": "qwen/qwen3.8-27b",
    "active_models": {
      "groq": "qwen/qwen3.8-27b"
    }
  },
  "removed_models": {
    "OpenRouter": ["old-deleted-model"]
  }
}`
	_ = os.WriteFile(filepath.Join(configMetisDir, "config_models.json"), []byte(initialJSON), 0600)
	_ = os.WriteFile(filepath.Join(configMetisDir, ".env"), []byte("DEFAULT_PROVIDER=\"4\"\nGROQ_MODEL=\"qwen/qwen3.8-27b\"\nOPENROUTER_MODEL=\"\"\n"), 0600)

	// 1. Inicia app
	cfg := Load()
	if cfg.Provider != "groq" {
		t.Fatalf("expected initial provider groq, got %s", cfg.Provider)
	}

	// 2. Usuário adiciona modelo no OpenRouter
	err := cfg.AddModel("openrouter", "test/awesome-model")
	if err != nil {
		t.Fatalf("failed to AddModel: %v", err)
	}
	err = cfg.SaveProviderAndModel("openrouter", "test/awesome-model")
	if err != nil {
		t.Fatalf("failed to SaveProviderAndModel: %v", err)
	}

	// Verifica se ficou salvo e ativo
	if cfg.Provider != "openrouter" {
		t.Errorf("provider should be openrouter, got %s", cfg.Provider)
	}
	if cfg.OpenRouterModel != "test/awesome-model" {
		t.Errorf("openrouter model should be test/awesome-model, got %s", cfg.OpenRouterModel)
	}

	// 3. Simula fechar e reabrir o programa
	cfgRestart1 := Load()
	if cfgRestart1.Provider != "openrouter" {
		t.Errorf("after restart 1, provider should be openrouter, got %s", cfgRestart1.Provider)
	}
	if cfgRestart1.OpenRouterModel != "test/awesome-model" {
		t.Errorf("after restart 1, openrouter model should be test/awesome-model, got %s", cfgRestart1.OpenRouterModel)
	}

	// 4. Usuário sincroniza com o Metis
	_ = cfgRestart1.SyncMetisModels()
	if cfgRestart1.OpenRouterModel != "test/awesome-model" {
		t.Errorf("after sync, openrouter model should remain test/awesome-model, got %s", cfgRestart1.OpenRouterModel)
	}

	// 5. Usuário remove o modelo do OpenRouter
	err = cfgRestart1.RemoveModel("openrouter", "test/awesome-model")
	if err != nil {
		t.Fatalf("failed to RemoveModel: %v", err)
	}

	// Como OpenRouter ficou com 0 modelos, deve ter feito fallback de provedor
	if cfgRestart1.Provider == "openrouter" {
		t.Errorf("after removing last model, provider should fallback from openrouter")
	}
	if cfgRestart1.isModelValidForProviderLocked("openrouter", "test/awesome-model") {
		t.Errorf("removed model should no longer be valid")
	}

	// 6. Simula fechar e reabrir o programa
	cfgRestart2 := Load()
	if cfgRestart2.isModelValidForProviderLocked("openrouter", "test/awesome-model") {
		t.Errorf("after restart 2, removed model must remain invalid")
	}
	for _, p := range cfgRestart2.GetProvidersList() {
		if p.ID == "openrouter" {
			if len(p.Models) != 0 {
				t.Errorf("openrouter models should be empty, got %v", p.Models)
			}
			if p.ActiveModel != "" {
				t.Errorf("openrouter active model should be empty, got %q", p.ActiveModel)
			}
		}
	}

	// 7. Sincroniza novamente e reabre
	_ = cfgRestart2.SyncMetisModels()
	cfgRestart3 := Load()
	for _, p := range cfgRestart3.GetProvidersList() {
		if p.ID == "openrouter" {
			if len(p.Models) != 0 {
				t.Errorf("after sync & restart 3, openrouter models must stay empty, got %v", p.Models)
			}
		}
	}
}



