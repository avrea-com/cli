package commands

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"os"
	"path/filepath"
)

const buildKeyType = "ssh-ed25519"

func sshString(buffer []byte, value []byte) []byte {
	buffer = binary.BigEndian.AppendUint32(buffer, uint32(len(value))) //nolint:gosec // Key material is far below 4 GiB.
	return append(buffer, value...)
}

// buildKeyFiles encodes an ed25519 key as OpenSSH reads it: an unencrypted
// openssh-key-v1 private key file and the public key line.
func buildKeyFiles(public ed25519.PublicKey, private ed25519.PrivateKey, check uint32, comment string) (privateFile []byte, publicLine string) {
	blob := sshString(sshString(nil, []byte(buildKeyType)), public)
	secret := binary.BigEndian.AppendUint32(nil, check)
	secret = binary.BigEndian.AppendUint32(secret, check)
	secret = sshString(secret, []byte(buildKeyType))
	secret = sshString(secret, public)
	secret = sshString(secret, private)
	secret = sshString(secret, []byte(comment))
	// Without a cipher the block size is 8; padding counts up from 1.
	for pad := byte(1); len(secret)%8 != 0; pad++ {
		secret = append(secret, pad)
	}
	body := []byte("openssh-key-v1\x00")
	body = sshString(body, []byte("none")) // cipher
	body = sshString(body, []byte("none")) // KDF
	body = sshString(body, nil)            // KDF options
	body = binary.BigEndian.AppendUint32(body, 1)
	body = sshString(body, blob)
	body = sshString(body, secret)
	privateFile = pem.EncodeToMemory(&pem.Block{Type: "OPENSSH PRIVATE KEY", Bytes: body})
	return privateFile, buildKeyType + " " + base64.StdEncoding.EncodeToString(blob) + " " + comment
}

// buildGenerateKey writes a fresh keypair's private half into directory and
// returns its path and the public key line. The key lives only as long as the
// build's temporary directory.
func buildGenerateKey(directory string) (string, string, error) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	var check [4]byte
	if _, err = rand.Read(check[:]); err != nil {
		return "", "", err
	}
	file, line := buildKeyFiles(public, private, binary.BigEndian.Uint32(check[:]), "avr-build")
	path := filepath.Join(directory, "id")
	if err = os.WriteFile(path, file, 0o600); err != nil {
		return "", "", err
	}
	return path, line, nil
}
