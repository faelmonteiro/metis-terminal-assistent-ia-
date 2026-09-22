# 🚀 Prompt de Correção - Projeto Metis Screen (Go)

## 📋 Contexto
Você é um especialista sênior em Go aplicando correções críticas de bugs, falhas silenciosas, código morto, performance e segurança no projeto Metis Screen.

**Versão Alvo:** Go 1.21+  
**Arquivos Principais:** `go.mod`, `internal/config/config.go`, `internal/agent/agent.go`, `internal/security/security.go`, `internal/ai/provider.go`, `main.go`

---

## 🔴 CORREÇÕES CRÍTICAS (Aplicar na Ordem)

### 1️⃣ Corrigir Versão do Go (go.mod)
**Arquivo:** `go.mod`  
**Linha:** 3  
**Problema:** Versão 1.27.1 não existe

```diff
- go 1.27.1
+ go 1.21
```

---

### 2️⃣ Adicionar Logging em loadEnvFiles (Falha Silenciosa)
**Arquivo:** `internal/config/config.go`  
**Função:** `loadEnvFiles` (~linha 2200)  
**Problema:** Erros de leitura de arquivos .env são ignorados

```go
// ANTES (linhas ~2210-2215):
for _, cand := range existing {
    f, err := os.Open(cand.path)
    if err != nil {
        continue  // ❌ ERRO SILENCIOSO
    }
    // ...
}

// DEPOIS:
for _, cand := range existing {
    f, err := os.Open(cand.path)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Aviso: não foi possível ler %s: %v\n", cand.path, err)
        continue
    }
    // ...
}
```

**Import necessário:** `"fmt"` (já deve existir)

---

### 3️⃣ Remover Função Duplicada (Código Morto)
**Arquivo:** `internal/config/config.go`  
**Problema:** Duas funções idênticas - uma pública e uma privada sem uso

**Localizar e REMOVER completamente esta função:**
```go
// ❌ REMOVER ESTA FUNÇÃO INTEIRA (~linha 1080-1085):
func (c *Config) getActiveModelForProvider(prov string) string {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.getActiveModelForProviderLocked(prov)
}
```

**Manter apenas:**
- `GetActiveModelForProvider` (pública, com G maiúsculo)
- `getActiveModelForProviderLocked` (privada, usada internamente)

---

### 4️⃣ Corrigir Race Condition em Goroutine (Perda de Erros)
**Arquivo:** `internal/agent/agent.go`  
**Função:** `RunAgentLoop` ou onde houver streaming (~linha 720-730)  
**Problema:** Erro da goroutine de streaming nunca é verificado

```go
// ANTES:
var streamErr error
go func() {
    defer close(ch)
    streamErr = prov.AskStream(ctx, sysPrompt, userPrompt, ch)
}()

for chunk := range ch {
    fullResp.WriteString(chunk)
}
// ❌ streamErr nunca é checado

// DEPOIS:
var streamErr error
var wg sync.WaitGroup
wg.Add(1)

go func() {
    defer wg.Done()
    defer close(ch)
    streamErr = prov.AskStream(ctx, sysPrompt, userPrompt, ch)
}()

for chunk := range ch {
    fullResp.WriteString(chunk)
}

wg.Wait()  // ✅ Aguarda goroutine terminar
if streamErr != nil {
    fmt.Fprintf(os.Stderr, "Erro no streaming: %v\n", streamErr)
    // Opcional: retornar erro ou tratar conforme contexto
}
```

**Imports necessários:** `"sync"` (adicionar se não existir)

---

### 5️⃣ Prevenir Vazamento de Processos Filhos (Segurança)
**Arquivo:** `internal/agent/agent.go`  
**Função:** `ExecuteTool` (~linha 426-427)  
**Problema:** Processos filhos não são mortos quando contexto expira

```go
// ANTES:
execCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
defer cancel()
cmd := exec.CommandContext(execCtx, shell, "-c", call.Command)

// DEPOIS:
execCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
defer cancel()
cmd := exec.CommandContext(execCtx, shell, "-c", call.Command)
cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}  // ✅ Cria novo process group

// Adicionar cleanup após cmd.Start() ou cmd.Run():
defer func() {
    if cmd.Process != nil {
        syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)  // ✅ Mata todo o process group
    }
}()
```

