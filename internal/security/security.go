package security

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var dangerousCmdPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bmkfs(\.[a-z0-9]+)?\b`),
	regexp.MustCompile(`(?i)\bdd\s+.*of=/dev/`),
	regexp.MustCompile(`(?i)>\s*/dev/sd[a-z]`),
	regexp.MustCompile(`(?i)\bchmod\s+(-R\s+)?(777|000)\s+([~/]|\$HOME)($|\s)`),
	regexp.MustCompile(`(?i)\b(reboot|shutdown|poweroff|init\s+[06])\b`),
	regexp.MustCompile(`:\(\)\s*\{\s*:\|:&\s*\};\s*:`), // Fork bomb
	regexp.MustCompile(`(?i)\bmv\s+.*(/dev/null|/etc/passwd)\b`),
	regexp.MustCompile(`(?i)\bgit\s+(reset\s+--hard|clean\s+-[a-z]*f|push\s+--force|push\s+-f)\b`),
	regexp.MustCompile(`(?i)\b(wipefs|fdisk|parted|umount|swapoff)\b`),
}

var riskyAutoPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(rm|mkfs|dd|shred|chown)\b`),
	regexp.MustCompile(`(?i)\bchmod\s+(-[a-z0-9_-]*R[a-z0-9_-]*|777|000)\b`),
	regexp.MustCompile(`(?i)\b(kill|killall|pkill)\b`),
	regexp.MustCompile(`(?i)\b(reboot|shutdown|poweroff|init\s+[06])\b`),
	regexp.MustCompile(`(?i)\bgit\s+(reset\s+--hard|push\s+--force|push\s+-f|clean\s+-[a-z0-9_-]*f)\b`),
	regexp.MustCompile(`(?i)\bsudo\s+(rm|dd|mkfs|shred)\b`),
	regexp.MustCompile(`(?i)>\s*/dev/`),
	regexp.MustCompile(`(?i)\bdrop\s+(database|table)\b`),
	regexp.MustCompile(`(?i)\bfind\s+.*(-delete|-exec|-execdir)\b`),
	regexp.MustCompile(`(?i)\b(nc|ncat|ssh|scp|rsync)\b`),
	regexp.MustCompile(`(?i)\|\s*(sh|bash|zsh)\b`),
	regexp.MustCompile(`(?i)\b(systemctl\s+(stop|disable)|service\s+\S+\s+stop)\b`),
	regexp.MustCompile(`(?i)\b(iptables\s+-F|ufw\s+disable)\b`),
	regexp.MustCompile(`(?i)\bcurl\b[^\n]*?(-O\b|--output\b|-o\b|--data\b|-d\b|-X\s*(POST|PUT|DELETE)\b)`),
	regexp.MustCompile(`(?i)\bwget\b[^\n]*?(-O\b|-qO\b)`),
}

// IsSensitivePath verifica se o caminho afeta diretórios críticos do sistema ou credenciais.
func IsSensitivePath(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}

	home, _ := os.UserHomeDir()
	cleanPath := filepath.Clean(path)
	if home != "" && strings.HasPrefix(cleanPath, "~/") {
		cleanPath = filepath.Join(home, cleanPath[2:])
	} else if !filepath.IsAbs(cleanPath) {
		cwd, _ := os.Getwd()
		cleanPath = filepath.Join(cwd, cleanPath)
	}

	// Tenta resolver links simbólicos se existirem
	if realPath, err := filepath.EvalSymlinks(cleanPath); err == nil {
		cleanPath = realPath
	}

	// Diretórios do sistema operacional
	sysPrefixes := []string{
		"/etc", "/boot", "/sys", "/proc", "/dev",
		"/usr/bin", "/usr/sbin", "/bin", "/sbin", "/lib", "/root",
	}
	for _, p := range sysPrefixes {
		if cleanPath == p || strings.HasPrefix(cleanPath, p+"/") {
			return true
		}
	}

	// Protege arquivos .env do Metis (inclusive em containers sem $HOME)
	base := filepath.Base(cleanPath)
	if strings.HasPrefix(base, ".env") {
		return true
	}

	// Arquivos de chaves e segredos do usuário
	if home != "" {
		sensitiveUserPaths := []string{
			filepath.Join(home, ".ssh"),
			filepath.Join(home, ".gnupg"),
			filepath.Join(home, ".aws"),
			filepath.Join(home, ".config", "claude"),
		}
		for _, p := range sensitiveUserPaths {
			if cleanPath == p || strings.HasPrefix(cleanPath, p+"/") {
				return true
			}
		}
	}

	return false
}

