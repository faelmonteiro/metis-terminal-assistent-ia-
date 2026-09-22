package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"metis-screen/internal/ai"
	"metis-screen/internal/config"
	"metis-screen/internal/kitty"
	"metis-screen/internal/security"
)

var (
	toolCallXmlRegex  = regexp.MustCompile(`(?is)<(?:tool_call|function|invoke)\b([^>]*)>(.*?)(?:</(?:tool_call|function|invoke)>|$)`)
	toolCallAttrRegex = regexp.MustCompile(`(?i)\bname\s*=\s*(?:"([^"]+)"|'([^']+)'|([^\s>]+))`)
	toolCallPathRegex = regexp.MustCompile(`(?i)\bpath\s*=\s*(?:"([^"]+)"|'([^']+)'|([^\s>]+))`)

	legacyBashRegex   = regexp.MustCompile(`(?is)<bash>(.*?)(?:</bash>|$)`)
	legacyReadRegex   = regexp.MustCompile(`(?is)<read_file>(.*?)(?:</read_file>|$)`)
	legacyWriteRegex  = regexp.MustCompile(`(?is)<write_file\s+path=["'](.*?)["']>(.*?)(?:</write_file>|$)`)
	legacyAppendRegex = regexp.MustCompile(`(?is)<append_file\s+path=["'](.*?)["']>(.*?)(?:</append_file>|$)`)
	legacyListDirReg  = regexp.MustCompile(`(?is)<list_dir>(.*?)(?:</list_dir>|$)`)

	codeBlockRegex = regexp.MustCompile("(?s)```(?:[a-zA-Z0-9_-]+)?\n(.*?)```")

	cdataRegex      = regexp.MustCompile(`(?s)<!\[CDATA\[(.*?)\]\]>`)
	paramRegex      = regexp.MustCompile(`(?is)<(?:parameter|arg)\b[^>]*>(.*?)(?:</(?:parameter|arg)>|$)`)
	residualRegex   = regexp.MustCompile(`(?i)</?(?:parameter|param|function|invoke|arg|arguments|cmd|command|bash|sh|exec|code|script|tool_call|call)[^>]*>`)
	fenceOpenRegex  = regexp.MustCompile(`(?i)^\s*` + "```" + `(?:[a-zA-Z0-9_-]+)?\s*\n?`)
	fenceCloseRegex = regexp.MustCompile(`(?i)\n?\s*` + "```" + `\s*$`)
	danglingRegex   = regexp.MustCompile(`(?is)\s*</[a-zA-Z0-9_-]+>\s*$`)
	jsonRegex       = regexp.MustCompile(`\{[^{}]*(?:\{[^{}]*\}[^{}]*)*\}`)
	cleanTagsRegex  = regexp.MustCompile(`(?is)</?(?:parameter|function|invoke|tool_call|tool_result)[^>]*>`)
	inlineCodeRegex = regexp.MustCompile("`([^`]+)`")
)

type ToolCall struct {
	Type    string // "bash", "read_file", "write_file", "append_file", "list_dir"
	Command string
	Path    string
	Content string
}

type ToolResult struct {
	Call   ToolCall
	Output string
	Err    error
}

// CleanToolValue sanitiza o conteúdo extraído das tags de ferramenta
func CleanToolValue(text string) string {
	if text == "" {
		return ""
	}

	// Remove CDATA
	text = cdataRegex.ReplaceAllString(text, "$1")
	text = strings.ReplaceAll(text, "<![CDATA[", "")
	text = strings.ReplaceAll(text, "]]>", "")

	// Extrai se vier encapsulado em <parameter> ou <arg>
	if m := paramRegex.FindStringSubmatch(text); len(m) > 1 {
		text = m[1]
	}

	// Remove tags residuais de parâmetros e chamadas
	text = residualRegex.ReplaceAllString(text, "")

	// Remove markdown fences ```bash ... ```
	trimVal := strings.TrimSpace(text)
	trimVal = fenceOpenRegex.ReplaceAllString(trimVal, "")
	trimVal = fenceCloseRegex.ReplaceAllString(trimVal, "")

	// Desescapa entidades HTML
	trimVal = html.UnescapeString(trimVal)

	// Remove qualquer tag de fechamento dangling
	trimVal = danglingRegex.ReplaceAllString(trimVal, "")

	return strings.TrimSpace(trimVal)
}