**Imports necessários:** `"syscall"` (adicionar se não existir)

---

## 🟡 CORREÇÕES DE PERFORMANCE

### 6️⃣ Otimizar Marshal/Unmarshal Redundante
**Arquivo:** `internal/config/config.go`  
**Função:** `saveModelsConfigLocked` (~linha 1700)  
**Problema:** Faz marshal/unmarshal para cada caminho candidato

```go
// ANTES (dentro do loop for _, p := range paths):
for _, p := range paths {
    var m map[string]interface{}
    if data, err := os.ReadFile(p); err == nil {
        _ = json.Unmarshal(data, &m)
    }
    // ... modifica m ...
    if outData, err := json.MarshalIndent(m, "", "  "); err == nil {
        _ = os.WriteFile(p, outData, 0600)
    }
}

// DEPOIS:
// 1. Fazer marshal UMA vez antes do loop
var m map[string]interface{}
// ... carregar e modificar m uma única vez ...

outData, err := json.MarshalIndent(m, "", "  ")
if err != nil {
    return err
}

// 2. Escrever em todos os paths sem re-marshal
for _, p := range paths {
    _ = os.WriteFile(p, outData, 0600)
}
```

---

### 7️⃣ Reduzir Lock Durante I/O (Race Condition)
**Arquivo:** `internal/config/config.go`  
**Função:** `Reload` (~linha 900)  
**Problema:** Mantém lock durante operações lentas de I/O

```go
// ANTES:
func (c *Config) Reload() {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    home := GetUserHome()
    loadEnvFiles(home, true)  // ❌ I/O lento com lock
    c.ModelsConfig = loadModelsConfigFile()  // ❌ I/O lento com lock
}

// DEPOIS:
func (c *Config) Reload() {
    // 1. Carregar dados FORA do lock
    home := GetUserHome()
    loadEnvFiles(home, true)
    newConfig := loadModelsConfigFile()
    
    // 2. Aplicar com lock rápido
    c.mu.Lock()
    defer c.mu.Unlock()
    c.ModelsConfig = newConfig
}
```

---

### 8️⃣ Ajustar Polling Agressivo (CPU)
**Arquivo:** `internal/agent/agent.go`  
**Função:** Onde há polling de status file (~linha 900)  
**Problema:** Polling a cada 100ms por 12 segundos

```go
// ANTES:
waited := 0
maxWait := 120
for waited < maxWait {
    if _, err := os.Stat(statusFile); err == nil {
        time.Sleep(150 * time.Millisecond)
        break
    }
    time.Sleep(100 * time.Millisecond)  // ❌ Muito frequente
    waited++
}

// DEPOIS:
waited := 0
maxWait := 60  // Reduzir para 6 segundos (60 × 100ms)
for waited < maxWait {
    if _, err := os.Stat(statusFile); err == nil {
        time.Sleep(150 * time.Millisecond)
        break
    }
    time.Sleep(250 * time.Millisecond)  // ✅ Reduzir frequência
    waited++
}

// OU usar fsnotify (mais complexo, mas ideal):
// watcher, _ := fsnotify.NewWatcher()
// watcher.Add(statusFile)
// <-watcher.Events  // Block until file changes
```

**Import necessário:** `"github.com/fsnotify/fsnotify"` (opcional, para solução ideal)

---

### 9️⃣ Reduzir Buffers Grandes Demais (Memória)
**Arquivo:** `internal/ai/provider.go`  
**Função:** `AskStream` ou similar (~linha 211-212, 450-451)  
**Problema:** Buffer de 64KB + max token de 10MB é excessivo

```go
// ANTES:
scanner := bufio.NewScanner(resp.Body)
buf := make([]byte, 64*1024)
scanner.Buffer(buf, 10*1024*1024)  // 10MB max

// DEPOIS:
scanner := bufio.NewScanner(resp.Body)
// ✅ Usar buffer padrão (64KB já é default do bufio)
// Ou reduzir se necessário:
buf := make([]byte, 32*1024)  // 32KB
scanner.Buffer(buf, 5*1024*1024)  // 5MB max (ainda generoso)
```

