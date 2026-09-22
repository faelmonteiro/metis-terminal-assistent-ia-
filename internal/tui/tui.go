package tui

import (
	"context"
	"fmt"
	"os"
	osUser "os/user"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"metis-screen/internal/agent"
	"metis-screen/internal/ai"
	"metis-screen/internal/config"
	"metis-screen/internal/kitty"
	"metis-screen/internal/security"
)

type ViewMode int

const (
	ViewDashboard ViewMode = iota
	ViewChat
	ViewModelSelect
)

type streamChunkMsg string
type streamDoneMsg error
type autoStepDoneMsg struct {
	response string
	hasTools bool
	tools    []agent.ToolCall
}

type MenuItem struct {
	KeyBadge    string
	Title       string
	Icon        string
	CauseHeader string
	CauseDesc   string
	Snippet     string
	ButtonLabel string
}

type Model struct {
	cfg          *config.Config
	km           *kitty.KittyManager
	provider     ai.Provider
	mode         ViewMode
	screenLines  int
	screenText   string
	selection    string
	lastCode     string
	lastQuery           string
	conversationHistory string
	viewport            viewport.Model
	textInput    textinput.Model
	spinner      spinner.Model
	isLoading    bool
	statusMsg    string
	menuIndex    int
	menuActions  []MenuItem
	provIndex    int
	provItems    []string
	autoMode     bool
	autoSteps    int
	fullResponse strings.Builder
	renderer     *glamour.TermRenderer
	width        int
	height       int
	cancelFunc   context.CancelFunc
}

func createMenuActions(prov ai.Provider, lines int, sel, screen string) []MenuItem {
	snippet1 := "// Sugestão de correção automática\nfunc ApplyFix() { ... }"
	if sel != "" {
		snippet1 = truncateLines(sel, 2)
	} else if screen != "" {
		snippet1 = truncateLines(screen, 2)
	}

	return []MenuItem{
		{
			KeyBadge:    "[F1] ",
			Title:       "Analyze ",
			Icon:        "✨",
			CauseHeader: "Probable Cause",
			CauseDesc:   fmt.Sprintf("Diagnóstico profundo de erros e traceback via %s API (%s).", prov.Name(), prov.Model()),
			Snippet:     snippet1,
			ButtonLabel: "Apply Fix [Enter]",
		},
		{
			KeyBadge:    "[F2] ",
			Title:       "Explain ",
			Icon:        "💬",
			CauseHeader: "Explicação Técnica",
			CauseDesc:   "Explica detalhadamente os comandos recentes, arquitetura e possíveis correções.",
			Snippet:     "// Análise didática passo a passo do contexto",
			ButtonLabel: "Explicar [Enter]",
		},
		{
			KeyBadge:    "[F3] ",
			Title:       "Config ",
			Icon:        "⚙️",
			CauseHeader: "Configurações do Sistema",
			CauseDesc:   fmt.Sprintf("Linhas de buffer: %d | Passos autônomos: 6 | Socket Kitty ativo.", lines),
			Snippet:     "DEFAULT_SCREEN_LINES=30\nAI_AUTO_MAX_STEPS=6",
			ButtonLabel: "Configurações [Enter]",
		},
		{
			KeyBadge:    "[F4] ",
			Title:       "AI Provedor ",
			Icon:        "🤖",
			CauseHeader: "Gerenciador de Modelos",
			CauseDesc:   "Alterna entre Groq, Gemini, Ollama Local, OpenRouter, NVIDIA e OpenAI.",
			Snippet:     fmt.Sprintf("Provedor Atual: %s\nModelo Ativo: %s", prov.Name(), prov.Model()),
			ButtonLabel: "Selecionar Provedor [Enter]",
		},
	}
}

