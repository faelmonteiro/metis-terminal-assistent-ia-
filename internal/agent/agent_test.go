package agent

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestParseToolCallsLegacy(t *testing.T) {
	text := `Vou executar o comando para você:
<bash>ls -la /tmp</bash>
E também ler um arquivo:
<read_file>/etc/hosts</read_file>
E criar um arquivo:
<write_file path="/tmp/test.txt">conteudo de teste</write_file>
`
	calls := ParseToolCalls(text)
	if len(calls) != 3 {
		t.Fatalf("Esperado 3 tool calls, obtido %d", len(calls))
	}

	if calls[0].Type != "bash" || calls[0].Command != "ls -la /tmp" {
		t.Errorf("Tool 0 inesperada: %+v", calls[0])
	}
	if calls[1].Type != "read_file" || calls[1].Path != "/etc/hosts" {
		t.Errorf("Tool 1 inesperada: %+v", calls[1])
	}
	if calls[2].Type != "write_file" || calls[2].Path != "/tmp/test.txt" || calls[2].Content != "conteudo de teste" {
		t.Errorf("Tool 2 inesperada: %+v", calls[2])
	}
}

func TestParseToolCallsModernXml(t *testing.T) {
	text := `Vou verificar os processos:
<tool_call name="bash">
<![CDATA[
ps aux | grep nginx
]]>
</tool_call>
E ler a configuração:
<tool_call name="read_file" path="/etc/nginx/nginx.conf">
</tool_call>
`
	calls := ParseToolCalls(text)
	if len(calls) != 2 {
		t.Fatalf("Esperado 2 tool calls modernos, obtido %d", len(calls))
	}

	if calls[0].Type != "bash" || calls[0].Command != "ps aux | grep nginx" {
		t.Errorf("Tool 0 inesperada: %+v", calls[0])
	}
	if calls[1].Type != "read_file" || calls[1].Path != "/etc/nginx/nginx.conf" {
		t.Errorf("Tool 1 inesperada: %+v", calls[1])
	}
}

func TestExtractAllCodeBlocks(t *testing.T) {
	text := "Aqui estão as soluções:\n```bash\ngit status\n```\nE depois:\n```bash\ngit pull origin main # atualiza\n```\n"
	blocks := ExtractAllCodeBlocks(text)
	if len(blocks) != 2 {
		t.Fatalf("Esperado 2 blocos de código, obtido %d", len(blocks))
	}
	if blocks[0] != "git status" {
		t.Errorf("Bloco 0 inesperado: %q", blocks[0])
	}
	if blocks[1] != "git pull origin main" {
		t.Errorf("Bloco 1 inesperado: %q", blocks[1])
	}
}

func TestLimitContextTurns(t *testing.T) {
	ctx := "[Sistema]: Instruções gerais.\n"
	for i := 1; i <= 6; i++ {
		ctx += "[Assistente]: resposta " + string(rune('0'+i)) + "\n[Usuário]: pergunta " + string(rune('0'+i)) + "\n"
	}

	trimmed := LimitContextTurns(ctx, 3)
	if !strings.HasPrefix(trimmed, "[Sistema]: Instruções gerais.") {
		t.Errorf("Header foi perdido no LimitContextTurns")
	}

	// Não deve ter a resposta 1
	if strings.Contains(trimmed, "resposta 1") {
		t.Errorf("Resposta 1 antiga deveria ter sido aparada")
	}
	// Deve conter as últimas
	if !strings.Contains(trimmed, "resposta 6") {
		t.Errorf("Resposta 6 recente deveria estar presente")
	}
}

func TestExecuteToolAutoChmod(t *testing.T) {
	tmpDir := t.TempDir()
	shPath := tmpDir + "/script.sh"

	call := ToolCall{
		Type:    "write_file",
		Path:    shPath,
		Content: "#!/bin/sh\necho ok",
	}

	res := ExecuteTool(context.Background(), call)
	if res.Err != nil {
		t.Fatalf("Erro ao executar write_file: %v", res.Err)
	}

	fi, err := os.Stat(shPath)
	if err != nil {
		t.Fatalf("Erro ao checar permissão: %v", err)
	}

	if fi.Mode()&0111 == 0 {
		t.Errorf("Esperado arquivo executável (+x), obtido modo %v", fi.Mode())
	}
}

func TestExecuteToolListDir(t *testing.T) {
	tmpDir := t.TempDir()
	call := ToolCall{
		Type: "list_dir",
		Path: tmpDir,
	}

	res := ExecuteTool(context.Background(), call)
	if res.Err != nil {
		t.Fatalf("Erro ao executar list_dir: %v", res.Err)
	}
	if len(res.Output) == 0 {
		t.Errorf("Esperado saída para list_dir, obtido vazio")
	}
}

