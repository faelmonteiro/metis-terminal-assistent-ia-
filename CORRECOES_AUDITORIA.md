# 🛠️ Prompt para Correção de Bugs e Melhorias - Projeto Metis Screen (Go)

## Contexto
Você é um desenvolvedor sênior Go responsável por aplicar correções críticas identificadas em uma auditoria de código. O projeto é um assistente de IA chamado "Metis Screen" que integra múltiplos provedores de LLM (Ollama, Groq, Gemini, etc.) com capacidade de execução de comandos shell seguros.

## Instruções Gerais
Aplique **TODAS** as correções listadas abaixo nos arquivos especificados. Mantenha o estilo de código existente, preserve comentários em português e não altere funcionalidades não relacionadas.

---

## 🔴 CORREÇÕES CRÍTICAS (Prioridade Máxima)

### 1. Corrigir Versão do Go no go.mod
**Arquivo:** `/workspace/go.mod`
**Linha:** 3

**Problema:** Versão `go 1.27.1` não existe (versão estável mais recente é 1.23.x)

**Correção:**
```go
// ANTES:
go 1.27.1

// DEPOIS:
go 1.21
```

---

### 2. Adicionar Logging em loadEnvFiles (Falha Silenciosa)
**Arquivo:** `/workspace/internal/config/config.go`
**Função:** `loadEnvFiles` (~linha 2227)
**Local específico:** Linha 2263-2264

**Problema:** Erros de leitura de arquivos .env são ignorados silenciosamente com `continue`

**Correção:**
```go
// ANTES (linhas 2262-2265):
for _, cand := range existing {
    f, err := os.Open(cand.path)
    if err != nil {
        continue
    }

// DEPOIS:
for _, cand := range existing {
    f, err := os.Open(cand.path)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Aviso: não foi possível ler %s: %v\n", cand.path, err)
        continue
    }
```

**Importação necessária:** Verifique se `"fmt"` já está importado no topo do arquivo (deve estar).

---

### 3. Remover Função Duplicada getActiveModelForProvider
**Arquivo:** `/workspace/internal/config/config.go`
**Linhas:** 872-876

**Problema:** Função privada duplicada da pública `GetActiveModelForProvider` sem uso

**Correção:**
```go
// REMOVER completamente estas linhas (872-876):
func (c *Config) getActiveModelForProvider(prov string) string {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.getActiveModelForProviderLocked(prov)
}
```

Manter apenas:
- `GetActiveModelForProvider` (pública, linha 866)
- `getActiveModelForProviderLocked` (privada com lock, linha 878)

---

### 4. Tratar Erro de Stream na Goroutine do Agent
**Arquivo:** `/workspace/internal/agent/agent.go`
**Linhas:** 739-754

**Problema:** Variável `streamErr` é definida dentro da goroutine mas pode ter race condition

**Correção:**
```go
// ANTES (linhas 739-754):
ch := make(chan string, 10)
var fullResp strings.Builder
var streamErr error

go func() {
    defer close(ch)
    streamErr = prov.AskStream(ctx, sysPrompt, currentContext, ch)
}()

for chunk := range ch {
    fullResp.WriteString(chunk)
}

if streamErr != nil {
    return "", streamErr
}

// DEPOIS (usar channel separado para erro):
ch := make(chan string, 10)
errCh := make(chan error, 1)
var fullResp strings.Builder

go func() {
    defer close(ch)
    defer close(errCh)
    err := prov.AskStream(ctx, sysPrompt, currentContext, ch)
    errCh <- err
}()

for chunk := range ch {
    fullResp.WriteString(chunk)
}

// Aguardar erro da goroutine
if streamErr := <-errCh; streamErr != nil {
    return "", streamErr
}
```

---

### 5. Prevenir Vazamento de Processos Filhos no ExecuteTool
**Arquivo:** `/workspace/internal/agent/agent.go`
**Linhas:** 426-445

**Problema:** `exec.CommandContext` não mata processos filhos spawnados pelo shell quando timeout ocorre