func NewModel(cfg *config.Config, km *kitty.KittyManager, initialQuery string, cliLines int) Model {
	ti := textinput.New()
	ti.Placeholder = "Digite uma pergunta ou instrução..."
	ti.Focus()
	ti.CharLimit = 1000
	ti.Prompt = "❯ "
	ti.PromptStyle = lipgloss.NewStyle().Foreground(colorTerminalPrompt).Bold(true)
	ti.TextStyle = lipgloss.NewStyle().Foreground(colorTerminalText)
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(colorInactiveText)

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colorTerminalText)

	lines := cfg.DefaultScreenLines
	if cliLines > 0 {
		lines = cliLines
	}

	screenBuf := km.GetScreenBuffer(lines)
	selBuf := km.GetSelectionBuffer()

	prov := ai.NewProvider(cfg, cfg.Provider)

	vp := viewport.New(80, 20)

	actions := createMenuActions(prov, lines, selBuf, screenBuf)

	m := Model{
		cfg:         cfg,
		km:          km,
		provider:    prov,
		mode:        ViewDashboard,
		screenLines: lines,
		screenText:  screenBuf,
		selection:   selBuf,
		textInput:   ti,
		spinner:     sp,
		viewport:    vp,
		menuActions: actions,
		provItems:   []string{"groq", "gemini", "ollama", "openrouter", "nvidia", "openai", "g4f"},
		width:       80,
		height:      24,
	}

	for i, item := range m.provItems {
		if strings.EqualFold(item, cfg.Provider) {
			m.provIndex = i
			break
		}
	}

	if initialQuery != "" {
		m.textInput.SetValue(initialQuery)
		m.mode = ViewChat
	}

	m.initRenderer(80)
	return m
}

func (m *Model) initRenderer(w int) {
	wrapWidth := w - 6
	if wrapWidth < 30 {
		wrapWidth = 30
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(wrapWidth),
	)
	if err == nil {
		m.renderer = r
	}
}

