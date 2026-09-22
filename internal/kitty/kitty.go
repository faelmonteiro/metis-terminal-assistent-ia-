package kitty

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ansiEscapeRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[()][A-Za-z0-9]|\r`)
)

type KittyManager struct {
	Socket   string
	WindowID string
	PID      string
}

// IsSocketAlive verifica em tempo real se o socket do Kitty responde.
func IsSocketAlive(sock string) bool {
	if strings.TrimSpace(sock) == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, "kitty", "@", "--to", sock, "ls")
	return cmd.Run() == nil
}

func Detect() *KittyManager {
	km := &KittyManager{
		Socket:   os.Getenv("KITTY_LISTEN_ON"),
		WindowID: os.Getenv("KITTY_WINDOW_ID"),
		PID:      os.Getenv("KITTY_PID"),
	}

	// 1. Tenta socket do ambiente se estiver vivo
	if km.Socket != "" && !IsSocketAlive(km.Socket) {
		km.Socket = ""
	}

	// 2. Tenta socket salvo em arquivo temporário pelo lançador ZSH
	if km.Socket == "" {
		candidates := []string{
			fmt.Sprintf("/tmp/orig_kitty_listen.%d", os.Getuid()),
			"/tmp/orig_kitty_listen",
		}
		for _, c := range candidates {
			s := readTrimmedFile(c)
			if s != "" && IsSocketAlive(s) {
				km.Socket = s
				break
			}
		}
	}

	// 3. Tenta socket via Hyprland activewindow
	if km.Socket == "" {
		if pid := getHyprlandActivePID(); pid != "" {
			sockPath := fmt.Sprintf("/tmp/mykitty-%s", pid)
			sockCandidate := "unix:" + sockPath
			if isSocket(sockPath) && IsSocketAlive(sockCandidate) {
				km.Socket = sockCandidate
				km.PID = pid
			}
		}
	}

	// 4. Fallback: socket do Kitty mais recente modificado em /tmp que responda
	if km.Socket == "" {
		if sockPath := findMostRecentLiveKittySocket(); sockPath != "" {
			km.Socket = "unix:" + sockPath
			parts := strings.Split(filepath.Base(sockPath), "-")
			if len(parts) > 1 {
				km.PID = parts[len(parts)-1]
			}
		}
	}

	// 5. Janela original de trabalho
	if km.WindowID == "" {
		candidates := []string{
			fmt.Sprintf("/tmp/orig_kitty_id.%d", os.Getuid()),
			"/tmp/orig_kitty_id",
		}
		for _, c := range candidates {
			w := readTrimmedFile(c)
			if w != "" {
				km.WindowID = w
				break
			}
		}
	}
	if km.PID == "" {
		km.PID = readTrimmedFile(fmt.Sprintf("/tmp/orig_kitty_pid.%d", os.Getuid()))
		if km.PID == "" {
			km.PID = readTrimmedFile("/tmp/orig_kitty_pid")
		}
	}

	// Se km.PID ainda estiver vazio mas tivermos km.Socket com unix:/tmp/mykitty-<pid>, extrai o PID real
	if km.PID == "" && km.Socket != "" {
		base := filepath.Base(km.Socket)
		if idx := strings.LastIndex(base, "-"); idx != -1 {
			p := base[idx+1:]
			if _, err := strconv.Atoi(p); err == nil {
				km.PID = p
			}
		}
	}

	// 6. Se tiver socket mas não tiver WindowID, descobre a janela ativa
	if km.Socket != "" && km.WindowID == "" {
		km.WindowID = km.getActiveWindowID()
	}

	return km
}

// GetScreenBuffer captura o texto da tela do terminal Kitty com prioridade ao buffer fresco do atalho
func (km *KittyManager) GetScreenBuffer(maxLines int) string {
	var raw string

	// Prioridade 1: Buffer do atalho recém-gravado pelo pipe do Kitty (se <= 3s)
	// Evita roundtrip IPC de 75ms durante a abertura da janela
	tmpCandidates := []string{
		fmt.Sprintf("/tmp/qwen_tela.%d.txt", os.Getuid()),
		"/tmp/qwen_tela.txt",
	}
	for _, f := range tmpCandidates {
		if fi, err := os.Stat(f); err == nil && fi.Size() > 0 && time.Since(fi.ModTime()) <= 3*time.Second {
			if data, err := os.ReadFile(f); err == nil && len(data) > 0 {
				raw = string(data)
				break
			}
		}
	}

	// Prioridade 2: Consulta direta via socket ativo do Kitty (tempo real / atualizações posteriores)
	if strings.TrimSpace(raw) == "" && km.Socket != "" && IsSocketAlive(km.Socket) {
		args := []string{"@", "--to", km.Socket, "get-text", "--extent=screen"}
		if km.WindowID != "" {
			args = append(args, fmt.Sprintf("--match=id:%s", km.WindowID))
		}
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "kitty", args...)
		if out, err := cmd.Output(); err == nil && len(out) > 0 {
			raw = string(out)
		}
	}

	cleaned := CleanScreenContent(raw)
	return TailLines(cleaned, maxLines)
}

// GetSelectionBuffer captura o texto selecionado exclusivamente na janela do terminal de trabalho
func (km *KittyManager) GetSelectionBuffer() string {
	// Prioridade 0: Arquivo gravado pelo atalho no instante do disparo (se <= 3s)
	selCandidates := []string{
		fmt.Sprintf("/tmp/qwen_selecao.%d.txt", os.Getuid()),
		"/tmp/qwen_selecao.txt",
	}
	for _, sf := range selCandidates {
		if fi, err := os.Stat(sf); err == nil && fi.Size() > 0 && time.Since(fi.ModTime()) <= 3*time.Second {
			if data, err := os.ReadFile(sf); err == nil {
				if s := strings.TrimSpace(string(data)); s != "" {
					return s
				}
			}
		}
	}

	// Prioridade 1: Terminal Kitty com socket ativo (isolamento estrito por janela e socket)
	if km.Socket != "" && IsSocketAlive(km.Socket) {
		args := []string{"@", "--to", km.Socket, "get-text", "--extent=selection"}
		if km.WindowID != "" {
			args = append(args, fmt.Sprintf("--match=id:%s", km.WindowID))
		}
		ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		defer cancel()
		cmd := exec.CommandContext(ctx, "kitty", args...)
		if out, err := cmd.Output(); err == nil {
			if s := strings.TrimSpace(string(out)); s != "" {
				return s
			}
		}
		// Se estamos conectados ao Kitty original, NUNCA recorre a wl-paste ou xclip global,
		// pois eles capturariam seleções de outros terminais ou janelas do sistema!
		return ""
	}

	// Prioridade 2: Outros emuladores de terminal (não-Kitty)
	// Se tivermos o PID do terminal de origem, verifica se o foco não foi desviado para outro terminal/app
	if km.PID != "" {
		activePID := getHyprlandActivePID()
		myPID := strconv.Itoa(os.Getpid())
		// Se a janela ativa no Hyprland for conhecida e NÃO for o terminal de origem nem a própria janela do Metis,
		// não captura seleção feita em outro aplicativo.
		if activePID != "" && activePID != km.PID && activePID != myPID {
			return ""
		}
	}

	// 2.1 Wayland wl-paste (apenas para terminais não-Kitty com foco validado)
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		cmd := exec.CommandContext(ctx, "wl-paste", "--primary", "--no-newline")
		if out, err := cmd.Output(); err == nil {
			if s := strings.TrimSpace(string(out)); s != "" {
				return s
			}
		}
	}

	// 2.2 X11 xclip / xsel (apenas para terminais não-Kitty)
	if os.Getenv("DISPLAY") != "" {
		ctxXclip, cancelXclip := context.WithTimeout(context.Background(), 200*time.Millisecond)
		out, err := exec.CommandContext(ctxXclip, "xclip", "-o", "-selection", "primary").Output()
		cancelXclip()
		if err == nil {
			if s := strings.TrimSpace(string(out)); s != "" {
				return s
			}
		}

		ctxXsel, cancelXsel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		outSel, errSel := exec.CommandContext(ctxXsel, "xsel", "-o", "-p").Output()
		cancelXsel()
		if errSel == nil {
			if s := strings.TrimSpace(string(outSel)); s != "" {
				return s
			}
		}
	}

	return ""
}

// ClearSelection limpa seleções ativas do mouse no terminal
func (km *KittyManager) ClearSelection() {
	_ = os.Remove("/tmp/qwen_selecao.txt")

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	if km.Socket != "" && IsSocketAlive(km.Socket) {
		if km.WindowID != "" {
			_ = exec.CommandContext(ctx, "kitty", "@", "--to", km.Socket, "action", fmt.Sprintf("--match=id:%s", km.WindowID), "clear_selection").Run()
			return
		}
		_ = exec.CommandContext(ctx, "kitty", "@", "--to", km.Socket, "action", "clear_selection").Run()
		return
	}

	// Apenas para terminais não-Kitty
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		_ = exec.CommandContext(ctx, "wl-copy", "--clear", "--primary").Run()
	}
	if os.Getenv("DISPLAY") != "" {
		_ = exec.CommandContext(ctx, "xclip", "-i", "/dev/null", "-selection", "primary").Run()
		_ = exec.CommandContext(ctx, "xsel", "-c", "-p").Run()
	}
}

// GetLastCommandStatus obtém o último comando e código de saída ($?) gravados pelo hook ZSH
func (km *KittyManager) GetLastCommandStatus() (cmdName string, exitCode string, cmdTime string) {
	var statusFile string
	var statusFi os.FileInfo

	if km.WindowID != "" {
		f := fmt.Sprintf("/tmp/metis_status_%s", km.WindowID)
		if fi, err := os.Stat(f); err == nil && !fi.IsDir() {
			statusFile = f
			statusFi = fi
		}
	}

	// Só faz fallback para /tmp/metis_last_status se km.WindowID não estiver definido
	if statusFile == "" && km.WindowID == "" {
		f := "/tmp/metis_last_status"
		if fi, err := os.Stat(f); err == nil && !fi.IsDir() {
			statusFile = f
			statusFi = fi
		}
	}

	if statusFile == "" || statusFi == nil {
		return "", "", ""
	}

	// Validação de tempo de vida da sessão do terminal:
	// Se tivermos km.Socket ativo (unix:/tmp/mykitty-<pid>), compara com a data de criação do socket
	if km.Socket != "" {
		sockPath := strings.TrimPrefix(km.Socket, "unix:")
		if sockFi, err := os.Stat(sockPath); err == nil {
			if statusFi.ModTime().Before(sockFi.ModTime().Add(-2 * time.Second)) {
				_ = os.Remove(statusFile)
				return "", "", ""
			}
		}
	}

	// Se tivermos PID do terminal, valida contra o início do processo em /proc/<pid>
	if km.PID != "" {
		if procFi, err := os.Stat("/proc/" + km.PID); err == nil {
			if statusFi.ModTime().Before(procFi.ModTime().Add(-2 * time.Second)) {
				_ = os.Remove(statusFile)
				return "", "", ""
			}
		}
	}

	data, err := os.ReadFile(statusFile)
	if err != nil {
		return "", "", ""
	}

	var winID string
	var cmdTimestamp int64
	lines := strings.Split(string(data), "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "CMD:") {
			cmdName = strings.TrimSpace(strings.TrimPrefix(l, "CMD:"))
		} else if strings.HasPrefix(l, "EXIT_CODE:") {
			exitCode = strings.TrimSpace(strings.TrimPrefix(l, "EXIT_CODE:"))
		} else if strings.HasPrefix(l, "TIME:") {
			cmdTime = strings.TrimSpace(strings.TrimPrefix(l, "TIME:"))
			if t, err := strconv.ParseInt(cmdTime, 10, 64); err == nil {
				cmdTimestamp = t
			}
		} else if strings.HasPrefix(l, "WIN:") {
			winID = strings.TrimSpace(strings.TrimPrefix(l, "WIN:"))
		}
	}

	// Isolamento estrito por janela: se o arquivo pertencer a outra janela, descarta
	if km.WindowID != "" && winID != "" && winID != km.WindowID {
		return "", "", ""
	}

	// Se o comando tiver mais de 1 hora de antiguidade, considera expirado
	if cmdTimestamp > 0 && time.Now().Unix()-cmdTimestamp > 3600 {
		return "", "", ""
	}

	return cmdName, exitCode, cmdTime
}

// SendText envia código limpo para a janela original do Kitty via stdin com limpeza de prompt (Ctrl+U)
func (km *KittyManager) SendText(code string, autoEnter bool) error {
	cleanCode := RemoveComments(code)
	if strings.TrimSpace(cleanCode) == "" {
		return fmt.Errorf("nenhum comando válido para enviar")
	}

	if km.Socket == "" || !IsSocketAlive(km.Socket) {
		// Fallback para área de transferência se Kitty não estiver acessível
		_ = copyClipboard(cleanCode)
		return nil
	}

	matchArgs := []string{}
	if km.WindowID != "" {
		matchArgs = append(matchArgs, fmt.Sprintf("--match=id:%s", km.WindowID))
	}

	const maxRetries = 3
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		// Verifica se o socket ainda está vivo antes de cada tentativa
		if !IsSocketAlive(km.Socket) {
			lastErr = fmt.Errorf("socket do Kitty não está mais acessível")
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// 1. Limpa o prompt ativo na janela do Kitty sem enviar Ctrl+C (\x03),
		// permitindo substituir comandos injetados anteriormente que ainda não foram executados.
		// Sequência: Ctrl+E (vai ao fim) -> Ctrl+U (apaga tudo até o início) -> Ctrl+A -> Ctrl+K (apaga restante)
		clearArgs := append([]string{"@", "--to", km.Socket, "send-text"}, matchArgs...)
		clearArgs = append(clearArgs, "--stdin")
		cmdClear := exec.Command("kitty", clearArgs...)
		cmdClear.Stdin = strings.NewReader("\x05\x15\x01\x0b\x15")
		if err := cmdClear.Run(); err != nil {
			lastErr = fmt.Errorf("falha ao limpar prompt (tentativa %d): %w", attempt+1, err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		time.Sleep(50 * time.Millisecond)

		// 2. Envia o comando via --stdin (evita problemas de flag parsing com comandos tipo -la)
		sendArgs := append([]string{"@", "--to", km.Socket, "send-text"}, matchArgs...)
		sendArgs = append(sendArgs, "--stdin")
		cmdSend := exec.Command("kitty", sendArgs...)
		cmdSend.Stdin = strings.NewReader(cleanCode)
		if err := cmdSend.Run(); err != nil {
			lastErr = fmt.Errorf("falha ao enviar comando (tentativa %d): %w", attempt+1, err)
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// 3. Se autoEnter for solicitado, envia Enter (\r)
		if autoEnter {
			time.Sleep(200 * time.Millisecond)
			enterArgs := append([]string{"@", "--to", km.Socket, "send-text"}, matchArgs...)
			enterArgs = append(enterArgs, "--stdin")
			cmdEnter := exec.Command("kitty", enterArgs...)
			cmdEnter.Stdin = strings.NewReader("\r")
			if err := cmdEnter.Run(); err != nil {
				lastErr = fmt.Errorf("falha ao enviar Enter (tentativa %d): %w", attempt+1, err)
				time.Sleep(100 * time.Millisecond)
				continue
			}
		}

		return nil
	}

	return fmt.Errorf("falha ao enviar comando após %d tentativas: %w", maxRetries, lastErr)
}

// RemoveComments remove comentários inline (# ...) preservando strings delimitadas por aspas
func RemoveComments(input string) string {
	lines := strings.Split(input, "\n")
	var cleanLines []string

	for _, line := range lines {
		inSingle := false
		inDouble := false
		escaped := false
		var sb strings.Builder
		prevRune := ' '

		runes := []rune(line)
		for i := 0; i < len(runes); i++ {
			r := runes[i]

			if escaped {
				sb.WriteRune(r)
				escaped = false
				prevRune = r
				continue
			}

			if r == '\\' {
				escaped = true
				sb.WriteRune(r)
				prevRune = r
				continue
			}

			if r == '\'' && !inDouble {
				inSingle = !inSingle
				sb.WriteRune(r)
				prevRune = r
				continue
			}

			if r == '"' && !inSingle {
				inDouble = !inDouble
				sb.WriteRune(r)
				prevRune = r
				continue
			}

			if r == '#' && !inSingle && !inDouble {
				if prevRune == ' ' || prevRune == '\t' || prevRune == ';' || prevRune == '&' || prevRune == '|' || i == 0 {
					break
				}
			}

			sb.WriteRune(r)
			prevRune = r
		}

		cleanL := strings.TrimRight(sb.String(), " \t")
		if cleanL != "" {
			cleanLines = append(cleanLines, cleanL)
		}
	}

	return strings.Join(cleanLines, "\n")
}

func copyClipboard(text string) error {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		cmd := exec.Command("wl-copy")
		cmd.Stdin = strings.NewReader(text)
		return cmd.Run()
	}
	if os.Getenv("DISPLAY") != "" {
		cmd := exec.Command("xclip", "-selection", "clipboard")
		cmd.Stdin = strings.NewReader(text)
		return cmd.Run()
	}
	return nil
}

// CleanScreenContent remove sequências de escape ANSI e caracteres de controle
func CleanScreenContent(raw string) string {
	cleaned := ansiEscapeRegex.ReplaceAllString(raw, "")
	lines := strings.Split(cleaned, "\n")
	// Remove linhas em branco consecutivas no final
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// TailLines retorna apenas as últimas N linhas do buffer
func TailLines(text string, n int) string {
	if n <= 0 || strings.TrimSpace(text) == "" {
		return text
	}
	lines := strings.Split(text, "\n")
	if len(lines) <= n {
		return text
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

func (km *KittyManager) getActiveWindowID() string {
	out, err := exec.Command("kitty", "@", "--to", km.Socket, "ls").Output()
	if err != nil {
		return "1"
	}

	var data []struct {
		Tabs []struct {
			Windows []struct {
				ID       int  `json:"id"`
				IsActive bool `json:"is_active"`
			} `json:"windows"`
		} `json:"tabs"`
	}

	if err := json.Unmarshal(out, &data); err != nil {
		return "1"
	}

	for _, osWin := range data {
		for _, tab := range osWin.Tabs {
			for _, win := range tab.Windows {
				if win.IsActive {
					return strconv.Itoa(win.ID)
				}
			}
		}
	}
	return "1"
}

func getHyprlandActiveWindowInfo() (pid string, class string) {
	out, err := exec.Command("hyprctl", "activewindow", "-j").Output()
	if err != nil {
		return "", ""
	}
	var res struct {
		PID   int    `json:"pid"`
		Class string `json:"class"`
	}
	if err := json.Unmarshal(out, &res); err == nil {
		var p string
		if res.PID > 0 {
			p = strconv.Itoa(res.PID)
		}
		return p, res.Class
	}
	return "", ""
}

func getHyprlandActivePID() string {
	pid, _ := getHyprlandActiveWindowInfo()
	return pid
}

func findMostRecentLiveKittySocket() string {
	matches, err := filepath.Glob("/tmp/mykitty*")
	if err != nil || len(matches) == 0 {
		return ""
	}

	type sockInfo struct {
		path  string
		mtime int64
	}
	var socks []sockInfo
	for _, m := range matches {
		if isSocket(m) {
			if fi, err := os.Stat(m); err == nil {
				socks = append(socks, sockInfo{path: m, mtime: fi.ModTime().UnixNano()})
			}
		}
	}

	if len(socks) == 0 {
		return ""
	}

	sort.Slice(socks, func(i, j int) bool {
		return socks[i].mtime > socks[j].mtime
	})

	for _, s := range socks {
		if IsSocketAlive("unix:" + s.path) {
			return s.path
		}
	}

	return ""
}

func isSocket(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeSocket != 0
}

func readTrimmedFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// IsDigits verifica se uma string contém apenas dígitos numéricos [0-9]
func IsDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func findParentTerminalProcess() (int, string) {
	ppid := os.Getppid()
	for i := 0; i < 6 && ppid > 1; i++ {
		if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", ppid)); err == nil {
			name := strings.TrimSpace(string(data))
			if isKnownTerminal(name) {
				return ppid, name
			}
		}
		if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", ppid)); err == nil {
			fields := strings.Fields(string(data))
			if len(fields) > 3 {
				if p, err := strconv.Atoi(fields[3]); err == nil && p > 1 {
					ppid = p
					continue
				}
			}
		}
		break
	}
	return 0, ""
}

// DetectTerminalPID descobre dinamicamente o PID real do emulador de terminal em execução
func DetectTerminalPID(km *KittyManager) string {
	if km != nil && km.PID != "" {
		return km.PID
	}
	if p, _ := getHyprlandActiveWindowInfo(); p != "" {
		return p
	}
	if ppid, _ := findParentTerminalProcess(); ppid > 1 {
		return strconv.Itoa(ppid)
	}
	if p := os.Getppid(); p > 1 {
		return strconv.Itoa(p)
	}
	return fmt.Sprintf("%d", os.Getpid())
}

// DetectTerminalName identifica dinamicamente o nome do emulador de terminal em uso
func DetectTerminalName(km *KittyManager) string {
	if km != nil && km.PID != "" {
		if data, err := os.ReadFile(fmt.Sprintf("/proc/%s/comm", km.PID)); err == nil {
			name := strings.TrimSpace(string(data))
			if name != "" && isKnownTerminal(name) {
				return FormatTerminalName(name)
			}
		}
	}
	if term := os.Getenv("TERM_PROGRAM"); term != "" {
		return FormatTerminalName(term)
	}
	if os.Getenv("KITTY_PID") != "" || os.Getenv("KITTY_WINDOW_ID") != "" || (km != nil && km.Socket != "") {
		return "Kitty"
	}
	if os.Getenv("ALACRITTY_WINDOW_ID") != "" || os.Getenv("ALACRITTY_LOG") != "" {
		return "Alacritty"
	}
	if os.Getenv("WEZTERM_PANE") != "" {
		return "WezTerm"
	}
	if os.Getenv("FOOT_SERVER_PATH") != "" {
		return "Foot"
	}
	if os.Getenv("GHOSTTY_RESOURCES_DIR") != "" {
		return "Ghostty"
	}

	if _, name := findParentTerminalProcess(); name != "" {
		return FormatTerminalName(name)
	}

	if _, class := getHyprlandActiveWindowInfo(); class != "" {
		if isKnownTerminal(class) {
			return FormatTerminalName(class)
		}
	}

	if km != nil && km.Socket != "" {
		return "Kitty"
	}
	return "Terminal"
}

func isKnownTerminal(comm string) bool {
	lower := strings.ToLower(comm)
	terminals := []string{
		"kitty", "alacritty", "wezterm", "foot", "ghostty", "konsole",
		"gnome-terminal", "ptyxis", "xterm", "tilix", "st", "terminator",
		"urxvt", "rxvt", "xfce4-terminal", "mate-terminal", "lxterminal",
		"tabby", "hyper", "rio", "contour", "blackbox", "terminology",
	}
	for _, t := range terminals {
		if strings.Contains(lower, t) {
			return true
		}
	}
	return false
}

// FormatTerminalName formata o nome comum do terminal com maiúsculas corretas
func FormatTerminalName(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "kitty"):
		return "Kitty"
	case strings.Contains(lower, "alacritty"):
		return "Alacritty"
	case strings.Contains(lower, "wezterm"):
		return "WezTerm"
	case strings.Contains(lower, "ghostty"):
		return "Ghostty"
	case strings.Contains(lower, "foot"):
		return "Foot"
	case strings.Contains(lower, "gnome-terminal"):
		return "GNOME Terminal"
	case strings.Contains(lower, "ptyxis"):
		return "Ptyxis"
	case strings.Contains(lower, "konsole"):
		return "Konsole"
	case strings.Contains(lower, "xfce4-terminal"):
		return "XFCE Terminal"
	case strings.Contains(lower, "mate-terminal"):
		return "MATE Terminal"
	case strings.Contains(lower, "terminator"):
		return "Terminator"
	case strings.Contains(lower, "tilix"):
		return "Tilix"
	case strings.Contains(lower, "xterm"):
		return "XTerm"
	case strings.Contains(lower, "urxvt") || strings.Contains(lower, "rxvt"):
		return "URxvt"
	case strings.Contains(lower, "tabby"):
		return "Tabby"
	case strings.Contains(lower, "hyper"):
		return "Hyper"
	case strings.Contains(lower, "rio"):
		return "Rio"
	case strings.Contains(lower, "blackbox"):
		return "Blackbox"
	case strings.Contains(lower, "terminology"):
		return "Terminology"
	case strings.Contains(lower, "tmux"):
		return "Tmux"
	default:
		clean := strings.TrimSpace(name)
		if len(clean) > 0 {
			return strings.ToUpper(clean[:1]) + clean[1:]
		}
		return "Terminal"
	}
}
