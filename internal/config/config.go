package config

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GetUserHome retorna o diretório HOME respeitando a variável $HOME do ambiente antes do os.UserHomeDir()
func GetUserHome() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	h, err := os.UserHomeDir()
	if err == nil && h != "" {
		return h
	}
	return os.TempDir()
}

func getCanonicalDefaults() map[string][]string {
	return map[string][]string{
		"Gemini":     {"gemini-2.0-flash", "gemini-1.5-flash", "gemini-1.5-pro", "gemini-2.0-flash-lite-preview-02-05"},
		"Groq":       {"qwen/qwen3.8-27b", "openai/gpt-oss-120b", "openai/gpt-oss-20b", "groq/compound"},
		"NVIDIA":     {"meta/llama-3.2-11b-vision-instruct", "meta/llama-3.2-90b-vision-instruct", "deepseek-ai/deepseek-r1"},
		"OpenRouter": {"liquid/lfm-2.5-2.6b:free", "inclusionai/ling-3.0-flash-fin:free", "poolside/laguna-s-2.1:free", "z-ai/glm-5.2:free", "qwen/qwen3.8-27b:free"},
		"Ollama":     {"qwen3.5:4b", "qwen3.5:9b", "deepseek-r1:7b", "llama3.2:3b", "qwen2.5-coder:7b"},
		"OpenAI":     {"gpt-4o-mini", "gpt-4o"},
		"G4F":        {"gpt-4o-mini", "gpt-4o", "deepseek-r1", "claude-3.5-sonnet", "llama-3.3-70b", "blackboxai", "gemini-2.0-flash", "qwen-2.5-coder-32b"},
	}
}

type ProviderItem struct {
	ID          string   `json:"id"`
	Num         string   `json:"num"`
	Name        string   `json:"name"`
	Icon        string   `json:"icon"`
	EnvVar      string   `json:"env_var"`
	ActiveModel string   `json:"active_model"`
	Models      []string `json:"models"`
}

type CustomServer struct {
	ID          string   `json:"id"`
	Nome        string   `json:"nome"`
	BaseURL     string   `json:"base_url"`
	APIKeyEnv   string   `json:"api_key_env"`
	APIKey      string   `json:"api_key"`
	ModeloAtual string   `json:"modelo_atual"`
	Modelos     []string `json:"modelos"`
}

type Preferences struct {
	ThemeID            string            `json:"theme_id"`
	WindowOpacity      int               `json:"window_opacity"`
	NeonGlow           bool              `json:"neon_glow"`
	WebSearchEnabled   bool              `json:"web_search_enabled"`
	LastActiveProvider string            `json:"last_active_provider,omitempty"`
	LastActiveModel    string            `json:"last_active_model,omitempty"`
	ActiveModels       map[string]string `json:"active_models,omitempty"`
}

type ModelsConfigFile struct {
	BuiltinModels  map[string][]string `json:"builtin_models"`
	RemovedModels  map[string][]string `json:"removed_models"`
	RemovedServers []string            `json:"removed_servers"`
	CustomServers  []CustomServer      `json:"custom_servers"`
	Preferences    Preferences         `json:"preferences"`
	ActiveProvider string              `json:"active_provider"`
	ActiveModel    string              `json:"active_model"`
}

type Theme struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Icon          string `json:"icon"`
	Category      string `json:"category"`
	Description   string `json:"description"`
	BgMain        string `json:"bg_main"`
	BgCard        string `json:"bg_card"`
	BgInput       string `json:"bg_input"`
	Border        string `json:"border"`
	BorderGlow    string `json:"border_glow"`
	FgText        string `json:"fg_text"`
	FgSub         string `json:"fg_sub"`
	AccentGold    string `json:"accent_gold"`
	AccentCyan    string `json:"accent_cyan"`
	UserBubble    string `json:"user_bubble"`
	AiBubble      string `json:"ai_bubble"`
	PrimaryBtnBg  string `json:"primary_btn_bg"`
	PrimaryBtnHov string `json:"primary_btn_hover"`
	PrimaryBtnFg  string `json:"primary_btn_fg"`
	BadgeBg       string `json:"badge_bg"`
	BadgeBorder   string `json:"badge_border"`
}

var MetisThemes = map[string]Theme{
	"metis_oracle": {
		ID: "metis_oracle", Name: "Metis Oracle", Icon: "🏛️", Category: "PADRÃO",
		Description: "Dourado âmbar com azul espacial profundo e sobriedade analítica.",
		BgMain:      "#080f1d", BgCard: "#0e1a30", BgInput: "#12223f", Border: "#1e3557",
		BorderGlow: "#fad094", FgText: "#f8fafc", FgSub: "#94a3b8", AccentGold: "#fad094",
		AccentCyan: "#67e8f9", UserBubble: "#193155", AiBubble: "#0e1a30",
		PrimaryBtnBg: "#d97706", PrimaryBtnHov: "#f59e0b", PrimaryBtnFg: "#ffffff",
		BadgeBg: "#0e1c2e", BadgeBorder: "#1c2e47",
	},
	"olympus_greek": {
		ID: "olympus_greek", Name: "Olimpo Sagrado", Icon: "🏺", Category: "MITOLOGIA GREGA",
		Description: "Mármore e bronze de Delfos, ouro divino e a nobreza sábia de Atena.",
		BgMain:      "#17120a", BgCard: "#231c11", BgInput: "#302617", Border: "#5a4522",
		BorderGlow: "#d4af37", FgText: "#fdfbf7", FgSub: "#d6d3d1", AccentGold: "#d4af37",
		AccentCyan: "#93c5fd", UserBubble: "#3d301c", AiBubble: "#231c11",
		PrimaryBtnBg: "#b48b18", PrimaryBtnHov: "#d4af37", PrimaryBtnFg: "#000000",
		BadgeBg: "#2b2111", BadgeBorder: "#5a4522",
	},
	"valhalla_nordic": {
		ID: "valhalla_nordic", Name: "Valhalla & Runas", Icon: "⚡", Category: "MITOLOGIA NÓRDICA",
		Description: "Fiordes glaciais, aço forjado e luzes místicas da Aurora Boreal.",
		BgMain:      "#061523", BgCard: "#0a2238", BgInput: "#0f2d4a", Border: "#184a75",
		BorderGlow: "#38bdf8", FgText: "#f0fdf4", FgSub: "#a5f3fc", AccentGold: "#34d399",
		AccentCyan: "#38bdf8", UserBubble: "#143e66", AiBubble: "#0a2238",
		PrimaryBtnBg: "#0284c7", PrimaryBtnHov: "#0ea5e9", PrimaryBtnFg: "#ffffff",
		BadgeBg: "#0a2842", BadgeBorder: "#184a75",
	},
	"dracula_synth": {
		ID: "dracula_synth", Name: "Dracula Synth", Icon: "🧛", Category: "CYBERPUNK",
		Description: "Roxo cósmico, rosa neon vibrante e estética retrowave futurista.",
		BgMain:      "#170b29", BgCard: "#23113d", BgInput: "#311754", Border: "#5e248f",
		BorderGlow: "#f472b6", FgText: "#fdf4ff", FgSub: "#f0abfc", AccentGold: "#f472b6",
		AccentCyan: "#c084fc", UserBubble: "#441b75", AiBubble: "#23113d",
		PrimaryBtnBg: "#9333ea", PrimaryBtnHov: "#a855f7", PrimaryBtnFg: "#ffffff",
		BadgeBg: "#331354", BadgeBorder: "#5e248f",
	},
	"nord_ocean": {
		ID: "nord_ocean", Name: "Nord Arctic", Icon: "❄️", Category: "MINIMALISTA",
		Description: "Azul glacial ártico, tons frios serenos e clareza polar cristalina.",
		BgMain:      "#0d1726", BgCard: "#142238", BgInput: "#1b2e4c", Border: "#2a4873",
		BorderGlow: "#88c0d0", FgText: "#eceff4", FgSub: "#88c0d0", AccentGold: "#ebcb8b",
		AccentCyan: "#88c0d0", UserBubble: "#263f66", AiBubble: "#142238",
		PrimaryBtnBg: "#434c5e", PrimaryBtnHov: "#5e81ac", PrimaryBtnFg: "#eceff4",
		BadgeBg: "#182a44", BadgeBorder: "#2a4873",
	},
	"matrix_emerald": {
		ID: "matrix_emerald", Name: "Matrix Emerald", Icon: "🌲", Category: "HACKER",
		Description: "Preto e verde fosforoso com realces esmeralda luminosos de terminal.",
		BgMain:      "#031409", BgCard: "#062210", BgInput: "#0a3017", Border: "#155c2d",
		BorderGlow: "#10b981", FgText: "#ecfdf5", FgSub: "#6ee7b7", AccentGold: "#10b981",
		AccentCyan: "#34d399", UserBubble: "#0f421f", AiBubble: "#062210",
		PrimaryBtnBg: "#059669", PrimaryBtnHov: "#10b981", PrimaryBtnFg: "#ffffff",
		BadgeBg: "#0a3618", BadgeBorder: "#155c2d",
	},
	"onyx_mono": {
		ID: "onyx_mono", Name: "Onyx Minimal", Icon: "🌑", Category: "MONOCROMÁTICO",
		Description: "Preto carvão e cinza titânio puro para foco extremo sem distrações.",
		BgMain:      "#121215", BgCard: "#1a1a1f", BgInput: "#24242b", Border: "#363642",
		BorderGlow: "#e4e4e7", FgText: "#fafafa", FgSub: "#a1a1aa", AccentGold: "#f4f4f5",
		AccentCyan: "#a1a1aa", UserBubble: "#2f2f38", AiBubble: "#1a1a1f",
		PrimaryBtnBg: "#27272a", PrimaryBtnHov: "#3f3f46", PrimaryBtnFg: "#ffffff",
		BadgeBg: "#1e1e24", BadgeBorder: "#363642",
	},
}

type Config struct {
	mu                 sync.RWMutex
	Provider           string
	OllamaURL          string
	OllamaModel        string
	GeminiKey          string
	GeminiModel        string
	GroqKey            string
	GroqModel          string
	NvidiaKey          string
	NvidiaModel        string
	OpenRouterKey      string
	OpenRouterModel    string
	OpenAIKey          string
	OpenAIModel        string
	G4FModel           string
	DefaultScreenLines int
	MaxSteps           int
	MaxContextTurns    int
	AutoAllowMultiline bool
	ActiveProviderFile string
	ModelsConfig       *ModelsConfigFile
}