func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.initRenderer(m.width)

		vpHeight := m.height - 10
		if vpHeight < 4 {
			vpHeight = 4
		}
		m.viewport.Width = m.width - 6
		m.viewport.Height = vpHeight

	case tea.KeyMsg:
		// Submenu de Seleção de Modelos
		if m.mode == ViewModelSelect {
			switch msg.String() {
			case "up", "k":
				if m.provIndex > 0 {
					m.provIndex--
				}
			case "down", "j":
				if m.provIndex < len(m.provItems)-1 {
					m.provIndex++
				}
			case "enter":
				selected := m.provItems[m.provIndex]
				_ = m.cfg.SaveProvider(selected)
				m.provider = ai.NewProvider(m.cfg, selected)
				m.menuActions = createMenuActions(m.provider, m.screenLines, m.selection, m.screenText)
				m.mode = ViewDashboard
				m.statusMsg = fmt.Sprintf("Provedor alterado para: %s (%s)", m.provider.Name(), m.provider.Model())
			case "esc", "ctrl+m", "ctrl+q":
				m.mode = ViewDashboard
			}
			return m, nil
		}

		// Modo Dashboard Principal
		if m.mode == ViewDashboard {
			switch msg.String() {
			case "esc", "ctrl+c", "ctrl+q":
				return m, tea.Quit

			case "up", "k":
				if m.menuIndex > 0 {
					m.menuIndex--
				}
				return m, nil

			case "down", "j":
				if m.menuIndex < len(m.menuActions)-1 {
					m.menuIndex++
				}
				return m, nil

			case "tab":
				m.mode = ViewChat
				return m, nil

			case "1", "f1":
				m.menuIndex = 0
				return m.triggerAction(0)
			case "2", "f2":
				m.menuIndex = 1
				return m.triggerAction(1)
			case "3", "f3":
				m.menuIndex = 2
				return m.triggerAction(2)
			case "4", "f4":
				m.menuIndex = 3
				return m.triggerAction(3)

			case "enter":
				if strings.TrimSpace(m.textInput.Value()) != "" {
					return m.startQuery(m.textInput.Value(), false)
				}
				return m.triggerAction(m.menuIndex)
			}
		}

		// Modo Chat / Resposta
		if m.mode == ViewChat {
			switch msg.String() {
			case "ctrl+c":
				if m.isLoading {
					if m.cancelFunc != nil {
						m.cancelFunc()
					}
					m.isLoading = false
					m.statusMsg = "Cancelado pelo usuário."
					return m, nil
				}
				return m, tea.Quit

			case "esc", "ctrl+q":
				if !m.isLoading {
					m.mode = ViewDashboard
					return m, nil
				}

			case "ctrl+m":
				m.mode = ViewModelSelect
				return m, nil

			case "ctrl+y":
				if m.lastCode != "" {
					_ = copyToClipboard(m.lastCode)
					m.statusMsg = "Código copiado!"
				}
				return m, nil

			case "ctrl+s":
				if m.lastCode != "" {
					_ = m.km.SendText(m.lastCode, false)
					m.statusMsg = "Comando preparado no Kitty!"
				}
				return m, nil

			case "enter":
				if m.isLoading {
					return m, nil
				}
				query := strings.TrimSpace(m.textInput.Value())
				if query == "" {
					if m.km != nil && strings.TrimSpace(m.km.GetSelectionBuffer()) != "" {
						query = "Analise o novo trecho selecionado no terminal com o mouse:"
					} else {
						return m, nil
					}
				}
				m.textInput.SetValue("")

				if strings.HasPrefix(query, "/clear") || strings.HasPrefix(query, "/limpar") {
					m.conversationHistory = ""
					m.fullResponse.Reset()
					m.viewport.SetContent("")
					m.statusMsg = "Conversa limpa."
					return m, nil
				}

				if strings.HasPrefix(query, "/salvar") || strings.HasPrefix(query, "/save") || strings.HasPrefix(query, "/export") {
					targetPath := query
					for _, prefix := range []string{"/salvar", "/save", "/export"} {
						if strings.HasPrefix(targetPath, prefix) {
							targetPath = strings.TrimSpace(strings.TrimPrefix(targetPath, prefix))
							break
						}
					}
					if targetPath == "" {
						home, _ := os.UserHomeDir()
						if home == "" {
							home = os.TempDir()
						}
						docsDir := filepath.Join(home, "metis-docs")
						_ = os.MkdirAll(docsDir, 0755)
						targetPath = filepath.Join(docsDir, fmt.Sprintf("metis_relatorio_%s.md", time.Now().Format("2006-01-02_150405")))
					} else if !filepath.IsAbs(targetPath) {
						cwd, _ := os.Getwd()
						targetPath = filepath.Join(cwd, targetPath)
					}
					_ = os.MkdirAll(filepath.Dir(targetPath), 0755)
					content := m.fullResponse.String()
					if strings.TrimSpace(content) == "" {
						m.statusMsg = "Nenhum relatório para salvar."
					} else {
						if err := os.WriteFile(targetPath, []byte(content), 0644); err != nil {
							m.statusMsg = fmt.Sprintf("Erro ao salvar: %v", err)
						} else {
							m.statusMsg = fmt.Sprintf("Salvo em: %s", targetPath)
						}
					}
					return m, nil
				}

				if strings.HasPrefix(query, "/retry") || strings.HasPrefix(query, "/refazer") {
					if m.lastQuery != "" {
						query = m.lastQuery
					} else {
						query = "Analise o terminal e ajude a resolver o erro atual:"
					}
				} else {
					m.lastQuery = query
				}

				return m.startQuery(query, strings.HasPrefix(query, "/auto") || strings.HasPrefix(query, "/resolver"))
			}
		}

	case streamChunkMsg:
		m.fullResponse.WriteString(string(msg))
		m.viewport.SetContent(m.fullResponse.String())
		m.viewport.GotoBottom()
		return m, nil

	case streamDoneMsg:
		m.isLoading = false
		rendered := m.renderMarkdown(m.fullResponse.String())
		m.viewport.SetContent(rendered)
		m.viewport.GotoBottom()

		if msg != nil {
			m.statusMsg = fmt.Sprintf("Erro: %v", msg)
		} else {
			m.statusMsg = "Concluído."
			m.conversationHistory += fmt.Sprintf("\n\n[Assistente]: %s", m.fullResponse.String())
			if blocks := agent.ExtractAllCodeBlocks(m.fullResponse.String()); len(blocks) > 0 {
				m.lastCode = blocks[0]
			}

			if m.autoMode && m.autoSteps > 0 {
				calls := agent.ParseToolCalls(m.fullResponse.String())
				if len(calls) > 0 {
					return m, m.executeAutoToolsCmd(calls)
				}
			}
		}
		return m, nil

	case autoStepDoneMsg:
		m.autoSteps--
		if msg.hasTools && m.autoSteps > 0 {
			m.isLoading = true
			m.statusMsg = fmt.Sprintf("Passo autônomo (%d restantes)...", m.autoSteps)

			ctx, cancel := context.WithCancel(context.Background())
			m.cancelFunc = cancel

			sysPrompt := agent.SystemPrompt(true)
			return m, tea.Batch(m.spinner.Tick, m.startStreamingCmd(ctx, sysPrompt, msg.response))
		}
		m.isLoading = false
		m.statusMsg = "Modo autônomo finalizado."
		return m, nil

	case spinner.TickMsg:
		if m.isLoading {
			var spCmd tea.Cmd
			m.spinner, spCmd = m.spinner.Update(msg)
			return m, spCmd
		}
	}

	var tiCmd, vpCmd tea.Cmd
	m.textInput, tiCmd = m.textInput.Update(msg)
	m.viewport, vpCmd = m.viewport.Update(msg)
	cmds = append(cmds, tiCmd, vpCmd)

	return m, tea.Batch(cmds...)
}

