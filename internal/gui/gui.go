package gui

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	osUser "os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/webview/webview_go"

	"metis-screen/internal/agent"
	"metis-screen/internal/ai"
	"metis-screen/internal/config"
	"metis-screen/internal/kitty"
	"metis-screen/internal/security"
)

//go:embed assets/index.html
var indexHTML string

func silenceStderr() func() {
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return func() {}
	}
	originalStderr, err := syscall.Dup(int(os.Stderr.Fd()))
	if err != nil {
		devNull.Close()
		return func() {}
	}

	_ = syscall.Dup2(int(devNull.Fd()), int(os.Stderr.Fd()))
	devNull.Close()

	return func() {
		_ = syscall.Dup2(originalStderr, int(os.Stderr.Fd()))
		syscall.Close(originalStderr)
	}
}

func Run(cfg *config.Config, km *kitty.KittyManager, initialQuery string, cliLines int) error {
	restoreStderr := silenceStderr()
	w := webview.New(false)
	restoreStderr()
	defer w.Destroy()
	defer km.ClearSelection()

	// Oculta a janela imediatamente para evitar piscar em branco antes do carregamento do DOM
	hideWindow(w.Window())

	w.SetTitle("METIS • Assistente de Terminal")
	w.SetSize(720, 560, webview.HintFixed)

	windowOpacity := cfg.GetWindowOpacity()
	applyWindowTransparency(w.Window(), windowOpacity)

	prov := ai.NewProvider(cfg, cfg.Provider)
	lines := 30
	if cliLines > 0 {
		lines = cliLines
	} else if cfg.DefaultScreenLines > 0 {
		lines = cfg.DefaultScreenLines
	}

	var mu sync.RWMutex
	var chatMu sync.Mutex
	defer func() {
		chatMu.Lock()
		if prov != nil && cfg != nil {
			activeM := prov.Model()
			if cfg.IsModelValidForProvider(prov.Name(), activeM) {
				_ = cfg.SaveProviderAndModel(prov.Name(), activeM)
			} else {
				_ = cfg.SaveProvider(prov.Name())
			}
		}
		chatMu.Unlock()
	}()
	screenBuf := strings.TrimSpace(km.GetScreenBuffer(lines))
	selBuf := strings.TrimSpace(km.GetSelectionBuffer())

	hasSelection := selBuf != ""
	selLines := 0
	if hasSelection {
		selLines = len(strings.Split(selBuf, "\n"))
	}
	screenLines := 0
	if screenBuf != "" {
		screenLines = len(strings.Split(screenBuf, "\n"))
	}

	// Watcher em background para detecção e atualização de seleção do mouse em tempo real
	stopWatcher := make(chan struct{})
	defer close(stopWatcher)

	// Captura SIGINT / SIGTERM do terminal (Ctrl+C no Kitty/terminal) para encerramento gracioso
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-sigCh:
			w.Dispatch(func() {
				w.Terminate()
			})
		case <-stopWatcher:
		}
	}()

	go func() {
		ticker := time.NewTicker(400 * time.Millisecond)
		defer ticker.Stop()

		var lastSel = selBuf

		for {
			select {
			case <-stopWatcher:
				return
			case <-ticker.C:
				currentSel := strings.TrimSpace(km.GetSelectionBuffer())
				if currentSel != lastSel {
					lastSel = currentSel
					mu.Lock()
					selBuf = currentSel
					hasSelection = selBuf != ""
					if hasSelection {
						selLines = len(strings.Split(selBuf, "\n"))
					} else {
						selLines = 0
					}
					isSel := hasSelection
					nSel := selLines
					mu.Unlock()

					selJSON, _ := json.Marshal(currentSel)
					w.Dispatch(func() {
						w.Eval(fmt.Sprintf("updateRealtimeSelection(%t, %d, %s);", isSel, nSel, string(selJSON)))
					})
				}
			}
		}
	}()

	var conversationHistory string

	// Helper para testar latência/conectividade em background
	testAndDispatchStatus := func(p ai.Provider) {
		go func() {
			lat, err := p.TestConnection(context.Background())
			w.Dispatch(func() {
				if err != nil {
					w.Eval(fmt.Sprintf("setProviderStatus(%q, %q, %t, %q, %d);", p.Name(), p.Model(), false, err.Error(), 0))
				} else {
					w.Eval(fmt.Sprintf("setProviderStatus(%q, %q, %t, %q, %d);", p.Name(), p.Model(), true, "", lat))
				}
			})
		}()
	}

	var showOnce sync.Once
	doShowWindow := func() {
		showOnce.Do(func() {
			w.Dispatch(func() {
				showWindow(w.Window())
			})
		})
	}
	w.Bind("windowReady", func() {
		doShowWindow()
	})
	w.Bind("goWindowReady", func() {
		doShowWindow()
	})

	// Fallback de segurança: caso o evento JS atrase mais de 120ms
	go func() {
		time.Sleep(120 * time.Millisecond)
		doShowWindow()
	}()

	// Bindings para o frontend JavaScript (nomes diretos e com prefixo go)
	w.Bind("close", func() {
		w.Terminate()
	})
	w.Bind("goClose", func() {
		w.Terminate()
	})

	copyClipboardFn := func(text string) {
		cleanText := kitty.RemoveComments(text)
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			cmd := exec.Command("wl-copy")
			cmd.Stdin = strings.NewReader(cleanText)
			_ = cmd.Run()
		} else if os.Getenv("DISPLAY") != "" {
			cmd := exec.Command("xclip", "-selection", "clipboard")
			cmd.Stdin = strings.NewReader(cleanText)
			_ = cmd.Run()
		}
	}
	w.Bind("copyClipboard", copyClipboardFn)
	w.Bind("goCopyClipboard", copyClipboardFn)

	var confirmMu sync.Mutex
	var confirmChan chan bool

	w.Bind("resolveConfirm", func(allowed bool) {
		confirmMu.Lock()
		defer confirmMu.Unlock()
		if confirmChan != nil {
			select {
			case confirmChan <- allowed:
			default:
			}
		}
	})

	type stepLimitResponse struct {
		moreSteps int
		proceed   bool
	}
	var stepLimitMu sync.Mutex
	var stepLimitChan chan stepLimitResponse

	resolveStepLimitFn := func(moreSteps int, proceed bool) {
		stepLimitMu.Lock()
		defer stepLimitMu.Unlock()
		if stepLimitChan != nil {
			select {
			case stepLimitChan <- stepLimitResponse{moreSteps: moreSteps, proceed: proceed}:
			default:
			}
			stepLimitChan = nil
		}
	}
	w.Bind("resolveStepLimit", resolveStepLimitFn)
	w.Bind("goResolveStepLimit", resolveStepLimitFn)

	sendKittyFn := func(text string) {
		cleanText := kitty.RemoveComments(text)
		if dangerous, warn := security.IsDangerousCommand(cleanText); dangerous {
			w.Dispatch(func() {
				warnJSON, _ := json.Marshal(fmt.Sprintf("⚠️ Comando Destrutivo Bloqueado por Segurança:\n%s", warn))
				w.Eval(fmt.Sprintf("alert(%s);", string(warnJSON)))
			})
			return
		}
		if err := km.SendText(cleanText, false); err != nil {
			w.Dispatch(func() {
				errJSON, _ := json.Marshal(fmt.Sprintf("⚠️ Falha ao enviar comando para o Kitty:\n%v", err))
				w.Eval(fmt.Sprintf("alert(%s);", string(errJSON)))
			})
		}
	}
	w.Bind("sendKitty", sendKittyFn)
	w.Bind("goSendKitty", sendKittyFn)

	syncScreenFn := func(nLines int) {
		if nLines <= 0 {
			nLines = lines
		}
		// Executa chamadas externas ao Kitty/Wayland fora do mutex para não travar a GUI
		newScreen := strings.TrimSpace(km.GetScreenBuffer(nLines))
		newSel := strings.TrimSpace(km.GetSelectionBuffer())
		hasSel := newSel != ""
		sLines := 0
		if hasSel {
			sLines = len(strings.Split(newSel, "\n"))
		}
		scLines := 0
		if newScreen != "" {
			scLines = len(strings.Split(newScreen, "\n"))
		}

		mu.Lock()
		screenBuf = newScreen
		selBuf = newSel
		hasSelection = hasSel
		selLines = sLines
		screenLines = scLines
		mu.Unlock()

		screenJSON, _ := json.Marshal(newScreen)
		selJSON, _ := json.Marshal(newSel)
		w.Dispatch(func() {
			w.Eval(fmt.Sprintf("setSmartContext(%t, %d, %d, %s, %s);", hasSel, sLines, scLines, string(selJSON), string(screenJSON)))
		})
	}
	w.Bind("syncScreen", syncScreenFn)
	w.Bind("goSyncScreen", syncScreenFn)

	selectProviderFn := func(name string) {
		_ = cfg.SaveProvider(name)
		newP := ai.NewProvider(cfg, name)
		chatMu.Lock()
		prov = newP
		chatMu.Unlock()
		provListJSON, _ := json.Marshal(cfg.GetProvidersList())
		w.Dispatch(func() {
			w.Eval(fmt.Sprintf("setProvider(%q, %q, %q);", newP.Name(), newP.Model(), "testando..."))
			w.Eval(fmt.Sprintf("setProvidersList(%s);", string(provListJSON)))
		})
		testAndDispatchStatus(newP)
	}
	w.Bind("selectProvider", selectProviderFn)
	w.Bind("goSelectProvider", selectProviderFn)

	selectProviderModelFn := func(provider, model string) {
		_ = cfg.SaveProviderAndModel(provider, model)
		newP := ai.NewProvider(cfg, provider)
		chatMu.Lock()
		prov = newP
		chatMu.Unlock()
		provListJSON, _ := json.Marshal(cfg.GetProvidersList())
		w.Dispatch(func() {
			w.Eval(fmt.Sprintf("setProvider(%q, %q, %q);", newP.Name(), newP.Model(), "testando..."))
			w.Eval(fmt.Sprintf("setProvidersList(%s);", string(provListJSON)))
		})
		testAndDispatchStatus(newP)
	}
	w.Bind("selectProviderModel", selectProviderModelFn)
	w.Bind("goSelectProviderModel", selectProviderModelFn)

	removeProviderModelFn := func(provider, model string) {
		_ = cfg.RemoveModel(provider, model)
		chatMu.Lock()
		if prov != nil && (strings.EqualFold(prov.Name(), provider) || strings.EqualFold(cfg.Provider, provider)) {
			prov = ai.NewProvider(cfg, cfg.Provider)
		}
		newProv := prov
		chatMu.Unlock()
		provListJSON, _ := json.Marshal(cfg.GetProvidersList())
		w.Dispatch(func() {
			w.Eval(fmt.Sprintf("setProvidersList(%s);", string(provListJSON)))
			if newProv != nil {
				w.Eval(fmt.Sprintf("setProvider(%q, %q, %q);", newProv.Name(), newProv.Model(), "online"))
			}
		})
	}
	w.Bind("removeProviderModel", removeProviderModelFn)
	w.Bind("goRemoveProviderModel", removeProviderModelFn)

	removeProviderModelsFn := func(provider, modelsJSON string) {
		var models []string
		if err := json.Unmarshal([]byte(modelsJSON), &models); err != nil {
			models = []string{modelsJSON}
		}
		_ = cfg.RemoveModels(provider, models)
		chatMu.Lock()
		if prov != nil && (strings.EqualFold(prov.Name(), provider) || strings.EqualFold(cfg.Provider, provider)) {
			prov = ai.NewProvider(cfg, cfg.Provider)
		}
		newProv := prov
		chatMu.Unlock()
		provListJSON, _ := json.Marshal(cfg.GetProvidersList())
		w.Dispatch(func() {
			w.Eval(fmt.Sprintf("setProvidersList(%s);", string(provListJSON)))
			if newProv != nil {
				w.Eval(fmt.Sprintf("setProvider(%q, %q, %q);", newProv.Name(), newProv.Model(), "online"))
			}
		})
	}
	w.Bind("removeProviderModels", removeProviderModelsFn)
	w.Bind("goRemoveProviderModels", removeProviderModelsFn)

	addProviderModelFn := func(provider, model string) {
		_ = cfg.AddModel(provider, model)
		chatMu.Lock()
		if prov != nil && (strings.EqualFold(prov.Name(), provider) || strings.EqualFold(cfg.Provider, provider)) {
			prov = ai.NewProvider(cfg, cfg.Provider)
		}
		newProv := prov
		chatMu.Unlock()
		provListJSON, _ := json.Marshal(cfg.GetProvidersList())
		w.Dispatch(func() {
			w.Eval(fmt.Sprintf("setProvidersList(%s);", string(provListJSON)))
			if newProv != nil {
				w.Eval(fmt.Sprintf("setProvider(%q, %q, %q);", newProv.Name(), newProv.Model(), "online"))
			}
		})
	}
	w.Bind("addProviderModel", addProviderModelFn)
	w.Bind("goAddProviderModel", addProviderModelFn)

	testProviderConnFn := func(provider, model string) {
		tempProv := ai.NewProvider(cfg, provider)
		testAndDispatchStatus(tempProv)
	}
	w.Bind("testProviderConnection", testProviderConnFn)
	w.Bind("goTestProviderConnection", testProviderConnFn)

	selectThemeFn := func(themeID string) {
		_ = cfg.SaveTheme(themeID)
		theme := cfg.GetActiveTheme()
		themeJSON, _ := json.Marshal(theme)
		op := cfg.GetWindowOpacity()
		applyWindowTransparency(w.Window(), op)
		w.Dispatch(func() {
			w.Eval(fmt.Sprintf("setMetisTheme(%s, %d);", string(themeJSON), op))
		})
	}
	w.Bind("selectTheme", selectThemeFn)
	w.Bind("goSelectTheme", selectThemeFn)

	setWindowOpacityFn := func(opacity int) {
		if opacity < 30 {
			opacity = 30
		} else if opacity > 100 {
			opacity = 100
		}
		_ = cfg.SaveWindowOpacity(opacity)
		applyWindowTransparency(w.Window(), opacity)
	}
	w.Bind("saveWindowOpacity", setWindowOpacityFn)
	w.Bind("goSaveWindowOpacity", setWindowOpacityFn)
	w.Bind("setWindowOpacity", setWindowOpacityFn)
	w.Bind("goSetWindowOpacity", setWindowOpacityFn)

	syncMetisFn := func() {
		go func() {
			_ = cfg.SyncMetisModels()
			provList := cfg.GetProvidersList()

			chatMu.Lock()
			currentProvName := cfg.Provider
			if currentProvName == "" && prov != nil {
				currentProvName = prov.Name()
			}
			// Valida se o provedor ativo ainda existe caso tenha sido removido no Metis
			validProv := false
			for _, p := range provList {
				if strings.EqualFold(p.ID, currentProvName) || strings.EqualFold(p.Name, currentProvName) {
					validProv = true
					break
				}
			}
			if !validProv && len(provList) > 0 {
				currentProvName = provList[0].ID
				_ = cfg.SaveProvider(currentProvName)
			}

			newP := ai.NewProvider(cfg, currentProvName)
			prov = newP
			chatMu.Unlock()

			activeTheme := cfg.GetActiveTheme()
			themeJSON, _ := json.Marshal(activeTheme)
			op := cfg.GetWindowOpacity()
			provListJSON, _ := json.Marshal(provList)

			w.Dispatch(func() {
				applyWindowTransparency(w.Window(), op)
				w.Eval(fmt.Sprintf("setMetisTheme(%s, %d);", string(themeJSON), op))
				w.Eval(fmt.Sprintf("setProvider(%q, %q, %q);", newP.Name(), newP.Model(), "testando..."))
				w.Eval(fmt.Sprintf("setProvidersList(%s);", string(provListJSON)))
				w.Eval("if (typeof onMetisSynced === 'function') { onMetisSynced(); }")
			})

			testAndDispatchStatus(newP)
		}()
	}
	w.Bind("syncMetis", syncMetisFn)
	w.Bind("goSyncMetis", syncMetisFn)
	w.Bind("syncMetisProviders", syncMetisFn)
	w.Bind("goSyncMetisProviders", syncMetisFn)

	clearSelectionFn := func() {
		km.ClearSelection()
		mu.Lock()
		selBuf = ""
		hasSelection = false
		selLines = 0
		mu.Unlock()
		w.Dispatch(func() {
			w.Eval("updateRealtimeSelection(false, 0, '');")
		})
	}
	w.Bind("clearSelection", clearSelectionFn)
	w.Bind("goClearSelection", clearSelectionFn)

	var (
		cancelFunc       context.CancelFunc
		lastCleanQuery   string
		lastIsAuto       bool
		lastFullResponse string
	)

	handleStartQuery := func(query string, auto bool) {
		chatMu.Lock()
		currentProv := prov
		if cancelFunc != nil {
			cancelFunc()
			cancelFunc = nil
		}
		chatMu.Unlock()

		cleanInput := strings.TrimSpace(query)
		mu.Lock()
		currentLines := lines
		mu.Unlock()

		stepLimitMu.Lock()
		isWaitingStepLimit := (stepLimitChan != nil)
		stepLimitMu.Unlock()

		if isWaitingStepLimit {
			low := strings.ToLower(cleanInput)
			if low == "sim" || low == "s" || low == "yes" || low == "y" || low == "+3" || low == "1" {
				resolveStepLimitFn(3, true)
				w.Dispatch(func() { w.Eval("closeStepLimitModalUI();") })
				return
			}
			if low == "não" || low == "nao" || low == "n" || low == "no" || low == "fim" || low == "parar" {
				resolveStepLimitFn(0, false)
				w.Dispatch(func() { w.Eval("closeStepLimitModalUI();") })
				return
			}
			if strings.HasPrefix(low, "-p") || strings.HasPrefix(low, "+") {
				_, steps, _, _ := agent.ParseFlagsAndQuery(cleanInput, currentLines)
				if steps > 0 {
					resolveStepLimitFn(steps, true)
					w.Dispatch(func() { w.Eval("closeStepLimitModalUI();") })
					return
				}
			}
			if num, err := strconv.Atoi(cleanInput); err == nil && num > 0 {
				resolveStepLimitFn(num, true)
				w.Dispatch(func() { w.Eval("closeStepLimitModalUI();") })
				return
			}
		}

		parsedLines, parsedSteps, cleanQuery, hasExplicitLines := agent.ParseFlagsAndQuery(cleanInput, currentLines)
		if hasExplicitLines && parsedLines > 0 {
			mu.Lock()
			lines = parsedLines
			screenBuf = strings.TrimSpace(km.GetScreenBuffer(lines))
			screenLines = len(strings.Split(screenBuf, "\n"))
			hasSelection = false
			selBuf = ""
			selLines = 0
			mu.Unlock()
			w.Dispatch(func() {
				w.Eval(fmt.Sprintf("setSmartContext(false, 0, %d, '', %q);", screenLines, screenBuf))
			})
		}
		if parsedSteps > 0 {
			cfg.SetMaxSteps(parsedSteps)
		}

		// Se o usuário digitou apenas -p4 ou similar sem texto, retoma o último objetivo autônomo
		if parsedSteps > 0 && cleanQuery == "" {
			chatMu.Lock()
			prevQuery := lastCleanQuery
			prevIsAuto := lastIsAuto
			chatMu.Unlock()
			if prevIsAuto && prevQuery != "" {
				cleanQuery = prevQuery
				auto = true
			}
		}

		lowQuery := strings.ToLower(cleanInput)
		if lowQuery == "sim" || lowQuery == "continuar" || lowQuery == "+3" {
			chatMu.Lock()
			prevQuery := lastCleanQuery
			prevIsAuto := lastIsAuto
			chatMu.Unlock()
			if prevIsAuto && prevQuery != "" {
				cleanQuery = prevQuery
				auto = true
				if parsedSteps <= 0 {
					parsedSteps = 3
					cfg.SetMaxSteps(3)
				}
			}
		}

		// 0. TRATAMENTO DE COMANDOS ESPECIAIS: /clear, /salvar e /retry
		if strings.HasPrefix(cleanQuery, "/clear") || strings.HasPrefix(cleanQuery, "/limpar") {
			chatMu.Lock()
			conversationHistory = ""
			lastFullResponse = ""
			chatMu.Unlock()
			w.Dispatch(func() {
				w.Eval("clearChatMessages();")
			})
			return
		}

		if strings.HasPrefix(cleanQuery, "/salvar") || strings.HasPrefix(cleanQuery, "/save") || strings.HasPrefix(cleanQuery, "/export") {
			targetPath := cleanQuery
			for _, prefix := range []string{"/salvar", "/save", "/export"} {
				if strings.HasPrefix(targetPath, prefix) {
					targetPath = strings.TrimSpace(strings.TrimPrefix(targetPath, prefix))
					break
				}
			}
			if targetPath == "" {
				home := config.GetUserHome()
				docsDir := filepath.Join(home, "metis-docs")
				_ = os.MkdirAll(docsDir, 0755)
				targetPath = filepath.Join(docsDir, fmt.Sprintf("metis_relatorio_%s.md", time.Now().Format("2006-01-02_150405")))
			} else if !filepath.IsAbs(targetPath) {
				cwd, _ := os.Getwd()
				targetPath = filepath.Join(cwd, targetPath)
			}
			_ = os.MkdirAll(filepath.Dir(targetPath), 0755)

			chatMu.Lock()
			toSave := conversationHistory
			if strings.TrimSpace(toSave) == "" {
				toSave = lastFullResponse
			}
			chatMu.Unlock()

			var msg string
			if strings.TrimSpace(toSave) == "" {
				msg = "\n\n⚠️ **Nenhum diagnóstico gerado ainda para salvar.** Realize uma consulta primeiro.\n"
			} else {
				err := os.WriteFile(targetPath, []byte(toSave), 0600)
				if err != nil {
					msg = fmt.Sprintf("\n\n⚠️ **Erro ao salvar arquivo em `%s`:** %v\n", targetPath, err)
				} else {
					msg = fmt.Sprintf("\n\n💾 **Diagnóstico salvo com sucesso em:**\n`%s`\n", targetPath)
				}
			}
			w.Dispatch(func() {
				chunkJSON, _ := json.Marshal(msg)
				w.Eval(fmt.Sprintf("appendResponseChunk(%s);", string(chunkJSON)))
			})
			return
		}

		isAuto := auto ||
			strings.HasPrefix(cleanQuery, "/auto") ||
			strings.HasPrefix(cleanQuery, "/resolver") ||
			strings.HasPrefix(cleanQuery, "/fix") ||
			strings.HasPrefix(cleanQuery, "/corrigir")

		if strings.HasPrefix(cleanQuery, "/retry") || strings.HasPrefix(cleanQuery, "/refazer") {
			chatMu.Lock()
			prevQuery := lastCleanQuery
			prevIsAuto := lastIsAuto
			chatMu.Unlock()

			if prevQuery != "" {
				cleanQuery = prevQuery
				isAuto = prevIsAuto
			} else {
				cleanQuery = "Analise o terminal e ajude a resolver o erro atual:"
			}
		} else {
			chatMu.Lock()
			lastCleanQuery = cleanQuery
			lastIsAuto = isAuto
			chatMu.Unlock()
		}

		firstWord := cleanQuery
		if idx := strings.IndexByte(cleanQuery, ' '); idx != -1 {
			firstWord = cleanQuery[:idx]
		}
		// Somente gatilhos de sincronização quando forem a PRIMEIRA palavra exata
		// (evita que "/status", "/software" etc. sejam tratados como "/s")
		isSync := firstWord == "/s" || firstWord == "/sync" || firstWord == "/tela"

		if isSync {
			mu.Lock()
			screenBuf = strings.TrimSpace(km.GetScreenBuffer(lines))
			selBuf = ""
			hasSelection = false
			screenLines = len(strings.Split(screenBuf, "\n"))
			selLines = 0
			mu.Unlock()

			// Remove prefixo /s, /sync ou /tela da query (somente como primeira palavra exata)
			for _, prefix := range []string{"/sync", "/s", "/tela"} {
				if cleanQuery == prefix {
					cleanQuery = ""
					break
				}
				if strings.HasPrefix(cleanQuery, prefix+" ") {
					cleanQuery = strings.TrimSpace(cleanQuery[len(prefix):])
					break
				}
			}
			if cleanQuery == "" {
				cleanQuery = "Analise a tela atualizada do terminal acima e me diga o que fazer:"
			}
		}

		// 1. MODO AUTÔNOMO: conectado diretamente ao terminal Kitty original
		if isAuto {
			// Remove comandos de gatilho do objetivo
			goal := cleanQuery
			for _, prefix := range []string{"/auto", "/resolver", "/fix", "/corrigir"} {
				if strings.HasPrefix(goal, prefix) {
					goal = strings.TrimSpace(strings.TrimPrefix(goal, prefix))
					break
				}
			}

			mu.Lock()
			autoSel := strings.TrimSpace(km.GetSelectionBuffer())
			if autoSel == "" && selBuf != "" {
				autoSel = selBuf
			}
			selBuf = ""
			hasSelection = false
			selLines = 0
			mu.Unlock()

			if autoSel != "" {
				sLines := len(strings.Split(autoSel, "\n"))
				if goal == "" {
					goal = fmt.Sprintf("Investigar a causa do erro selecionado com o mouse no terminal (%d linhas):\n%s\nAplicar as correções necessárias e resolver o problema.", sLines, autoSel)
				} else {
					goal = fmt.Sprintf("%s\n\n[Trecho Selecionado com o Mouse no Terminal (%d linhas)]:\n%s", goal, sLines, autoSel)
				}
				km.ClearSelection()
			}

			// Timeout mais longo para o ciclo multi-step do agente autônomo
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
			chatMu.Lock()
			cancelFunc = cancel
			chatMu.Unlock()

			go func() {
				defer func() {
					chatMu.Lock()
					cancelFunc = nil
					chatMu.Unlock()
				}()

				w.Dispatch(func() {
					var extraSelNotice string
					if autoSel != "" {
						extraSelNotice = fmt.Sprintf(" com foco nas **%d linhas selecionadas**", len(strings.Split(autoSel, "\n")))
					}
					startNotice, _ := json.Marshal(fmt.Sprintf("🚀 **Modo Autônomo Ativado**%s (Limite: %d passos | Buffer: %d linhas): Enviando e inspecionando ações diretamente no terminal Kitty de trabalho...\n\n", extraSelNotice, cfg.GetMaxSteps(), lines))
					w.Eval(fmt.Sprintf("appendResponseChunk(%s);", string(startNotice)))
				})

				onProgress := func(step, maxSteps int, command, terminalOutput, statusInfo string, isFinished bool) {
					w.Dispatch(func() {
						var stepMsg string
						if command != "" {
							stepMsg = fmt.Sprintf("\n### ⚡ [Passo %d/%d - Terminal Kitty]\n```bash\n%s\n```\n> **Status:** %s\n", step, maxSteps, command, statusInfo)
						} else {
							stepMsg = fmt.Sprintf("\n*Passo %d/%d: %s*\n", step, maxSteps, statusInfo)
						}
						chunkJSON, _ := json.Marshal(stepMsg)
						w.Eval(fmt.Sprintf("appendResponseChunk(%s);", string(chunkJSON)))
					})
				}

				onConfirm := func(command, reason string) bool {
					confirmMu.Lock()
					ch := make(chan bool, 1)
					confirmChan = ch
					confirmMu.Unlock()

					w.Dispatch(func() {
						cmdJSON, _ := json.Marshal(command)
						reasonJSON, _ := json.Marshal(reason)
						w.Eval(fmt.Sprintf("showConfirmModal(%s, %s);", string(cmdJSON), string(reasonJSON)))
					})

					select {
					case allowed := <-ch:
						return allowed
					case <-ctx.Done():
						return false
					}
				}

				onStepLimit := func(currentSteps, maxSteps int) (int, bool) {
					stepLimitMu.Lock()
					ch := make(chan stepLimitResponse, 1)
					stepLimitChan = ch
					stepLimitMu.Unlock()

					w.Dispatch(func() {
						w.Eval(fmt.Sprintf("showStepLimitModal(%d, %d);", currentSteps, maxSteps))
					})

					select {
					case resp := <-ch:
						return resp.moreSteps, resp.proceed
					case <-ctx.Done():
						return 0, false
					}
				}

				finalReport, err := agent.RunAutonomousAgent(ctx, currentProv, km, cfg, goal, lines, onProgress, onConfirm, onStepLimit)
				w.Dispatch(func() {
					if err != nil {
						if ctx.Err() == context.Canceled {
							w.Eval(fmt.Sprintf("appendResponseChunk(%q);", "\n\n⚠️ Operação autônoma cancelada pelo usuário."))
						} else {
							w.Eval(fmt.Sprintf("appendResponseChunk(%q);", fmt.Sprintf("\n\n### ⚠️ Erro no Modo Autônomo\n%v", err)))
						}
					} else {
						chatMu.Lock()
						lastFullResponse = finalReport
						chatMu.Unlock()
						reportJSON, _ := json.Marshal("\n\n" + finalReport)
						w.Eval(fmt.Sprintf("appendResponseChunk(%s);", string(reportJSON)))
					}
					w.Eval("onStreamFinished();")
				})
			}()

			return
		}

		// 2. MODO CHAT / CONSULTA COM HISTÓRICO MULTI-TURN
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		chatMu.Lock()
		cancelFunc = cancel
		curHistory := conversationHistory
		chatMu.Unlock()

		sysPrompt := agent.SystemPrompt(false)

		var promptToSend string
		if curHistory == "" {
			var fullPrompt strings.Builder
			lastCmd, lastExit, _ := km.GetLastCommandStatus()
			if lastCmd != "" {
				statusDesc := "0 (Sucesso)"
				if lastExit != "" && lastExit != "0" {
					statusDesc = lastExit + " (Falha / Erro)"
				}
				fullPrompt.WriteString(fmt.Sprintf("[Último Comando Executado no Terminal]: %s\n[Código de Retorno / Exit Code]: %s\n\n", lastCmd, statusDesc))
			}

			mu.Lock()
			// Captura seleção fresca em tempo real se o usuário acabou de selecionar (a menos que tenha especificado linhas na query)
			latestSel := strings.TrimSpace(km.GetSelectionBuffer())
			if latestSel != "" && !hasExplicitLines {
				selBuf = latestSel
				hasSelection = true
				selLines = len(strings.Split(selBuf, "\n"))
			}
			curSelBuf := selBuf
			curScreenBuf := screenBuf
			curHasSel := hasSelection
			curSelLines := selLines
			curScreenLines := screenLines
			mu.Unlock()

			if curHasSel && curSelBuf != "" {
				fullPrompt.WriteString(fmt.Sprintf("[Terminal do Usuário / Seleção do Mouse (%d linhas)]:\n%s\n\n", curSelLines, curSelBuf))
				km.ClearSelection()
				mu.Lock()
				selBuf = ""
				hasSelection = false
				selLines = 0
				mu.Unlock()
			} else if curScreenBuf != "" {
				fullPrompt.WriteString(fmt.Sprintf("[Terminal do Usuário / Últimas %d Linhas]:\n%s\n\n", curScreenLines, curScreenBuf))
			}

			isDefaultDiag := cleanQuery == "Diagnosticar e analisar a tela recente do terminal" ||
				cleanQuery == "Analise a tela atualizada do terminal acima e me diga o que fazer:" ||
				cleanQuery == "Analisar o trecho selecionado no terminal com o mouse" ||
				cleanQuery == ""

			if cleanQuery != "" {
				if isDefaultDiag && lastCmd == "" && !curHasSel && !hasErrorIndicators(curScreenBuf) {
					fullPrompt.WriteString("\n[Situação da Tela]: O terminal está em um prompt inicial limpo sem comandos recentes executados e sem erros.\n[Instrução]: Apresente o diagnóstico confirmando com clareza que o terminal está limpo e sem falhas nesta janela, e ofereça orientações sobre como executar comandos ou tirar dúvidas no chat.")
				} else {
					fullPrompt.WriteString(fmt.Sprintf("[Pergunta/Pedido do Usuário]: %s", cleanQuery))
				}
			} else {
				if !curHasSel && (curScreenBuf == "" || !hasErrorIndicators(curScreenBuf)) {
					fullPrompt.WriteString("[Instrução]: O terminal está em um prompt limpo sem erros ou comandos executados. Apresente o diagnóstico confirmando que o terminal está pronto para uso e explique amigavelmente como você pode ajudar.")
				} else {
					fullPrompt.WriteString("[Instrução]: Analise o terminal do usuário ou seleção do mouse acima. Explique o erro ou situação encontrada de forma didática e forneça os comandos para solucionar.")
				}
			}

			chatMu.Lock()
			conversationHistory = fmt.Sprintf("[Instruções]: %s\n\n%s", sysPrompt, fullPrompt.String())
			promptToSend = conversationHistory
			chatMu.Unlock()
		} else {
			var userEntry strings.Builder

			mu.Lock()
			latestSel := strings.TrimSpace(km.GetSelectionBuffer())
			if latestSel == "" && selBuf != "" {
				latestSel = selBuf
			}
			mu.Unlock()

			// Se o usuário explicitamente pediu /s ou linhas (-3), prioriza o terminal atualizado
			if isSync || hasExplicitLines {
				sLines := len(strings.Split(screenBuf, "\n"))
				userEntry.WriteString(fmt.Sprintf("[Terminal Atualizado do Usuário (últimas %d linhas)]:\n%s\n\n", sLines, screenBuf))
			} else if latestSel != "" {
				// Se o usuário selecionou um novo trecho no terminal com o mouse durante a conversa
				sLines := len(strings.Split(latestSel, "\n"))
				userEntry.WriteString(fmt.Sprintf("[Novo Trecho Selecionado pelo Usuário no Terminal (%d linhas)]:\n%s\n\n", sLines, latestSel))
				km.ClearSelection()
				mu.Lock()
				selBuf = ""
				hasSelection = false
				selLines = 0
				mu.Unlock()
			}

			if cleanQuery != "" {
				userEntry.WriteString(cleanQuery)
			} else if isSync || hasExplicitLines {
				userEntry.WriteString("Analise a tela atualizada do terminal acima e me diga o que fazer para corrigir:")
			} else if latestSel != "" {
				userEntry.WriteString("Analise o novo trecho selecionado no terminal acima:")
			} else {
				userEntry.WriteString("Analise a situação atual do terminal e me ajude a resolver:")
			}

			chatMu.Lock()
			conversationHistory += fmt.Sprintf("\n\n[Usuário]: %s", userEntry.String())
			conversationHistory = agent.LimitContextTurns(conversationHistory, cfg.MaxContextTurns)
			promptToSend = conversationHistory
			chatMu.Unlock()
		}

		ch := make(chan string, 20)
		var fullResp strings.Builder

		go func() {
			defer close(ch)
			err := currentProv.AskStream(ctx, "", promptToSend, ch)
			if err != nil {
				if ctx.Err() == context.DeadlineExceeded {
					ch <- fmt.Sprintf("\n\n### ⏱️ Timeout na Conexão com a IA\n> O provedor **%s** (%s) não respondeu dentro do limite de 60 segundos.\n\n**Possíveis Causas e Soluções:**\n- Instabilidade de conexão ou lentidão no servidor remoto.\n- Pressione **[3]** para usar a **IA Local (Ollama)** (100%% offline sem timeout) ou selecione outro modelo em **[4] APIs & Provedores**.", currentProv.Name(), currentProv.Model())
				} else if ctx.Err() != context.Canceled {
					ch <- fmt.Sprintf("\n\n### ⚠️ Falha na Conexão com a IA\n> Não foi possível obter resposta de **%s (%s)**.\n\n**Detalhes do Erro:**\n`%s`\n\n**Como Resolver:**\n1. Verifique se a chave de API para **%s** está configurada em `~/.config/metis/.env`.\n2. Verifique se o modelo selecionado está disponível no provedor.\n3. Pressione `[Esc]` para voltar ao dashboard e selecione outro provedor (ex: **Groq**, **OpenRouter** ou **Ollama Local**).", currentProv.Name(), currentProv.Model(), err.Error(), currentProv.Name())
				}
			}
		}()

		go func() {
			for chunk := range ch {
				fullResp.WriteString(chunk)
				chunkJSON, _ := json.Marshal(chunk)
				w.Dispatch(func() {
					w.Eval(fmt.Sprintf("appendResponseChunk(%s);", string(chunkJSON)))
				})
			}
			if fullResp.Len() > 0 {
				chatMu.Lock()
				lastFullResponse = fullResp.String()
				conversationHistory += fmt.Sprintf("\n\n[Assistente]: %s", fullResp.String())
				chatMu.Unlock()
			}
			chatMu.Lock()
			cancelFunc = nil
			chatMu.Unlock()
			w.Dispatch(func() {
				w.Eval("onStreamFinished();")
			})
		}()
	}

	w.Bind("startQuery", handleStartQuery)
	w.Bind("goStartQuery", handleStartQuery)
	w.Bind("startQueryBackend", handleStartQuery)
	w.Bind("clearConversation", func() {
		chatMu.Lock()
		conversationHistory = ""
		lastFullResponse = ""
		chatMu.Unlock()
	})
	w.Bind("saveReport", func(path string) {
		if path != "" {
			handleStartQuery("/salvar "+path, false)
		} else {
			handleStartQuery("/salvar", false)
		}
	})
	w.Bind("retryQuery", func() {
		handleStartQuery("/retry", false)
	})
	cancelQueryFn := func() {
		confirmMu.Lock()
		if confirmChan != nil {
			select {
			case confirmChan <- false:
			default:
			}
			confirmChan = nil
		}
		confirmMu.Unlock()

		chatMu.Lock()
		if cancelFunc != nil {
			cancelFunc()
			cancelFunc = nil
		}
		chatMu.Unlock()
	}
	w.Bind("cancelQuery", cancelQueryFn)
	w.Bind("goCancelQuery", cancelQueryFn)

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
		user = "user"
	}
	termName := kitty.DetectTerminalName(km)
	kittyPID := kitty.DetectTerminalPID(km)

	activeTheme := cfg.GetActiveTheme()
	windowOpacity = cfg.GetWindowOpacity()
	themeJSON, _ := json.Marshal(activeTheme)

	// Injeta diretamente os valores estáticos no HTML antes do carregamento da webview (garante exibição sem delay)
	htmlContent := strings.Replace(indexHTML, "id=\"diagAiPid\" style=\"color:var(--accent-cyan); font-family:monospace; font-weight:700;\">---</span>", fmt.Sprintf("id=\"diagAiPid\" style=\"color:var(--accent-cyan); font-family:monospace; font-weight:700;\">%s</span>", kittyPID), 1)
	htmlContent = strings.Replace(htmlContent, "id=\"diagAiTerm\" style=\"color:var(--accent-gold); font-weight:600;\" title=\"Terminal em uso\">---</span>", fmt.Sprintf("id=\"diagAiTerm\" style=\"color:var(--accent-gold); font-weight:600;\" title=\"Terminal em uso\">%s</span>", termName), 1)
	htmlContent = strings.Replace(htmlContent, "<span id=\"themeBadgeIcon\">🏛️</span>", fmt.Sprintf("<span id=\"themeBadgeIcon\">%s</span>", activeTheme.Icon), 1)
	htmlContent = strings.Replace(htmlContent, "<span id=\"themeBadgeText\">Metis Oracle</span>", fmt.Sprintf("<span id=\"themeBadgeText\">%s</span>", activeTheme.Name), 1)

	provListInitJSON, _ := json.Marshal(cfg.GetProvidersList())
	w.Init(fmt.Sprintf(`(function(){
		window.__INITIAL_THEME__ = %s;
		window.__INITIAL_OPACITY__ = %d;
		window.__INITIAL_PROVIDER__ = %q;
		window.__INITIAL_MODEL__ = %q;
		window.__INITIAL_USER__ = %q;
		window.__INITIAL_HOST__ = %q;
		window.__INITIAL_TERM__ = %q;
		window.__INITIAL_PID__ = %q;
		window.__INITIAL_PROVIDERS_LIST__ = %s;
	})();`, string(themeJSON), windowOpacity, prov.Name(), prov.Model(), user, host, termName, kittyPID, string(provListInitJSON)))

	w.SetHtml(htmlContent)

	// Inicializa os dados reais do buffer, tema e provedores no frontend
	w.Dispatch(func() {
		// 0. Informações da máquina local (Host / Usuário / PID da janela do terminal / Nome do terminal)
		w.Eval(fmt.Sprintf("setSystemHost(%q, %q, %q, %q, %q);", host, user, kittyPID, km.WindowID, termName))

		// 1. Aplica o Tema ativo do Metis
		w.Eval(fmt.Sprintf("setMetisTheme(%s, %d);", string(themeJSON), windowOpacity))

		allThemesJSON, _ := json.Marshal(config.MetisThemes)
		w.Eval(fmt.Sprintf("setThemesList(%s);", string(allThemesJSON)))

		// 2. Provedor e modelos
		w.Eval(fmt.Sprintf("setProvider(%q, %q, %q);", prov.Name(), prov.Model(), "testando..."))

		provListJSON, _ := json.Marshal(cfg.GetProvidersList())
		w.Eval(fmt.Sprintf("setProvidersList(%s);", string(provListJSON)))

		screenJSON, _ := json.Marshal(screenBuf)
		selJSON, _ := json.Marshal(selBuf)
		w.Eval(fmt.Sprintf("setSmartContext(%t, %d, %d, %s, %s);", hasSelection, selLines, screenLines, string(selJSON), string(screenJSON)))

		// 3. Dispara teste de conexão em tempo real
		testAndDispatchStatus(prov)

		if initialQuery != "" {
			w.Eval(fmt.Sprintf("startQuery(%q, %t);", initialQuery, strings.HasPrefix(initialQuery, "/auto")))
		}
	})

	w.Run()
	return nil
}

func hasErrorIndicators(text string) bool {
	low := strings.ToLower(text)
	indicators := []string{
		"error:", "error [", "err:", "failed", "failure", "fatal:",
		"command not found", "não encontrado", "permission denied",
		"permissão negada", "syntaxerror", "exception", "traceback",
		"panic:", "segfault", "core dumped", "connection refused",
		"could not resolve", "timed out", "timeout", "no such file",
		"unknown command", "is not recognized",
	}
	for _, ind := range indicators {
		if strings.Contains(low, ind) {
			return true
		}
	}
	return false
}
