package email

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"slices"
	"strings"
	"testing"
	"time"
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
