package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestRedactErrorKeepsNilAndIdentityButHidesSecrets(t *testing.T) {
	if redactError(nil) != nil {
		t.Fatal("nil must stay nil")
	}
	inner := fmt.Errorf("fetch https://bob:hunter2@example.com/repo.git: %w", context.Canceled)
	err := redactError(inner)
	if strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("secret leaked: %s", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatal("the wrapped error must stay detectable")
	}
	if shown := fmt.Sprintf("failed: %v", err); strings.Contains(shown, "hunter2") {
		t.Fatalf("secret leaked through formatting: %s", shown)
	}
}
