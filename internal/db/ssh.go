package db

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"strconv"
	"time"

	"github.com/Suporte-3v3/email-sender/internal/config"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// sshTimeout limita a conexão TCP e o handshake SSH.
const sshTimeout = 15 * time.Second

// ErrHostKey indica que a host key do servidor não confere com o known_hosts.
// A atualização do known_hosts é manual e intencional.
var ErrHostKey = errors.New("host key SSH não confere com o known_hosts")

// DialSSH abre o SSH só com chave (SSH_KEY_PATH) e host key conferida contra
// SSH_KNOWN_HOSTS. O *ssh.Client devolvido serve de DialFunc para Open.
func DialSSH(ctx context.Context, c *config.Config) (*ssh.Client, error) {
	key, err := os.ReadFile(c.SSHKeyPath)
	if err != nil {
		return nil, fmt.Errorf("ler chave SSH: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("chave SSH %s: %w", c.SSHKeyPath, err)
	}
	hostKeys, err := knownhosts.New(c.SSHKnownHosts)
	if err != nil {
		return nil, fmt.Errorf("known_hosts: %w", err)
	}

	addr := net.JoinHostPort(c.SSHHost, strconv.Itoa(c.SSHPort))
	d := net.Dialer{Timeout: sshTimeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("conectar SSH em %s: %w", addr, err)
	}
	// ssh.ClientConfig.Timeout só cobre o TCP; o deadline cobre o handshake.
	if err := conn.SetDeadline(time.Now().Add(sshTimeout)); err != nil {
		conn.Close()
		return nil, err
	}
	sc, chans, reqs, err := ssh.NewClientConn(conn, addr, &ssh.ClientConfig{
		User:            c.SSHUser,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: checkHostKey(hostKeys),
	})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("SSH %s: %w", addr, err)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		sc.Close()
		return nil, err
	}
	return ssh.NewClient(sc, chans, reqs), nil
}

// checkHostKey troca o erro genérico do knownhosts por uma mensagem que diz o
// que fazer, e o marca com ErrHostKey.
func checkHostKey(cb ssh.HostKeyCallback) ssh.HostKeyCallback {
	return func(host string, remote net.Addr, key ssh.PublicKey) error {
		err := cb(host, remote, key)
		var ke *knownhosts.KeyError
		if !errors.As(err, &ke) {
			return err
		}
		switch {
		case len(ke.Want) == 0:
			return fmt.Errorf("%w: %s não está no known_hosts", ErrHostKey, host)
		case !slices.ContainsFunc(ke.Want, func(k knownhosts.KnownKey) bool { return k.Key.Type() == key.Type() }):
			return fmt.Errorf("%w: o known_hosts não tem chave %s para %s (gere com ssh-keyscan sem -t)", ErrHostKey, key.Type(), host)
		default:
			return fmt.Errorf("%w: a chave de %s mudou (%s); se a troca for legítima, atualize o known_hosts à mão", ErrHostKey, host, ssh.FingerprintSHA256(key))
		}
	}
}