// ParseToolCalls extrai invocações de ferramentas em formatos XML (<tool_call>, <bash>, etc.) ou JSON
func ParseToolCalls(raw string) []ToolCall {
	var calls []ToolCall
	if strings.TrimSpace(raw) == "" {
		return calls
	}

	// 1. Parser XML moderno: <tool_call name="..." path="...">
	matches := toolCallXmlRegex.FindAllStringSubmatch(raw, -1)
	for _, m := range matches {
		attrs := m[1]
		content := m[2]

		name := "bash"
		if nm := toolCallAttrRegex.FindStringSubmatch(attrs); len(nm) > 0 {
			for i := 1; i <= 3; i++ {
				if nm[i] != "" {
					name = strings.ToLower(nm[i])
					break
				}
			}
		}

		path := ""
		if pm := toolCallPathRegex.FindStringSubmatch(attrs); len(pm) > 0 {
			for i := 1; i <= 3; i++ {
				if pm[i] != "" {
					path = pm[i]
					break
				}
			}
		}

		cVal := CleanToolValue(content)
		pVal := CleanToolValue(path)

		switch name {
		case "bash", "sh", "exec", "run_command":
			cmd := cVal
			if cmd == "" {
				cmd = pVal
			}
			if cmd != "" {
				calls = append(calls, ToolCall{Type: "bash", Command: cmd})
			}
		case "read_file", "cat", "ler_arquivo":
			p := pVal
			if p == "" {
				p = cVal
			}
			if p != "" {
				calls = append(calls, ToolCall{Type: "read_file", Path: p})
			}
		case "write_file", "create_file", "salvar_arquivo":
			calls = append(calls, ToolCall{Type: "write_file", Path: pVal, Content: cVal})
		case "append_file", "adicionar_ao_arquivo":
			calls = append(calls, ToolCall{Type: "append_file", Path: pVal, Content: cVal})
		case "list_dir", "ls", "listar_pasta":
			p := pVal
			if p == "" {
				p = cVal
			}
			if p == "" {
				p = "."
			}
			calls = append(calls, ToolCall{Type: "list_dir", Path: p})
		}
	}

	if len(calls) > 0 {
		return calls
	}

	// 2. Parser legados diretos: <bash>, <read_file>, <write_file>, <append_file>, <list_dir>
	for _, m := range legacyBashRegex.FindAllStringSubmatch(raw, -1) {
		if cmd := CleanToolValue(m[1]); cmd != "" {
			calls = append(calls, ToolCall{Type: "bash", Command: cmd})
		}
	}
	for _, m := range legacyReadRegex.FindAllStringSubmatch(raw, -1) {
		if p := CleanToolValue(m[1]); p != "" {
			calls = append(calls, ToolCall{Type: "read_file", Path: p})
		}
	}
	for _, m := range legacyWriteRegex.FindAllStringSubmatch(raw, -1) {
		path := CleanToolValue(m[1])
		content := CleanToolValue(m[2])
		if path != "" {
			calls = append(calls, ToolCall{Type: "write_file", Path: path, Content: content})
		}
	}
	for _, m := range legacyAppendRegex.FindAllStringSubmatch(raw, -1) {
		path := CleanToolValue(m[1])
		content := CleanToolValue(m[2])
		if path != "" {
			calls = append(calls, ToolCall{Type: "append_file", Path: path, Content: content})
		}
	}
	for _, m := range legacyListDirReg.FindAllStringSubmatch(raw, -1) {
		path := CleanToolValue(m[1])
		if path == "" {
			path = "."
		}
		calls = append(calls, ToolCall{Type: "list_dir", Path: path})
	}

	if len(calls) > 0 {
		return calls
	}

	// 3. Fallback JSON tool_call
	if strings.Contains(raw, `"name"`) || strings.Contains(raw, `"function"`) || strings.Contains(raw, `"cmd"`) {
		if jm := jsonRegex.FindString(raw); jm != "" {
			var data map[string]interface{}
			if err := json.Unmarshal([]byte(jm), &data); err == nil {
				name := "bash"
				if n, ok := data["name"].(string); ok && n != "" {
					name = strings.ToLower(n)
				}
				cmd := ""
				path := ""
				if c, ok := data["cmd"].(string); ok {
					cmd = c
				} else if c, ok := data["command"].(string); ok {
					cmd = c
				}
				if p, ok := data["path"].(string); ok {
					path = p
				}

				if args, ok := data["arguments"].(map[string]interface{}); ok {
					if c, ok := args["cmd"].(string); ok && c != "" {
						cmd = c
					} else if c, ok := args["command"].(string); ok && c != "" {
						cmd = c
					}
					if p, ok := args["path"].(string); ok && p != "" {
						path = p
					}
				}

				cmd = CleanToolValue(cmd)
				path = CleanToolValue(path)

				if name == "bash" && cmd != "" {
					calls = append(calls, ToolCall{Type: "bash", Command: cmd})
				} else if name == "read_file" && path != "" {
					calls = append(calls, ToolCall{Type: "read_file", Path: path})
				}
			}
		}
	}

	// 4. Fallback: blocos de código markdown ```bash ... ```
	if len(calls) == 0 {
		codeBlocks := ExtractAllCodeBlocks(raw)
		for _, block := range codeBlocks {
			lines := strings.Split(block, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				cleanLine := kitty.RemoveComments(line)
				if IsValidCommandCandidate(cleanLine) {
					calls = append(calls, ToolCall{Type: "bash", Command: cleanLine})
				}
			}
		}
	}

	return calls
}

// ExtractFirstCodeBlock extrai o primeiro comando sugerido
func ExtractFirstCodeBlock(text string) string {
	blocks := ExtractAllCodeBlocks(text)
	if len(blocks) > 0 {
		return blocks[0]
	}
	return ""
}

// ExtractAllCodeBlocks extrai todos os comandos executáveis gerados na resposta
func ExtractAllCodeBlocks(text string) []string {
	var results []string
	seen := make(map[string]bool)

	// Blocos cercados por ```bash ... ```
	matches := codeBlockRegex.FindAllStringSubmatch(text, -1)
	for _, m := range matches {
		block := strings.TrimSpace(m[1])
		lines := strings.Split(block, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			line = strings.Trim(line, "`")
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			cleanLine := kitty.RemoveComments(line)
			if IsValidCommandCandidate(cleanLine) && !seen[cleanLine] {
				seen[cleanLine] = true
				results = append(results, cleanLine)
			}
		}
	}

	// Comandos inline se nenhum bloco ``` tiver sido encontrado
	if len(results) == 0 {
		for _, m := range inlineCodeRegex.FindAllStringSubmatch(text, -1) {
			candidate := strings.TrimSpace(m[1])
			if candidate != "" && !strings.HasPrefix(candidate, "#") {
				cleanCandidate := kitty.RemoveComments(candidate)
				if IsValidCommandCandidate(cleanCandidate) && !seen[cleanCandidate] {
					seen[cleanCandidate] = true
					results = append(results, cleanCandidate)
				}
			}
		}
	}

	return results
}

// IsValidCommandCandidate avalia se a linha é candidata a comando Unix válido
func IsValidCommandCandidate(str string) bool {
	str = strings.TrimSpace(str)
	if str == "" {
		return false
	}

	// Remove prefixos comuns de prompt
	str = strings.TrimPrefix(str, "$ ")
	str = strings.TrimPrefix(str, "❯ ")
	str = strings.TrimPrefix(str, "# ")
	str = strings.TrimPrefix(str, "> ")
	str = strings.TrimSpace(str)

	if str == "" {
		return false
	}

	// Ignora divisores markdown
	if strings.HasPrefix(str, "---") || strings.HasPrefix(str, "===") || strings.HasPrefix(str, "___") {
		return false
	}

	// Variáveis de ambiente no início (ex: VAR=1 cmd)
	if strings.Contains(str, "=") && !strings.HasPrefix(str, "=") {
		parts := strings.SplitN(str, "=", 2)
		if !strings.Contains(parts[0], " ") {
			return true
		}
	}

	words := strings.Fields(str)
	if len(words) == 0 {
		return false
	}
	firstWord := words[0]

	// Se começar com maiúsculas repetidas (logs), descarta
	if len(firstWord) >= 2 && firstWord == strings.ToUpper(firstWord) {
		return false
	}

	if firstWord == "sudo" || firstWord == "doas" || firstWord == "env" {
		if len(words) > 1 {
			firstWord = words[1]
		}
	}

	if _, err := exec.LookPath(firstWord); err == nil {
		return true
	}
	if strings.HasPrefix(firstWord, "./") || strings.HasPrefix(firstWord, "/") {
		return true
	}

	return false
}

