package kitty

import (
	"testing"
)

func TestRemoveComments(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{
			input:    "ls -la # lista arquivos",
			expected: "ls -la",
		},
		{
			input:    `echo "teste # dentro de aspas" # comentario fora`,
			expected: `echo "teste # dentro de aspas"`,
		},
		{
			input:    `curl -s "http://example.com/#anchor"`,
			expected: `curl -s "http://example.com/#anchor"`,
		},
		{
			input:    "git status\ngit add . # adiciona tudo\ngit commit -m 'feat: teste'",
			expected: "git status\ngit add .\ngit commit -m 'feat: teste'",
		},
	}

	for _, c := range cases {
		got := RemoveComments(c.input)
		if got != c.expected {
			t.Errorf("RemoveComments(%q) = %q; esperado %q", c.input, got, c.expected)
		}
	}
}

func TestTailLines(t *testing.T) {
	text := "linha 1\nlinha 2\nlinha 3\nlinha 4\nlinha 5"
	tailed := TailLines(text, 2)
	expected := "linha 4\nlinha 5"
	if tailed != expected {
		t.Fatalf("TailLines esperado %q, obtido %q", expected, tailed)
	}
}

func TestDetect(t *testing.T) {
	km := Detect()
	t.Logf("DETECT -> Socket: %q, PID: %q, WindowID: %q", km.Socket, km.PID, km.WindowID)
}

func TestFormatTerminalName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"kitty", "Kitty"},
		{"alacritty", "Alacritty"},
		{"wezterm-gui", "WezTerm"},
		{"foot", "Foot"},
		{"ghostty", "Ghostty"},
		{"gnome-terminal-server", "GNOME Terminal"},
		{"ptyxis", "Ptyxis"},
		{"konsole", "Konsole"},
		{"xterm", "XTerm"},
	}

	for _, tc := range tests {
		if got := FormatTerminalName(tc.input); got != tc.expected {
			t.Errorf("FormatTerminalName(%q) = %q; expected %q", tc.input, got, tc.expected)
		}
	}
}

func TestDetectTerminalPID(t *testing.T) {
	km := &KittyManager{PID: "99999", WindowID: "1"}
	if pid := DetectTerminalPID(km); pid != "99999" {
		t.Errorf("DetectTerminalPID com km.PID=99999 esperava '99999', obteve %q", pid)
	}
}

func TestGetSelectionBufferIsolation(t *testing.T) {
	// Com um socket inexistente, não deve quebrar nem vazar dados
	km := &KittyManager{Socket: "unix:/tmp/fake-socket-99999", PID: "99999", WindowID: "1"}
	_ = km.GetSelectionBuffer()
}

func TestSendTextEmpty(t *testing.T) {
	km := &KittyManager{}
	err := km.SendText("   ", false)
	if err == nil {
		t.Error("esperava erro ao enviar comando vazio, obteve nil")
	}
}


