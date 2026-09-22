package tui

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var (
	cachedKittyIcon string
	iconOnce        sync.Once
)

func isKittyTerminal() bool {
	return os.Getenv("TERM") == "xterm-kitty" ||
		os.Getenv("KITTY_WINDOW_ID") != "" ||
		os.Getenv("KITTY_PID") != "" ||
		os.Getenv("KITTY_LISTEN_ON") != ""
}

func GetKittyPNGIcon() string {
	iconOnce.Do(func() {
		if !isKittyTerminal() {
			return
		}

		home, _ := os.UserHomeDir()
		var paths []string
		if home != "" {
			paths = []string{
				filepath.Join(home, "Imagens", "Screenshots", "metis", "metis_icons", "metis_emoji_48x48.png"),
				filepath.Join(home, ".ZSH", "ai", "assets", "icons", "metis_emoji_48x48.png"),
				filepath.Join(home, "Metis", "assets", "icons", "metis_emoji_48x48.png"),
				filepath.Join(home, ".config", "metis", "icons", "metis_emoji_48x48.png"),
			}
		}

		var data []byte
		for _, p := range paths {
			if b, err := os.ReadFile(p); err == nil && len(b) > 0 {
				data = b
				break
			}
		}

		if len(data) > 0 {
			b64 := base64.StdEncoding.EncodeToString(data)
			// Kitty Graphics Protocol: renderiza o PNG original em 2 colunas
			cachedKittyIcon = fmt.Sprintf("\x1b_Ga=T,f=100,c=2,r=1,q=2;%s\x1b\\", b64)
		}
	})

	return cachedKittyIcon
}
