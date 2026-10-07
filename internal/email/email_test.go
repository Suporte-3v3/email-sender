package email

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Suporte-3v3/email-sender/internal/consumption"
	"github.com/Suporte-3v3/email-sender/internal/xlsx"
)

var msg = Message{
	From:     "relatorio@example.com",
	To:       []string{"a@example.com", "b@example.com"},
	Cc:       []string{"c@example.com"},
	Subject:  Subject([]string{"SEL-751-1", "SFR_001_1"}, "Fazenda São João"),
	FileName: "relatorio_consolidado.xlsx",
	// 200 bytes binários: força várias linhas de base64 e bytes não-ASCII.
	File: bytes.Repeat([]byte{0x00, 0xff, 'P', 'K', 0x80}, 40),
}

func TestSubject(t *testing.T) {
	got := Subject([]string{"SEL-751-1", "SFR_001_1", "SFR_001_2"}, "Fazenda São João")
	want := "Relatório Medidores (SEL-751-1, SFR_001_1, SFR_001_2) Fazenda São João"
	if got != want {
		t.Errorf("Subject = %q, quero %q", got, want)
	}
}

func TestRecipients(t *testing.T) {
	want := []string{"a@example.com", "b@example.com", "c@example.com"}
	if got := msg.Recipients(); !slices.Equal(got, want) {
		t.Errorf("Recipients = %v, quero %v", got, want)
	}
}

var now = time.Date(2026, 10, 5, 7, 0, 0, 0, time.FixedZone("-0300", -3*3600))

func parse(t *testing.T, m Message) *mail.Message {
	t.Helper()
	b, err := m.Build(now)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if bytes.Contains(bytes.ReplaceAll(b, []byte("\r\n"), nil), []byte("\n")) {
		t.Fatal("mensagem tem LF sem CR")
	}
	pm, err := mail.ReadMessage(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	return pm
}

func TestBuildHeaders(t *testing.T) {
	pm := parse(t, msg)
	h := pm.Header

	subj, err := new(mime.WordDecoder).DecodeHeader(h.Get("Subject"))
	if err != nil {
		t.Fatal(err)
	}
	if subj != msg.Subject {
		t.Errorf("Subject decodificado = %q, quero %q", subj, msg.Subject)
	}
	if !strings.HasPrefix(h.Get("Subject"), "=?UTF-8?q?") {
		t.Errorf("Subject não está em Q-encoding: %q", h.Get("Subject"))
	}
	for k, want := range map[string]string{
		"From":         "relatorio@example.com",
		"To":           "a@example.com, b@example.com",
		"Cc":           "c@example.com",
		"MIME-Version": "1.0",
	} {
		if got := h.Get(k); got != want {
			t.Errorf("%s = %q, quero %q", k, got, want)
		}
	}
	if d, err := h.Date(); err != nil || !d.Equal(now) {
		t.Errorf("Date = %v (%v), quero %v", d, err, now)
	}
	if id := h.Get("Message-ID"); !strings.HasPrefix(id, "<") || !strings.HasSuffix(id, "@example.com>") {
		t.Errorf("Message-ID = %q", id)
	}
}

func TestBuildWithoutCc(t *testing.T) {
	m := msg
	m.Cc = nil
	if _, ok := parse(t, m).Header["Cc"]; ok {
		t.Error("Cc vazio não deveria gerar o header Cc")
	}
	if got := m.Recipients(); len(got) != 2 {
		t.Errorf("Recipients = %v, quero só o To", got)
	}
}

func TestBuildParts(t *testing.T) {
	pm := parse(t, msg)
	mt, params, err := mime.ParseMediaType(pm.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/mixed" {
		t.Fatalf("Content-Type = %q (%v), quero multipart/mixed", pm.Header.Get("Content-Type"), err)
	}
	mr := multipart.NewReader(pm.Body, params["boundary"])

	// Parte 1: o multipart.Reader já decodifica o quoted-printable.
	p, err := mr.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if ct := p.Header.Get("Content-Type"); ct != "text/html; charset=UTF-8" {
		t.Errorf("parte 1 Content-Type = %q", ct)
	}
	html, _ := io.ReadAll(p)
	if string(html) != body {
		t.Errorf("HTML = %q, quero %q", html, body)
	}

	// Parte 2: anexo em base64, linhas de no máximo 76 colunas.
	p, err = mr.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if ct, _, _ := mime.ParseMediaType(p.Header.Get("Content-Type")); ct != ContentTypeXLSX {
		t.Errorf("anexo Content-Type = %q", ct)
	}
	if p.FileName() != "relatorio_consolidado.xlsx" {
		t.Errorf("anexo filename = %q", p.FileName())
	}
	if te := p.Header.Get("Content-Transfer-Encoding"); te != "base64" {
		t.Errorf("anexo Content-Transfer-Encoding = %q", te)
	}
	raw, _ := io.ReadAll(p)
	lines := strings.Split(strings.TrimRight(string(raw), "\r\n"), "\r\n")
	if len(lines) < 2 {
		t.Fatalf("base64 em %d linha(s); a fixture deveria quebrar em várias", len(lines))
	}
	for i, l := range lines {
		if len(l) > 76 {
			t.Errorf("linha %d do base64 tem %d colunas", i+1, len(l))
		}
	}
	got, err := base64.StdEncoding.DecodeString(strings.Join(lines, ""))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, msg.File) {
		t.Error("anexo decodificado difere dos bytes de entrada")
	}

	if _, err := mr.NextPart(); err != io.EOF {
		t.Errorf("esperava 2 partes; NextPart = %v", err)
	}
}