func Load() *Config {
	home := GetUserHome()

	loadEnvFiles(home, true)

	cfgFile := loadModelsConfigFile()

	cfg := &Config{
		OllamaURL:          getEnvOrDefault("OLLAMA_URL", "http://localhost:11434"),
		OllamaModel:        getEnvOrDefault("OLLAMA_MODEL", "llama3.2:3b"),
		GeminiKey:          os.Getenv("GEMINI_API_KEY"),
		GeminiModel:        getEnvOrDefault("GEMINI_MODEL", "gemini-2.0-flash"),
		GroqKey:            os.Getenv("GROQ_API_KEY"),
		GroqModel:          getEnvOrDefault("GROQ_MODEL", "qwen/qwen3.8-27b"),
		NvidiaKey:          os.Getenv("NVIDIA_API_KEY"),
		NvidiaModel:        getEnvOrDefault("NVIDIA_MODEL", "meta/llama-3.2-11b-vision-instruct"),
		OpenRouterKey:      os.Getenv("OPENROUTER_API_KEY"),
		OpenRouterModel:    getEnvOrDefault("OPENROUTER_MODEL", "inclusionai/ling-3.0-flash-fin:free"),
		OpenAIKey:          os.Getenv("OPENAI_API_KEY"),
		OpenAIModel:        getEnvOrDefault("OPENAI_MODEL", "gpt-4o-mini"),
		G4FModel:           getEnvOrDefault("G4F_MODEL", "gpt-4o"),
		DefaultScreenLines: getEnvIntOrDefault("DEFAULT_SCREEN_LINES", 30),
		MaxSteps:           getEnvIntOrDefault("AI_AUTO_MAX_STEPS", 6),
		MaxContextTurns:    getEnvIntOrDefault("MAX_CONTEXT_TURNS", 5),
		AutoAllowMultiline: getEnvBoolOrDefault("AI_AUTO_ALLOW_MULTILINE", false),
		ModelsConfig:       cfgFile,
	}

	// Caminho do arquivo que armazena o último provedor selecionado
	configDir := filepath.Join(home, ".config", "metis")
	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		configDir = filepath.Join(home, ".ZSH", "ai")
	}
	cfg.ActiveProviderFile = filepath.Join(configDir, ".last_provider")

	// Recupera o provedor ativo salvo ou usa fallback
	cfg.Provider = cfg.GetSavedProvider()
	cfg.mu.Lock()
	cfg.applyActiveModelsLocked()
	cfg.mu.Unlock()

	return cfg
}

func getAllModelsConfigFilePaths(home string) []string {
	if home == "" {
		home = GetUserHome()
	}
	candidates := []string{
		filepath.Join(home, ".config", "metis", "config_models.json"),
		filepath.Join(home, "Metis", "config_models.json"),
		filepath.Join(home, ".local", "share", "metis", "app", "config_models.json"),
		filepath.Join(home, ".ZSH", "ai", "config_models.json"),
	}
	var res []string
	seen := make(map[string]bool)
	for _, p := range candidates {
		realP := p
		if target, err := filepath.EvalSymlinks(p); err == nil {
			realP = target
		}
		if !seen[realP] {
			seen[realP] = true
			res = append(res, realP)
		}
	}
	return res
}

func getAllEnvFilePaths(home string) []string {
	if home == "" {
		home = GetUserHome()
	}
	candidates := []string{
		filepath.Join(home, ".config", "metis", ".env"),
		filepath.Join(home, "Metis", ".env"),
		filepath.Join(home, ".local", "share", "metis", ".env"),
		filepath.Join(home, ".ZSH", "ai", ".env_local"),
		filepath.Join(home, ".ZSH", ".env"),
	}
	var res []string
	seen := make(map[string]bool)
	for _, p := range candidates {
		realP := p
		if target, err := filepath.EvalSymlinks(p); err == nil {
			realP = target
		}
		if !seen[realP] {
			seen[realP] = true
			res = append(res, realP)
		}
	}
	return res
}

func getAllLastProviderDirs(home string) []string {
	if home == "" {
		home = GetUserHome()
	}
	return []string{
		filepath.Join(home, ".config", "metis"),
		filepath.Join(home, ".ZSH", "ai"),
		filepath.Join(home, ".local", "share", "metis", "zsh"),
	}
}

func loadModelsConfigFile() *ModelsConfigFile {
	home := GetUserHome()
	candidates := getAllModelsConfigFilePaths(home)

	type fileCandidate struct {
		path    string
		modTime time.Time
	}
	var existing []fileCandidate
	seen := make(map[string]bool)

	for _, p := range candidates {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Size() > 0 {
			realP := p
			if target, err := filepath.EvalSymlinks(p); err == nil {
				realP = target
			}
			if !seen[realP] {
				seen[realP] = true
				existing = append(existing, fileCandidate{path: p, modTime: fi.ModTime()})
			}
		}
	}

	// Ordena do mais recente para o mais antigo (o mais novo é o prioritário)
	sort.Slice(existing, func(i, j int) bool {
		return existing[i].modTime.After(existing[j].modTime)
	})

	defaults := getCanonicalDefaults()

	for _, cand := range existing {
		if data, err := os.ReadFile(cand.path); err == nil && len(data) > 0 {
			var m ModelsConfigFile
			if err := json.Unmarshal(data, &m); err == nil {
				if m.BuiltinModels == nil {
					m.BuiltinModels = make(map[string][]string)
				}
				if m.RemovedModels == nil {
					m.RemovedModels = make(map[string][]string)
				}
				// Normaliza valores null dentro do mapa para slices vazios
				for k, v := range m.RemovedModels {
					if v == nil {
						m.RemovedModels[k] = []string{}
					}
				}

				// Consolida chaves legado/variantes em BuiltinModels (ex: "web g4f" -> "G4F")
				for k, list := range m.BuiltinModels {
					canonK := providerToCanonicalKey(k)
					if canonK != k && canonK != "" {
						existing := m.BuiltinModels[canonK]
						seen := make(map[string]bool)
						for _, em := range existing {
							seen[strings.ToLower(strings.TrimSpace(em))] = true
						}
						for _, em := range list {
							emClean := strings.TrimSpace(em)
							if emClean != "" && !seen[strings.ToLower(emClean)] {
								existing = append(existing, emClean)
								seen[strings.ToLower(emClean)] = true
							}
						}
						m.BuiltinModels[canonK] = existing
						delete(m.BuiltinModels, k)
					}
				}

				// Consolida chaves legado/variantes em RemovedModels
				for k, list := range m.RemovedModels {
					canonK := providerToCanonicalKey(k)
					if canonK != k && canonK != "" {
						existing := m.RemovedModels[canonK]
						seen := make(map[string]bool)
						for _, em := range existing {
							seen[strings.ToLower(strings.TrimSpace(em))] = true
						}
						for _, em := range list {
							emClean := strings.TrimSpace(em)
							if emClean != "" && !seen[strings.ToLower(emClean)] {
								existing = append(existing, emClean)
								seen[strings.ToLower(emClean)] = true
							}
						}
						m.RemovedModels[canonK] = existing
						delete(m.RemovedModels, k)
					}
				}

				for k, defs := range defaults {
					canonDefK := providerToCanonicalKey(k)
					removedMap := make(map[string]bool)
					if m.RemovedModels != nil {
						for rk, rList := range m.RemovedModels {
							if strings.EqualFold(providerToCanonicalKey(rk), canonDefK) {
								for _, rm := range rList {
									removedMap[strings.ToLower(strings.TrimSpace(rm))] = true
								}
							}
						}
					}

					existingMods, hasKey := m.BuiltinModels[k]
					if !hasKey {
						for bk, bList := range m.BuiltinModels {
							if strings.EqualFold(providerToCanonicalKey(bk), canonDefK) {
								existingMods = bList
								hasKey = true
								break
							}
						}
					}

					var cleanExisting []string
					seenMod := make(map[string]bool)
					for _, em := range existingMods {
						cleanM := strings.TrimSpace(em)
						cleanLower := strings.ToLower(cleanM)
						if cleanM != "" && !removedMap[cleanLower] && !seenMod[cleanLower] {
							cleanExisting = append(cleanExisting, cleanM)
							seenMod[cleanLower] = true
						}
					}

					// Se a chave não existia no JSON do usuário, inicializa com os defaults filtrando removidos
					if !hasKey {
						for _, defM := range defs {
							dClean := strings.TrimSpace(defM)
							dLower := strings.ToLower(dClean)
							if dClean != "" && !seenMod[dLower] && !removedMap[dLower] {
								cleanExisting = append(cleanExisting, dClean)
								seenMod[dLower] = true
							}
						}
					}

					m.BuiltinModels[k] = cleanExisting
				}

				// Filtra modelos removidos de CustomServers também
				for i, srv := range m.CustomServers {
					canonK := providerToCanonicalKey(srv.ID)
					normK := normalizeProvider(srv.ID)
					removedMap := make(map[string]bool)
					if m.RemovedModels != nil {
						for rk, rList := range m.RemovedModels {
							if strings.EqualFold(providerToCanonicalKey(rk), canonK) ||
								strings.EqualFold(normalizeProvider(rk), normK) ||
								strings.EqualFold(rk, srv.Nome) ||
								strings.EqualFold(rk, srv.ID) {
								for _, rm := range rList {
									removedMap[strings.ToLower(strings.TrimSpace(rm))] = true
								}
							}
						}
					}
					var cleanModelos []string
					for _, mod := range srv.Modelos {
						modClean := strings.TrimSpace(mod)
						if modClean != "" && !removedMap[strings.ToLower(modClean)] {
							cleanModelos = append(cleanModelos, modClean)
						}
					}
					m.CustomServers[i].Modelos = cleanModelos
					if removedMap[strings.ToLower(strings.TrimSpace(srv.ModeloAtual))] {
						if len(cleanModelos) > 0 {
							m.CustomServers[i].ModeloAtual = cleanModelos[0]
						} else {
							m.CustomServers[i].ModeloAtual = ""
						}
					}
				}

				if m.Preferences.ActiveModels == nil {
					m.Preferences.ActiveModels = make(map[string]string)
				}
				if m.ActiveProvider == "" && m.Preferences.LastActiveProvider != "" {
					m.ActiveProvider = normalizeProvider(m.Preferences.LastActiveProvider)
				}
				if m.ActiveModel == "" && m.Preferences.LastActiveModel != "" {
					m.ActiveModel = m.Preferences.LastActiveModel
				}
				if m.ActiveProvider != "" && m.ActiveModel != "" {
					canon := normalizeProvider(m.ActiveProvider)
					if _, ok := m.Preferences.ActiveModels[canon]; !ok {
						m.Preferences.ActiveModels[canon] = m.ActiveModel
					}
				}
				if m.Preferences.LastActiveProvider != "" && m.Preferences.LastActiveModel != "" {
					canon := normalizeProvider(m.Preferences.LastActiveProvider)
					if _, ok := m.Preferences.ActiveModels[canon]; !ok {
						m.Preferences.ActiveModels[canon] = m.Preferences.LastActiveModel
					}
				}

				// Purga modelos que estejam em RemovedModels de Preferences.ActiveModels e ActiveModel
				if m.RemovedModels != nil {
					for provKey, actM := range m.Preferences.ActiveModels {
						canonP := providerToCanonicalKey(provKey)
						normP := normalizeProvider(provKey)
						actMLow := strings.ToLower(strings.TrimSpace(actM))
						isRem := false
						for rk, rList := range m.RemovedModels {
							if strings.EqualFold(providerToCanonicalKey(rk), canonP) || strings.EqualFold(normalizeProvider(rk), normP) || strings.EqualFold(rk, provKey) {
								for _, rm := range rList {
									if strings.ToLower(strings.TrimSpace(rm)) == actMLow {
										isRem = true
										break
									}
								}
							}
							if isRem {
								break
							}
						}
						if isRem {
							delete(m.Preferences.ActiveModels, provKey)
							if strings.EqualFold(m.ActiveProvider, provKey) {
								m.ActiveModel = ""
							}
							if strings.EqualFold(m.Preferences.LastActiveProvider, provKey) {
								m.Preferences.LastActiveModel = ""
							}
						}
					}
				}

				return &m
			}
		}
	}

	return &ModelsConfigFile{
		BuiltinModels:  defaults,
		RemovedModels:  make(map[string][]string),
		RemovedServers: []string{},
		CustomServers:  []CustomServer{},
		Preferences: Preferences{
			ThemeID:      "metis_oracle",
			ActiveModels: make(map[string]string),
		},
	}
}

