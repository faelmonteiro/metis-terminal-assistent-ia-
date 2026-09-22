package security

import "testing"

func TestIsSensitivePath(t *testing.T) {
	cases := []struct {
		path      string
		sensitive bool
	}{
		{"/etc/shadow", true},
		{"/etc/passwd", true},
		{"/root/.bashrc", true},
		{"~/.ssh/id_rsa", true},
		{"~/.config/claude/config.json", true},
		{"/tmp/normal_file.txt", false},
		{"/home/reator/Documents/code.py", false},
	}

	for _, c := range cases {
		got := IsSensitivePath(c.path)
		if got != c.sensitive {
			t.Errorf("IsSensitivePath(%q) = %v; esperado %v", c.path, got, c.sensitive)
		}
	}
}

func TestIsDangerousCommand(t *testing.T) {
	cases := []struct {
		cmd       string
		dangerous bool
	}{
		{"rm -rf /", true},
		{"rm -rf /*", true},
		{"rm -r -f /", true},
		{"rm -f -r /", true},
		{"rm -rf ~", true},
		{"mkfs.ext4 /dev/sda1", true},
		{"dd if=/dev/zero of=/dev/sda", true},
		{":(){ :|:& };:", true},
		{"reboot", true},
		{"ls -la", false},
		{"git status", false},
		{"cat /tmp/test.txt", false},
	}

	for _, c := range cases {
		got, _ := IsDangerousCommand(c.cmd)
		if got != c.dangerous {
			t.Errorf("IsDangerousCommand(%q) = %v; esperado %v", c.cmd, got, c.dangerous)
		}
	}
}

func TestIsRiskyAutoCommand(t *testing.T) {
	cases := []struct {
		cmd   string
		risky bool
	}{
		{"rm file.txt", true},
		{"dd if=/dev/zero of=file.bin", true},
		{"kill 1234", true},
		{"git add file.go", false}, // não deve ter falso positivo com "dd "
		{"sync", false},            // não deve ter falso positivo com "nc "
		{"async_test", false},
		{"ls -la", false},
	}

	for _, c := range cases {
		got, _ := IsRiskyAutoCommand(c.cmd)
		if got != c.risky {
			t.Errorf("IsRiskyAutoCommand(%q) = %v; esperado %v", c.cmd, got, c.risky)
		}
	}
}

func TestIsSafeReadCommand(t *testing.T) {
	cases := []struct {
		cmd  string
		safe bool
	}{
		{"cat /tmp/test.txt", true},
		{"ls -la /home", true},
		{"head -n 5 log.txt", true},
		{"cat /etc/passwd", false},            // bloqueado por caminho sensível
		{"head ~/.config/claude/keys", false}, // bloqueado por credencial
		{"cat ~/.ssh/id_rsa", false},
		{"cat .env", false},
		{"rm file.txt", false},
	}

	for _, c := range cases {
		got := IsSafeReadCommand(c.cmd)
		if got != c.safe {
			t.Errorf("IsSafeReadCommand(%q) = %v; esperado %v", c.cmd, got, c.safe)
		}
	}
}
