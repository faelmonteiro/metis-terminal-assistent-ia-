# 🚀 Prompt de Correções Finais - Projeto Metis Screen

**Status:** Versão do Go corrigida (go 1.21). Pontos #4, #5, #10, #12 validados como "Intencional/Correto".
**Objetivo:** Aplicar as 8 correções restantes de Bugs, Performance e Código Morto.

---

## 🔴 1. Falha Silenciosa em `loadEnvFiles` (Bug Crítico)
**Arquivo:** `internal/config/config.go`
**Problema:** Erros de leitura de arquivos `.env` são ignorados (`continue` sem log).
**Impacto:** Usuário não sabe se configurações falharam ao carregar.

### Código Atual (Linhas ~2200)
```go
for _, cand := range existing {
    f, err := os.Open(cand.path)
    if err != nil {
        continue // ❌ Silencioso
    }
    // ...
}
```

### Código Corrigido
```go
import "fmt" // Garantir import

// ... dentro do loop
for _, cand := range existing {
    f, err := os.Open(cand.path)
    if err != nil {
        // ✅ Aviso visível mas não fatal
        fmt.Fprintf(os.Stderr, "Aviso: não foi possível ler %s: %v\n", cand.path, err)
        continue
    }
    // ... restante do código
}
```

---

## 🟠 2. Função Duplicada (Código Morto)
**Arquivo:** `internal/config/config.go`
**Problema:** Existência de `getActiveModelForProvider` (privada) idêntica à pública.

### Ação
Localize e **DELETE** a seguinte função inteira (geralmente logo após a versão pública):
```go
// ❌ REMOVER ESTA FUNÇÃO
func (c *Config) getActiveModelForProvider(prov string) string {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.getActiveModelForProviderLocked(prov)
}
```
*Mantenha apenas `GetActiveModelForProvider` (pública) e `getActiveModelForProviderLocked` (interna).*

---

## 🟠 3. Race Condition em `Reload` (Robustez)
**Arquivo:** `internal/config/config.go` (~linha 900)
**Problema:** Lock mantido durante I/O lento (`os.ReadFile`, `loadEnvFiles`).

### Código Atual
```go
func (c *Config) Reload() {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    home := GetUserHome()
    loadEnvFiles(home, true) // ❌ I/O com lock
    c.ModelsConfig = loadModelsConfigFile() // ❌ I/O com lock
}
```

### Código Corrigido
```go
func (c *Config) Reload() {
    // 1. Carregar dados FORA do lock
    home := GetUserHome()
    // Nota: loadEnvFiles modifica env global, então chamamos fora, mas o impacto é mínimo
    loadEnvFiles(home, true) 
    
    newConfig := loadModelsConfigFile()

    // 2. Aplicar mudanças DENTRO do lock (rápido)
    c.mu.Lock()
    defer c.mu.Unlock()
    c.ModelsConfig = newConfig
}
```

---

## 🟢 4. Marshal Redundante (Performance)
**Arquivo:** `internal/config/config.go` (~linha 1700)
**Problema:** Faz Marshal/Unmarshal para cada caminho candidato.

### Código Corrigido (Lógica)
```go
func (c *Config) saveModelsConfigLocked() error {
    // 1. Preparar dados UMA vez
    data, err := json.MarshalIndent(c.ModelsConfig, "", "  ")
    if err != nil {
        return err
    }

    // 2. Escrever em todos os paths possíveis
    for _, p := range paths {
        // Criar dir se necessário
        if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
            continue
        }
        // Escrever mesmo buffer 'data'
        if err := os.WriteFile(p, data, 0600); err != nil {
            // log erro
        }
    }
    return nil
}
```

---

## 🟢 5. Polling Agressivo (Performance/CPU)
**Arquivo:** `internal/agent/agent.go` (~linha 900)
**Problema:** Loop de 12s checando a cada 100ms (120 iterações).

### Código Corrigido
```go
waited := 0
maxWait := 120 
for waited < maxWait {
    if _, err := os.Stat(statusFile); err == nil {
        time.Sleep(50 * time.Millisecond) // Pequeno debounce
        break
    }
    // ✅ Aumentar intervalo para 250ms ou 500ms
    time.Sleep(250 * time.Millisecond) 
    waited++
}
```