func (c *Config) GetActiveTheme() Theme {
	c.mu.RLock()
	defer c.mu.RUnlock()

	themeID := ""
	if c.ModelsConfig != nil && c.ModelsConfig.Preferences.ThemeID != "" {
		themeID = c.ModelsConfig.Preferences.ThemeID
	} else {
		home := GetUserHome()
		themeFile := filepath.Join(home, ".config", "metis", ".last_theme")
		if data, err := os.ReadFile(themeFile); err == nil {
			themeID = strings.TrimSpace(string(data))
		}
	}

	if t, ok := MetisThemes[themeID]; ok {
		return t
	}
	return MetisThemes["metis_oracle"]
}

func (c *Config) GetWindowOpacity() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if v := os.Getenv("METIS_WINDOW_OPACITY"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i >= 30 && i <= 100 {
			return i
		}
	}

	if c.ModelsConfig != nil && c.ModelsConfig.Preferences.WindowOpacity >= 30 && c.ModelsConfig.Preferences.WindowOpacity <= 100 {
		return c.ModelsConfig.Preferences.WindowOpacity
	}
	return 85
}

func (c *Config) GetProvidersList() []ProviderItem {
	c.mu.RLock()
	m := c.ModelsConfig
	g4fModel := c.G4FModel
	ollamaModel := c.OllamaModel
	geminiModel := c.GeminiModel
	groqModel := c.GroqModel
	nvidiaModel := c.NvidiaModel
	openRouterModel := c.OpenRouterModel
	c.mu.RUnlock()

	if m == nil || m.BuiltinModels == nil {
		c.mu.Lock()
		c.ModelsConfig = loadModelsConfigFile()
		m = c.ModelsConfig
		c.mu.Unlock()
	}

	getModelList := func(key string, fallback []string) []string {
		var list []string
		seen := make(map[string]bool)

		// 1. Modelos do BuiltinModels
		if m != nil && m.BuiltinModels != nil {
			var raw []string
			if models, ok := m.BuiltinModels[key]; ok && len(models) > 0 {
				raw = models
			} else {
				for k, v := range m.BuiltinModels {
					if strings.EqualFold(k, key) && len(v) > 0 {
						raw = v
						break
					}
				}
			}
			for _, mod := range raw {
				mod = strings.TrimSpace(mod)
				if mod != "" && !seen[strings.ToLower(mod)] {
					seen[strings.ToLower(mod)] = true
					list = append(list, mod)
				}
			}
		}

		// 2. Modelos em custom_servers com ID ou Nome correspondente
		if m != nil && len(m.CustomServers) > 0 {
			for _, srv := range m.CustomServers {
				if strings.EqualFold(srv.ID, key) || strings.EqualFold(srv.Nome, key) {
					for _, mod := range srv.Modelos {
						mod = strings.TrimSpace(mod)
						if mod != "" && !seen[strings.ToLower(mod)] {
							seen[strings.ToLower(mod)] = true
							list = append(list, mod)
						}
					}
				}
			}
		}

		if len(list) == 0 && (m == nil || m.BuiltinModels == nil || len(m.BuiltinModels) == 0) {
			for _, mod := range fallback {
				mod = strings.TrimSpace(mod)
				if mod != "" && !seen[strings.ToLower(mod)] {
					seen[strings.ToLower(mod)] = true
					list = append(list, mod)
				}
			}
		}

		// 3. Filtra estritamente os modelos marcados como removidos em removed_models
		if m != nil && m.RemovedModels != nil {
			removedMap := make(map[string]bool)
			canonKey := providerToCanonicalKey(key)
			for k, rList := range m.RemovedModels {
				if strings.EqualFold(providerToCanonicalKey(k), canonKey) {
					for _, rm := range rList {
						removedMap[strings.ToLower(strings.TrimSpace(rm))] = true
					}
				}
			}
			var filtered []string
			for _, mod := range list {
				if !removedMap[strings.ToLower(mod)] {
					filtered = append(filtered, mod)
				}
			}
			return filtered
		}

		return list
	}

	geminiModels := getModelList("Gemini", []string{"gemini-2.0-flash", "gemini-1.5-flash", "gemini-1.5-pro"})
	groqModels := getModelList("Groq", []string{"qwen/qwen3.8-27b", "openai/gpt-oss-120b", "openai/gpt-oss-20b", "groq/compound"})
	nvidiaModels := getModelList("NVIDIA", []string{"meta/llama-3.2-11b-vision-instruct", "meta/llama-3.2-90b-vision-instruct"})
	openRouterModels := getModelList("OpenRouter", []string{"liquid/lfm-2.5-2.6b:free", "inclusionai/ling-3.0-flash-fin:free", "poolside/laguna-s-2.1:free", "z-ai/glm-5.2:free", "qwen/qwen3.8-27b:free"})
	ollamaModels := getModelList("Ollama", []string{"llama3.2:3b", "qwen2.5-coder:7b", "qwen3.5:9b", "deepseek-r1:7b"})
	g4fModels := getModelList("G4F", []string{"gpt-4o", "gpt-4o-mini", "deepseek-r1", "llama-3.3-70b"})

	sanitizeActiveModel := func(curActive string, models []string) string {
		if len(models) == 0 {
			return ""
		}
		for _, m := range models {
			if strings.EqualFold(strings.TrimSpace(m), strings.TrimSpace(curActive)) {
				return m
			}
		}
		return models[0]
	}

	ollamaModel = sanitizeActiveModel(ollamaModel, ollamaModels)
	g4fModel = sanitizeActiveModel(g4fModel, g4fModels)
	geminiModel = sanitizeActiveModel(geminiModel, geminiModels)
	groqModel = sanitizeActiveModel(groqModel, groqModels)
	nvidiaModel = sanitizeActiveModel(nvidiaModel, nvidiaModels)
	openRouterModel = sanitizeActiveModel(openRouterModel, openRouterModels)

	list := []ProviderItem{
		{
			ID:          "ollama",
			Num:         "1",
			Name:        "Ollama Local",
			Icon:        "🤖",
			EnvVar:      "OLLAMA_MODEL",
			ActiveModel: ollamaModel,
			Models:      ollamaModels,
		},
		{
			ID:          "g4f",
			Num:         "2",
			Name:        "Web G4F",
			Icon:        "🌍",
			EnvVar:      "G4F_MODEL",
			ActiveModel: g4fModel,
			Models:      g4fModels,
		},
		{
			ID:          "gemini",
			Num:         "3",
			Name:        "Gemini",
			Icon:        "✨",
			EnvVar:      "GEMINI_MODEL",
			ActiveModel: geminiModel,
			Models:      geminiModels,
		},
		{
			ID:          "groq",
			Num:         "4",
			Name:        "Groq",
			Icon:        "🚀",
			EnvVar:      "GROQ_MODEL",
			ActiveModel: groqModel,
			Models:      groqModels,
		},
		{
			ID:          "nvidia",
			Num:         "5",
			Name:        "NVIDIA",
			Icon:        "🟢",
			EnvVar:      "NVIDIA_MODEL",
			ActiveModel: nvidiaModel,
			Models:      nvidiaModels,
		},
		{
			ID:          "openrouter",
			Num:         "6",
			Name:        "OpenRouter",
			Icon:        "🪐",
			EnvVar:      "OPENROUTER_MODEL",
			ActiveModel: openRouterModel,
			Models:      openRouterModels,
		},
	}

	// Adiciona os servidores customizados cadastrados no Metis
	if m != nil && len(m.CustomServers) > 0 {
		removedMap := make(map[string]bool)
		for _, rs := range m.RemovedServers {
			removedMap[strings.ToLower(rs)] = true
		}

		for idx, srv := range m.CustomServers {
			srvID := strings.ToLower(srv.ID)
			if srvID == "" || removedMap[srvID] || removedMap[strings.ToLower(srv.Nome)] {
				continue
			}
			// Ignora se for builtin já listado
			if srvID == "groq" || srvID == "gemini" || srvID == "nvidia" || srvID == "openrouter" || srvID == "ollama" || srvID == "g4f" {
				continue
			}

			// Filtra modelos removidos deste servidor
			srvRemoved := make(map[string]bool)
			if m.RemovedModels != nil {
				for _, rm := range m.RemovedModels[providerToCanonicalKey(srv.ID)] {
					srvRemoved[strings.ToLower(strings.TrimSpace(rm))] = true
				}
				for _, rm := range m.RemovedModels[srv.Nome] {
					srvRemoved[strings.ToLower(strings.TrimSpace(rm))] = true
				}
			}
			var cleanModels []string
			for _, mod := range srv.Modelos {
				modClean := strings.TrimSpace(mod)
				if modClean != "" && !srvRemoved[strings.ToLower(modClean)] {
					cleanModels = append(cleanModels, modClean)
				}
			}

			activeM := sanitizeActiveModel(srv.ModeloAtual, cleanModels)

			list = append(list, ProviderItem{
				ID:          srv.ID,
				Num:         strconv.Itoa(7 + idx),
				Name:        srv.Nome,
				Icon:        "🌐",
				EnvVar:      srv.APIKeyEnv,
				ActiveModel: activeM,
				Models:      cleanModels,
			})
		}
	}

	return list
}