// LimitContextTurns limita a quantidade de turnos de diálogo anteriores
func LimitContextTurns(ctx string, maxTurns int) string {
	if maxTurns <= 0 {
		maxTurns = 5
	}

	assistTag := "[Assistente]:"
	firstIdx := strings.Index(ctx, assistTag)
	if firstIdx == -1 {
		return ctx
	}

	header := ctx[:firstIdx]
	dialog := ctx[firstIdx:]

	turns := strings.Split(dialog, assistTag)
	nonEmptyTurns := []string{}
	for _, t := range turns {
		if strings.TrimSpace(t) != "" {
			nonEmptyTurns = append(nonEmptyTurns, t)
		}
	}

	if len(nonEmptyTurns) <= maxTurns {
		return ctx
	}

	preservedTurns := nonEmptyTurns[len(nonEmptyTurns)-maxTurns:]
	var sb strings.Builder
	sb.WriteString(header)
	for _, t := range preservedTurns {
		sb.WriteString(assistTag)
		sb.WriteString(t)
	}

	return sb.String()
}

// ExecuteTool executa ferramentas locais de inspeção ou arquivo em segurança
func ExecuteTool(ctx context.Context, call ToolCall) ToolResult {
	res := ToolResult{Call: call}

	switch call.Type {
	case "bash":
		if dangerous, warn := security.IsDangerousCommand(call.Command); dangerous {
			res.Err = fmt.Errorf("ação bloqueada por segurança: %s", warn)
			res.Output = res.Err.Error()
			return res
		}

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
		combined := outBuf.String()
		if errBuf.Len() > 0 {
			if combined != "" {
				combined += "\n"
			}
			combined += errBuf.String()
		}

		// Se houver erro de timeout, matar process group
		if err != nil && execCtx.Err() == context.DeadlineExceeded {
			// Tenta matar o process group
			syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}

		if err != nil {
			res.Err = fmt.Errorf("comando falhou (%w):\n%s", err, combined)
			res.Output = combined
			return res
		}

		res.Output = strings.TrimSpace(combined)
		if res.Output == "" {
			res.Output = "[Comando executado com sucesso sem saída]"
		}

	case "read_file":
		if security.IsSensitivePath(call.Path) {
			res.Err = fmt.Errorf("leitura de arquivo sensível bloqueada: %s", call.Path)
			res.Output = res.Err.Error()
			return res
		}

		f, err := os.Open(call.Path)
		if err != nil {
			res.Err = err
			res.Output = fmt.Sprintf("Erro ao ler arquivo: %v", err)
			return res
		}
		defer f.Close()

		const maxReadBytes = 100 * 1024
		buf, err := io.ReadAll(io.LimitReader(f, maxReadBytes+1))
		if err != nil {
			res.Err = err
			res.Output = fmt.Sprintf("Erro ao ler arquivo: %v", err)
			return res
		}

		if len(buf) > maxReadBytes {
			res.Output = string(buf[:maxReadBytes]) + "\n...[truncado]"
		} else {
			res.Output = string(buf)
		}

	case "write_file":
		if security.IsSensitivePath(call.Path) {
			res.Err = fmt.Errorf("escrita em arquivo sensível bloqueada: %s", call.Path)
			res.Output = res.Err.Error()
			return res
		}

		_ = os.MkdirAll(filepath.Dir(call.Path), 0755)
		perm := os.FileMode(0644)
		ext := strings.ToLower(filepath.Ext(call.Path))
		isExecutable := ext == ".sh" || ext == ".bash" || ext == ".zsh" || ext == ".py" || ext == ".rb" || ext == ".pl" || strings.HasPrefix(strings.TrimSpace(call.Content), "#!")
		if isExecutable {
			perm = 0755
		}

		if err := os.WriteFile(call.Path, []byte(call.Content), perm); err != nil {
			res.Err = err
			res.Output = fmt.Sprintf("Erro ao escrever arquivo: %v", err)
			return res
		}
		if isExecutable {
			_ = os.Chmod(call.Path, 0755)
			res.Output = fmt.Sprintf("Arquivo %s escrito com sucesso (%d bytes) [permissão de execução +x aplicada automaticamente].", call.Path, len(call.Content))
		} else {
			res.Output = fmt.Sprintf("Arquivo %s escrito com sucesso (%d bytes).", call.Path, len(call.Content))
		}

	case "append_file":
		if security.IsSensitivePath(call.Path) {
			res.Err = fmt.Errorf("modificação de arquivo sensível bloqueada: %s", call.Path)
			res.Output = res.Err.Error()
			return res
		}

		_ = os.MkdirAll(filepath.Dir(call.Path), 0755)
		f, err := os.OpenFile(call.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			res.Err = err
			res.Output = fmt.Sprintf("Erro ao abrir arquivo para append: %v", err)
			return res
		}
		defer f.Close()

		if _, err := f.WriteString("\n" + call.Content); err != nil {
			res.Err = err
			res.Output = fmt.Sprintf("Erro ao anexar no arquivo: %v", err)
			return res
		}
		res.Output = fmt.Sprintf("Conteúdo adicionado ao final de %s com sucesso.", call.Path)

	case "list_dir":
		dir := call.Path
		if dir == "" {
			dir = "."
		}
		var cmd *exec.Cmd
		if _, err := exec.LookPath("eza"); err == nil {
			cmd = exec.CommandContext(ctx, "eza", "-la", "--group-directories-first", "--icons=never", "--", dir)
		} else if _, err := exec.LookPath("tree"); err == nil {
			cmd = exec.CommandContext(ctx, "tree", "-L", "2", "-a", "-F", "--", dir)
		} else {
			cmd = exec.CommandContext(ctx, "ls", "-lah", "--", dir)
		}

		if out, err := cmd.CombinedOutput(); err == nil {
			outStr := strings.TrimSpace(string(out))
			if outStr == "" {
				outStr = "[Diretório vazio]"
			}
			res.Output = outStr
		} else {
			// Fallback seguro para ls se eza ou tree falharem
			fbCmd := exec.CommandContext(ctx, "ls", "-lah", "--", dir)
			if fbOut, fbErr := fbCmd.CombinedOutput(); fbErr == nil {
				outStr := strings.TrimSpace(string(fbOut))
				if outStr == "" {
					outStr = "[Diretório vazio]"
				}
				res.Output = outStr
			} else {
				res.Err = err
				res.Output = fmt.Sprintf("Erro ao listar diretório: %v", err)
			}
		}

	default:
		res.Err = fmt.Errorf("tipo de ferramenta desconhecido: %s", call.Type)
		res.Output = res.Err.Error()
	}

	return res
}

// AutoProgressCallback função de notificação durante os passos do agente autônomo
type AutoProgressCallback func(step, maxSteps int, command, terminalOutput, statusInfo string, isFinished bool)
type AutoConfirmCallback func(command, reason string) bool
type AutoStepLimitCallback func(currentSteps, maxSteps int) (moreSteps int, shouldContinue bool)

