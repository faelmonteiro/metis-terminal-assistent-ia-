package tui

import (
	"encoding/base64"
	"fmt"
	"os"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestKittyIconWidth(t *testing.T) {
	iconPath := "/home/reator/Imagens/Screenshots/metis/metis_icons/metis_emoji_48x48.png"
	data, err := os.ReadFile(iconPath)
	if err != nil {
		t.Fatalf("Não conseguiu ler o arquivo de ícone: %v", err)
	}

	b64 := base64.StdEncoding.EncodeToString(data)
	kittyEsc := fmt.Sprintf("\x1b_Ga=T,f=100,c=2,r=1;%s\x1b\\ ", b64)

	t.Logf("Tamanho do base64: %d bytes", len(b64))
	w := lipgloss.Width(kittyEsc + "METIS")
	t.Logf("lipgloss.Width do texto com escape Kitty: %d", w)
}