---

## 🟢 6. Buffers Grandes Demais (Memória)
**Arquivo:** `internal/ai/provider.go` (~linha 211 e 450)
**Problema:** Buffer inicial de 64KB e Max de 10MB é excessivo para chat típico.

### Código Corrigido
```go
scanner := bufio.NewScanner(resp.Body)
// ✅ Reduzir para 32KB inicial, 2MB max (suficiente para maioria dos contextos)
buf := make([]byte, 32*1024) 
scanner.Buffer(buf, 2*1024*1024) 
```

---

## 🟢 7. Validação de Modelo Inconsistente (Lógica)
**Arquivo:** `internal/config/config.go` (~linha 1450)
**Problema:** Se modelo é inválido e não há fallback, define como "" mas não valida se o Provider Atual usa esse modelo vazio.

### Código Corrigido (Adicionar ao final do loop)
```go
// ... após o loop for _, p := range []string{...}
    
// ✅ Verificar consistência do Provider Atual
currentProv := c.Provider
if currentProv != "" {
    model := c.getActiveModelForProviderLocked(currentProv)
    if model == "" {
        // Tentar achar qualquer modelo válido para este provider
        if first := c.getFirstValidModelForProviderLocked(currentProv); first != "" {
            c.setActiveModelForProviderLocked(currentProv, first)
        } else {
            // Warning: Provider ativo sem modelos
            fmt.Fprintf(os.Stderr, "Aviso: Provider '%s' selecionado não possui modelos válidos.\n", currentProv)
        }
    }
}
```

---

## 🟢 8. Falsos Positivos em Segurança (Usabilidade)
**Arquivo:** `internal/security/security.go` (~linha 220)
**Problema:** Bloqueia `$(...)` sempre, impedindo `echo "$(date)"`.

### Código Corrigido (Estratégia de Whitelist Contextual)
*Nota: Mantenha a bloqueio geral, mas adicione exceção para comandos de leitura seguros.*

```go
func IsSafeReadCommand(cmd string) bool {
    trimmed := strings.TrimSpace(cmd)
    
    // Lista de comandos seguros que PODEM usar subshell
    safeWithSubshell := []string{"echo", "printf", "date", "whoami", "pwd"}
    
    hasSubshell := strings.Contains(trimmed, "$(") || strings.Contains(trimmed, "`")
    
    if hasSubshell {
        // Verificar se começa com comando seguro
        isSafeContext := false
        for _, safe := range safeWithSubshell {
            if strings.HasPrefix(trimmed, safe + " ") || trimmed == safe {
                isSafeContext = true
                break
            }
        }
        
        if !isSafeContext {
            return false // ❌ Bloqueia se não for contexto seguro
        }
        // ✅ Permite se for contexto seguro (ex: echo "$(date)")
    }

    // ... restante das validações originais
    return true
}
```

---

## ✅ Checklist de Validação Final

Após aplicar as 8 correções acima, execute:

```bash
# 1. Build Limpo
go build -o metis . && echo "✅ BUILD OK"

# 2. Vet (Análise Estática)
go vet ./... && echo "✅ VET OK"

# 3. Race Detector (Teste de Stress)
# Execute o programa com carga e flag race se possível em teste
go run -race . --help 

# 4. Teste Funcional Rápido
./metis --version
```

## 📊 Resumo do Impacto

| ID | Tipo | Impacto Esperado | Dificuldade |
|----|------|------------------|-------------|
| 1 | Bug | Visibilidade de erros de config | Baixa |
| 2 | Clean | Código mais limpo | Baixa |
| 3 | Race | Menor bloqueio de threads | Média |
| 4 | Perf | Save de config mais rápido | Baixa |
| 5 | Perf | Menor uso de CPU em wait | Baixa |
| 6 | Perf | Menor alocação de memória | Baixa |
| 7 | Logic | Prevenção de estado inválido | Média |
| 8 | UX | Menos falsos positivos | Média |

**Total de Arquivos Modificados:** ~4 arquivos (`config.go`, `agent.go`, `provider.go`, `security.go`).