// RunAutonomousAgent gerencia o ciclo fechado Sense -> Plan -> Act -> Observe -> Report
func RunAutonomousAgent(
	ctx context.Context,
	prov ai.Provider,
	km *kitty.KittyManager,
	cfg *config.Config,
	goal string,
	screenLines int,
	onProgress AutoProgressCallback,
	onConfirm AutoConfirmCallback,
	onStepLimit ...AutoStepLimitCallback,
) (string, error) {
	var stepLimitCb AutoStepLimitCallback
	if len(onStepLimit) > 0 {
		stepLimitCb = onStepLimit[0]
	}

	defaultLines := 30
	maxSteps := 6
	maxTurns := 5
	if cfg != nil {
		if cfg.DefaultScreenLines > 0 {
			defaultLines = cfg.DefaultScreenLines
		}
		if cfg.MaxSteps > 0 {
			maxSteps = cfg.MaxSteps
		}
		if cfg.MaxContextTurns > 0 {
			maxTurns = cfg.MaxContextTurns
		}
	}

	if screenLines <= 0 {
		screenLines = defaultLines
	}

	// 1. Captura inicial do terminal e status do último comando
	screenInit := km.GetScreenBuffer(screenLines)
	selInit := strings.TrimSpace(km.GetSelectionBuffer())
	lastCmd, lastExit, _ := km.GetLastCommandStatus()

	lastCmdHeader := ""
	if lastCmd != "" {
		statusLabel := "0 (Sucesso)"
		if lastExit != "" && lastExit != "0" {
			statusLabel = fmt.Sprintf("%s (Falha / Erro)", lastExit)
		}
		lastCmdHeader = fmt.Sprintf("\n[Último Comando Executado no Terminal]: %s\n[Código de Retorno / Exit Code]: %s\n", lastCmd, statusLabel)
	}

	selHeader := ""
	if selInit != "" && !strings.Contains(goal, selInit) {
		sLines := len(strings.Split(selInit, "\n"))
		selHeader = fmt.Sprintf("\n[Texto Selecionado pelo Usuário com o Mouse no Terminal (%d linhas)]:\n%s\n", sLines, selInit)
		km.ClearSelection()
	}

	if strings.TrimSpace(goal) == "" {
		if selInit != "" {
			goal = "Investigar a causa do erro selecionado com o mouse no terminal, aplicar as correções necessárias e resolver o problema."
		} else {
			goal = "Investigar a causa do erro na tela do terminal, aplicar as correções necessárias e resolver o problema."
		}
	}

	autoPrompt := fmt.Sprintf(`Você é um Agente Linux autônomo conectado diretamente ao terminal do usuário.%s%s
[Terminal do Usuário (últimas %d linhas)]:
%s

O usuário pediu: %s

═══════════════════════════════════════════════════════════
ESTRATÉGIA (planeje antes de agir)
═══════════════════════════════════════════════════════════
1. Liste sub-tarefas do pedido  2. Comando p/ cada uma  3. Ordem lógica
4. JÁ EXECUTEI? (veja histórico acima)

EXEMPLOS:
✅ "Dispositivos": lsblk → lsusb → lspci → PARAR
✅ "IP, internet, portas": ip -br addr → ping -c 3 8.8.8.8 → ss -tlnp → PARAR
❌ Loop: lsblk → lsblk → lsblk (BLOQUEADO)

═══════════════════════════════════════════════════════════
FORMATO (XML obrigatório, NÃO markdown)
═══════════════════════════════════════════════════════════
<tool_call name="bash">
comando
</

═══════════════════════════════════════════════════════════
REGRAS
════════════════════════════════════════════════════════════
• 1 comando por call • NÃO repita (sistema bloqueia) • NÃO pagers (use -c, --no-pager)
• NÃO invente saídas • Múltiplos objetivos = TODOS comandos p/ CADA objetivo

═══════════════════════════════════════════════════════════
CHECKLIST FINAL (todos SIM = parar)
═══════════════════════════════════════════════════════════
□ Cada sub-tarefa executada com comando real?
□ Saída de CADA comando analisada?
□ Sem comandos pendentes?
□ Responde completamente ao pedido?

═══════════════════════════════════════════════════════════
RELATÓRIO FINAL (formato exato)
═══════════════════════════════════════════════════════════
### 🎯 Diagnóstico
(3-5 linhas: explique a causa raiz E detalhe o que foi feito e por quê, de forma didática e técnica)

### ⚡ Ações e Comandos Executados
**1. Objetivo**`+"```bash\ncmd\n```"+`
> **Resultado:** resumo

### ✅ Confirmação
(O problema foi resolvido? Confirme claramente sim/não, resuma as ações executadas e o estado final do terminal)

### 💡 Dicas e Próximos Passos
(Validação, prevenção e próximos passos em 2 a 3 tópicos)`,
		lastCmdHeader, selHeader, screenLines, screenInit, goal)

	sysPrompt := SystemPrompt(true)
	currentContext := fmt.Sprintf("[Instrução do Agente]: %s", autoPrompt)
	step := 1
	executedOutputs := make(map[string]string)
	loopStopReason := ""

	for step <= maxSteps {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		// Prompt curto para passos 2+ (economiza ~80% tokens)
		if step > 1 {
			shortPrompt := fmt.Sprintf(`[LEMBRETE PASSO %d/%d]
• NÃO REPITA comandos (sistema bloqueia)
• Checklist antes de finalizar:
  □ Todas sub-tarefas executadas?
  □ Saídas analisadas?
  □ Sem pendentes?
  □ Responde completamente?
• Formato: <tool_call name="bash">cmd</
• Final: ### 🎯 Diagnóstico / ### ⚡ Ações / ### ✅ Confirmação / ### 💡 Dicas e Próximos Passos`, step, maxSteps)

			if cfg != nil {
				// Adiciona o lembrete ao cabeçalho da instrução
				currentContext = strings.Replace(currentContext, "[Instrução do Agente]: ", "[Instrução do Agente]: "+shortPrompt+"\n\n", 1)
			}
		}

		if onProgress != nil {
			onProgress(step, maxSteps, "", "", "Planejando próxima ação...", false)
		}

		// Chamada ao provedor de IA acumulando resposta
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

		respText := fullResp.String()
		calls := ParseToolCalls(respText)

		if len(calls) == 0 {
			// Não há chamadas de ferramenta: o agente concluiu a investigação!
			cleaned := DeduplicateReportCommands(CleanResponseFinal(respText))
			banner := fmt.Sprintf("> ╔══════════════════════════════════════════════════════════════════╗\n> ║  🎉 **OBJETIVO CONCLUÍDO COM SUCESSO** (Resolvido em %d passos)    ║\n> ╚══════════════════════════════════════════════════════════════════╝\n\n", step)
			if onProgress != nil {
				onProgress(step, maxSteps, "", "", "🎉 Concluído com sucesso!", true)
			}
			return banner + cleaned, nil
		}

		// Executa TODAS as ferramentas retornadas nesta rodada (não apenas a primeira)
		anyExecuted := false
		for _, tool := range calls {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}

			// Se for ferramenta de arquivo/diretório (read_file, write_file, append_file, list_dir),
			// executa com segurança via ExecuteTool sem tentar rodar o caminho como comando shell no Kitty
			if tool.Type != "bash" && tool.Type != "" {
				if onProgress != nil {
					onProgress(step, maxSteps, "", "", fmt.Sprintf("Executando ferramenta %s: %s", tool.Type, tool.Path), false)
				}
				res := ExecuteTool(ctx, tool)
				statusLabel := "Sucesso"
				if res.Err != nil {
					statusLabel = fmt.Sprintf("Erro: %v", res.Err)
				}
				anyExecuted = true
				currentContext += fmt.Sprintf("\n[Assistente]: %s\n<tool_result>\n[Ferramenta executada]: %s (%s)\n[Status]: %s\n[Saída]:\n%s\n</tool_result>\n[Sistema]: Analise o resultado acima. Se a informação obtida for suficiente para responder ao usuário, apresente o relatório final no formato especificado. Caso ainda precise de outras verificações ou ações, envie a próxima <tool_call>:",
					respText, tool.Type, tool.Path, statusLabel, res.Output)
				currentContext = LimitContextTurns(currentContext, maxTurns)
				continue
			}

			cmd := tool.Command
			if cmd == "" {
				cmd = tool.Path
			}
			cmd = strings.TrimSpace(cmd)
			cmd = SanitizeAutoCommand(kitty.RemoveComments(cmd))

			if cmd == "" {
				currentContext += fmt.Sprintf("\n[Assistente]: %s\n<tool_result>\nErro: tool_call sem comando válido.\n</tool_result>", respText)
				continue
			}

			// 3. Evita repetição de comandos já executados com sucesso
			if prevOutput, executed := executedOutputs[cmd]; executed {
				if onProgress != nil {
					onProgress(step, maxSteps, "", "", fmt.Sprintf("⏭️ Comando já executado anteriormente, pulando repetição: %s", cmd), false)
				}
				currentContext += fmt.Sprintf("\n[Assistente]: %s\n<tool_result>\nComando '%s' JÁ FOI EXECUTADO neste passo anterior.\n[Saída anterior]:\n%s\n\nNão execute novamente. Se a informação já for suficiente, apresente o relatório final. Caso precise de outra verificação, use um comando DIFERENTE.\n</tool_result>", respText, cmd, prevOutput)
				continue
			}

			// 1. Bloqueio incondicional de comandos comprovadamente destrutivos
			if dangerous, warn := security.IsDangerousCommand(cmd); dangerous {
				AutoAuditLog(fmt.Sprintf("BLOQUEADO: %s (%s)", cmd, warn))
				if onProgress != nil {
					onProgress(step, maxSteps, cmd, "", fmt.Sprintf("⚠️ BLOQUEADO: %s", warn), false)
				}
				currentContext += fmt.Sprintf("\n[Assistente]: %s\n<tool_result>\nAção '%s' estritamente BLOQUEADA por segurança pelo sistema: %s. Escolha uma alternativa segura sem risco de destruição.\n</tool_result>", respText, cmd, warn)
				continue
			}

			// 2. Comandos de risco / sensíveis: requer confirmação explícita do usuário
			if risky, reason := security.IsRiskyAutoCommand(cmd); risky {
				AutoAuditLog(fmt.Sprintf("CONFIRMAR: %s (%s)", cmd, reason))
				confirmed := false
				if onConfirm != nil {
					confirmed = onConfirm(cmd, reason)
				} else {
					confirmed = DefaultTTYConfirm(cmd, reason)
				}

				if !confirmed {
					AutoAuditLog(fmt.Sprintf("NEGADO: %s (%s)", cmd, reason))
					if onProgress != nil {
						onProgress(step, maxSteps, cmd, "", fmt.Sprintf("✖ Ação cancelada pelo usuário (%s)", reason), false)
					}
					currentContext += fmt.Sprintf("\n[Assistente]: %s\n<tool_result>\nAção '%s' cancelada pelo usuário. Sugira outra abordagem ou finalize.\n</tool_result>", respText, cmd)
					continue
				}
			}

			// 2.1 Comandos multilinha exigem confirmação explícita, a menos que
			// AI_AUTO_ALLOW_MULTILINE=1 (equivalente ao controlador zsh antigo).
			needMultiConfirm := false
			if cfg != nil && !cfg.AutoAllowMultiline {
				needMultiConfirm = strings.Contains(cmd, "\n")
			}
			if needMultiConfirm {
				AutoAuditLog(fmt.Sprintf("CONFIRMAR_MULTILINHA: %s", firstLineOf(cmd)))
				confirmed := false
				if onConfirm != nil {
					confirmed = onConfirm(cmd, "Comando multilinha no modo autônomo")
				} else {
					confirmed = DefaultTTYConfirm(cmd, "Comando multilinha no modo autônomo")
				}
				if !confirmed {
					AutoAuditLog(fmt.Sprintf("NEGADO_MULTILINHA: %s", firstLineOf(cmd)))
					if onProgress != nil {
						onProgress(step, maxSteps, cmd, "", "✖ Ação multilinha cancelada pelo usuário", false)
					}
					currentContext += fmt.Sprintf("\n[Assistente]: %s\n<tool_result>\nAção multilinha '%s' cancelada pelo usuário. Sugira uma alternativa em uma única linha ou finalize.\n</tool_result>", respText, cmd)
					continue
				}
			}

			// Notificação pré-execução como status (sem card duplicado de ação):
			// o card "### ⚡" com o comando é renderizado apenas UMA vez, após a execução.
			if onProgress != nil {
				onProgress(step, maxSteps, "", "", fmt.Sprintf("Executando no terminal Kitty: $ %s", cmd), false)
			}

			// Remove arquivos de status antigos para esperar o novo
			if km.WindowID != "" {
				_ = os.Remove(fmt.Sprintf("/tmp/metis_status_%s", km.WindowID))
			}
			_ = os.Remove("/tmp/metis_last_status")

			// Envia o comando diretamente à janela de trabalho do Kitty com autoEnter
			if err := km.SendText(cmd, true); err != nil {
				AutoAuditLog(fmt.Sprintf("FALHA: %s (%v)", cmd, err))
				currentContext += fmt.Sprintf("\n[Assistente]: %s\n<tool_result>\nErro ao enviar comando ao terminal Kitty: %v\n</tool_result>", respText, err)
				if onProgress != nil {
					onProgress(step, maxSteps, cmd, "", fmt.Sprintf("❌ Falha ao enviar comando ao terminal Kitty: %v", err), false)
				}
				continue
			}
			AutoAuditLog(fmt.Sprintf("EXECUTADO: %s", cmd))

			// Aguarda o término da execução (espera hook ZSH ou timeout)
			waited := 0
			maxWait := 60 // 6 segundos (60 iterações com backoff)
			statusFile := "/tmp/metis_last_status"
			if km.WindowID != "" {
				statusFile = fmt.Sprintf("/tmp/metis_status_%s", km.WindowID)
			}
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

			// Recaptura a tela atualizada do Kitty
			newScreen := km.GetScreenBuffer(screenLines)
			if idx := strings.LastIndex(newScreen, cmd); idx != -1 {
				newScreen = newScreen[idx+len(cmd):]
			}

			_, exitCode, _ := km.GetLastCommandStatus()
			statusInfo := "[Status de Retorno / Exit Code]: 0 (Sucesso / OK)"
			if exitCode != "" && exitCode != "0" {
				statusInfo = fmt.Sprintf("[Status de Retorno / Exit Code]: %s (Erro / Falha)", exitCode)
			}

			// Registra comando executado para evitar repetição
			executedOutputs[cmd] = newScreen
			anyExecuted = true

			if onProgress != nil {
				onProgress(step, maxSteps, cmd, newScreen, statusInfo, false)
			}

			currentContext += fmt.Sprintf("\n[Assistente]: %s\n<tool_result>\n[Comando executado]: %s\n%s\n[Saída capturada do terminal]:\n%s\n</tool_result>\n[Sistema]: Analise o status de retorno e a saída acima. Se o comando resolveu o erro com sucesso ou se ainda faltam verificações ou comandos para cumprir integralmente o que o usuário pediu, envie a próxima <tool_call name=\"bash\">. NUNCA invente saídas de comandos que não rodaram. Apenas envie o relatório final se TODOS os itens solicitados já foram executados e confirmados:",
				respText, cmd, statusInfo, newScreen)

			currentContext = LimitContextTurns(currentContext, maxTurns)
		}

		// Se nenhuma ação nova foi executada (comandos repetidos, bloqueados ou cancelados),
		// o modelo está em loop: encerra e consolida o relatório final em vez de repetir passos.
		if len(calls) > 0 && !anyExecuted {
			loopStopReason = "repeticao"
			break
		}

		// Se atingiu o limite de passos e ainda há ações a tomar, consulta se o usuário quer estender os passos
		if step == maxSteps && stepLimitCb != nil {
			if onProgress != nil {
				onProgress(step, maxSteps, "", "", fmt.Sprintf("⚠️ Limite de %d passos atingido. Deseja continuar com mais passos?", maxSteps), false)
			}
			moreSteps, shouldContinue := stepLimitCb(step, maxSteps)
			if shouldContinue && moreSteps > 0 {
				maxSteps += moreSteps
				if onProgress != nil {
					onProgress(step, maxSteps, "", "", fmt.Sprintf("▶️ Continuando execução (+%d passos, novo limite: %d)...", moreSteps, maxSteps), false)
				}
			}
		}

		step++
	}

	// Solicita o relatório final consolidado
	if onProgress != nil {
		onProgress(maxSteps, maxSteps, "", "", "📝 Consolidando diagnóstico e gerando relatório final...", false)
	}

	var stopDirective string
	switch loopStopReason {
	case "repeticao":
		stopDirective = "Nenhuma ação nova foi executada (comandos repetidos, bloqueados ou cancelados). NÃO envie novas ferramentas."
	default:
		stopDirective = "O limite de passos foi atingido. NÃO envie novas ferramentas ou comandos."
	}
	finalPrompt := currentContext + "\n[Sistema]: " + stopDirective + " Apresente agora o relatório final consolidado com Diagnóstico (detalhando o que foi feito e por quê), Ações Realizadas, Confirmação e Dicas e Próximos Passos. Seja enxuto: liste cada comando executado apenas UMA única vez (não repita os comandos dos passos anteriores) e não relate o seu raciocínio interno, apenas o resultado final:"
	ch := make(chan string, 10)
	var finalReport strings.Builder
	var finalErr error

	go func() {
		defer close(ch)
		finalErr = prov.AskStream(ctx, sysPrompt, finalPrompt, ch)
	}()

	for chunk := range ch {
		finalReport.WriteString(chunk)
	}

	cleaned := DeduplicateReportCommands(CleanResponseFinal(finalReport.String()))
	var bannerTitle string
	if loopStopReason == "repeticao" {
		bannerTitle = "◼️ **EXECUÇÃO ENCERRADA — NENHUMA AÇÃO NOVA** (Relatório Consolidado)"
	} else {
		bannerTitle = fmt.Sprintf("⚠️ **LIMITE DE %d PASSOS ATINGIDO** (Ações Consolidadas)", maxSteps)
	}
	banner := fmt.Sprintf("> ╔══════════════════════════════════════════════════════════════════╗\n> ║  %-66s║\n> ╚══════════════════════════════════════════════════════════════════╝\n\n", bannerTitle)
	if onProgress != nil {
		onProgress(maxSteps, maxSteps, "", "", "Relatório consolidado finalizado.", true)
	}
	if finalErr != nil && cleaned == "" {
		return banner + fmt.Sprintf("*(Não foi possível obter o relatório consolidado final da IA: %v)*", finalErr), finalErr
	}
	return banner + cleaned, nil
}