**Correção:**
```go
// ANTES (linhas 426-445):
execCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
defer cancel()

shell := os.Getenv("SHELL")
if shell == "" {
    if _, err := exec.LookPath("zsh"); err == nil {
        shell = "zsh"
    } else if _, err := exec.LookPath("bash"); err == nil {
        shell = "bash"
    } else {
        shell = "sh"
    }
}

cmd := exec.CommandContext(execCtx, shell, "-c", call.Command)
var outBuf, errBuf bytes.Buffer
cmd.Stdout = &outBuf
cmd.Stderr = &errBuf

err := cmd.Run()

// DEPOIS (adicionar Setpgid e cleanup):
execCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
defer cancel()

shell := os.Getenv("SHELL")
if shell == "" {
    if _, err := exec.LookPath("zsh"); err == nil {
        shell = "zsh"
    } else if _, err := exec.LookPath("bash"); err == nil {
        shell = "bash"
    } else {
        shell = "sh"
    }
}

cmd := exec.CommandContext(execCtx, shell, "-c", call.Command)
var outBuf, errBuf bytes.Buffer
cmd.Stdout = &outBuf
cmd.Stderr = &errBuf

// Configurar para matar process group inteiro
cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

err := cmd.Run()

// Se houver erro de timeout, matar process group
if err != nil && execCtx.Err() == context.DeadlineExceeded {
    // Tenta matar o process group
    syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
```

**Importação necessária:** Adicionar `"syscall"` aos imports se não existir.

---

### 6. Otimizar saveModelsConfigLocked (Marshal Redundante)
**Arquivo:** `/workspace/internal/config/config.go`
**Função:** `saveModelsConfigLocked` (~linha 1466)
**Seção:** Loop nas linhas 1505-1560

**Problema:** Faz MarshalIndent para CADA caminho de arquivo, quando poderia fazer uma vez

**Correção:**
```go
// ANTES (estrutura geral):
for _, p := range paths {
    var m map[string]interface{}
    if data, err := os.ReadFile(p); err == nil {
        _ = json.Unmarshal(data, &m)
    }
    // ... modifica m ...
    
    // Marshal DENTRO do loop (ineficiente)
    if outData, err := json.MarshalIndent(m, "", "  "); err == nil {
        _ = os.WriteFile(p, outData, 0600)
    }
}

// DEPOIS (fazer Marshal uma vez):
// Primeiro, prepara os dados
var m map[string]interface{}
primaryPath := ""
if len(paths) > 0 {
    primaryPath = paths[0]
    if data, err := os.ReadFile(primaryPath); err == nil {
        _ = json.Unmarshal(data, &m)
    }
}
if m == nil {
    m = make(map[string]interface{})
}

// Aplica todas as modificações uma vez
m["builtin_models"] = c.ModelsConfig.BuiltinModels
m["removed_models"] = c.ModelsConfig.RemovedModels
m["removed_servers"] = c.ModelsConfig.RemovedServers
m["custom_servers"] = c.ModelsConfig.CustomServers
// ... resto das modificações ...

// Marshal UMA VEZ
outData, marshalErr := json.MarshalIndent(m, "", "  ")
if marshalErr != nil {
    return marshalErr
}

// Escreve em TODOS os paths
var lastErr error
written := false
for _, p := range paths {
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
```

**Nota:** Esta é uma refatoração maior. Se preferir abordagem conservadora, apenas adicione comentário documentando a ineficiência conhecida.

---

### 7. Refatorar Reload para Minimizar Tempo de Lock
**Arquivo:** `/workspace/internal/config/config.go`
**Função:** `Reload` (~linha 811)

**Problema:** Mantém lock durante operações lentas de I/O (loadEnvFiles, loadModelsConfigFile)