// Reload sincroniza e recarrega em tempo real todas as configurações do Metis (.env e config_models.json)
func (c *Config) Reload() {
	home := GetUserHome()

	// 1. Carregar dados FORA do lock (I/O lento)
	loadEnvFiles(home, true)
	newConfig := loadModelsConfigFile()

	// 2. Aplicar mudanças DENTRO do lock (rápido)
	c.mu.Lock()
	defer c.mu.Unlock()

	c.ModelsConfig = newConfig

	// Atualiza propriedades internas
	c.OllamaURL = getEnvOrDefault("OLLAMA_URL", "http://localhost:11434")
	c.OllamaModel = getEnvOrDefault("OLLAMA_MODEL", "llama3.2:3b")
	c.GeminiKey = os.Getenv("GEMINI_API_KEY")
	c.GeminiModel = getEnvOrDefault("GEMINI_MODEL", "gemini-2.0-flash")
	c.GroqKey = os.Getenv("GROQ_API_KEY")
	c.GroqModel = getEnvOrDefault("GROQ_MODEL", "qwen/qwen3.8-27b")
	c.NvidiaKey = os.Getenv("NVIDIA_API_KEY")
	c.NvidiaModel = getEnvOrDefault("NVIDIA_MODEL", "meta/llama-3.2-11b-vision-instruct")
	c.OpenRouterKey = os.Getenv("OPENROUTER_API_KEY")
	c.OpenRouterModel = getEnvOrDefault("OPENROUTER_MODEL", "inclusionai/ling-3.0-flash-fin:free")
	c.OpenAIKey = os.Getenv("OPENAI_API_KEY")
	c.OpenAIModel = getEnvOrDefault("OPENAI_MODEL", "gpt-4o-mini")
	c.G4FModel = getEnvOrDefault("G4F_MODEL", "gpt-4o")
	c.AutoAllowMultiline = getEnvBoolOrDefault("AI_AUTO_ALLOW_MULTILINE", false)

	// Atualiza o provedor ativo
	c.Provider = c.getSavedProviderLocked()
	c.applyActiveModelsLocked()
}

func (c *Config) GetSavedProvider() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.getSavedProviderLocked()
}

func (c *Config) getSavedProviderLocked() string {
	if data, err := os.ReadFile(c.ActiveProviderFile); err == nil {
		p := strings.TrimSpace(string(data))
		if p != "" {
			return normalizeProvider(p)
		}
	}
	if c.ModelsConfig != nil && c.ModelsConfig.ActiveProvider != "" {
		return normalizeProvider(c.ModelsConfig.ActiveProvider)
	}
	if def := os.Getenv("DEFAULT_PROVIDER"); def != "" {
		return normalizeProvider(def)
	}
	return "groq"
}

func (c *Config) GetActiveModelForProvider(prov string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.getActiveModelForProviderLocked(prov)
}

func (c *Config) getActiveModelForProviderLocked(prov string) string {
	norm := normalizeProvider(prov)
	switch norm {
	case "gemini":
		if c.GeminiModel != "" {
			return c.GeminiModel
		}
	case "groq":
		if c.GroqModel != "" {
			return c.GroqModel
		}
	case "nvidia":
		if c.NvidiaModel != "" {
			return c.NvidiaModel
		}
	case "openrouter":
		if c.OpenRouterModel != "" {
			return c.OpenRouterModel
		}
	case "ollama":
		if c.OllamaModel != "" {
			return c.OllamaModel
		}
	case "g4f":
		if c.G4FModel != "" {
			return c.G4FModel
		}
	case "openai":
		if c.OpenAIModel != "" {
			return c.OpenAIModel
		}
	default:
		if c.ModelsConfig != nil {
			for _, srv := range c.ModelsConfig.CustomServers {
				if strings.EqualFold(srv.ID, prov) || strings.EqualFold(srv.Nome, prov) {
					if srv.ModeloAtual != "" {
						return srv.ModeloAtual
					}
					if len(srv.Modelos) > 0 {
						return srv.Modelos[0]
					}
				}
			}
		}
	}
	mods := c.getModelsForProviderLocked(norm)
	if len(mods) > 0 {
		return mods[0]
	}
	return ""
}

func (c *Config) setActiveModelForProviderLocked(prov, model string) {
	norm := normalizeProvider(prov)
	switch norm {
	case "gemini":
		c.GeminiModel = model
	case "groq":
		c.GroqModel = model
	case "nvidia":
		c.NvidiaModel = model
	case "openrouter":
		c.OpenRouterModel = model
	case "ollama":
		c.OllamaModel = model
	case "g4f":
		c.G4FModel = model
	case "openai":
		c.OpenAIModel = model
	}

	if c.ModelsConfig != nil {
		for i, srv := range c.ModelsConfig.CustomServers {
			if strings.EqualFold(srv.ID, prov) || strings.EqualFold(srv.Nome, prov) ||
				strings.EqualFold(normalizeProvider(srv.ID), norm) || strings.EqualFold(normalizeProvider(srv.Nome), norm) {
				c.ModelsConfig.CustomServers[i].ModeloAtual = model
			}
		}
	}
}

func (c *Config) IsModelValidForProvider(prov, model string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isModelValidForProviderLocked(prov, model)
}

func (c *Config) isModelRemovedLocked(prov, model string) bool {
	norm := normalizeProvider(prov)
	canonKey := providerToCanonicalKey(norm)
	m := c.ModelsConfig
	if m == nil || m.RemovedModels == nil {
		return false
	}
	modelLower := strings.ToLower(strings.TrimSpace(model))
	if modelLower == "" {
		return false
	}

	for rk, rList := range m.RemovedModels {
		if strings.EqualFold(providerToCanonicalKey(rk), canonKey) ||
			strings.EqualFold(normalizeProvider(rk), norm) ||
			strings.EqualFold(rk, prov) {
			for _, rm := range rList {
				if strings.ToLower(strings.TrimSpace(rm)) == modelLower {
					return true
				}
			}
		}
	}
	return false
}

func (c *Config) isModelKnownLocked(prov, model string) bool {
	norm := normalizeProvider(prov)
	canonKey := providerToCanonicalKey(norm)
	m := c.ModelsConfig
	if m == nil {
		return false
	}
	modelLower := strings.ToLower(strings.TrimSpace(model))
	if modelLower == "" {
		return false
	}

	if m.BuiltinModels != nil {
		for bk, bList := range m.BuiltinModels {
			if strings.EqualFold(providerToCanonicalKey(bk), canonKey) || strings.EqualFold(bk, norm) {
				for _, mod := range bList {
					if strings.ToLower(strings.TrimSpace(mod)) == modelLower {
						return true
					}
				}
			}
		}
	}

	for _, srv := range m.CustomServers {
		if strings.EqualFold(srv.ID, prov) || strings.EqualFold(srv.Nome, prov) ||
			strings.EqualFold(normalizeProvider(srv.ID), norm) || strings.EqualFold(normalizeProvider(srv.Nome), norm) {
			for _, mod := range srv.Modelos {
				if strings.ToLower(strings.TrimSpace(mod)) == modelLower {
					return true
				}
			}
		}
	}

	return false
}

func (c *Config) isModelValidForProviderLocked(prov, model string) bool {
	if strings.TrimSpace(model) == "" {
		return false
	}
	if c.isModelRemovedLocked(prov, model) {
		return false
	}
	models := c.getModelsForProviderLocked(prov)
	for _, m := range models {
		if strings.EqualFold(strings.TrimSpace(m), strings.TrimSpace(model)) {
			return true
		}
	}
	return false
}

func (c *Config) getFirstValidModelForProviderLocked(prov string) string {
	models := c.getModelsForProviderLocked(prov)
	if len(models) > 0 {
		return models[0]
	}
	return ""
}

func (c *Config) getModelsForProviderLocked(prov string) []string {
	norm := normalizeProvider(prov)
	m := c.ModelsConfig
	canonKey := providerToCanonicalKey(norm)
	var list []string
	seen := make(map[string]bool)

	// 1. BuiltinModels
	if m != nil && m.BuiltinModels != nil {
		var raw []string
		if mods, ok := m.BuiltinModels[canonKey]; ok && len(mods) > 0 {
			raw = mods
		} else {
			for k, v := range m.BuiltinModels {
				if strings.EqualFold(k, canonKey) && len(v) > 0 {
					raw = v
					break
				}
			}
		}
		for _, mod := range raw {
			mod = strings.TrimSpace(mod)
			if mod != "" && !seen[strings.ToLower(mod)] {
				seen[strings.ToLower(mod)] = true
				list = append(list, mod)
			}
		}
	}

	// 2. CustomServers
	if m != nil && len(m.CustomServers) > 0 {
		for _, srv := range m.CustomServers {
			if strings.EqualFold(srv.ID, norm) || strings.EqualFold(srv.Nome, norm) ||
				strings.EqualFold(normalizeProvider(srv.ID), norm) || strings.EqualFold(normalizeProvider(srv.Nome), norm) {
				for _, mod := range srv.Modelos {
					mod = strings.TrimSpace(mod)
					if mod != "" && !seen[strings.ToLower(mod)] {
						seen[strings.ToLower(mod)] = true
						list = append(list, mod)
					}
				}
			}
		}
	}

	// 3. Defaults fallback SOMENTE se o provedor for totalmente desconhecido no JSON
	if len(list) == 0 && (m == nil || m.BuiltinModels == nil || len(m.BuiltinModels) == 0) {
		defaults := getCanonicalDefaults()
		if defs, ok := defaults[canonKey]; ok {
			for _, mod := range defs {
				mod = strings.TrimSpace(mod)
				if mod != "" && !seen[strings.ToLower(mod)] {
					seen[strings.ToLower(mod)] = true
					list = append(list, mod)
				}
			}
		}
	}

	// 4. Filtra RemovedModels
	if m != nil && m.RemovedModels != nil {
		removedMap := make(map[string]bool)
		for k, rList := range m.RemovedModels {
			if strings.EqualFold(providerToCanonicalKey(k), canonKey) ||
				strings.EqualFold(normalizeProvider(k), norm) ||
				strings.EqualFold(k, prov) {
				for _, rm := range rList {
					removedMap[strings.ToLower(strings.TrimSpace(rm))] = true
				}
			}
		}
		for _, srv := range m.CustomServers {
			if strings.EqualFold(srv.ID, norm) || strings.EqualFold(srv.Nome, norm) ||
				strings.EqualFold(normalizeProvider(srv.ID), norm) || strings.EqualFold(normalizeProvider(srv.Nome), norm) {
				for k, rList := range m.RemovedModels {
					if strings.EqualFold(k, srv.Nome) || strings.EqualFold(k, srv.ID) {
						for _, rm := range rList {
							removedMap[strings.ToLower(strings.TrimSpace(rm))] = true
						}
					}
				}
			}
		}
		var filtered []string
		for _, mod := range list {
			if !removedMap[strings.ToLower(mod)] {
				filtered = append(filtered, mod)
			}
		}
		return filtered
	}

	return list
}