func (m Model) triggerAction(idx int) (tea.Model, tea.Cmd) {
	switch idx {
	case 0: // Analyze
		if strings.TrimSpace(m.selection) != "" {
			return m.startQuery("Analise detalhadamente o erro ou código selecionado no terminal e mostre como corrigir:", false)
		}
		return m.startQuery("Analise a saída recente do meu terminal, identifique o contexto/erros e mostre a solução:", false)

	case 1: // Explain
		return m.startQuery("Explique detalhadamente o contexto técnico e os comandos da saída recente do terminal:", false)

	case 2: // Config
		m.mode = ViewChat
		m.textInput.SetValue("/config ")
		m.textInput.CursorEnd()
		return m, nil

	case 3: // AI Provedor
		m.mode = ViewModelSelect
		return m, nil
	}
	return m, nil
}

func (m Model) startQuery(query string, auto bool) (tea.Model, tea.Cmd) {
	m.mode = ViewChat

	parsedLines, parsedSteps, cleanQuery, hasExplicitLines := agent.ParseFlagsAndQuery(query, m.screenLines)
	if hasExplicitLines && parsedLines > 0 {
		m.screenLines = parsedLines
		m.screenText = m.km.GetScreenBuffer(m.screenLines)
		m.selection = ""
	}
	if parsedSteps > 0 {
		m.cfg.SetMaxSteps(parsedSteps)
	}

	isAuto := auto ||
		strings.HasPrefix(cleanQuery, "/auto") ||
		strings.HasPrefix(cleanQuery, "/resolver") ||
		strings.HasPrefix(cleanQuery, "/fix") ||
		strings.HasPrefix(cleanQuery, "/corrigir")

	for _, prefix := range []string{"/auto", "/resolver", "/fix", "/corrigir"} {
		if strings.HasPrefix(cleanQuery, prefix) {
			cleanQuery = strings.TrimSpace(strings.TrimPrefix(cleanQuery, prefix))
			break
		}
	}

	// Remove gatilho de sync (/s, /sync, /tela) apenas quando for a primeira palavra exata
	firstQWord := cleanQuery
	if idx := strings.IndexByte(cleanQuery, ' '); idx != -1 {
		firstQWord = cleanQuery[:idx]
	}
	if firstQWord == "/s" || firstQWord == "/sync" || firstQWord == "/tela" {
		cleanQuery = strings.TrimSpace(strings.TrimPrefix(cleanQuery, firstQWord))
	}

	if isAuto {
		tuiSel := ""
		if m.km != nil {
			tuiSel = strings.TrimSpace(m.km.GetSelectionBuffer())
		}
		if tuiSel == "" && m.selection != "" {
			tuiSel = strings.TrimSpace(m.selection)
		}
		if tuiSel != "" {
			sLines := len(strings.Split(tuiSel, "\n"))
			if cleanQuery == "" {
				cleanQuery = fmt.Sprintf("Investigar a causa do erro selecionado com o mouse no terminal (%d linhas):\n%s\nAplicar as correções necessárias e resolver o problema.", sLines, tuiSel)
			} else {
				cleanQuery = fmt.Sprintf("%s\n\n[Trecho Selecionado com o Mouse no Terminal (%d linhas)]:\n%s", cleanQuery, sLines, tuiSel)
			}
			if m.km != nil {
				m.km.ClearSelection()
			}
		} else if cleanQuery == "" {
			cleanQuery = "Investigar a causa do erro na tela do terminal, aplicar as correções necessárias e resolver o problema."
		}
	} else if cleanQuery == "" && hasExplicitLines {
		cleanQuery = fmt.Sprintf("Analise as últimas %d linhas do terminal e me diga o que fazer:", m.screenLines)
	}

	m.autoMode = isAuto
	m.autoSteps = m.cfg.MaxSteps
	m.isLoading = true
	m.statusMsg = fmt.Sprintf("Consultando %s (%s)...", m.provider.Name(), m.provider.Model())

	var promptToSend string
	var sysPrompt string
	if m.conversationHistory == "" {
		m.fullResponse.Reset()
		prompt := m.buildPrompt(cleanQuery)
		sysPrompt = agent.SystemPrompt(m.autoMode)
		m.conversationHistory = fmt.Sprintf("[Instruções]: %s\n\n%s", sysPrompt, prompt)
		promptToSend = m.conversationHistory
	} else {
		var userEntry strings.Builder
		latestSel := strings.TrimSpace(m.km.GetSelectionBuffer())
		syncWord := query
		if idx := strings.IndexByte(query, ' '); idx != -1 {
			syncWord = query[:idx]
		}
		isSync := syncWord == "/s" || syncWord == "/sync" || syncWord == "/tela"

		if isSync || hasExplicitLines {
			m.screenText = m.km.GetScreenBuffer(m.screenLines)
			sLines := len(strings.Split(m.screenText, "\n"))
			userEntry.WriteString(fmt.Sprintf("[Terminal Atualizado do Usuário (últimas %d linhas)]:\n%s\n\n", sLines, m.screenText))
		} else if latestSel != "" {
			sLines := len(strings.Split(latestSel, "\n"))
			userEntry.WriteString(fmt.Sprintf("[Novo Trecho Selecionado pelo Usuário no Terminal (%d linhas)]:\n%s\n\n", sLines, latestSel))
			m.km.ClearSelection()
		}

		if cleanQuery != "" {
			userEntry.WriteString(cleanQuery)
		} else if isSync || hasExplicitLines {
			userEntry.WriteString("Analise a tela atualizada do terminal acima e me diga o que fazer para corrigir:")
		} else if latestSel != "" {
			userEntry.WriteString("Analise o novo trecho selecionado no terminal acima:")
		}

		host, _ := os.Hostname()
		if host == "" {
			host = "terminal"
		}
		user := os.Getenv("USER")
		if user == "" {
			user = os.Getenv("LOGNAME")
		}
		if user == "" {
			if u, err := osUser.Current(); err == nil && u != nil && u.Username != "" {
				user = u.Username
			}
		}
		if user == "" {
			user = "você"
		}
		m.fullResponse.WriteString(fmt.Sprintf("\n\n---\n**👤 %s:** %s\n\n", user, userEntry.String()))
		m.conversationHistory += fmt.Sprintf("\n\n[Usuário]: %s", userEntry.String())
		m.conversationHistory = agent.LimitContextTurns(m.conversationHistory, m.cfg.MaxContextTurns)
		promptToSend = m.conversationHistory
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.cancelFunc = cancel

	return m, tea.Batch(m.spinner.Tick, m.startStreamingCmd(ctx, sysPrompt, promptToSend))
}

func (m Model) View() string {
	w := m.width
	if w < 40 {
		w = 80
	}

	modalWidth := w - 2
	innerWidth := modalWidth - 4
	if innerWidth < 30 {
		innerWidth = 30
	}

	var innerContent string

	switch m.mode {
	case ViewModelSelect:
		innerContent = lipgloss.JoinVertical(
			lipgloss.Left,
			m.renderHeader(innerWidth),
			"\n"+m.renderModelMenu(innerWidth)+"\n",
			m.renderFooter(innerWidth),
		)

	case ViewChat:
		innerContent = lipgloss.JoinVertical(
			lipgloss.Left,
			m.renderHeader(innerWidth),
			"\n"+m.viewport.View()+"\n",
			styleInputBox.Width(innerWidth-2).Render(m.textInput.View())+"\n",
			m.renderChatFooter(innerWidth),
		)

	default: // ViewDashboard (Mockup ideias.png)
		innerContent = lipgloss.JoinVertical(
			lipgloss.Left,
			m.renderHeader(innerWidth),
			"\n"+m.renderTerminalErrorSection(innerWidth)+"\n",
			m.renderDashboard(innerWidth)+"\n",
			m.renderFooter(innerWidth),
		)
	}

	winStyle := styleWindow.Width(modalWidth)
	if m.cfg != nil {
		th := m.cfg.GetActiveTheme()
		if th.BgMain != "" {
			winStyle = winStyle.Background(lipgloss.Color(th.BgMain))
		}
		if th.BorderGlow != "" {
			winStyle = winStyle.BorderForeground(lipgloss.Color(th.BorderGlow))
		} else if th.Border != "" {
			winStyle = winStyle.BorderForeground(lipgloss.Color(th.Border))
		}
	}

	styledWindow := winStyle.Render(innerContent)

	if m.height > 0 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, styledWindow)
	}
	return styledWindow
}