**Correção:**
```go
// ANTES (linhas 811-840):
func (c *Config) Reload() {
    c.mu.Lock()
    defer c.mu.Unlock()

    home := GetUserHome()
    loadEnvFiles(home, true)  // I/O lento com lock!
    c.ModelsConfig = loadModelsConfigFile()  // I/O lento com lock!
    
    // ... atualiza propriedades ...
}

// DEPOIS (carregar fora do lock, aplicar rápido):
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
    c.G4FModel = getEnvOrDefault("G4F_MODEL", "gpt-4o-mini")
    
    c.Provider = getEnvOrDefault("DEFAULT_PROVIDER", c.getDefaultProvider())
    c.applyActiveModelsLocked()
}
```

---

### 8. Reduzir Falsos Positivos em IsSafeReadCommand
**Arquivo:** `/workspace/internal/security/security.go`
**Linhas:** 217-220

**Problema:** Bloqueia QUALQUER uso de `$(...)` mesmo em comandos seguros como `echo "$(date)"`

**Correção Conservadora (recomendada):**
```go
// ANTES (linhas 217-220):
// Operadores perigosos
if strings.Contains(trimmed, "$(") || strings.Contains(trimmed, "`") || strings.Contains(trimmed, "sudo ") {
    return false
}

// DEPOIS (permitir subshell em comandos whitelistados):
// Operadores perigosos (sudo sempre bloqueado)
if strings.Contains(trimmed, "sudo ") {
    return false
}

// Subshell: permitir apenas em comandos seguros conhecidos
hasSubshell := strings.Contains(trimmed, "$(") || strings.Contains(trimmed, "`")
if hasSubshell {
    // Extrair primeiro comando antes do pipe/redirect
    firstCmd := trimmed
    if idx := strings.IndexAny(trimmed, "|;&"); idx != -1 {
        firstCmd = trimmed[:idx]
    }
    firstWord := strings.Fields(firstCmd)
    if len(firstWord) > 0 {
        safeWithSubshell := map[string]bool{
            "echo": true, "printf": true, "cat": true,
        }
        if !safeWithSubshell[firstWord[0]] {
            return false
        }
    } else {
        return false
    }
}
```

---

### 9. Ajustar Buffer do Scanner no Provider
**Arquivo:** `/workspace/internal/ai/provider.go`
**Linhas:** 210-212, 449-451, 613-615

**Problema:** Buffer de 64KB e max token de 10MB é excessivo para respostas típicas de chat

**Correção:**
```go
// ANTES (3 ocorrências similares):
scanner := bufio.NewScanner(resp.Body)
buf := make([]byte, 64*1024)
scanner.Buffer(buf, 10*1024*1024)

// DEPOIS (buffers mais razoáveis):
scanner := bufio.NewScanner(resp.Body)
buf := make([]byte, 32*1024)  // 32KB suficiente para maioria dos casos
scanner.Buffer(buf, 5*1024*1024)  // 5MB ainda suporta respostas longas
```

**Aplicar em:**
- Linha ~210-212 (Ollama)
- Linha ~449-451 (Gemini)
- Linha ~613-615 (Groq/NVIDIA/etc)

---

### 10. Melhorar Polling de Status File
**Arquivo:** `/workspace/internal/agent/agent.go`
**Linhas:** 929-939

**Problema:** Polling agressivo a cada 100ms por até 12 segundos

**Correção:**
```go
// ANTES (linhas 929-939):
for waited < maxWait {
    if ctx.Err() != nil {
        return "", ctx.Err()
    }
    if _, err := os.Stat(statusFile); err == nil {
        time.Sleep(150 * time.Millisecond)
        break
    }
    time.Sleep(100 * time.Millisecond)
    waited++
}

// DEPOIS (intervalo maior + backoff exponencial simples):
waited := 0
maxWait := 60  // Reduzir para 6 segundos (60 * 100ms)
baseDelay := 150 * time.Millisecond

