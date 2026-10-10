package sshkey

import (
	"os"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestGenerateClusterKeyPair(t *testing.T) {
	dir := t.TempDir()

	pub, err := GenerateClusterKeyPair(dir, "demo")
	if err != nil {
		t.Fatalf("GenerateClusterKeyPair: %v", err)
	}
	if !strings.HasSuffix(pub, " vmm-cluster-demo-admin") {
		t.Errorf("unexpected key comment: %q", pub)
	}

	privPath := ClusterKeyPath(dir, "demo")
	info, err := os.Stat(privPath)
	if err != nil {
		t.Fatalf("private key not written: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("private key mode = %o, want 600", perm)
	}

	// The private key must match the returned public key.
	data, err := os.ReadFile(privPath)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		t.Fatalf("parse private key: %v", err)
	}
	wantPub := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	if !strings.HasPrefix(pub, wantPub+" ") {
		t.Errorf("public key %q does not match private key %q", pub, wantPub)
	}

	// Regenerating replaces the old pair.
	pub2, err := GenerateClusterKeyPair(dir, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if pub2 == pub {
		t.Error("expected a fresh key pair on regeneration")
	}

	if err := RemoveClusterKeyPair(dir, "demo"); err != nil {
		t.Fatalf("RemoveClusterKeyPair: %v", err)
	}
	for _, p := range []string{privPath, privPath + ".pub"} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s still exists after removal", p)
		}
	}
	// Removing again is not an error.
	if err := RemoveClusterKeyPair(dir, "demo"); err != nil {
		t.Errorf("second RemoveClusterKeyPair: %v", err)
	}
}

func TestJoinKeys(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"", ""}, ""},
		{[]string{"a", ""}, "a"},
		{[]string{"", "b"}, "b"},
		{[]string{"a\n", "  b  "}, "a\nb"},
	}
	for _, c := range cases {
		if got := JoinKeys(c.in...); got != c.want {
			t.Errorf("JoinKeys(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