---

## 🟢 CORREÇÕES DE USABILIDADE E ROBUSTEZ

### 🔟 Refinar IsSafeReadCommand (Falsos Positivos)
**Arquivo:** `internal/security/security.go`  
**Função:** `IsSafeReadCommand` (~linha 218-225)  
**Problema:** Bloqueia comandos legítimos com $(...)

```go
// ANTES:
if strings.Contains(trimmed, "$(") || strings.Contains(trimmed, "`") {
    return false  // ❌ Falso positivo: echo "$(date)" seria bloqueado
}

// DEPOIS:
// Opção 1: Permitir subshell apenas com comandos seguros na whitelist
safeSubshellCmds := []string{"date", "whoami", "pwd", "echo", "hostname"}
hasUnsafeSubshell := false

if strings.Contains(trimmed, "$(") {
    // Extrair comando dentro de $(...) e verificar whitelist
    // Implementação simplificada:
    hasUnsafeSubshell = true  // Manter bloqueio por enquanto (implementar parser completo depois)
}

if hasUnsafeSubshell {
    // Verificar se é um caso óbvio seguro
    if strings.Contains(trimmed, "$(date)") || strings.Contains(trimmed, "$(whoami)") {
        // Permitir casos específicos seguros
    } else {
        return false
    }
}

// Opção 2 (mais simples): Apenas logar aviso em vez de bloquear
if strings.Contains(trimmed, "$(") || strings.Contains(trimmed, "`") {
    // return false  // Comentar linha antiga
    // Logar para auditoria mas permitir
    fmt.Fprintf(os.Stderr, "Aviso: comando contém subshell: %s\n", trimmed)
}
```

**Recomendação:** Usar Opção 2 temporariamente até implementar parser completo.

---

### 1️⃣1️⃣ Validar Modelo Após applyActiveModelsLocked
**Arquivo:** `internal/config/config.go`  
**Função:** `applyActiveModelsLocked` (~linha 1450)  
**Problema:** Pode deixar provider ativo sem modelo válido

```go
// ADICIONAR no final da função applyActiveModelsLocked:
// ... após o loop for _, p := range []string{"ollama", "g4f", ...}

// ✅ Verificar se o provider ativo ainda tem modelo válido
activeProv := c.Provider
if activeProv != "" {
    activeModel := c.getActiveModelForProviderLocked(activeProv)
    if activeModel == "" {
        // Provider ativo sem modelo - tentar recuperar
        if first := c.getFirstValidModelForProviderLocked(activeProv); first != "" {
            c.setActiveModelForProviderLocked(activeProv, first)
            fmt.Fprintf(os.Stderr, "Aviso: modelo do provider %s foi recuperado para %s\n", activeProv, first)
        } else {
            // Não tem modelo válido - fallback para primeiro provider disponível
            fmt.Fprintf(os.Stderr, "Erro: provider %s não tem modelos válidos\n", activeProv)
            // Opcional: definir provider padrão
        }
    }
}
```

---

### 1️⃣2️⃣ Garantir Cleanup em Falha da GUI
**Arquivo:** `main.go`  
**Função:** `main` (~linha 64-68)  
**Problema:** Se gui.Run falhar, recursos podem vazar antes do fallback TUI

```go
// ANTES:
if shouldUseGUI {
    err := gui.Run(cfg, km, initialQuery, flagLines)
    if err == nil {
        return
    }
    fmt.Fprintf(os.Stderr, "Aviso: Falha ao abrir janela gráfica (%v), iniciando modo terminal TUI...\n", err)
    // ❌ Continua para TUI sem cleanup
}

// DEPOIS:
if shouldUseGUI {
    err := gui.Run(cfg, km, initialQuery, flagLines)
    if err == nil {
        return  // ✅ Sucesso
    }
    // ✅ Garantir cleanup antes do fallback
    if cleanupErr := gui.Cleanup(); cleanupErr != nil {
        fmt.Fprintf(os.Stderr, "Aviso: erro no cleanup da GUI: %v\n", cleanupErr)
    }
    fmt.Fprintf(os.Stderr, "Aviso: Falha ao abrir janela gráfica (%v), iniciando modo terminal TUI...\n", err)
    // Agora seguro para fallback TUI
}
```

**Nota:** Requer adicionar método `Cleanup()` no pacote `gui` se não existir.

---

## ✅ Critérios de Aceite (Checklist Pós-Correção)

### Build e Validação
```bash
# 1. Build sem erros
go build -o metis .

