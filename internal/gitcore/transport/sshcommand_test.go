package transport

import (
	"errors"
	"slices"
	"testing"
)

func TestParseSSHCommand(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    SSHCommand
	}{
		{name: "empty", command: "  ", want: SSHCommand{}},
		{
			name:    "every supported flag",
			command: "ssh -i ~/.ssh/work -p 2222 -o StrictHostKeyChecking=no -oUser=git -F /tmp/cfg -l bob -J jump -v -T host",
			want: SSHCommand{
				ConfigFile: "/tmp/cfg",
				Overrides:  []string{"IdentityFile ~/.ssh/work", "Port 2222", "StrictHostKeyChecking=no", "User=git", "User bob", "ProxyJump jump"},
				Ignored:    []string{"-v", "-T", "host"},
			},
		},
		{
			name:    "quoted windows program and key with a space",
			command: `"C:\Program Files\OpenSSH\ssh.exe" -i "C:\keys\my key"`,
			want:    SSHCommand{Overrides: []string{`IdentityFile "C:\keys\my key"`}},
		},
		{
			name:    "single quotes and escaped spaces",
			command: "'ssh' -o 'IdentityFile=/a b' -i /path/with\\ space\t-p\n22",
			want:    SSHCommand{Overrides: []string{"IdentityFile=/a b", `IdentityFile "/path/with space"`, "Port 22"}},
		},
		{
			name:    "escaped quote inside double quotes",
			command: `ssh -o "User=\"q\"" -i key#1`,
			want:    SSHCommand{Overrides: []string{`User="q"`, `IdentityFile "key#1"`}},
		},
		{name: "another program", command: "plink -P 22", want: SSHCommand{Ignored: []string{"plink"}}},
		{name: "flag without a value", command: "ssh -i", want: SSHCommand{Ignored: []string{"-i"}}},
		{name: "trailing backslash", command: `ssh \`, want: SSHCommand{Ignored: []string{`\`}}},
		{name: "a lone dash", command: "ssh -", want: SSHCommand{Ignored: []string{"-"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSSHCommand(tt.command)
			if err != nil {
				t.Fatalf("ParseSSHCommand returned error %v", err)
			}
			if got.ConfigFile != tt.want.ConfigFile || !slices.Equal(got.Overrides, tt.want.Overrides) || !slices.Equal(got.Ignored, tt.want.Ignored) {
				t.Fatalf("ParseSSHCommand = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseSSHCommandRejectsUnterminatedQuotes(t *testing.T) {
	for _, command := range []string{"ssh -i 'key", `ssh -i "key`} {
		if _, err := ParseSSHCommand(command); !errors.Is(err, ErrSSHCommand) {
			t.Fatalf("ParseSSHCommand(%q) returned %v, want ErrSSHCommand", command, err)
		}
	}
}

func TestSSHCommandOverridesReadBackThroughTheConfigParser(t *testing.T) {
	parsed, err := ParseSSHCommand(`ssh -i "/keys/my key" -p 2200`)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := loadSSHConfig(SSHOptions{Overrides: parsed.Overrides})
	if err != nil {
		t.Fatalf("loadSSHConfig returned error %v", err)
	}
	settings := cfg.resolve("example.com")
	if settings.value("port") != "2200" || len(settings.identityFiles) != 1 || settings.identityFiles[0] != "/keys/my key" {
		t.Fatalf("settings = %+v", settings)
	}
}
