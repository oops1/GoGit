package transport

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/config"
)

const (
	protocolPolicyAlways = "always"
	protocolPolicyNever  = "never"
	protocolPolicyUser   = "user"
)

func ProtocolAllowed(cfg *config.Config, scheme Scheme) error {
	name := string(scheme)
	if list, ok := os.LookupEnv("GIT_ALLOW_PROTOCOL"); ok {
		if slices.Contains(strings.Split(list, ":"), name) {
			return nil
		}
		return fmt.Errorf("%w: %s", ErrProtocolNotAllowed, name)
	}
	switch policy := protocolPolicy(cfg, name); policy {
	case protocolPolicyAlways:
		return nil
	case protocolPolicyUser:
		if protocolFromUser() {
			return nil
		}
	case protocolPolicyNever:
	default:
		return fmt.Errorf("%w: unknown policy %q for %s", ErrProtocolNotAllowed, policy, name)
	}
	return fmt.Errorf("%w: %s", ErrProtocolNotAllowed, name)
}

func protocolPolicy(cfg *config.Config, name string) string {
	if cfg != nil {
		for _, key := range []string{"protocol." + name + ".allow", "protocol.allow"} {
			if value, ok := cfg.Get(key); ok {
				return value
			}
		}
	}
	switch name {
	case string(SchemeHTTP), string(SchemeHTTPS), string(SchemeGit), string(SchemeSSH):
		return protocolPolicyAlways
	case "ext":
		return protocolPolicyNever
	}
	return protocolPolicyUser
}

func protocolFromUser() bool {
	value, ok := os.LookupEnv("GIT_PROTOCOL_FROM_USER")
	if !ok {
		return true
	}
	allowed, err := config.ParseBool(value)
	return err == nil && allowed
}
