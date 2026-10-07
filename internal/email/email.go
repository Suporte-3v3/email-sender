// Package email monta a mensagem do relatório (MIME, só stdlib) e a envia por
// SMTP com STARTTLS obrigatório.
package email

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"net/textproto"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ContentTypeXLSX é o tipo MIME do anexo.
const ContentTypeXLSX = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// body é o corpo do e-mail, o mesmo texto e estilo do script Python.
const body = `<html><body><p style="font-family: Arial, sans-serif; font-size: 14px; color: #333;">Segue em anexo o relatório dos equipamentos. E-mail automático gerado pela 3v3 Tecnologia.</p></body></html>`

// Subject monta o assunto como o script Python: tabelas separadas por ", ".
func Subject(tables []string, site string) string {
	return fmt.Sprintf("Relatório Medidores (%s) %s", strings.Join(tables, ", "), site)
}

// Message é o e-mail do relatório. Os endereços devem vir validados pelo
// config (endereço puro, sem nome de exibição).
type Message struct {
	From     string
	To       []string
	Cc       []string
	Subject  string
	FileName string // nome do anexo
	File     []byte // conteúdo do anexo (xlsx)
}

// Recipients é o envelope RCPT TO: To + Cc.
func (m Message) Recipients() []string { return slices.Concat(m.To, m.Cc) }

// Build devolve a mensagem RFC 5322 com CRLF: corpo HTML em quoted-printable
// e o anexo em base64 com linhas de 76 colunas. now vira o header Date.
func (m Message) Build(now time.Time) ([]byte, error) {
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	domain := m.From[strings.LastIndex(m.From, "@")+1:]

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	header := func(k, v string) { fmt.Fprintf(&buf, "%s: %s\r\n", k, v) }
	header("From", m.From)
	header("To", strings.Join(m.To, ", "))
	if len(m.Cc) > 0 {
		header("Cc", strings.Join(m.Cc, ", "))
	}
	// QEncoding também escapa CR/LF, então o nome do local vindo do banco
	// não consegue injetar headers.
	header("Subject", mime.QEncoding.Encode("UTF-8", m.Subject))
	header("Date", now.Format(time.RFC1123Z))
	header("Message-ID", fmt.Sprintf("<%s@%s>", hex.EncodeToString(id), domain))
	header("MIME-Version", "1.0")
	header("Content-Type", mime.FormatMediaType("multipart/mixed", map[string]string{"boundary": mw.Boundary()}))
	buf.WriteString("\r\n")

	html, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {"text/html; charset=UTF-8"},
		"Content-Transfer-Encoding": {"quoted-printable"},
	})
	if err != nil {
		return nil, err
	}
	qp := quotedprintable.NewWriter(html)
	if _, err := qp.Write([]byte(body)); err != nil {
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}

	att, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {mime.FormatMediaType(ContentTypeXLSX, map[string]string{"name": m.FileName})},
		"Content-Disposition":       {mime.FormatMediaType("attachment", map[string]string{"filename": m.FileName})},
		"Content-Transfer-Encoding": {"base64"},
	})
	if err != nil {
		return nil, err
	}
	enc := base64.StdEncoding.EncodeToString(m.File)
	for len(enc) > 76 {
		if _, err := fmt.Fprintf(att, "%s\r\n", enc[:76]); err != nil {
			return nil, err
		}
		enc = enc[76:]
	}
	if _, err := fmt.Fprintf(att, "%s\r\n", enc); err != nil {
		return nil, err
	}

	if err := mw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Server é o servidor SMTP de envio.
type Server struct {
	Host     string
	Port     int
	User     string
	Password string
}

const (
	dialTimeout = 15 * time.Second
	// ioTimeout limita a conversa SMTP inteira; o watchdog da #10 é a última
	// barreira, não a única.
	ioTimeout = 60 * time.Second
)

// ErrNoStartTLS indica que o servidor não oferece STARTTLS; nesse caso nada é
// autenticado, para a senha nunca trafegar em claro.
var ErrNoStartTLS = errors.New("servidor SMTP não oferece STARTTLS")

// Send entrega m via s: STARTTLS obrigatório, AUTH PLAIN, MAIL FROM m.From,
// RCPT TO para To + Cc.
func Send(s Server, m Message) error {
	msg, err := m.Build(time.Now())
	if err != nil {
		return fmt.Errorf("montar e-mail: %w", err)
	}

	addr := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	conn, err := net.DialTimeout("tcp", addr, dialTimeout)
	if err != nil {
		return fmt.Errorf("conectar em %s: %w", addr, err)
	}
	if err := conn.SetDeadline(time.Now().Add(ioTimeout)); err != nil {
		conn.Close()
		return err
	}
	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("SMTP %s: %w", addr, err)
	}
	defer c.Close()

	if ok, _ := c.Extension("STARTTLS"); !ok {
		return ErrNoStartTLS
	}
	if err := c.StartTLS(&tls.Config{ServerName: s.Host}); err != nil {
		return fmt.Errorf("STARTTLS: %w", err)
	}
	if err := c.Auth(smtp.PlainAuth("", s.User, s.Password, s.Host)); err != nil {
		return fmt.Errorf("autenticar no SMTP: %w", err)
	}
	if err := c.Mail(m.From); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	for _, r := range m.Recipients() {
		if err := c.Rcpt(r); err != nil {
			return fmt.Errorf("RCPT TO %s: %w", r, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("enviar mensagem: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("enviar mensagem: %w", err)
	}
	return c.Quit()
}
