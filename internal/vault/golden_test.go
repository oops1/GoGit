package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/chacha20poly1305"
)

func goldenPassword() []byte {
	return []byte("correct horse battery staple")
}

func goldenSlotParams() SlotParams {
	return SlotParams{Time: 1, Memory: 8192, Threads: 1}
}

func goldenDEK() []byte {
	dek := make([]byte, chacha20poly1305.KeySize)
	for i := range dek {
		dek[i] = byte(i + 1)
	}
	return dek
}

func goldenSalt() []byte {
	salt := make([]byte, passwordSaltSize)
	for i := range salt {
		salt[i] = byte(i + 10)
	}
	return salt
}

func goldenSlotNonce() []byte {
	nonce := make([]byte, slotNonceSize)
	for i := range nonce {
		nonce[i] = byte(i + 50)
	}
	return nonce
}

func goldenPayloadNonce() []byte {
	nonce := make([]byte, payloadNonceSize)
	for i := range nonce {
		nonce[i] = byte(i + 150)
	}
	return nonce
}

func goldenPayloadJSON(t *testing.T) []byte {
	t.Helper()
	p := payload{
		Credentials: []Credential{{Resource: "git.example.com", Username: "golden", Secret: []byte("golden-secret")}},
		SSHKeys:     []SSHKey{{Host: "git.example.com", Path: "id_ed25519", Passphrase: []byte("phrase"), Private: []byte("private-key-bytes")}},
	}
	data, err := json.Marshal(&p)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func buildGoldenVaultFile(t *testing.T, version uint16, generation uint64) []byte {
	t.Helper()
	salt := goldenSalt()
	kek := deriveKey(goldenPassword(), salt, goldenSlotParams())
	slotAEAD, err := newXChaCha20Poly1305(kek)
	if err != nil {
		t.Fatal(err)
	}
	dek := goldenDEK()
	wrapped := slotAEAD.Seal(nil, goldenSlotNonce(), dek, slotAAD(SlotPassword, salt))
	slot := Slot{Kind: SlotPassword, Salt: salt, Nonce: goldenSlotNonce(), Wrapped: wrapped, Params: goldenSlotParams()}
	h := header{version: version, generation: generation, slots: []Slot{slot}}
	headerRaw, err := encodeHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	payloadAEAD, err := newXChaCha20Poly1305(dek)
	if err != nil {
		t.Fatal(err)
	}
	payloadNonce := goldenPayloadNonce()
	ciphertext := payloadAEAD.Seal(nil, payloadNonce, goldenPayloadJSON(t), headerRaw)
	data := make([]byte, 0, len(headerRaw)+len(payloadNonce)+len(ciphertext))
	data = append(data, headerRaw...)
	data = append(data, payloadNonce...)
	data = append(data, ciphertext...)
	return data
}

func compareOrUpdateGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("GOGIT_VAULT_UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s drifted from golden file\ngot:  %x\nwant: %x", name, got, want)
	}
}

func writeGoldenVault(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vault.bin")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUnnumberedVaultFileFormatGoldenUpgradesOnWrite(t *testing.T) {
	data := buildGoldenVaultFile(t, formatVersionUnnumbered, 0)
	compareOrUpdateGolden(t, "vault_v1_unnumbered.golden", data)
	path := writeGoldenVault(t, data)

	v, err := Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Unlock(context.Background(), NewPasswordUnlocker(goldenPassword(), goldenSlotParams())); err != nil {
		t.Fatal(err)
	}
	got, ok := v.Credential("git.example.com")
	if !ok || got.Username != "golden" || string(got.Secret) != "golden-secret" {
		t.Fatalf("credential = %+v ok=%v", got, ok)
	}
	if v.Generation() != 0 {
		t.Fatalf("generation = %d, want 0 before any write", v.Generation())
	}

	if err := v.SetCredential(Credential{Resource: "new.example.com", Secret: []byte("s")}); err != nil {
		t.Fatal(err)
	}

	upgraded, err := parseVaultFile(readFileBytes(t, path))
	if err != nil {
		t.Fatal(err)
	}
	if upgraded.header.version != formatVersion || upgraded.header.generation != 1 {
		t.Fatalf("header = %+v, want version %d generation 1", upgraded.header, formatVersion)
	}
	if len(upgraded.header.slots) != 1 || upgraded.header.slots[0].Kind != SlotPassword {
		t.Fatalf("slots = %+v", upgraded.header.slots)
	}

	reopened, err := Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Unlock(context.Background(), NewPasswordUnlocker(goldenPassword(), goldenSlotParams())); err != nil {
		t.Fatal(err)
	}
	if got, ok := reopened.Credential("git.example.com"); !ok || string(got.Secret) != "golden-secret" {
		t.Fatalf("original credential lost after upgrade: %+v ok=%v", got, ok)
	}
	if got, ok := reopened.Credential("new.example.com"); !ok || string(got.Secret) != "s" {
		t.Fatalf("new credential missing after upgrade: %+v ok=%v", got, ok)
	}
}

func TestVaultFileFormatV2Golden(t *testing.T) {
	data := buildGoldenVaultFile(t, formatVersion, 3)
	compareOrUpdateGolden(t, "vault_v2.golden", data)
	path := writeGoldenVault(t, data)

	v, err := Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if v.Generation() != 3 {
		t.Fatalf("generation = %d, want 3", v.Generation())
	}
	if err := v.Unlock(context.Background(), NewPasswordUnlocker(goldenPassword(), goldenSlotParams())); err != nil {
		t.Fatal(err)
	}
	got, ok := v.SSHKey("git.example.com")
	if !ok || got.Path != "id_ed25519" || string(got.Passphrase) != "phrase" || string(got.Private) != "private-key-bytes" {
		t.Fatalf("ssh key = %+v ok=%v", got, ok)
	}
	cred, ok := v.Credential("git.example.com")
	if !ok || cred.Username != "golden" || string(cred.Secret) != "golden-secret" {
		t.Fatalf("credential = %+v ok=%v", cred, ok)
	}
}