func (c *Config) applyActiveModelsLocked() {
	if c.ModelsConfig == nil {
		return
	}
	if c.ModelsConfig.Preferences.ActiveModels == nil {
		c.ModelsConfig.Preferences.ActiveModels = make(map[string]string)
	}

	// 1. Aplica modelos configurados em Preferences.ActiveModels para cada provedor se forem válidos
	for provID, model := range c.ModelsConfig.Preferences.ActiveModels {
		norm := normalizeProvider(provID)
		if model != "" && c.isModelValidForProviderLocked(norm, model) {
			c.setActiveModelForProviderLocked(norm, model)
		}
	}

	// 2. Se o active_model do arquivo JSON corresponder ao provedor ativo e for válido
	normActive := normalizeProvider(c.ModelsConfig.ActiveProvider)
	if normActive != "" && c.ModelsConfig.ActiveModel != "" {
		if c.isModelValidForProviderLocked(normActive, c.ModelsConfig.ActiveModel) {
			c.setActiveModelForProviderLocked(normActive, c.ModelsConfig.ActiveModel)
			c.ModelsConfig.Preferences.ActiveModels[normActive] = c.ModelsConfig.ActiveModel
		}
	}

	// 3. Valida todos os provedores padrão para garantir que nenhum tenha modelo vazio ou inválido
	for _, p := range []string{"ollama", "g4f", "gemini", "groq", "nvidia", "openrouter"} {
		cur := c.getActiveModelForProviderLocked(p)
		if !c.isModelValidForProviderLocked(p, cur) {
			if first := c.getFirstValidModelForProviderLocked(p); first != "" {
				c.setActiveModelForProviderLocked(p, first)
				c.ModelsConfig.Preferences.ActiveModels[p] = first
			} else {
				c.setActiveModelForProviderLocked(p, "")
				delete(c.ModelsConfig.Preferences.ActiveModels, p)
			}
		} else {
			c.ModelsConfig.Preferences.ActiveModels[p] = cur
		}
	}

	// 4. Valida servidores customizados
	for i, srv := range c.ModelsConfig.CustomServers {
		sid := normalizeProvider(srv.ID)
		if sid == "groq" || sid == "gemini" || sid == "nvidia" || sid == "openrouter" || sid == "ollama" || sid == "g4f" {
			c.ModelsConfig.CustomServers[i].ModeloAtual = c.getActiveModelForProviderLocked(sid)
			continue
		}
		cur := srv.ModeloAtual
		if !c.isModelValidForProviderLocked(sid, cur) {
			if first := c.getFirstValidModelForProviderLocked(sid); first != "" {
				c.ModelsConfig.CustomServers[i].ModeloAtual = first
				c.ModelsConfig.Preferences.ActiveModels[sid] = first
			} else {
				c.ModelsConfig.CustomServers[i].ModeloAtual = ""
				delete(c.ModelsConfig.Preferences.ActiveModels, sid)
			}
		} else if cur != "" {
			c.ModelsConfig.Preferences.ActiveModels[sid] = cur
		}
	}

	// 5. Garante que c.ModelsConfig.ActiveModel reflita o modelo real do provedor ativo atual
	activeM := c.getActiveModelForProviderLocked(c.Provider)
	if activeM == "" || !c.isModelValidForProviderLocked(c.Provider, activeM) {
		for _, p := range []string{"groq", "gemini", "nvidia", "openrouter", "ollama", "g4f"} {
			if m := c.getFirstValidModelForProviderLocked(p); m != "" {
				c.Provider = p
				activeM = m
				c.setActiveModelForProviderLocked(p, m)
				home := GetUserHome()
				for _, ef := range getAllEnvFilePaths(home) {
					updateEnvVarInFile(ef, "DEFAULT_PROVIDER", providerToNum(p))
				}
				for _, d := range getAllLastProviderDirs(home) {
					_ = os.WriteFile(filepath.Join(d, ".last_provider"), []byte(providerToNum(p)+"\n"), 0644)
					_ = os.WriteFile(filepath.Join(d, ".fix_ia_selected"), []byte(providerToLabel(p)+"\n"), 0644)
				}
				break
			}
		}
	}
	c.ModelsConfig.ActiveProvider = c.Provider
	c.ModelsConfig.ActiveModel = activeM
	c.ModelsConfig.Preferences.LastActiveProvider = c.Provider
	c.ModelsConfig.Preferences.LastActiveModel = activeM
	if activeM != "" {
		c.ModelsConfig.Preferences.ActiveModels[c.Provider] = activeM
	}

	// 6. Verificação final: garantir que o Provider ativo tenha modelo válido
	finalActiveModel := c.getActiveModelForProviderLocked(c.Provider)
	if finalActiveModel == "" || !c.isModelValidForProviderLocked(c.Provider, finalActiveModel) {
		// Provider ativo perdeu seu modelo, tentar fallback
		for _, p := range []string{"groq", "gemini", "nvidia", "openrouter", "ollama", "g4f"} {
			if m := c.getFirstValidModelForProviderLocked(p); m != "" {
				c.Provider = p
				c.setActiveModelForProviderLocked(p, m)
				c.ModelsConfig.Preferences.ActiveModels[p] = m
				break
			}
		}
	}
}

func (c *Config) SaveProvider(provider string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	norm := normalizeProvider(provider)
	c.Provider = norm

	if c.ModelsConfig == nil {
		c.ModelsConfig = loadModelsConfigFile()
	}

	model := c.getActiveModelForProviderLocked(norm)
	if model == "" || c.isModelRemovedLocked(norm, model) {
		model = c.getFirstValidModelForProviderLocked(norm)
	}

	c.setActiveModelForProviderLocked(norm, model)

	c.ModelsConfig.ActiveProvider = norm
	c.ModelsConfig.ActiveModel = model
	c.ModelsConfig.Preferences.LastActiveProvider = norm
	c.ModelsConfig.Preferences.LastActiveModel = model

	if c.ModelsConfig.Preferences.ActiveModels == nil {
		c.ModelsConfig.Preferences.ActiveModels = make(map[string]string)
	}
	if model != "" {
		c.ModelsConfig.Preferences.ActiveModels[norm] = model
	} else {
		delete(c.ModelsConfig.Preferences.ActiveModels, norm)
	}

	envVar := getEnvVarForProvider(norm)
	if envVar == "" && c.ModelsConfig != nil {
		for _, srv := range c.ModelsConfig.CustomServers {
			if strings.EqualFold(srv.ID, provider) || strings.EqualFold(srv.Nome, provider) ||
				strings.EqualFold(normalizeProvider(srv.ID), norm) {
				cleanID := strings.ToUpper(strings.ReplaceAll(srv.ID, "-", "_"))
				envVar = cleanID + "_MODEL"
				break
			}
		}
	}

	home := GetUserHome()
	envFiles := getAllEnvFilePaths(home)
	for _, ef := range envFiles {
		updateEnvVarInFile(ef, "DEFAULT_PROVIDER", providerToNum(norm))
		if envVar != "" {
			updateEnvVarInFile(ef, envVar, model)
		}
	}

	dirs := getAllLastProviderDirs(home)
	for _, d := range dirs {
		_ = os.MkdirAll(d, 0755)
		_ = os.WriteFile(filepath.Join(d, ".last_provider"), []byte(providerToNum(norm)+"\n"), 0644)
		_ = os.WriteFile(filepath.Join(d, ".fix_ia_selected"), []byte(providerToLabel(norm)+"\n"), 0644)
	}

	return c.saveModelsConfigLocked()
}

func (c *Config) SaveTheme(themeID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ModelsConfig == nil {
		c.ModelsConfig = loadModelsConfigFile()
	}
	c.ModelsConfig.Preferences.ThemeID = themeID
	err := c.saveModelsConfigLocked()

	home := GetUserHome()
	dirs := getAllLastProviderDirs(home)
	for _, d := range dirs {
		_ = os.MkdirAll(d, 0755)
		_ = os.WriteFile(filepath.Join(d, ".last_theme"), []byte(themeID+"\n"), 0644)
	}

	return err
}

func (c *Config) SaveWindowOpacity(opacity int) error {
	if opacity < 30 {
		opacity = 30
	} else if opacity > 100 {
		opacity = 100
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ModelsConfig == nil {
		c.ModelsConfig = loadModelsConfigFile()
	}
	c.ModelsConfig.Preferences.WindowOpacity = opacity
	return c.saveModelsConfigLocked()
}

func (c *Config) GetMaxSteps() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.MaxSteps
}

func (c *Config) SetMaxSteps(steps int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.MaxSteps = steps
}

