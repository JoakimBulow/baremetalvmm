package sshkey

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

const (
	PrivateKeyFile = "vmm_ed25519"
	PublicKeyFile  = "vmm_ed25519.pub"
)

func EnsureKeyPair(sshDir string) error {
	privPath := filepath.Join(sshDir, PrivateKeyFile)
	if _, err := os.Stat(privPath); err == nil {
		return nil
	}
	_, err := generateKeyPair(privPath, "vmm-managed-key")
	return err
}

// generateKeyPair writes a new Ed25519 key pair to privPath and privPath.pub,
// replacing any existing pair, and returns the public key in authorized_keys form.
func generateKeyPair(privPath, comment string) (string, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("failed to generate ed25519 key: %w", err)
	}

	privBytes, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return "", fmt.Errorf("failed to marshal private key: %w", err)
	}

	if err := os.WriteFile(privPath, pem.EncodeToMemory(privBytes), 0600); err != nil {
		return "", fmt.Errorf("failed to write private key: %w", err)
	}

	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", fmt.Errorf("failed to create SSH public key: %w", err)
	}

	pubKeyStr := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))) + " " + comment
	if err := os.WriteFile(privPath+".pub", []byte(pubKeyStr+"\n"), 0644); err != nil {
		return "", fmt.Errorf("failed to write public key: %w", err)
	}

	return pubKeyStr, nil
}

func GetPublicKey(sshDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(sshDir, PublicKeyFile))
	if err != nil {
		return "", fmt.Errorf("failed to read vmm public key: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

func PrivateKeyPath(sshDir string) string {
	return filepath.Join(sshDir, PrivateKeyFile)
}

// BuildAuthorizedKeys combines the vmm managed key with an optional user key.
func BuildAuthorizedKeys(sshDir, userKey string) (string, error) {
	vmmKey, err := GetPublicKey(sshDir)
	if err != nil {
		return "", err
	}

	keys := vmmKey
	if userKey = strings.TrimSpace(userKey); userKey != "" {
		keys += "\n" + userKey
	}
	return keys, nil
}

// ClusterKeyPath returns the private key path of a cluster's admin key, which
// lets the cluster's admin workstation SSH to the cluster nodes.
func ClusterKeyPath(sshDir, clusterName string) string {
	return filepath.Join(sshDir, "clusters", clusterName+"_ed25519")
}

// GenerateClusterKeyPair creates a fresh admin key pair for a cluster, replacing
// any left over from an earlier cluster of the same name, and returns the
// public key in authorized_keys form.
func GenerateClusterKeyPair(sshDir, clusterName string) (string, error) {
	privPath := ClusterKeyPath(sshDir, clusterName)
	if err := os.MkdirAll(filepath.Dir(privPath), 0700); err != nil {
		return "", fmt.Errorf("failed to create cluster key directory: %w", err)
	}
	return generateKeyPair(privPath, "vmm-cluster-"+clusterName+"-admin")
}

// RemoveClusterKeyPair deletes a cluster's admin key pair, if present.
func RemoveClusterKeyPair(sshDir, clusterName string) error {
	privPath := ClusterKeyPath(sshDir, clusterName)
	for _, p := range []string{privPath, privPath + ".pub"} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// JoinKeys combines authorized_keys entries, skipping empty ones.
func JoinKeys(keys ...string) string {
	var parts []string
	for _, k := range keys {
		if k = strings.TrimSpace(k); k != "" {
			parts = append(parts, k)
		}
	}
	return strings.Join(parts, "\n")
}