// fakeSMTP aceita uma conexão, anuncia ESMTP sem STARTTLS e grava os comandos
// recebidos.
func fakeSMTP(t *testing.T) (host string, port int, cmds func() []string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	var mu sync.Mutex
	var got []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.WriteString(conn, "220 fake ESMTP\r\n")
		sc := bufio.NewScanner(conn)
		for sc.Scan() {
			line := sc.Text()
			mu.Lock()
			got = append(got, line)
			mu.Unlock()
			switch strings.ToUpper(strings.Fields(line + " x")[0]) {
			case "EHLO":
				io.WriteString(conn, "250-fake\r\n250 AUTH PLAIN\r\n")
			case "QUIT":
				io.WriteString(conn, "221 tchau\r\n")
				return
			default:
				io.WriteString(conn, "250 ok\r\n")
			}
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port, func() []string {
		<-done
		mu.Lock()
		defer mu.Unlock()
		return got
	}
}

func TestSendRequiresStartTLS(t *testing.T) {
	host, port, cmds := fakeSMTP(t)
	err := Send(Server{Host: host, Port: port, User: "u", Password: "segredo"}, msg)
	if !errors.Is(err, ErrNoStartTLS) {
		t.Fatalf("Send = %v, quero ErrNoStartTLS", err)
	}
	for _, c := range cmds() {
		if strings.HasPrefix(strings.ToUpper(c), "AUTH") || strings.HasPrefix(strings.ToUpper(c), "MAIL") {
			t.Errorf("servidor sem STARTTLS recebeu %q", c)
		}
	}
}

// TestSendReal envia de verdade (critério de aceite da #8). Só roda com
// EMAIL_SMOKE_TO definido; usa SMTP_USER e SMTP_PASSWORD do ambiente.
func TestSendReal(t *testing.T) {
	to := os.Getenv("EMAIL_SMOKE_TO")
	if to == "" {
		t.Skip("defina EMAIL_SMOKE_TO (e SMTP_USER/SMTP_PASSWORD) para enviar de verdade")
	}
	user, pass := os.Getenv("SMTP_USER"), os.Getenv("SMTP_PASSWORD")
	if user == "" || pass == "" {
		t.Fatal("EMAIL_SMOKE_TO exige SMTP_USER e SMTP_PASSWORD")
	}
	tables := []string{"SEL-751-1", "SFR_001_1"}
	file, err := xlsx.Build(consumption.Report{
		Days:    []string{"2026-09-29", "2026-09-30"},
		Columns: tables,
		Values:  [][]float64{{12.5, 3}, {0.25, 0}},
	})
	if err != nil {
		t.Fatal(err)
	}
	m := Message{
		From:     user,
		To:       []string{to},
		Subject:  Subject(tables, "TESTE ação çãõ éíú"),
		FileName: xlsx.FileName,
		File:     file,
	}
	if err := Send(Server{Host: "smtp.gmail.com", Port: 587, User: user, Password: pass}, m); err != nil {
		t.Fatal(err)
	}
}