func (m Model) renderHeader(w int) string {
	titleText := styleTitle.Render("  METIS")



	badgeContent := "● Online"
	badgeText := styleStatusBadge.Render(badgeContent)

	spacing := w - lipgloss.Width(titleText) - lipgloss.Width(badgeText)
	if spacing < 2 {
		spacing = 2
	}
	spacer := strings.Repeat(" ", spacing)

	headerRow := lipgloss.JoinHorizontal(lipgloss.Center, titleText, spacer, badgeText)

	if icon := GetKittyPNGIcon(); icon != "" {
		headerRow = strings.Replace(headerRow, "  METIS", icon+" METIS", 1)
	} else {
		headerRow = strings.Replace(headerRow, "  METIS", "󰧑 METIS", 1)
	}

	return headerRow
}

func (m Model) renderTerminalErrorSection(w int) string {
	title := "Terminal Error"
	if m.km != nil && m.km.PID != "" {
		title = fmt.Sprintf("Terminal Error (PID %s)", m.km.PID)
	}
	termHeader := styleSectionHeader.Render(title)

	var content string
	if strings.TrimSpace(m.selection) != "" {
		lines := strings.Split(strings.TrimSpace(m.selection), "\n")
		content = lipgloss.NewStyle().Foreground(colorTerminalPrompt).Render("$ [Seleção do mouse]\n") +
			lipgloss.NewStyle().Foreground(colorTerminalError).Render(truncateLines(strings.Join(lines, " "), 2))
	} else if strings.TrimSpace(m.screenText) != "" {
		lines := strings.Split(strings.TrimSpace(m.screenText), "\n")
		var lastLines []string
		for i := len(lines) - 1; i >= 0 && len(lastLines) < 2; i-- {
			l := strings.TrimSpace(lines[i])
			if l != "" {
				lastLines = append([]string{l}, lastLines...)
			}
		}
		if len(lastLines) > 0 {
			content = lipgloss.NewStyle().Foreground(colorTerminalPrompt).Render("$ ") +
				lipgloss.NewStyle().Foreground(colorTerminalText).Render(lastLines[0])
			if len(lastLines) > 1 {
				content += "\n" + lipgloss.NewStyle().Foreground(colorTerminalError).Render(lastLines[1])
			}
		}
	}

	if content == "" {
		content = lipgloss.NewStyle().Foreground(colorTerminalText).Render("Terminal ativo e pronto para análise.")
	}

	termBox := styleTerminalBox.Width(w - 2).Render(content)
	return termHeader + "\n" + termBox
}