// SanitizeAutoCommand garante que comandos enviados no modo autônomo nunca travem esperando entrada interativa ou pagers
func SanitizeAutoCommand(cmd string) string {
	clean := strings.TrimSpace(cmd)
	// Evita que comandos do systemd ou journalctl abram pager interativo (less) no terminal Kitty
	if strings.Contains(clean, "systemctl") && !strings.Contains(clean, "--no-pager") && !strings.Contains(clean, "SYSTEMD_PAGER") {
		clean = "SYSTEMD_PAGER=cat " + clean
	}
	if strings.Contains(clean, "journalctl") && !strings.Contains(clean, "--no-pager") && !strings.Contains(clean, "SYSTEMD_PAGER") {
		clean = "SYSTEMD_PAGER=cat " + clean
	}
	if strings.Contains(clean, "git ") && !strings.Contains(clean, "--no-pager") && !strings.Contains(clean, "PAGER") {
		clean = "PAGER=cat " + clean
	}
	// Garante que ping não rode infinitamente
	if strings.HasPrefix(clean, "ping ") && !strings.Contains(clean, "-c") {
		clean = strings.Replace(clean, "ping ", "ping -c 3 ", 1)
	}
	return clean
}

// CleanResponseFinal remove tags residuais de ferramentas da resposta final
func CleanResponseFinal(raw string) string {
	res := toolCallXmlRegex.ReplaceAllString(raw, "")
	res = cleanTagsRegex.ReplaceAllString(res, "")
	return strings.TrimSpace(res)
}