for waited < maxWait {
    if ctx.Err() != nil {
        return "", ctx.Err()
    }
    if _, err := os.Stat(statusFile); err == nil {
        time.Sleep(100 * time.Millisecond)
        break
    }
    
    // Backoff exponencial simples: 150ms, 200ms, 250ms... máx 500ms
    delay := baseDelay + time.Duration(waited*50)*time.Millisecond
    if delay > 500*time.Millisecond {
        delay = 500 * time.Millisecond
    }
    time.Sleep(delay)
    waited++
}
```

---

## ⚠️ CORREÇÕES ADICIONAIS (Prioridade Média)

### 11. Validação de Modelo após applyActiveModelsLocked
**Arquivo:** `/workspace/internal/config/config.go`
**Função:** `applyActiveModelsLocked` (~linha 1148)
**Adicionar no final da função (após linha 1225):**

**Problema:** Após validar todos providers, não verifica se o Provider ativo ainda tem modelo válido

**Correção:**
```go
// Adicionar ao FINAL da função applyActiveModelsLocked (antes do fechamento }):

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
```

---

### 12. Adicionar Cleanup no gui.Run antes de Retornar Erro
**Arquivo:** `/workspace/main.go`
**Linhas:** 64-68

**Problema:** Se `gui.Run` falhar após alocação de recursos, não há garantia de cleanup

**Correção:**
```go
// ANTES (linhas 64-68):
if shouldUseGUI {
    err := gui.Run(cfg, km, initialQuery, flagLines)
    if err == nil {
        return
    }
    fmt.Fprintf(os.Stderr, "Aviso: Falha ao abrir janela gráfica (%v), iniciando modo terminal TUI...\n", err)
}

// DEPOIS (garantir cleanup explícito):
if shouldUseGUI {
    err := gui.Run(cfg, km, initialQuery, flagLines)
    if err == nil {
        return
    }
    // Garantir que recursos da GUI sejam liberados
    if cleaner, ok := interface{}(km).(interface{ Cleanup() }); ok {
        cleaner.Cleanup()
    }
    fmt.Fprintf(os.Stderr, "Aviso: Falha ao abrir janela gráfica (%v), iniciando modo terminal TUI...\n", err)
}
```

**Nota:** Se `km` (KittyManager) não tiver método Cleanup, esta verificação é segura pois usa type assertion.

---

## ✅ VERIFICAÇÃO PÓS-CORREÇÃO

Após aplicar todas as correções, execute:

```bash
# 1. Verificar se compila
go build -o metis-screen .

# 2. Rodar testes existentes
go test ./...

# 3. Verificar race conditions
go build -race -o metis-screen-race .

# 4. Validar go.mod
go mod tidy
go mod verify
```

---

## 📋 RESUMO DAS MUDANÇAS

| # | Arquivo | Tipo | Impacto |
|---|---------|------|---------|
| 1 | go.mod | Crítico | Build quebrado sem isso |
| 2 | config.go | Crítico | Erros silenciosos de config |
| 3 | config.go | Médio | Código morto |
| 4 | agent.go | Crítico | Perda de erros de stream |
| 5 | agent.go | Crítico | Vazamento de processos |
| 6 | config.go | Baixo | Performance (otimização) |
| 7 | config.go | Médio | Race condition potencial |
| 8 | security.go | Médio | Falsos positivos |
| 9 | provider.go | Baixo | Performance (memória) |
| 10 | agent.go | Baixo | Performance (CPU) |
| 11 | config.go | Médio | Consistência de estado |
| 12 | main.go | Baixo | Cleanup de recursos |

---

## 🎯 CRITÉRIOS DE ACEITE

- [ ] Projeto compila sem erros com `go build`
- [ ] Nenhum warning de variável não utilizada
- [ ] Todos os testes existentes passam
- [ ] `go vet` não reporta issues
- [ ] `go mod tidy` não altera go.mod
- [ ] Função duplicada removida
- [ ] Logs de erro adicionados onde havia silent fail
- [ ] Race detector limpo em teste básico

---

**Instrução Final:** Aplique as correções **UMA POR UMA**, verificando após cada mudança que o código ainda compila. Comece pelas correções críticas (1-5), depois as médias (6-8, 11), e finalmente as de otimização (9-10, 12).

Se encontrar dependências circulares ou conflitos de merge, priorize a estabilidade do código sobre otimizações.