func (c *Config) SaveProviderAndModel(provider, model string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	norm := normalizeProvider(provider)
	c.Provider = norm

	if c.ModelsConfig == nil {
		c.ModelsConfig = loadModelsConfigFile()
	}

	// Se o modelo fornecido foi explicitamente removido pelo usuário ou é vazio, busca o modelo válido atual ou o primeiro válido
	if model == "" || c.isModelRemovedLocked(norm, model) {
		model = c.getActiveModelForProviderLocked(norm)
		if model == "" || c.isModelRemovedLocked(norm, model) {
			model = c.getFirstValidModelForProviderLocked(norm)
		}
	}

	c.setActiveModelForProviderLocked(norm, model)

	c.ModelsConfig.ActiveProvider = norm
	c.ModelsConfig.ActiveModel = model
	c.ModelsConfig.Preferences.LastActiveProvider = norm
	c.ModelsConfig.Preferences.LastActiveModel = model

	if c.ModelsConfig.Preferences.ActiveModels == nil {
		c.ModelsConfig.Preferences.ActiveModels = make(map[string]string)
	}
	if model != "" {
		c.ModelsConfig.Preferences.ActiveModels[norm] = model
	} else {
		delete(c.ModelsConfig.Preferences.ActiveModels, norm)
	}

	envVar := getEnvVarForProvider(norm)
	if envVar == "" && c.ModelsConfig != nil {
		for _, srv := range c.ModelsConfig.CustomServers {
			if strings.EqualFold(srv.ID, provider) || strings.EqualFold(srv.Nome, provider) ||
				strings.EqualFold(normalizeProvider(srv.ID), norm) {
				cleanID := strings.ToUpper(strings.ReplaceAll(srv.ID, "-", "_"))
				envVar = cleanID + "_MODEL"
				break
			}
		}
	}

	home := GetUserHome()

	// 1. Grava nos arquivos .env
	envFiles := getAllEnvFilePaths(home)
	for _, ef := range envFiles {
		updateEnvVarInFile(ef, "DEFAULT_PROVIDER", providerToNum(norm))
		if envVar != "" {
			updateEnvVarInFile(ef, envVar, model)
		}
	}

	// 2. Grava nos arquivos .last_provider e .fix_ia_selected em todos os diretórios
	dirs := getAllLastProviderDirs(home)
	for _, d := range dirs {
		_ = os.MkdirAll(d, 0755)
		_ = os.WriteFile(filepath.Join(d, ".last_provider"), []byte(providerToNum(norm)+"\n"), 0644)
		_ = os.WriteFile(filepath.Join(d, ".fix_ia_selected"), []byte(providerToLabel(norm)+"\n"), 0644)
	}

	// 3. Se for um modelo legítimo que ainda não exista na lista de modelos do provedor e não esteja removido, adiciona
	if model != "" && !c.isModelRemovedLocked(norm, model) && !c.isModelKnownLocked(norm, model) {
		return c.addModelLocked(provider, model)
	}
	return c.saveModelsConfigLocked()
}

func (c *Config) unremoveModelLocked(provider, model string) {
	if c.ModelsConfig == nil || c.ModelsConfig.RemovedModels == nil {
		return
	}
	canonicalKey := providerToCanonicalKey(provider)
	norm := normalizeProvider(provider)
	for rk, rList := range c.ModelsConfig.RemovedModels {
		if strings.EqualFold(providerToCanonicalKey(rk), canonicalKey) ||
			strings.EqualFold(normalizeProvider(rk), norm) ||
			strings.EqualFold(rk, provider) {
			var newR []string
			for _, m := range rList {
				if !strings.EqualFold(strings.TrimSpace(m), strings.TrimSpace(model)) {
					newR = append(newR, m)
				}
			}
			c.ModelsConfig.RemovedModels[rk] = newR
		}
	}
}

func providerToCanonicalKey(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "gemini", "google":
		return "Gemini"
	case "groq":
		return "Groq"
	case "nvidia":
		return "NVIDIA"
	case "openrouter", "open router", "open_router", "open-router":
		return "OpenRouter"
	case "ollama", "local", "ollama local", "ollama_local", "ollama-local":
		return "Ollama"
	case "g4f", "web", "web g4f", "web_g4f", "web-g4f", "g4f (web)":
		return "G4F"
	case "openai", "chatgpt", "open ai", "open_ai", "open-ai":
		return "OpenAI"
	default:
		return p
	}
}

func (c *Config) saveModelsConfigLocked() error {
	if c.ModelsConfig == nil {
		return nil
	}
	home := GetUserHome()
	paths := getAllModelsConfigFilePaths(home)

	// Garante que slices nil sejam substituídos por slices vazios antes de serializar,
	// para evitar "null" no JSON que causa perda de dados de remoção na próxima leitura
	if c.ModelsConfig.RemovedModels == nil {
		c.ModelsConfig.RemovedModels = make(map[string][]string)
	}
	for k, v := range c.ModelsConfig.RemovedModels {
		if v == nil {
			c.ModelsConfig.RemovedModels[k] = []string{}
		}
	}
	if c.ModelsConfig.RemovedServers == nil {
		c.ModelsConfig.RemovedServers = []string{}
	}
	if c.ModelsConfig.BuiltinModels == nil {
		c.ModelsConfig.BuiltinModels = make(map[string][]string)
	}
	for k, v := range c.ModelsConfig.BuiltinModels {
		if v == nil {
			c.ModelsConfig.BuiltinModels[k] = []string{}
		}
	}
	if c.ModelsConfig.CustomServers == nil {
		c.ModelsConfig.CustomServers = []CustomServer{}
	}
	for i := range c.ModelsConfig.CustomServers {
		if c.ModelsConfig.CustomServers[i].Modelos == nil {
			c.ModelsConfig.CustomServers[i].Modelos = []string{}
		}
	}

	// Preparar o mapa de dados uma vez
	var m map[string]interface{}
	if len(paths) > 0 {
		if data, err := os.ReadFile(paths[0]); err == nil {
			_ = json.Unmarshal(data, &m)
		}
	}
	if m == nil {
		m = make(map[string]interface{})
	}

	m["builtin_models"] = c.ModelsConfig.BuiltinModels
	m["removed_models"] = c.ModelsConfig.RemovedModels
	m["removed_servers"] = c.ModelsConfig.RemovedServers
	m["custom_servers"] = c.ModelsConfig.CustomServers
	if c.ModelsConfig.ActiveProvider != "" {
		m["active_provider"] = c.ModelsConfig.ActiveProvider
	}
	if c.ModelsConfig.ActiveModel != "" {
		m["active_model"] = c.ModelsConfig.ActiveModel
	}

	prefs, ok := m["preferences"].(map[string]interface{})
	if !ok {
		prefs = make(map[string]interface{})
	}
	if c.ModelsConfig.Preferences.ThemeID != "" {
		prefs["theme_id"] = c.ModelsConfig.Preferences.ThemeID
	}
	if c.ModelsConfig.Preferences.WindowOpacity > 0 {
		prefs["window_opacity"] = c.ModelsConfig.Preferences.WindowOpacity
	}
	if c.ModelsConfig.Preferences.LastActiveProvider != "" {
		prefs["last_active_provider"] = c.ModelsConfig.Preferences.LastActiveProvider
	}
	if c.ModelsConfig.Preferences.LastActiveModel != "" {
		prefs["last_active_model"] = c.ModelsConfig.Preferences.LastActiveModel
	}
	if c.ModelsConfig.Preferences.ActiveModels != nil {
		prefs["active_models"] = c.ModelsConfig.Preferences.ActiveModels
	}
	m["preferences"] = prefs

	// Marshal UMA VEZ
	outData, marshalErr := json.MarshalIndent(m, "", "  ")
	if marshalErr != nil {
		return marshalErr
	}

	// Escreve em TODOS os paths
	var lastErr error
	written := false
	for _, p := range paths {
		_ = os.MkdirAll(filepath.Dir(p), 0755)
		if err := os.WriteFile(p, outData, 0600); err != nil {
			lastErr = err
			continue
		}
		written = true
	}
	if !written && lastErr != nil {
		return lastErr
	}
	return nil
}

func (c *Config) ensureDefaultsLocked() {
	if c.ModelsConfig == nil {
		c.ModelsConfig = loadModelsConfigFile()
		return
	}
	defaults := getCanonicalDefaults()
	if c.ModelsConfig.BuiltinModels == nil {
		c.ModelsConfig.BuiltinModels = make(map[string][]string)
	}
	for k, defs := range defaults {
		canonDefK := providerToCanonicalKey(k)
		removedMap := make(map[string]bool)
		if c.ModelsConfig.RemovedModels != nil {
			for rk, rList := range c.ModelsConfig.RemovedModels {
				if strings.EqualFold(providerToCanonicalKey(rk), canonDefK) {
					for _, rm := range rList {
						removedMap[strings.ToLower(strings.TrimSpace(rm))] = true
					}
				}
			}
		}

		existing, hasKey := c.ModelsConfig.BuiltinModels[k]
		if !hasKey {
			for bk, bList := range c.ModelsConfig.BuiltinModels {
				if strings.EqualFold(providerToCanonicalKey(bk), canonDefK) {
					existing = bList
					hasKey = true
					break
				}
			}
		}

		var cleanExisting []string
		seen := make(map[string]bool)
		for _, em := range existing {
			emClean := strings.TrimSpace(em)
			emLower := strings.ToLower(emClean)
			if emClean != "" && !seen[emLower] && !removedMap[emLower] {
				cleanExisting = append(cleanExisting, emClean)
				seen[emLower] = true
			}
		}
		if !hasKey {
			for _, defM := range defs {
				dClean := strings.TrimSpace(defM)
				dLower := strings.ToLower(dClean)
				if dClean != "" && !seen[dLower] && !removedMap[dLower] {
					cleanExisting = append(cleanExisting, dClean)
					seen[dLower] = true
				}
			}
		}
		if cleanExisting == nil {
			cleanExisting = []string{}
		}
		c.ModelsConfig.BuiltinModels[k] = cleanExisting
	}

	// Também purga modelos removidos de CustomServers
	for i, srv := range c.ModelsConfig.CustomServers {
		canonK := providerToCanonicalKey(srv.ID)
		normK := normalizeProvider(srv.ID)
		removedMap := make(map[string]bool)
		if c.ModelsConfig.RemovedModels != nil {
			for rk, rList := range c.ModelsConfig.RemovedModels {
				if strings.EqualFold(providerToCanonicalKey(rk), canonK) ||
					strings.EqualFold(normalizeProvider(rk), normK) ||
					strings.EqualFold(rk, srv.Nome) ||
					strings.EqualFold(rk, srv.ID) {
					for _, rm := range rList {
						removedMap[strings.ToLower(strings.TrimSpace(rm))] = true
					}
				}
			}
		}
		var cleanModelos []string
		for _, mod := range srv.Modelos {
			modClean := strings.TrimSpace(mod)
			if modClean != "" && !removedMap[strings.ToLower(modClean)] {
				cleanModelos = append(cleanModelos, modClean)
			}
		}
		if cleanModelos == nil {
			cleanModelos = []string{}
		}
		c.ModelsConfig.CustomServers[i].Modelos = cleanModelos
		if removedMap[strings.ToLower(strings.TrimSpace(srv.ModeloAtual))] {
			if len(cleanModelos) > 0 {
				c.ModelsConfig.CustomServers[i].ModeloAtual = cleanModelos[0]
			} else {
				c.ModelsConfig.CustomServers[i].ModeloAtual = ""
			}
		}
	}
}