// DeduplicateReportCommands remove blocos de código identicos repetidos no
// relatorio final (ex.: a IA listando o mesmo comando 2x em "Ações e Comandos
// Executados"). Mantem apenas a primeira ocorrencia, removendo tambem o titulo
// "**N.** ..." acima e a citacao "> Resultado" abaixo, quando associados a ele.
func DeduplicateReportCommands(text string) string {
	if text == "" {
		return text
	}
	seen := make(map[string]bool)
	matches := codeBlockRegex.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return text
	}

	var sb strings.Builder
	cursor := 0
	for _, m := range matches {
		fullStart, fullEnd := m[0], m[1]
		contentStart, contentEnd := m[2], m[3]
		block := strings.TrimSpace(text[contentStart:contentEnd])
		firstLine := strings.TrimSpace(strings.SplitN(block, "\n", 2)[0])

		if seen[firstLine] {
			// Remove a linha de titulo "**N. Objetivo**" acima do bloco, subindo
			// por cima de eventuais linhas em branco entre o titulo e o bloco.
			cut := fullStart
			before := text[:fullStart]
			i := len(before)
			for i > 0 {
				j := strings.LastIndex(before[:i], "\n")
				line := strings.TrimSpace(before[j+1 : i])
				if line == "" {
					i = j
					continue
				}
				if strings.HasPrefix(line, "**") {
					cut = j + 1
				}
				break
			}
			sb.WriteString(text[cursor:cut])

			// Pula o bloco duplicado e remove a citacao "> Resultado..." logo abaixo.
			cursor = fullEnd
			after := text[cursor:]
			trimmed := strings.TrimLeft(after, " \n\t")
			leading := len(after) - len(trimmed)
			if strings.HasPrefix(trimmed, ">") {
				cursor += leading
				if j := strings.IndexByte(trimmed, '\n'); j != -1 {
					cursor += j + 1
				} else {
					cursor = len(text)
				}
				for cursor < len(text) && text[cursor] == '\n' {
					cursor++
				}
			}
			continue
		}

		seen[firstLine] = true
		sb.WriteString(text[cursor:fullEnd])
		cursor = fullEnd
	}

	if cursor < len(text) {
		sb.WriteString(text[cursor:])
	}
	return strings.TrimSpace(sb.String())
}