// IsDangerousCommand avalia se a linha de comando contém comandos destrutivos.
// isDestructiveRM detecta `rm` recursivo + forçado (ou --no-preserve-root) atingindo
// destinos de alto risco: absolutos (/...), home (~, ~/), $HOME, globs (*) ou . / ..
func isDestructiveRM(cmd string) bool {
	fields := strings.Fields(cmd)
	for i, tok := range fields {
		if tok != "rm" {
			continue
		}

		hasR, hasF, preserveRoot := false, false, false
		j := i + 1
		for ; j < len(fields); j++ {
			arg := fields[j]
			if arg == "--" {
				j++
				break
			}
			if strings.HasPrefix(arg, "--") {
				switch arg {
				case "--recursive":
					hasR = true
				case "--force":
					hasF = true
				case "--no-preserve-root":
					preserveRoot = true
				}
				continue
			}
			if strings.HasPrefix(arg, "-") && len(arg) > 1 {
				for _, ch := range arg[1:] {
					if ch == 'r' {
						hasR = true
					} else if ch == 'f' {
						hasF = true
					}
				}
				continue
			}
			break
		}

		if !(preserveRoot || (hasR && hasF)) {
			continue
		}

		for ; j < len(fields); j++ {
			t := fields[j]
			switch {
			case t == "~" || strings.HasPrefix(t, "~/"):
				return true
			case strings.HasPrefix(t, "/"):
				return true
			case strings.HasPrefix(t, "$HOME") || strings.HasPrefix(t, "${HOME}"):
				return true
			case strings.Contains(t, "*"):
				return true
			case t == "." || t == ".." || strings.HasPrefix(t, "./") || strings.HasPrefix(t, "../"):
				return true
			}
		}
		return false
	}
	return false
}

func IsDangerousCommand(cmd string) (bool, string) {
	trimmed := strings.TrimSpace(cmd)
	if isDestructiveRM(trimmed) {
		return true, fmt.Sprintf("Comando rm recursivo/forçado sobre destino de alto risco %q", trimmed)
	}
	for _, pattern := range dangerousCmdPatterns {
		if pattern.MatchString(trimmed) {
			return true, fmt.Sprintf("Comando potencialmente destrutivo detectado: %s", trimmed)
		}
	}
	return false, ""
}

// IsRiskyAutoCommand verifica se um comando deve exigir confirmação estrita no modo /auto
func IsRiskyAutoCommand(cmd string) (bool, string) {
	trimmed := strings.TrimSpace(cmd)
	if trimmed == "" {
		return false, ""
	}

	// Checa comandos perigosos clássicos
	if dangerous, reason := IsDangerousCommand(trimmed); dangerous {
		return true, reason
	}

	// Checa padrões de risco com fronteiras de palavra (evita falsos positivos como git add ou sync)
	for _, p := range riskyAutoPatterns {
		if p.MatchString(trimmed) {
			return true, fmt.Sprintf("Comando com potencial de alteração do sistema: %s", p.String())
		}
	}

	return false, ""
}

// IsSafeReadCommand verifica se o comando é puramente de inspeção/leitura
func IsSafeReadCommand(cmd string) bool {
	trimmed := strings.TrimSpace(cmd)
	if trimmed == "" {
		return false
	}

	// Remove comentários de fim de linha
	if idx := strings.Index(trimmed, "#"); idx != -1 {
		trimmed = strings.TrimSpace(trimmed[:idx])
	}
	if trimmed == "" {
		return false
	}

	// Multilinha não é safe read
	if strings.Contains(trimmed, "\n") {
		return false
	}

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

	// Redirecionamento além de /dev/null
	if strings.Contains(trimmed, ">") && !strings.Contains(trimmed, ">/dev/null") && !strings.Contains(trimmed, "> /dev/null") {
		return false
	}

	// Arquivos sensíveis
	sensitive := []string{".ssh", ".aws", ".gnupg", ".env", "shadow", "sudoers", "/root", "/etc"}
	for _, s := range sensitive {
		if strings.Contains(trimmed, s) {
			return false
		}
	}

	// Lista de binários seguros
	safeBins := map[string]bool{
		"ss": true, "netstat": true, "ip": true, "ifconfig": true, "ping": true, "traceroute": true,
		"host": true, "dig": true, "nslookup": true, "route": true, "ps": true, "pgrep": true,
		"pidof": true, "uptime": true, "whoami": true, "id": true, "uname": true, "hostname": true,
		"df": true, "du": true, "free": true, "lsblk": true, "lscpu": true, "lshw": true,
		"lspci": true, "lsusb": true, "inxi": true, "dmesg": true, "journalctl": true, "which": true,
		"whereis": true, "type": true, "file": true, "stat": true, "cat": true, "head": true,
		"tail": true, "grep": true, "egrep": true, "fgrep": true, "awk": true, "cut": true,
		"wc": true, "sort": true, "uniq": true, "tr": true, "sed": true, "column": true,
		"echo": true, "printf": true, "ls": true, "eza": true, "tree": true,
	}

	// Divide pipelines (&&, ||, ;, |)
	replacer := strings.NewReplacer("&&", "|", "||", "|", ";", "|")
	normalized := replacer.Replace(trimmed)
	segments := strings.Split(normalized, "|")

	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			return false
		}
		words := strings.Fields(seg)
		if len(words) == 0 {
			return false
		}
		bin := filepath.Base(words[0])
		if bin == "systemctl" {
			if len(words) < 2 {
				return false
			}
			sub := words[1]
			switch sub {
			case "status", "is-active", "is-enabled", "is-failed", "list-units", "list-unit-files":
				continue
			default:
				return false
			}
		}
		if bin == "find" {
			if strings.Contains(seg, "-delete") || strings.Contains(seg, "-exec") {
				return false
			}
		}
		if !safeBins[bin] {
			return false
		}

		for _, w := range words[1:] {
			cleanArg := strings.Trim(w, `"'`)
			if cleanArg == "" || strings.HasPrefix(cleanArg, "-") {
				continue
			}
			if IsSensitivePath(cleanArg) {
				return false
			}
		}
	}

	return true
}
