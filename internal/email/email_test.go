package email

import (
	"bytes"
	"slices"
	"testing"
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
