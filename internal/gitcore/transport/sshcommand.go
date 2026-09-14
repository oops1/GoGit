package transport

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

var ErrSSHCommand = errors.New("transport: invalid ssh command")

type SSHCommand struct {
	ConfigFile string
	Overrides  []string
	Ignored    []string
}

func ParseSSHCommand(command string) (SSHCommand, error) {
	words, err := splitShellWords(command)
	if err != nil {
		return SSHCommand{}, fmt.Errorf("%w: %w", ErrSSHCommand, err)
	}
	var parsed SSHCommand
	if len(words) == 0 {
		return parsed, nil
	}
	program := strings.ToLower(filepath.Base(strings.ReplaceAll(words[0], `\`, "/")))
	if strings.TrimSuffix(program, ".exe") != "ssh" {
		parsed.Ignored = append(parsed.Ignored, words[0])
		return parsed, nil
	}
	args := words[1:]
	for i := 0; i < len(args); i++ {
		arg := args[i]
		keyword, valued := sshFlagKeywords[flagName(arg)]
		if !valued {
			parsed.Ignored = append(parsed.Ignored, arg)
			continue
		}
		value := arg[2:]
		if value == "" {
			if i+1 == len(args) {
				parsed.Ignored = append(parsed.Ignored, arg)
				continue
			}
			i++
			value = args[i]
		}
		switch keyword {
		case "":
			parsed.ConfigFile = value
		case "-o":
			parsed.Overrides = append(parsed.Overrides, value)
		default:
			parsed.Overrides = append(parsed.Overrides, keyword+" "+quoteSSHConfigValue(value))
		}
	}
	return parsed, nil
}

var sshFlagKeywords = map[string]string{
	"-F": "",
	"-o": "-o",
	"-i": "IdentityFile",
	"-p": "Port",
	"-l": "User",
	"-J": "ProxyJump",
}

func flagName(arg string) string {
	if len(arg) < 2 || arg[0] != '-' {
		return arg
	}
	return arg[:2]
}

func quoteSSHConfigValue(value string) string {
	if strings.ContainsAny(value, " \t#") {
		return `"` + value + `"`
	}
	return value
}

func splitShellWords(command string) ([]string, error) {
	var words []string
	var current strings.Builder
	inWord := false
	for i := 0; i < len(command); i++ {
		c := command[i]
		switch {
		case c == '\'':
			end := strings.IndexByte(command[i+1:], '\'')
			if end < 0 {
				return nil, errUnterminatedQuote
			}
			current.WriteString(command[i+1 : i+1+end])
			i += end + 1
			inWord = true
		case c == '"':
			closed := false
			for i++; i < len(command); i++ {
				if command[i] == '"' {
					closed = true
					break
				}
				if command[i] == '\\' && i+1 < len(command) && strings.IndexByte(`"\$`+"`", command[i+1]) >= 0 {
					i++
				}
				current.WriteByte(command[i])
			}
			if !closed {
				return nil, errUnterminatedQuote
			}
			inWord = true
		case c == '\\' && i+1 < len(command):
			i++
			current.WriteByte(command[i])
			inWord = true
		case c == ' ' || c == '\t' || c == '\n':
			if inWord {
				words = append(words, current.String())
				current.Reset()
				inWord = false
			}
		default:
			current.WriteByte(c)
			inWord = true
		}
	}
	if inWord {
		words = append(words, current.String())
	}
	return words, nil
}
