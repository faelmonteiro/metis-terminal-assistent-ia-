package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"metis-screen/internal/config"
	"metis-screen/internal/gui"
	"metis-screen/internal/kitty"
	"metis-screen/internal/tui"
)

func main() {
	var (
		flagLines int
		flagSteps int
		flagTUI   bool
		flagGUI   bool
		flagHelp  bool
	)

	flag.IntVar(&flagLines, "n", 0, "Número de linhas da tela do terminal a capturar (ex: 30)")
	flag.IntVar(&flagLines, "lines", 0, "Número de linhas da tela do terminal a capturar")
	flag.IntVar(&flagSteps, "p", 0, "Limite de passos para o modo autônomo /auto")
	flag.IntVar(&flagSteps, "steps", 0, "Limite de passos para o modo autônomo /auto")
	flag.BoolVar(&flagTUI, "tui", false, "Executa em modo terminal TUI")
	flag.BoolVar(&flagGUI, "gui", false, "Executa em modo janela flutuante moderna (GUI)")
	flag.BoolVar(&flagHelp, "h", false, "Exibe ajuda")
	flag.BoolVar(&flagHelp, "help", false, "Exibe ajuda")

	_ = flag.CommandLine.Parse(normalizeCLIArgs(os.Args[1:]))

	if flagHelp {
		fmt.Println("Metis Screen - Assistente Inteligente de Terminal para Kitty e Hyprland")
		fmt.Println("\nUso: metis-screen [opções] [pergunta inicial]")
		fmt.Println("\nOpções:")
		flag.PrintDefaults()
		fmt.Println("  -<linhas> (atalho numérico direto, ex: -3, -15, -30)")
		return
	}

	// Captura qualquer texto passado diretamente após as flags como query inicial
	initialQuery := strings.Join(flag.Args(), " ")

	// 1. Carrega configurações do ambiente (.env, .env_local)
	cfg := config.Load()
	if flagSteps > 0 {
		cfg.MaxSteps = flagSteps
	}

	// 2. Detecta o terminal Kitty e janelas ativas
	km := kitty.Detect()
	defer km.ClearSelection()

	hasDisplay := os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("DISPLAY") != ""
	shouldUseGUI := (hasDisplay && !flagTUI) || flagGUI

	// 3. Executa em modo Janela Flutuante Moderna (Glassmorphism GUI)
	if shouldUseGUI {
		err := gui.Run(cfg, km, initialQuery, flagLines)
		if err == nil {
			return
		}
		fmt.Fprintf(os.Stderr, "Aviso: Falha ao abrir janela gráfica (%v), iniciando modo terminal TUI...\n", err)
	}

	// 4. Fallback: Modo Terminal TUI
	model := tui.NewModel(cfg, km, initialQuery, flagLines)
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	tui.SetProgram(p)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao executar interface Metis Screen: %v\n", err)
		os.Exit(1)
	}
}

func normalizeCLIArgs(args []string) []string {
	var normalized []string
	seenQuery := false
	for _, arg := range args {
		// O atalho -<dígitos> (ex: -30) só é interpretado como linhas ANTES da pergunta
		if !seenQuery && len(arg) > 1 && arg[0] == '-' && kitty.IsDigits(arg[1:]) {
			normalized = append(normalized, "-n", arg[1:])
			continue
		}
		normalized = append(normalized, arg)
		if arg != "" && !strings.HasPrefix(arg, "-") {
			seenQuery = true
		}
	}
	return normalized
}