func TestParseFlagsAndQuery(t *testing.T) {
	tests := []struct {
		input            string
		defaultLines     int
		expectedLines    int
		expectedSteps    int
		expectedQuery    string
		expectedExplicit bool
	}{
		{
			input:            "/auto corrija o erro de build",
			defaultLines:     30,
			expectedLines:    30,
			expectedSteps:    0,
			expectedQuery:    "/auto corrija o erro de build",
			expectedExplicit: false,
		},
		{
			input:            "/auto -p 10 resolva o erro",
			defaultLines:     30,
			expectedLines:    30,
			expectedSteps:    10,
			expectedQuery:    "/auto resolva o erro",
			expectedExplicit: false,
		},
		{
			input:            "/auto -p 10 -3 resolva o erro",
			defaultLines:     30,
			expectedLines:    3,
			expectedSteps:    10,
			expectedQuery:    "/auto resolva o erro",
			expectedExplicit: true,
		},
		{
			input:            "-3",
			defaultLines:     30,
			expectedLines:    3,
			expectedSteps:    0,
			expectedQuery:    "",
			expectedExplicit: true,
		},
		{
			input:            "-3 o que significa esse erro?",
			defaultLines:     30,
			expectedLines:    3,
			expectedSteps:    0,
			expectedQuery:    "o que significa esse erro?",
			expectedExplicit: true,
		},
		{
			input:            "/s -5",
			defaultLines:     30,
			expectedLines:    5,
			expectedSteps:    0,
			expectedQuery:    "/s",
			expectedExplicit: true,
		},
		{
			input:            "-steps=8 -lines=12 analise",
			defaultLines:     30,
			expectedLines:    12,
			expectedSteps:    8,
			expectedQuery:    "analise",
			expectedExplicit: true,
		},
		{
			input:            "-p4",
			defaultLines:     30,
			expectedLines:    30,
			expectedSteps:    4,
			expectedQuery:    "",
			expectedExplicit: false,
		},
		{
			input:            "+3",
			defaultLines:     30,
			expectedLines:    30,
			expectedSteps:    3,
			expectedQuery:    "",
			expectedExplicit: false,
		},
		{
			input:            "/auto -p4 verificar rede",
			defaultLines:     30,
			expectedLines:    30,
			expectedSteps:    4,
			expectedQuery:    "/auto verificar rede",
			expectedExplicit: false,
		},
	}

	for _, tt := range tests {
		lines, steps, query, explicit := ParseFlagsAndQuery(tt.input, tt.defaultLines)
		if lines != tt.expectedLines {
			t.Errorf("input %q: esperado lines %d, obtido %d", tt.input, tt.expectedLines, lines)
		}
		if steps != tt.expectedSteps {
			t.Errorf("input %q: esperado steps %d, obtido %d", tt.input, tt.expectedSteps, steps)
		}
		if query != tt.expectedQuery {
			t.Errorf("input %q: esperado query %q, obtido %q", tt.input, tt.expectedQuery, query)
		}
		if explicit != tt.expectedExplicit {
			t.Errorf("input %q: esperado explicit %v, obtido %v", tt.input, tt.expectedExplicit, explicit)
		}
	}
}


func TestDeduplicateReportCommands(t *testing.T) {
	in := `### 🎯 Diagnóstico
Rede ok.

### ⚡ Ações e Comandos Executados
**1. Interfaces de rede**
` + "```bash\nip -br addr\n```" + `
> **Resultado:** endereços listados.

**2. Interfaces de rede (repetido)**
` + "```bash\nip -br addr\n```" + `
> **Resultado:** endereços listados novamente.

**3. Conectividade**
` + "```bash\nping -c 3 8.8.8.8\n```" + `
> **Resultado:** respondeu (3/3).

### 🏁 Conclusão
OK.`

	out := DeduplicateReportCommands(in)
	if strings.Count(out, "ip -br addr") != 1 {
		t.Errorf("Comando 'ip -br addr' deveria aparecer 1x, mas apareceu %d vezes:\n%s", strings.Count(out, "ip -br addr"), out)
	}
	if strings.Count(out, "ping -c 3 8.8.8.8") != 1 {
		t.Errorf("Comando 'ping -c 3 8.8.8.8' deveria aparecer 1x, mas apareceu %d vezes", strings.Count(out, "ping -c 3 8.8.8.8"))
	}
	if strings.Contains(out, "repetido") {
		t.Errorf("Título do bloco duplicado deveria ter sido removido:\n%s", out)
	}
	if !strings.HasPrefix(out, "### 🎯 Diagnóstico") {
		t.Errorf("Cabeçalho do diagnóstico foi perdido:\n%s", out)
	}
}

func TestDeduplicateReportCommandsNoChange(t *testing.T) {
	in := "Texto sem blocos de código.\n" + "```bash\nls -la\n```" + "\n> ok"
	out := DeduplicateReportCommands(in)
	if out != strings.TrimSpace(in) {
		t.Errorf("Sem duplicatas o texto deveria ficar inalterado:\n%q", out)
	}
}
