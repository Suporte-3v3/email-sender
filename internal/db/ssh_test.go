package db

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Suporte-3v3/email-sender/internal/config"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func newSigner(t *testing.T) (ssh.Signer, ed25519.PrivateKey) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s, priv
}

// sshServer sobe um servidor SSH em 127.0.0.1 que aceita só clientKey e
// atende canais direct-tcpip (o que o driver MySQL usa) conectando no destino.
func sshServer(t *testing.T, hostKey ssh.Signer, clientKey ssh.PublicKey) string {
	t.Helper()
	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
			if string(k.Marshal()) == string(clientKey.Marshal()) {
				return nil, nil
			}
			return nil, errors.New("chave recusada")
		},
	}
	cfg.AddHostKey(hostKey)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, chans, reqs, err := ssh.NewServerConn(conn, cfg)
				if err != nil {
					conn.Close()
					return
				}
				go ssh.DiscardRequests(reqs)
				for nc := range chans {
					if nc.ChannelType() != "direct-tcpip" {
						nc.Reject(ssh.UnknownChannelType, "só direct-tcpip")
						continue
					}
					var p struct {
						Host     string
						Port     uint32
						OrigHost string
						OrigPort uint32
					}
					if err := ssh.Unmarshal(nc.ExtraData(), &p); err != nil {
						nc.Reject(ssh.ConnectionFailed, err.Error())
						continue
					}
					dst, err := net.Dial("tcp", net.JoinHostPort(p.Host, strconv.Itoa(int(p.Port))))
					if err != nil {
						nc.Reject(ssh.ConnectionFailed, err.Error())
						continue
					}
					ch, creqs, err := nc.Accept()
					if err != nil {
						dst.Close()
						continue
					}
					go ssh.DiscardRequests(creqs)
					go func() { io.Copy(ch, dst); ch.CloseWrite() }()
					go func() { io.Copy(dst, ch); dst.Close() }()
				}
			}()
		}
	}()
	return ln.Addr().String()
}

// echoServer devolve tudo o que recebe.
func echoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { io.Copy(c, c); c.Close() }()
		}
	}()
	return ln.Addr().String()
}

// sshConfig grava a chave do cliente e o known_hosts (com knownKey para o
// endereço do servidor; nil = arquivo vazio) e devolve o config apontando para eles.
func sshConfig(t *testing.T, addr string, clientPriv ed25519.PrivateKey, knownKey ssh.PublicKey) *config.Config {
	t.Helper()
	dir := t.TempDir()
	block, err := ssh.MarshalPrivateKey(clientPriv, "")
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	var kh string
	if knownKey != nil {
		kh = knownhosts.Line([]string{knownhosts.Normalize(addr)}, knownKey) + "\n"
	}
	khPath := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(khPath, []byte(kh), 0o600); err != nil {
		t.Fatal(err)
	}
	host, port, _ := net.SplitHostPort(addr)
	p, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	return &config.Config{SSHHost: host, SSHPort: p, SSHUser: "pi", SSHKeyPath: keyPath, SSHKnownHosts: khPath}
}

func TestDialSSHForwards(t *testing.T) {
	hostKey, _ := newSigner(t)
	client, clientPriv := newSigner(t)
	addr := sshServer(t, hostKey, client.PublicKey())

	c, err := DialSSH(context.Background(), sshConfig(t, addr, clientPriv, hostKey.PublicKey()))
	if err != nil {
		t.Fatalf("DialSSH: %v", err)
	}
	defer c.Close()

	conn, err := c.DialContext(context.Background(), "tcp", echoServer(t))
	if err != nil {
		t.Fatalf("canal direct-tcpip: %v", err)
	}
	defer conn.Close()
	if _, err := io.WriteString(conn, "ping"); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil || string(buf) != "ping" {
		t.Errorf("eco = %q (%v), quero ping", buf, err)
	}
}

func TestDialSSHHostKeyChanged(t *testing.T) {
	hostKey, _ := newSigner(t)
	other, _ := newSigner(t)
	client, clientPriv := newSigner(t)
	addr := sshServer(t, hostKey, client.PublicKey())

	_, err := DialSSH(context.Background(), sshConfig(t, addr, clientPriv, other.PublicKey()))
	if !errors.Is(err, ErrHostKey) {
		t.Fatalf("DialSSH = %v, quero ErrHostKey", err)
	}
	if !strings.Contains(err.Error(), "mudou") {
		t.Errorf("mensagem não diz que a chave mudou: %v", err)
	}
}

func TestDialSSHUnknownHost(t *testing.T) {
	hostKey, _ := newSigner(t)
	client, clientPriv := newSigner(t)
	addr := sshServer(t, hostKey, client.PublicKey())

	_, err := DialSSH(context.Background(), sshConfig(t, addr, clientPriv, nil))
	if !errors.Is(err, ErrHostKey) {
		t.Fatalf("DialSSH = %v, quero ErrHostKey", err)
	}
	if !strings.Contains(err.Error(), "não está no known_hosts") {
		t.Errorf("mensagem não diz que o host é desconhecido: %v", err)
	}
}

func TestDialSSHWrongClientKey(t *testing.T) {
	hostKey, _ := newSigner(t)
	client, _ := newSigner(t)
	_, otherPriv := newSigner(t)
	addr := sshServer(t, hostKey, client.PublicKey())

	_, err := DialSSH(context.Background(), sshConfig(t, addr, otherPriv, hostKey.PublicKey()))
	if err == nil || errors.Is(err, ErrHostKey) {
		t.Fatalf("DialSSH = %v, quero erro de autenticação", err)
	}
}