func (m Model) renderDashboard(w int) string {
	leftColWidth := (w * 44) / 100
	if leftColWidth < 26 {
		leftColWidth = 26
	}
	rightColWidth := w - leftColWidth - 2
	if rightColWidth < 26 {
		rightColWidth = 26
	}

	// 1. Coluna da Esquerda (Quick Actions com paleta do Ícone Metis)
	var leftB strings.Builder
	leftB.WriteString(styleMenuHeader.Render("Quick Actions ▾") + "\n\n")

	for i, act := range m.menuActions {
		keyStyle := lipgloss.NewStyle().Foreground(colorMetisPeach)
		titleStyle := lipgloss.NewStyle().Foreground(colorInactiveText)
		iconStyle := lipgloss.NewStyle().Foreground(colorMetisGold)

		if i == m.menuIndex {
			keyStyle = lipgloss.NewStyle().Foreground(colorMetisGreen).Bold(true)
			titleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#fff7ed")).Bold(true)
			iconStyle = lipgloss.NewStyle().Foreground(colorMetisGold).Bold(true)
		}

		btnContent := keyStyle.Render(act.KeyBadge) + titleStyle.Render(act.Title) + iconStyle.Render(act.Icon)

		if i == m.menuIndex {
			leftB.WriteString(styleMenuItemActive.Width(leftColWidth - 2).Render(btnContent) + "\n\n")
		} else {
			leftB.WriteString(styleMenuItemInactive.Width(leftColWidth - 2).Render(btnContent) + "\n\n")
		}
	}

	// 2. Coluna da Direita (AI Diagnostic)
	diagHeader := styleSectionHeader.Render("AI Diagnostic")

	curr := m.menuActions[m.menuIndex]
	var cardContent strings.Builder

	cardContent.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#f8fafc")).Bold(true).Render(curr.CauseHeader) + "\n")
	cardContent.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#94a3b8")).Render(wrapText(curr.CauseDesc, rightColWidth-4)) + "\n\n")

	if curr.Snippet != "" {
		snippetFormatted := lipgloss.NewStyle().Foreground(colorSnippetText).Render(curr.Snippet)
		cardContent.WriteString(styleSnippetBox.Width(rightColWidth - 6).Render(snippetFormatted) + "\n\n")
	}

	applyBtn := styleButton.Width(rightColWidth - 6).Render(curr.ButtonLabel)
	cardContent.WriteString(applyBtn)

	diagCard := styleDiagCard.Width(rightColWidth - 2).Render(cardContent.String())
	rightCol := lipgloss.JoinVertical(lipgloss.Left, diagHeader+"\n", diagCard)

	return lipgloss.JoinHorizontal(lipgloss.Top, leftB.String(), "  ", rightCol)
}

