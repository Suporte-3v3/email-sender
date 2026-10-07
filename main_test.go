package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Suporte-3v3/email-sender/internal/config"
	"github.com/Suporte-3v3/email-sender/internal/consumption"
	"github.com/Suporte-3v3/email-sender/internal/email"
	"github.com/xuri/excelize/v2"
	"golang.org/x/crypto/ssh"
)

// Com METER_REPORT_MAIN=1 o binário de teste vira o próprio meter-report, para
// os testes conferirem o código de saída de verdade.
func TestMain(m *testing.M) {
	if os.Getenv("METER_REPORT_MAIN") == "1" {
		if d, err := time.ParseDuration(os.Getenv("METER_REPORT_TIMEOUT")); err == nil {
			timeout = d
		}
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

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

func TestWatchdog(t *testing.T) {
	code := make(chan int, 1)
	watchdog(10*time.Millisecond, func(c int) { code <- c })
	select {
	case c := <-code:
		if c != 1 {
			t.Errorf("exit(%d), quero exit(1)", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog não disparou")
	}
}

// runMain roda o binário de teste como meter-report com só o ambiente dado.
func runMain(t *testing.T, env ...string) (code int, out string, elapsed time.Duration) {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append([]string{"METER_REPORT_MAIN=1"}, env...)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	start := time.Now()
	err := cmd.Run()
	elapsed = time.Since(start)
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		code = ee.ExitCode()
	default:
		t.Fatalf("executar: %v", err)
	}
	return code, buf.String(), elapsed
}

func TestMainWithoutEnv(t *testing.T) {
	code, out, _ := runMain(t)
	if code != 1 {
		t.Errorf("exit %d, quero 1", code)
	}
	if !strings.Contains(out, "ausentes: SSH_HOST") {
		t.Errorf("saída não lista as variáveis ausentes:\n%s", out)
	}
	if strings.Contains(out, "período") {
		t.Errorf("passou da configuração sem as variáveis:\n%s", out)
	}
}

// Um "SSH" que aceita a conexão e nunca responde: o handshake fica parado (ele
// não respeita o contexto), e só o watchdog encerra antes dos 15 s do SSH.
func TestMainWatchdogStopsHungSSH(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			// Fica aberta e muda até o listener fechar e esta goroutine sair.
			defer c.Close()
		}
	}()
	host, port, _ := net.SplitHostPort(ln.Addr().String())

	dir := t.TempDir()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	key, kh := filepath.Join(dir, "id_ed25519"), filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(key, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kh, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	code, out, elapsed := runMain(t,
		"METER_REPORT_TIMEOUT=1s",
		"SSH_HOST="+host, "SSH_PORT="+port, "SSH_KEY_PATH="+key, "SSH_KNOWN_HOSTS="+kh,
		"DB_USER=u", "DB_PASSWORD=p", "TABLES=SEL-751-1", "SENSOR_TYPE=kWh",
		"SMTP_USER=r@example.com", "SMTP_PASSWORD=s", "MAIL_TO=a@example.com",
	)
	if code != 1 {
		t.Errorf("exit %d, quero 1\n%s", code, out)
	}
	if !strings.Contains(out, "watchdog") {
		t.Errorf("quem encerrou não foi o watchdog:\n%s", out)
	}
	if elapsed > 10*time.Second {
		t.Errorf("levou %s; o watchdog de 1s deveria ter encerrado antes", elapsed)
	}
}