func (c *Config) addModelLocked(provider, model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil
	}

	canonicalKey := providerToCanonicalKey(provider)
	norm := normalizeProvider(provider)
	if c.ModelsConfig == nil {
		c.ModelsConfig = loadModelsConfigFile()
	}
	if c.ModelsConfig.BuiltinModels == nil {
		c.ModelsConfig.BuiltinModels = make(map[string][]string)
	}

	// 1. Remove de RemovedModels em todas as variantes de chave
	if c.ModelsConfig.RemovedModels != nil {
		for rk, rList := range c.ModelsConfig.RemovedModels {
			if strings.EqualFold(providerToCanonicalKey(rk), canonicalKey) ||
				strings.EqualFold(normalizeProvider(rk), norm) ||
				strings.EqualFold(rk, provider) {
				var newR []string
				for _, m := range rList {
					if !strings.EqualFold(strings.TrimSpace(m), model) {
						newR = append(newR, m)
					}
				}
				c.ModelsConfig.RemovedModels[rk] = newR
			}
		}
	}

	// 2. Insere no topo de BuiltinModels se não existir
	existing := c.ModelsConfig.BuiltinModels[canonicalKey]
	found := false
	for _, m := range existing {
		if strings.EqualFold(strings.TrimSpace(m), model) {
			found = true
			break
		}
	}
	if !found {
		c.ModelsConfig.BuiltinModels[canonicalKey] = append([]string{model}, existing...)
	}

	// 3. Se for custom_server, atualiza também
	for i, srv := range c.ModelsConfig.CustomServers {
		if strings.EqualFold(srv.ID, provider) || strings.EqualFold(srv.Nome, provider) ||
			strings.EqualFold(normalizeProvider(srv.ID), norm) || strings.EqualFold(normalizeProvider(srv.Nome), norm) {
			sFound := false
			for _, m := range srv.Modelos {
				if strings.EqualFold(strings.TrimSpace(m), model) {
					sFound = true
					break
				}
			}
			if !sFound {
				c.ModelsConfig.CustomServers[i].Modelos = append([]string{model}, srv.Modelos...)
			}
			c.ModelsConfig.CustomServers[i].ModeloAtual = model
		}
	}

	// 4. Salva no disco
	return c.saveModelsConfigLocked()
}

// AddModel adiciona um modelo à lista do provedor e persiste
func (c *Config) AddModel(provider, model string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.addModelLocked(provider, model)
}

// RemoveModels remove múltiplos modelos da lista do provedor de uma vez, grava em removed_models e persiste
func (c *Config) RemoveModels(provider string, models []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(models) == 0 {
		return nil
	}

	canonicalKey := providerToCanonicalKey(provider)
	norm := normalizeProvider(provider)
	if c.ModelsConfig == nil {
		c.ModelsConfig = loadModelsConfigFile()
	}

	modelMap := make(map[string]bool)
	var validModels []string
	for _, m := range models {
		m = strings.TrimSpace(m)
		if m != "" {
			modelMap[strings.ToLower(m)] = true
			validModels = append(validModels, m)
		}
	}

	if len(validModels) == 0 {
		return nil
	}

	// 1. Remove de BuiltinModels (em todas as chaves variantes)
	if c.ModelsConfig.BuiltinModels != nil {
		for bk, bList := range c.ModelsConfig.BuiltinModels {
			if strings.EqualFold(providerToCanonicalKey(bk), canonicalKey) || strings.EqualFold(bk, norm) {
				var newB []string
				for _, m := range bList {
					if !modelMap[strings.ToLower(strings.TrimSpace(m))] {
						newB = append(newB, m)
					}
				}
				c.ModelsConfig.BuiltinModels[bk] = newB
			}
		}
	}

	// 2. Remove de CustomServers
	for i, srv := range c.ModelsConfig.CustomServers {
		if strings.EqualFold(srv.ID, provider) || strings.EqualFold(srv.Nome, provider) ||
			strings.EqualFold(normalizeProvider(srv.ID), norm) || strings.EqualFold(normalizeProvider(srv.Nome), norm) {
			var newM []string
			for _, m := range srv.Modelos {
				if !modelMap[strings.ToLower(strings.TrimSpace(m))] {
					newM = append(newM, m)
				}
			}
			c.ModelsConfig.CustomServers[i].Modelos = newM
			if modelMap[strings.ToLower(strings.TrimSpace(srv.ModeloAtual))] {
				if len(newM) > 0 {
					c.ModelsConfig.CustomServers[i].ModeloAtual = newM[0]
				} else {
					c.ModelsConfig.CustomServers[i].ModeloAtual = ""
				}
			}
		}
	}

	// 3. Adiciona em RemovedModels (com canonicalKey e norm para garantir casamento exato)
	if c.ModelsConfig.RemovedModels == nil {
		c.ModelsConfig.RemovedModels = make(map[string][]string)
	}
	for _, targetKey := range []string{canonicalKey, norm} {
		rExisting := c.ModelsConfig.RemovedModels[targetKey]
		rSeen := make(map[string]bool)
		for _, m := range rExisting {
			rSeen[strings.ToLower(strings.TrimSpace(m))] = true
		}
		for _, m := range validModels {
			mLow := strings.ToLower(m)
			if !rSeen[mLow] {
				rExisting = append(rExisting, m)
				rSeen[mLow] = true
			}
		}
		c.ModelsConfig.RemovedModels[targetKey] = rExisting
	}

	// 4. Se algum dos modelos removidos era o modelo ativo do provedor, faz o fallback
	for _, m := range validModels {
		c.fallbackActiveModelLocked(provider, m)
	}

	// 5. Salva no disco
	return c.saveModelsConfigLocked()
}

// RemoveModel remove um modelo da lista do provedor, grava em removed_models e persiste
func (c *Config) RemoveModel(provider, model string) error {
	return c.RemoveModels(provider, []string{model})
}

func (c *Config) fallbackActiveModelLocked(provider, removedModel string) {
	norm := normalizeProvider(provider)

	currentActive := c.getActiveModelForProviderLocked(norm)

	if strings.EqualFold(currentActive, removedModel) || !c.isModelValidForProviderLocked(norm, currentActive) {
		remaining := c.getModelsForProviderLocked(norm)
		newActive := ""
		if len(remaining) > 0 {
			newActive = remaining[0]
		}

		c.setActiveModelForProviderLocked(norm, newActive)
		if c.ModelsConfig != nil {
			if strings.EqualFold(c.Provider, norm) {
				c.ModelsConfig.ActiveModel = newActive
				c.ModelsConfig.Preferences.LastActiveModel = newActive
			}
			if newActive != "" {
				c.ModelsConfig.Preferences.ActiveModels[norm] = newActive
			} else {
				delete(c.ModelsConfig.Preferences.ActiveModels, norm)
			}
		}

		home := GetUserHome()
		envFiles := getAllEnvFilePaths(home)
		envVar := getEnvVarForProvider(norm)
		if envVar == "" && c.ModelsConfig != nil {
			for _, srv := range c.ModelsConfig.CustomServers {
				if strings.EqualFold(srv.ID, provider) || strings.EqualFold(srv.Nome, provider) ||
					strings.EqualFold(normalizeProvider(srv.ID), norm) {
					cleanID := strings.ToUpper(strings.ReplaceAll(srv.ID, "-", "_"))
					envVar = cleanID + "_MODEL"
					break
				}
			}
		}
		for _, ef := range envFiles {
			if envVar != "" {
				updateEnvVarInFile(ef, envVar, newActive)
			}
		}

		// Se o provedor ativo atual ficou sem nenhum modelo, faz fallback para outro provedor válido
		if strings.EqualFold(c.Provider, norm) && newActive == "" {
			for _, p := range []string{"groq", "gemini", "nvidia", "ollama", "g4f"} {
				if m := c.getFirstValidModelForProviderLocked(p); m != "" {
					c.Provider = p
					if c.ModelsConfig != nil {
						c.ModelsConfig.ActiveProvider = p
						c.ModelsConfig.ActiveModel = m
						c.ModelsConfig.Preferences.LastActiveProvider = p
						c.ModelsConfig.Preferences.LastActiveModel = m
						c.ModelsConfig.Preferences.ActiveModels[p] = m
					}
					c.setActiveModelForProviderLocked(p, m)
					for _, ef := range envFiles {
						updateEnvVarInFile(ef, "DEFAULT_PROVIDER", providerToNum(p))
					}
					dirs := getAllLastProviderDirs(home)
					for _, d := range dirs {
						_ = os.WriteFile(filepath.Join(d, ".last_provider"), []byte(providerToNum(p)+"\n"), 0644)
						_ = os.WriteFile(filepath.Join(d, ".fix_ia_selected"), []byte(providerToLabel(p)+"\n"), 0644)
					}
					break
				}
			}
		}

		c.saveModelsConfigLocked()
	}
}

func getEnvVarForProvider(norm string) string {
	switch norm {
	case "gemini":
		return "GEMINI_MODEL"
	case "groq":
		return "GROQ_MODEL"
	case "nvidia":
		return "NVIDIA_MODEL"
	case "openrouter":
		return "OPENROUTER_MODEL"
	case "ollama":
		return "OLLAMA_MODEL"
	case "g4f":
		return "G4F_MODEL"
	case "openai":
		return "OPENAI_MODEL"
	default:
		return ""
	}
}