func (m Model) renderModelMenu(w int) string {
	var b strings.Builder
	headerStyle := styleMenuHeader.Width(w - 2)
	cardStyle := styleDiagCard
	itemActiveStyle := styleMenuItemActive
	itemInactiveStyle := styleMenuItemInactive

	if m.cfg != nil {
		th := m.cfg.GetActiveTheme()
		if th.AccentGold != "" {
			headerStyle = headerStyle.Foreground(lipgloss.Color(th.AccentGold))
		}
		if th.BgCard != "" {
			cardStyle = cardStyle.Background(lipgloss.Color(th.BgCard))
		}
		if th.BorderGlow != "" {
			cardStyle = cardStyle.BorderForeground(lipgloss.Color(th.BorderGlow))
		}
		if th.UserBubble != "" {
			itemActiveStyle = itemActiveStyle.Background(lipgloss.Color(th.UserBubble))
		}
		if th.AccentGold != "" {
			itemActiveStyle = itemActiveStyle.BorderForeground(lipgloss.Color(th.AccentGold))
		}
	}

	b.WriteString(headerStyle.Render("⚙️ SELECIONE O PROVEDOR DE IA") + "\n\n")

	for i, item := range m.provItems {
		cursor := "  "
		style := itemInactiveStyle
		if i == m.provIndex {
			cursor = "➜ "
			style = itemActiveStyle
		}
		b.WriteString(style.Width(w - 4).Render(fmt.Sprintf("%s%s", cursor, strings.ToUpper(item))) + "\n\n")
	}

	return cardStyle.Render(b.String())
}

func (m Model) renderFooter(w int) string {
	k1 := renderFooterKey("Tab", "Chat")
	k2 := renderFooterKey("↑↓", "Navegar")
	k3 := renderFooterKey("Enter", "Executar")
	k4 := renderFooterKey("Ctrl+Q", "Fechar")

	gap := (w - lipgloss.Width(k1) - lipgloss.Width(k2) - lipgloss.Width(k3) - lipgloss.Width(k4)) / 3
	if gap < 2 {
		gap = 2
	}
	spacer := strings.Repeat(" ", gap)

	return lipgloss.JoinHorizontal(lipgloss.Center, k1, spacer, k2, spacer, k3, spacer, k4)
}

func (m Model) renderChatFooter(w int) string {
	k1 := renderFooterKey("Enter", "Enviar")
	k2 := renderFooterKey("Ctrl+Y", "Copiar Código")
	k3 := renderFooterKey("Ctrl+S", "Enviar Kitty")
	k4 := renderFooterKey("Esc", "Voltar")

	gap := (w - lipgloss.Width(k1) - lipgloss.Width(k2) - lipgloss.Width(k3) - lipgloss.Width(k4)) / 3
	if gap < 2 {
		gap = 2
	}
	spacer := strings.Repeat(" ", gap)

	row := lipgloss.JoinHorizontal(lipgloss.Center, k1, spacer, k2, spacer, k3, spacer, k4)
	if m.km != nil {
		if sel := strings.TrimSpace(m.km.GetSelectionBuffer()); sel != "" {
			nLines := len(strings.Split(sel, "\n"))
			badge := lipgloss.NewStyle().Foreground(colorMetisPeach).Bold(true).Render(fmt.Sprintf("  ⚡ [Seleção ativa: %d lin no terminal]", nLines))
			return row + badge
		}
	}
	return row
}

func renderFooterKey(key, label string) string {
	return styleFooterBracket.Render("[") +
		styleFooterKey.Render(key) +
		styleFooterBracket.Render("] ") +
		styleFooterLabel.Render(label)
}

func truncateLines(text string, maxLines int) string {
	lines := strings.Split(text, "\n")
	var nonEmpty []string
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed != "" {
			nonEmpty = append(nonEmpty, trimmed)
		}
	}
	if len(nonEmpty) == 0 {
		return ""
	}
	if len(nonEmpty) > maxLines {
		nonEmpty = nonEmpty[:maxLines]
	}
	return strings.Join(nonEmpty, "\n")
}