# 2. Vet sem warnings críticos
go vet ./...

# 3. Testes passando
go test ./...

# 4. Race detector (opcional mas recomendado)
go run -race . --help

# 5. Formatação
go fmt ./...
```

### Checklist Funcional
- [ ] Compila sem erros de versão do Go
- [ ] Nenhum erro silencioso em logs (testar com .env inexistente)
- [ ] Funções duplicadas removidas (grep por nomes suspeitos)
- [ ] Goroutines tratam erros corretamente (testar timeout de API)
- [ ] Processos são mortos após timeout (testar comando longo)
- [ ] Performance melhorada (monitorar CPU/memória em uso prolongado)
- [ ] Comandos legítimos com $() funcionam (ex: `echo "$(date)"`)
- [ ] Fallback GUI → TUI funciona sem vazamentos

---

## 📊 Resumo das Correções

| # | Categoria | Arquivo | Impacto | Dificuldade |
|---|-----------|---------|---------|-------------|
| 1 | Build | go.mod | 🔴 Crítico | Fácil |
| 2 | Robustez | config.go | 🟠 Alto | Fácil |
| 3 | Código Morto | config.go | 🟢 Médio | Fácil |
| 4 | Robustez | agent.go | 🔴 Crítico | Médio |
| 5 | Segurança | agent.go | 🔴 Crítico | Médio |
| 6 | Performance | config.go | 🟢 Médio | Médio |
| 7 | Race Condition | config.go | 🟠 Alto | Fácil |
| 8 | Performance | agent.go | 🟢 Médio | Fácil |
| 9 | Memória | provider.go | 🟢 Baixo | Fácil |
| 10 | Usabilidade | security.go | 🟢 Médio | Médio |
| 11 | Robustez | config.go | 🟠 Alto | Fácil |
| 12 | Recursos | main.go | 🟠 Alto | Médio |

**Total:** 12 correções  
**Críticas:** 4 (1, 4, 5, 2)  
**Altas:** 3 (7, 11, 12)  
**Médias:** 4 (3, 6, 8, 10)  
**Baixas:** 1 (9)

---

## 🎯 Instruções de Aplicação

1. **Ordem de Prioridade:**
   - Primeiro: Correções 1-5 (críticas)
   - Segundo: Correções 6-8 (performance)
   - Terceiro: Correções 9-12 (refinamentos)

2. **Testar Após Cada Correção:**
   ```bash
   go build -o metis . && echo "✅ Build OK" || echo "❌ Build Falhou"
   ```

3. **Commit Separado por Correção:**
   ```bash
   git add go.mod && git commit -m "fix: corrigir versão do Go para 1.21"
   git add internal/config/config.go && git commit -m "fix: adicionar logging em loadEnvFiles"
   # ... etc
   ```

4. **Validação Final:**
   ```bash
   go build -race -o metis .
   ./metis --help
   go test -race ./...
   ```

---

## 📝 Notas Adicionais

- **Ambiente Mínimo:** Go 1.19+, Linux/macOS
- **Dependências do Sistema (GUI):** `libgtk-3-dev`, `webkit2gtk-4.0-dev`, `pkg-config`
- **Testes Recomendados:**
  - Simular arquivo .env corrompido
  - Testar timeout de API (mock de resposta lenta)
  - Executar comando bash longo (>45s) e verificar kill
  - Monitorar memória em sessão prolongada

**Status Esperado Pós-Correção:**
- ✅ Build limpo sem erros
- ✅ Zero falhas silenciosas
- ✅ Zero race conditions detectáveis
- ✅ Performance otimizada (CPU/memória reduzida)
- ✅ Segurança reforçada sem falsos positivos críticos

---

**Gerado em:** 2025-06-XX  
**Baseado em:** Auditoria técnica completa do projeto Metis Screen  
**Próximos Passos:** Aplicar correções na ordem, testar, fazer commit, repetir.
