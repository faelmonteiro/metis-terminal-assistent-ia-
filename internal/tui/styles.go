package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Paleta do Mockup ideias.png com mistura das cores do Ícone Metis (Dourado/Pêssego + Verde Terminal)
	colorWindowBg    = lipgloss.Color("#0b0f19") // Fundo geral escuro
	colorBorderOuter = lipgloss.Color("#2a3b5c") // Borda externa suave

	// Cabeçalho e Badges
	colorTitleText   = lipgloss.Color("#f8fafc") // Título branco limpo
	colorBadgeBg     = lipgloss.Color("#083344") // Fundo Dark Teal
	colorBadgeBorder = lipgloss.Color("#0891b2") // Borda Teal
	colorBadgeText   = lipgloss.Color("#22d3ee") // Texto e dot Ciano neon

	// Terminal Error Box
	colorSectionLabel   = lipgloss.Color("#cbd5e1") // Rótulo das seções (Slate 300)
	colorTerminalBoxBg  = lipgloss.Color("#070a12") // Fundo do terminal box
	colorTerminalBorder = lipgloss.Color("#1e293b") // Borda do terminal box
	colorTerminalPrompt = lipgloss.Color("#22c55e") // Prompt $ em verde
	colorTerminalText   = lipgloss.Color("#38bdf8") // Texto ciano
	colorTerminalError  = lipgloss.Color("#f87171") // Erro em vermelho coral

	// Cores inspiradas no Ícone Metis (Ouro, Pêssego e Verde Terminal)
	colorMetisGold       = lipgloss.Color("#f59e0b") // Ouro quente
	colorMetisPeach      = lipgloss.Color("#fbd38d") // Pêssego suave
	colorMetisGreen      = lipgloss.Color("#22c55e") // Verde prompt >_
	colorActiveMenuBg    = lipgloss.Color("#261c14") // Fundo âmbar escuro selecionado
	colorActiveMenuBrd   = lipgloss.Color("#f59e0b") // Borda dourada selecionada
	colorInactiveMenuBg  = lipgloss.Color("#121622") // Fundo dos itens inativos
	colorInactiveMenuBrd = lipgloss.Color("#1e2536") // Borda dos itens inativos
	colorInactiveText    = lipgloss.Color("#94a3b8") // Texto inativo

	// Card de Diagnóstico da IA
	colorDiagBorder    = lipgloss.Color("#38bdf8") // Borda ciano brilhante com glow
	colorDiagBg        = lipgloss.Color("#0d121f") // Fundo do card
	colorSnippetBg     = lipgloss.Color("#060911") // Fundo do snippet
	colorSnippetBorder = lipgloss.Color("#1e293b") // Borda do snippet
	colorSnippetText   = lipgloss.Color("#7dd3fc") // Texto do snippet
	colorButtonBg      = lipgloss.Color("#22d3ee") // Botão Apply Fix Ciano vibrante
	colorButtonText    = lipgloss.Color("#090d16") // Texto preto/dark slate do botão

	// Rodapé
	colorFooterKey     = lipgloss.Color("#c084fc") // Roxo/lilás dos atalhos
	colorFooterLabel   = lipgloss.Color("#64748b") // Cinza dos rótulos
	colorFooterBracket = lipgloss.Color("#475569") // Colchetes

	// Estilos Lipgloss
	styleWindow = lipgloss.NewStyle().
			Background(colorWindowBg).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorderOuter).
			Padding(0, 1)

	styleTitle = lipgloss.NewStyle().
			Foreground(colorTitleText).
			Bold(true)

	styleStatusBadge = lipgloss.NewStyle().
				Background(colorBadgeBg).
				Foreground(colorBadgeText).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorBadgeBorder).
				Padding(0, 1).
				Bold(true)

	styleSectionHeader = lipgloss.NewStyle().
				Foreground(colorSectionLabel).
				Bold(true)

	styleTerminalBox = lipgloss.NewStyle().
				Background(colorTerminalBoxBg).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorTerminalBorder).
				Padding(0, 1)

	styleMenuHeader = lipgloss.NewStyle().
			Foreground(colorMetisPeach).
			Bold(true)

	styleMenuItemActive = lipgloss.NewStyle().
				Background(colorActiveMenuBg).
				Foreground(lipgloss.Color("#fff7ed")).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorActiveMenuBrd).
				Bold(true).
				Padding(0, 1)

	styleMenuItemInactive = lipgloss.NewStyle().
				Background(colorInactiveMenuBg).
				Foreground(colorInactiveText).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorInactiveMenuBrd).
				Padding(0, 1)

	styleDiagCard = lipgloss.NewStyle().
			Background(colorDiagBg).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorDiagBorder).
			Padding(0, 1)

	styleSnippetBox = lipgloss.NewStyle().
			Background(colorSnippetBg).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorSnippetBorder).
			Foreground(colorSnippetText).
			Padding(0, 1)

	styleButton = lipgloss.NewStyle().
			Background(colorButtonBg).
			Foreground(colorButtonText).
			Bold(true).
			Align(lipgloss.Center).
			Padding(0, 1)

	styleInputBox = lipgloss.NewStyle().
			Background(colorTerminalBoxBg).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorDiagBorder).
			Foreground(colorTerminalText).
			Padding(0, 1)

	styleFooterKey = lipgloss.NewStyle().
			Foreground(colorFooterKey).
			Bold(true)

	styleFooterLabel = lipgloss.NewStyle().
				Foreground(colorFooterLabel)

	styleFooterBracket = lipgloss.NewStyle().
				Foreground(colorFooterBracket)
)