func wrapText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	words := strings.Fields(text)
	var lines []string
	var curLine strings.Builder

	for _, w := range words {
		if curLine.Len()+len(w)+1 > maxLen {
			if curLine.Len() > 0 {
				lines = append(lines, curLine.String())
				curLine.Reset()
			}
		}
		if curLine.Len() > 0 {
			curLine.WriteString(" ")
		}
		curLine.WriteString(w)
	}
	if curLine.Len() > 0 {
		lines = append(lines, curLine.String())
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderMarkdown(raw string) string {
	if m.renderer != nil {
		if out, err := m.renderer.Render(raw); err == nil {
			return out
		}
	}
	return raw
}

func (m Model) buildPrompt(userQuery string) string {
	var b strings.Builder
	if m.screenText != "" {
		b.WriteString("=== CONTEXTO DA TELA DO TERMINAL ===\n")
		b.WriteString(m.screenText)
		b.WriteString("\n====================================\n\n")
	}
	if m.selection != "" {
		b.WriteString("=== TEXTO SELECIONADO NO TERMINAL ===\n")
		b.WriteString(m.selection)
		b.WriteString("\n====================================\n\n")
	}
	b.WriteString("INSTRUÇÃO:\n" + userQuery)
	return b.String()
}

func (m Model) startStreamingCmd(ctx context.Context, sysPrompt, userPrompt string) tea.Cmd {
	return func() tea.Msg {
		ch := make(chan string, 10)
		var streamErr error

		go func() {
			defer close(ch)
			streamErr = m.provider.AskStream(ctx, sysPrompt, userPrompt, ch)
		}()

		for chunk := range ch {
			sendMsg(streamChunkMsg(chunk))
		}

		return streamDoneMsg(streamErr)
	}
}

var globalProgram *tea.Program

func SetProgram(p *tea.Program) {
	globalProgram = p
}

func sendMsg(msg tea.Msg) {
	if globalProgram != nil {
		globalProgram.Send(msg)
	}
}

func (m Model) executeAutoToolsCmd(calls []agent.ToolCall) tea.Cmd {
	return func() tea.Msg {
		var toolReport strings.Builder
		toolReport.WriteString("RESULTADO DAS FERRAMENTAS:\n\n")

		for _, call := range calls {
			switch call.Type {
			case "bash":
				cmd := strings.TrimSpace(call.Command)
				cmd = kitty.RemoveComments(cmd)

				// 1. Bloqueio incondicional de comandos comprovadamente destrutivos
				if dangerous, warn := security.IsDangerousCommand(cmd); dangerous {
					toolReport.WriteString(fmt.Sprintf("[Ação Bloqueada por Segurança]: %s. O comando %q é estritamente proibido.\n\n", warn, cmd))
					continue
				}

				// 2. Comandos de risco / sensíveis: requer confirmação explícita
				if risky, reason := security.IsRiskyAutoCommand(cmd); risky {
					if !agent.DefaultTTYConfirm(cmd, reason) {
						toolReport.WriteString(fmt.Sprintf("[Ação Cancelada pelo Usuário]: O comando %q foi cancelado (%s). Sugira outra abordagem ou finalize.\n\n", cmd, reason))
						continue
					}
				}

				statusFile := "/tmp/metis_last_status"
				if m.km.WindowID != "" {
					statusFile = fmt.Sprintf("/tmp/metis_status_%s", m.km.WindowID)
				}
				_ = os.Remove(statusFile)

				if err := m.km.SendText(cmd, true); err != nil {
					toolReport.WriteString(fmt.Sprintf("[Erro ao enviar comando para o Kitty]: %v\n\n", err))
					continue
				}

				waited := 0
				for waited < 120 {
					if _, err := os.Stat(statusFile); err == nil {
						time.Sleep(150 * time.Millisecond)
						break
					}
					time.Sleep(100 * time.Millisecond)
					waited++
				}

				newScreen := m.km.GetScreenBuffer(m.screenLines)
				if idx := strings.LastIndex(newScreen, cmd); idx != -1 {
					newScreen = newScreen[idx+len(cmd):]
				}
				_, exitCode, _ := m.km.GetLastCommandStatus()
				toolReport.WriteString(fmt.Sprintf("[Comando executado no Kitty]: %s\n[Exit Code]: %s\n[Saída da Tela]:\n%s\n\n", cmd, exitCode, newScreen))
			case "read_file", "write_file", "append_file", "list_dir":
				res := agent.ExecuteTool(context.Background(), call)
				toolReport.WriteString(fmt.Sprintf("[%s]: %s\n\n", call.Type, res.Output))
			}
		}

		toolReport.WriteString("Analise os resultados acima e prossiga com o objetivo.")
		return autoStepDoneMsg{
			response: toolReport.String(),
			hasTools: true,
			tools:    calls,
		}
	}
}

func copyToClipboard(text string) error {
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
