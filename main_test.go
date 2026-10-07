package main

import (
	"bytes"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/Suporte-3v3/email-sender/internal/config"
	"github.com/Suporte-3v3/email-sender/internal/consumption"
	"github.com/Suporte-3v3/email-sender/internal/email"
	"github.com/xuri/excelize/v2"
)

var cfg = &config.Config{
	Tables:   []string{"SEL-751-1", "SFR_001_1"},
	SMTPUser: "relatorio@example.com",
	MailTo:   []string{"a@example.com"},
	MailCc:   []string{"c@example.com"},
}

func at(ts string, v float64) consumption.Reading {
	t, err := time.ParseInLocation(time.DateTime, ts, time.UTC)
	if err != nil {
		panic(err)
	}
	return consumption.Reading{Time: t, Value: v}
}

func TestPrepare(t *testing.T) {
	readings := map[string][]consumption.Reading{
		"SEL-751-1": {at("2026-09-29 00:00:00", 1000), at("2026-09-29 23:00:00", 1012.5)},
	}
	msg, warnings, err := prepare(cfg, "Fazenda São João", readings)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if want := []string{"SFR_001_1: sem dados no período; coluna preenchida com 0"}; !slices.Equal(warnings, want) {
		t.Errorf("avisos = %q, quero %q", warnings, want)
	}
	if want := email.Subject(cfg.Tables, "Fazenda São João"); msg.Subject != want {
		t.Errorf("Subject = %q, quero %q", msg.Subject, want)
	}
	if msg.From != cfg.SMTPUser || !slices.Equal(msg.To, cfg.MailTo) || !slices.Equal(msg.Cc, cfg.MailCc) {
		t.Errorf("endereços = %q %q %q", msg.From, msg.To, msg.Cc)
	}
	if msg.FileName != "relatorio_consolidado.xlsx" {
		t.Errorf("FileName = %q", msg.FileName)
	}

	f, err := excelize.OpenReader(bytes.NewReader(msg.File))
	if err != nil {
		t.Fatalf("anexo não é xlsx: %v", err)
	}
	defer f.Close()
	rows, err := f.GetRows("Sheet1")
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"Data", "SEL-751-1", "SFR_001_1"}, {"2026-09-29", "12.5", "0"}}
	if len(rows) != len(want) || !slices.Equal(rows[0], want[0]) || !slices.Equal(rows[1], want[1]) {
		t.Errorf("planilha = %v, quero %v", rows, want)
	}
}

func TestPrepareNoData(t *testing.T) {
	msg, warnings, err := prepare(cfg, "X", map[string][]consumption.Reading{})
	if !errors.Is(err, consumption.ErrNoData) {
		t.Fatalf("err = %v, quero ErrNoData", err)
	}
	if msg.File != nil || len(msg.Recipients()) != 0 {
		t.Errorf("sem dados não pode haver mensagem: %+v", msg)
	}
	if len(warnings) != 2 {
		t.Errorf("avisos = %q, quero um por tabela", warnings)
	}
}