// SyncMetisModels executa a sincronização completa com o Metis e Ollama local
func (c *Config) SyncMetisModels() error {
	home := GetUserHome()

	// Preserva o provedor e modelo ativos atuais antes do sync para não perder seleção do usuário
	c.mu.RLock()
	savedProvider := c.Provider
	savedModel := c.getActiveModelForProviderLocked(savedProvider)
	// Preserva os modelos removidos pelo usuário para não reintroduzi-los após o Reload/ensureDefaults
	var savedRemovedModels map[string][]string
	if c.ModelsConfig != nil && c.ModelsConfig.RemovedModels != nil {
		savedRemovedModels = make(map[string][]string)
		for k, v := range c.ModelsConfig.RemovedModels {
			if v != nil {
				cp := make([]string, len(v))
				copy(cp, v)
				savedRemovedModels[k] = cp
			} else {
				savedRemovedModels[k] = []string{}
			}
		}
	}
	var savedRemovedServers []string
	if c.ModelsConfig != nil && c.ModelsConfig.RemovedServers != nil {
		savedRemovedServers = make([]string, len(c.ModelsConfig.RemovedServers))
		copy(savedRemovedServers, c.ModelsConfig.RemovedServers)
	}
	c.mu.RUnlock()

	// 1. Sincroniza modelos baixados no Ollama local se estiver rodando
	c.syncLocalOllama()

	// 2. Executa manage_models.py sync PRIMEIRO (para harmonizar réplicas sem sobrescrever dados novos de ~/Metis)
	pyScript := filepath.Join(home, ".ZSH", "ai", "manage_models.py")
	if _, err := os.Stat(pyScript); err == nil {
		cmd := exec.Command("python3", pyScript, "sync")
		cmd.Env = append(os.Environ(), "HOME="+home)
		_ = cmd.Run()
	}

	// 3. Recarrega as configurações atualizadas do disco
	c.Reload()

	// 4. Garante modelos canônicos padrão e salva réplicas limpas
	c.mu.Lock()

	// Restaura os modelos e servidores removidos que o usuário havia configurado,
	// mesclando com quaisquer novas remoções que possam ter vindo do disco
	if savedRemovedModels != nil {
		if c.ModelsConfig.RemovedModels == nil {
			c.ModelsConfig.RemovedModels = make(map[string][]string)
		}
		for k, savedList := range savedRemovedModels {
			existingList := c.ModelsConfig.RemovedModels[k]
			seen := make(map[string]bool)
			for _, m := range existingList {
				seen[strings.ToLower(strings.TrimSpace(m))] = true
			}
			for _, m := range savedList {
				mLow := strings.ToLower(strings.TrimSpace(m))
				if !seen[mLow] {
					existingList = append(existingList, m)
					seen[mLow] = true
				}
			}
			c.ModelsConfig.RemovedModels[k] = existingList
		}
	}
	if savedRemovedServers != nil {
		if c.ModelsConfig.RemovedServers == nil {
			c.ModelsConfig.RemovedServers = []string{}
		}
		seen := make(map[string]bool)
		for _, s := range c.ModelsConfig.RemovedServers {
			seen[strings.ToLower(strings.TrimSpace(s))] = true
		}
		for _, s := range savedRemovedServers {
			if !seen[strings.ToLower(strings.TrimSpace(s))] {
				c.ModelsConfig.RemovedServers = append(c.ModelsConfig.RemovedServers, s)
			}
		}
	}

	c.ensureDefaultsLocked()
	// Restaura provedor e modelo ativos se forem válidos
	if savedProvider != "" {
		c.Provider = savedProvider
		if savedModel != "" && c.isModelValidForProviderLocked(savedProvider, savedModel) {
			c.setActiveModelForProviderLocked(savedProvider, savedModel)
			c.ModelsConfig.Preferences.ActiveModels[savedProvider] = savedModel
		}
	}
	c.applyActiveModelsLocked()
	c.saveModelsConfigLocked()
	c.mu.Unlock()

	// 5. Verifica se o provedor ativo ainda existe (caso um servidor customizado tenha sido removido no Metis)
	provs := c.GetProvidersList()
	found := false
	for _, p := range provs {
		if strings.EqualFold(p.ID, c.Provider) || strings.EqualFold(p.Name, c.Provider) {
			found = true
			break
		}
	}
	if !found && len(provs) > 0 {
		_ = c.SaveProvider(provs[0].ID)
	} else if c.Provider != "" {
		currentModel := c.GetActiveModelForProvider(c.Provider)
		if c.IsModelValidForProvider(c.Provider, currentModel) {
			_ = c.SaveProviderAndModel(c.Provider, currentModel)
		} else {
			_ = c.SaveProvider(c.Provider)
		}
	}

	return nil
}

func (c *Config) syncLocalOllama() {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://localhost:11434/api/tags")
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return
	}

	var data struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return
	}

	if len(data.Models) == 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ModelsConfig == nil {
		c.ModelsConfig = loadModelsConfigFile()
	}
	if c.ModelsConfig.BuiltinModels == nil {
		c.ModelsConfig.BuiltinModels = make(map[string][]string)
	}

	ollamaExisting := c.ModelsConfig.BuiltinModels["Ollama"]
	seen := make(map[string]bool)
	for _, m := range ollamaExisting {
		seen[strings.ToLower(strings.TrimSpace(m))] = true
	}

	removed := make(map[string]bool)
	if c.ModelsConfig.RemovedModels != nil {
		canonOllama := providerToCanonicalKey("Ollama")
		for k, rList := range c.ModelsConfig.RemovedModels {
			if strings.EqualFold(providerToCanonicalKey(k), canonOllama) {
				for _, m := range rList {
					removed[strings.ToLower(strings.TrimSpace(m))] = true
				}
			}
		}
	}

	changed := false
	for _, m := range data.Models {
		name := strings.TrimSpace(m.Name)
		if name != "" && !seen[strings.ToLower(name)] && !removed[strings.ToLower(name)] {
			ollamaExisting = append(ollamaExisting, name)
			seen[strings.ToLower(name)] = true
			changed = true
		}
	}

	if changed {
		c.ModelsConfig.BuiltinModels["Ollama"] = ollamaExisting
		c.saveModelsConfigLocked()
	}
}

func providerToNum(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "ollama", "local", "ollama local", "ollama_local", "1":
		return "1"
	case "g4f", "web", "web g4f", "web_g4f", "2":
		return "2"
	case "gemini", "google", "3":
		return "3"
	case "groq", "4":
		return "4"
	case "nvidia", "5":
		return "5"
	case "openrouter", "open router", "open_router", "6":
		return "6"
	default:
		clean := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(p)), "custom:")
		if clean != "" {
			return clean
		}
		return "1"
	}
}

func providerToLabel(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "ollama", "local", "ollama local", "ollama_local", "1":
		return "Local: Ollama"
	case "g4f", "web", "web g4f", "web_g4f", "2":
		return "Web: G4F"
	case "gemini", "google", "3":
		return "API: Gemini"
	case "groq", "4":
		return "API: Groq"
	case "nvidia", "5":
		return "API: NVIDIA"
	case "openrouter", "open router", "open_router", "6":
		return "API: OpenRouter"
	default:
		clean := strings.TrimPrefix(strings.TrimSpace(p), "custom:")
		return "API: " + strings.ToUpper(clean)
	}
}

func updateEnvVarInFile(path, key, val string) {
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	lines := []string{}
	found := false

	if f, err := os.Open(path); err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			l := scanner.Text()
			if strings.HasPrefix(strings.TrimSpace(l), key+"=") {
				lines = append(lines, key+`="`+val+`"`)
				found = true
			} else {
				lines = append(lines, l)
			}
		}
		f.Close()
	}

	if !found {
		lines = append(lines, key+`="`+val+`"`)
	}

	_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600)
}

func normalizeProvider(p string) string {
	p = strings.ToLower(strings.TrimSpace(p))
	if strings.HasPrefix(p, "custom:") {
		p = strings.TrimPrefix(p, "custom:")
	}
	switch p {
	case "1", "ollama", "local", "ollama local", "ollama_local":
		return "ollama"
	case "2", "g4f", "web", "web g4f", "web_g4f":
		return "g4f"
	case "3", "gemini", "google":
		return "gemini"
	case "4", "groq":
		return "groq"
	case "5", "nvidia":
		return "nvidia"
	case "6", "openrouter", "open router", "open_router":
		return "openrouter"
	case "openai", "chatgpt":
		return "openai"
	default:
		return p
	}
}

var (
	initialOSEnv     map[string]bool
	initialOSEnvOnce sync.Once
)

func initOSEnv() {
	initialOSEnvOnce.Do(func() {
		initialOSEnv = make(map[string]bool)
		for _, env := range os.Environ() {
			parts := strings.SplitN(env, "=", 2)
			if len(parts) == 2 {
				initialOSEnv[parts[0]] = true
			}
		}
	})
}

func loadEnvFiles(home string, forceOverwrite ...bool) {
	initOSEnv()
	force := len(forceOverwrite) > 0 && forceOverwrite[0]

	var candidatePaths []string
	if home != "" {
		candidatePaths = getAllEnvFilePaths(home)
	}

	type envCandidate struct {
		path    string
		modTime time.Time
	}
	var existing []envCandidate
	seen := make(map[string]bool)

	for _, p := range candidatePaths {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			realP := p
			if target, err := filepath.EvalSymlinks(p); err == nil {
				realP = target
			}
			if !seen[realP] {
				seen[realP] = true
				existing = append(existing, envCandidate{path: p, modTime: fi.ModTime()})
			}
		}
	}

	// Ordena do mais antigo para o mais recente (o arquivo modificado por último vence e sobrescreve)
	sort.Slice(existing, func(i, j int) bool {
		return existing[i].modTime.Before(existing[j].modTime)
	})

	for _, cand := range existing {
		f, err := os.Open(cand.path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Aviso: não foi possível ler %s: %v\n", cand.path, err)
			continue
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			if (strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"")) ||
				(strings.HasPrefix(val, "'") && strings.HasSuffix(val, "'")) {
				if len(val) >= 2 {
					val = val[1 : len(val)-1]
				}
			}

			// Permite sobreposição das variáveis definidas nos arquivos .env
			if force || !initialOSEnv[key] || val != "" {
				_ = os.Setenv(key, val)
			}
		}
		_ = f.Close()
	}
}

func getEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvIntOrDefault(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			return i
		}
	}
	return def
}

func getEnvBoolOrDefault(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "sim", "on", "y", "s":
			return true
		case "0", "false", "no", "nao", "off", "n":
			return false
		}
	}
	return def
}