// DefaultTTYConfirm solicita confirmação no terminal via /dev/tty caso não haja UI gráfica
func DefaultTTYConfirm(cmd, reason string) bool {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer tty.Close()

	fmt.Fprintf(tty, "\n\033[1;33m⚠️  Confirmação [%s]\033[0m\n", reason)
	fmt.Fprintf(tty, "  \033[1;37m$\033[0m \033[1;38;5;214m%s\033[0m\n", cmd)
	fmt.Fprintf(tty, "\033[1;33mPermitir execução no terminal? (s/N): \033[0m")

	var ans string
	_, _ = fmt.Fscanln(tty, &ans)
	ans = strings.ToLower(strings.TrimSpace(ans))
	return ans == "s" || ans == "sim" || ans == "y" || ans == "yes"
}

// AutoAuditLog registra cada decisão do modo /auto (BLOQUEADO, CONFIRMAR, NEGADO,
// EXECUTADO, FALHA) em arquivo de auditoria, espelhando o _auto_log do agente zsh.
// Diretório configurável via AI_AUTO_LOG_DIR; padrão: ~/.cache/metis/logs.
func AutoAuditLog(msg string) {
	dir := os.Getenv("AI_AUTO_LOG_DIR")
	if dir == "" {
		dir = filepath.Join(config.GetUserHome(), ".cache", "metis", "logs")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	line := fmt.Sprintf("%s [%s] %s\n",
		time.Now().Format("2006-01-02 15:04:05"),
		"auto", msg)
	f, err := os.OpenFile(filepath.Join(dir, "auto.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

func firstLineOf(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx != -1 {
		return s[:idx]
	}
	return s
}

// SystemPrompt retorna as instruções do assistente e catálogo de ferramentas
func SystemPrompt(autoMode bool) string {
	base := `Você é um assistente de terminal e agente Linux especialista.
Seu objetivo é analisar saídas de terminal, erros e comandos de forma rápida, didática e visualmente impecável.

DIRETRIZES DE FORMATAÇÃO E APRESENTAÇÃO (RIGOROSO):
1. SEM ENROLAÇÃO OU PREÂMBULOS:
   - NUNCA comece com saudações, desculpas, discursos moralistas/legais longos ou frases como "Entendo o cenário...", "Com certeza posso te ajudar...", "Dito isso...".
   - Inicie IMEDIATAMENTE na primeira linha com "### 🎯 Diagnóstico".

0. IDIOMA (RIGOROSO):
   - Responda SEMPRE em português brasileiro, mesmo que o terminal, erros, saídas e documentação estejam em inglês.
   - Nomes de comandos, flags, variáveis, caminhos, pacotes e mensagens técnicas citadas permanecem em inglês; apenas o texto explicativo deve ser em português.
   - NUNCA escreva explicações em inglês e NUNCA misture os dois idiomas na mesma frase.
   - NÃO inclua na resposta o seu raciocínio interno (chain-of-thought), pensamentos em voz alta ou hipóteses descartadas: entregue apenas o diagnóstico e a solução finais.
   - NÃO se repita: cada sintoma, comando e explicação deve aparecer UMA única vez na resposta.

2. DIVISÃO EM 3 SEÇÕES ESTRUTURADAS EM CARDS:
   Sua resposta final deve conter exatamente estas 3 seções estruturadas:

### 🎯 Diagnóstico
(Explicação técnica direta e clara em 3 a 5 linhas sobre a causa raiz do erro ou o que está acontecendo na tela. Detalhe o que foi feito e por quê, de forma didática. Se houver mais de uma causa provável, liste com tópicos simples com hífen "-" sem aninhamentos complexos.)

### 🛠️ Solução Recomendada
(Para CADA comando ou ação necessária, use SEMPRE o formato de CARD limpo):

**1. Título do objetivo da ação**
` + "```bash\ncomando_executavel\n```" + `
> Explicação concisa (1 a 2 linhas) do que o comando faz e o que observar.

**2. Próximo passo (se houver)**
` + "```bash\noutro_comando\n```" + `
> Explicação concisa do próximo passo.

### ✅ Confirmação
(Confirme claramente se o problema foi resolvido, resuma as ações recomendadas e o impacto esperado no terminal.)

### 💡 Dicas e Próximos Passos
- **Validação:** Como verificar se funcionou ou testar o resultado.
- **Prevenção / Alternativa:** O que checar se o erro persistir (máximo 2 a 3 tópicos diretos).

3. REGRAS OBRIGATÓRIAS DE COMANDOS:
   - TODO comando sugerido DEVE estar isolado dentro de seu próprio bloco de código Markdown com crases triplas (` + "```bash ... ```" + `).
   - NUNCA coloque comandos soltos no meio de parágrafos ou apenas indentados com espaços.
   - Os comandos dentro dos blocos devem ser limpos e executáveis. NÃO coloque comentários longos no final da linha (# ...) que estourem a largura do terminal. A explicação deve ficar SEMPRE na citação (> ...) logo abaixo do bloco.
   - Se quiser adicionar um comentário inline no código, use no MÁXIMO 3 palavras (ex: ping -c 4 1.1.1.1 # testa conectividade).

4. ZERO POLUIÇÃO VISUAL:
   - NUNCA use tabelas Markdown (| coluna |), pois quebram e poluem telas estreitas de terminal.
   - Não sobrecarregue o texto com excesso de emojis repetitivos em cada sub-item. Use apenas os ícones dos títulos principais.
   - Mantenha espaçamento limpo entre seções e cards para uma leitura visualmente agradável e organizada.

5. TERMINAL LIMPO / SEM ERROS:
   - Se o terminal do usuário estiver limpo ou apenas com o prompt inicial (sem nenhum comando ou erro executado recentemente), informe com precisão em '### 🎯 Diagnóstico':
     "Terminal limpo e pronto para uso. Nenhum comando recente ou erro foi detectado nesta janela."
   - Em '### 💡 Dicas e Próximos Passos', oriente o usuário a rodar o comando que deseja diagnosticar ou a enviar dúvidas técnicas no chat. NUNCA invente comandos ou problemas que não aconteceram.`

	if !autoMode {
		return base
	}

	return base + `

FERRAMENTAS DISPONÍVEIS (MODO AGENTE AUTÔNOMO /auto):
1. Executar comandos bash no terminal:
<tool_call name="bash">
comando
</tool_call>
2. Ler o conteúdo de um arquivo:
<tool_call name="read_file" path="/caminho/do/arquivo">
</tool_call>
3. Criar ou sobrescrever um arquivo completo:
<tool_call name="write_file" path="/caminho/do/arquivo">
conteúdo completo
</tool_call>

Regras do modo autônomo:
1. Sempre inspecione o problema usando ferramentas antes de concluir.
2. Emita apenas uma ou duas ações por etapa para analisar os resultados.
3. Quando o objetivo estiver totalmente concluído, apresente a conclusão final no formato estruturado (Diagnóstico, Ações, Confirmação e Dicas e Próximos Passos) SEM usar nenhuma tag de ferramenta.`
}

// ParseFlagsAndQuery extrai opções como -p N, -n N, -N, /s de perguntas e comandos
func ParseFlagsAndQuery(input string, defaultLines int) (lines int, steps int, cleanQuery string, hasExplicitLines bool) {
	if defaultLines <= 0 {
		defaultLines = 30
	}
	lines = defaultLines
	words := strings.Fields(input)
	var remaining []string

	for i := 0; i < len(words); i++ {
		w := words[i]
		switch {
		// Suporte para -p, -steps, --steps, --passos
		case w == "-p" || w == "--passos" || w == "--steps" || w == "-steps":
			if i+1 < len(words) {
				if s, err := strconv.Atoi(words[i+1]); err == nil && s > 0 {
					steps = s
					i++
					continue
				}
			}
		// Suporte para -p=10, -steps=10, --steps=10
		case strings.HasPrefix(w, "-p=") || strings.HasPrefix(w, "-steps=") || strings.HasPrefix(w, "--steps="):
			parts := strings.SplitN(w, "=", 2)
			if len(parts) == 2 {
				if s, err := strconv.Atoi(parts[1]); err == nil && s > 0 {
					steps = s
					continue
				}
			}
		case strings.HasPrefix(w, "-p") && len(w) > 2 && kitty.IsDigits(w[2:]):
			if s, err := strconv.Atoi(w[2:]); err == nil && s > 0 {
				steps = s
				continue
			}

		// Suporte para atalho com sinal de mais para passos, ex: +3, +4, +5
		case strings.HasPrefix(w, "+") && len(w) > 1 && kitty.IsDigits(w[1:]):
			if s, err := strconv.Atoi(w[1:]); err == nil && s > 0 {
				steps = s
				continue
			}

		// Suporte para -n, -lines, --lines
		case w == "-n" || w == "--lines" || w == "-lines":
			if i+1 < len(words) {
				if l, err := strconv.Atoi(words[i+1]); err == nil && l > 0 {
					lines = l
					hasExplicitLines = true
					i++
					continue
				}
			}
		// Suporte para -n=10, -lines=10, --lines=10
		case strings.HasPrefix(w, "-n=") || strings.HasPrefix(w, "-lines=") || strings.HasPrefix(w, "--lines="):
			parts := strings.SplitN(w, "=", 2)
			if len(parts) == 2 {
				if l, err := strconv.Atoi(parts[1]); err == nil && l > 0 {
					lines = l
					hasExplicitLines = true
					continue
				}
			}
		case strings.HasPrefix(w, "-n") && len(w) > 2 && kitty.IsDigits(w[2:]):
			if l, err := strconv.Atoi(w[2:]); err == nil && l > 0 {
				lines = l
				hasExplicitLines = true
				continue
			}

		// Suporte para atalho numérico direto como -3, -5, -30, -50
		case strings.HasPrefix(w, "-") && len(w) > 1 && kitty.IsDigits(w[1:]):
			if l, err := strconv.Atoi(w[1:]); err == nil && l > 0 {
				lines = l
				hasExplicitLines = true
				continue
			}

		// Suporte para /s ou /sync com argumento numérico
		case w == "/s" || w == "/sync":
			if i+1 < len(words) {
				arg := words[i+1]
				if strings.HasPrefix(arg, "-") && kitty.IsDigits(arg[1:]) {
					if l, err := strconv.Atoi(arg[1:]); err == nil && l > 0 {
						lines = l
						hasExplicitLines = true
						i++
						remaining = append(remaining, w)
						continue
					}
				} else if kitty.IsDigits(arg) {
					if l, err := strconv.Atoi(arg); err == nil && l > 0 {
						lines = l
						hasExplicitLines = true
						i++
						remaining = append(remaining, w)
						continue
					}
				}
			}
			remaining = append(remaining, w)
		default:
			remaining = append(remaining, w)
		}
	}

	cleanQuery = strings.TrimSpace(strings.Join(remaining, " "))
	return lines, steps, cleanQuery, hasExplicitLines
}
