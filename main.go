// Command meter-report gera o relatório diário de consumo dos medidores
// e o envia por e-mail. Roda uma vez e sai (exit 0 = e-mail enviado).
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"
	_ "time/tzdata" // a imagem scratch não tem /usr/share/zoneinfo

	"github.com/Suporte-3v3/email-sender/internal/config"
	"github.com/Suporte-3v3/email-sender/internal/consumption"
	"github.com/Suporte-3v3/email-sender/internal/db"
	"github.com/Suporte-3v3/email-sender/internal/email"
	"github.com/Suporte-3v3/email-sender/internal/period"
	"github.com/Suporte-3v3/email-sender/internal/xlsx"
)

// timeout é o teto de uma execução. O contexto expira nesse prazo; o que não
// respeita contexto (handshake SSH, SMTP) é encerrado pelo watchdog. É var só
// para o teste encurtar.
var timeout = 3 * time.Minute

func main() {
	watchdog(timeout, os.Exit)
	if err := run(os.Getenv); err != nil {
		log.Printf("erro: %v", err)
		os.Exit(1)
	}
}

// watchdog chama exit(1) depois de d. Sem ele, um container pendurado ficaria
// ocupando RAM indefinidamente.
func watchdog(d time.Duration, exit func(int)) *time.Timer {
	return time.AfterFunc(d, func() {
		log.Printf("watchdog: execução passou de %s; abortando", d)
		exit(1)
	})
}

// run executa a sequência da spec (§4). Qualquer erro devolvido vira exit 1;
// avisos só vão para o log.
func run(getenv func(string) string) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return fmt.Errorf("configuração inválida:\n%w", err)
	}
	r := period.Compute(cfg, time.Now(), time.Local)
	log.Printf("meter-report: %d tabela(s), período %s, fuso %s", len(cfg.Tables), r, time.Local)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	tunnel, err := db.DialSSH(ctx, cfg)
	if err != nil {
		return err
	}
	defer tunnel.Close()
	conn, err := db.Open(ctx, cfg, time.Local, tunnel.DialContext)
	if err != nil {
		return err
	}
	defer conn.Close()

	site, err := db.SiteName(ctx, conn)
	if err != nil {
		log.Printf("aviso: %v; usando %q", err, site)
	}
	readings, err := db.ReadAll(ctx, conn, cfg.Tables, cfg.SensorType, r)
	if err != nil {
		return fmt.Errorf("leitura das tabelas: %w", err)
	}

	msg, warnings, err := prepare(cfg, site, readings)
	for _, w := range warnings {
		log.Printf("aviso: %s", w)
	}
	if err != nil {
		return err
	}

	srv := email.Server{Host: cfg.SMTPHost, Port: cfg.SMTPPort, User: cfg.SMTPUser, Password: cfg.SMTPPassword}
	if err := email.Send(srv, msg); err != nil {
		return fmt.Errorf("envio do e-mail: %w", err)
	}
	log.Printf("e-mail enviado para %d destinatário(s): %s", len(msg.Recipients()), msg.Subject)
	return nil
}

// prepare monta o e-mail a partir das leituras, sem I/O: consolida, gera o
// xlsx e preenche a mensagem. Com consumption.ErrNoData não há mensagem, e
// nada deve ser enviado.
func prepare(cfg *config.Config, site string, readings map[string][]consumption.Reading) (email.Message, []string, error) {
	rep, warnings, err := consumption.Build(cfg.Tables, readings)
	if err != nil {
		return email.Message{}, warnings, err
	}
	file, err := xlsx.Build(rep)
	if err != nil {
		return email.Message{}, warnings, fmt.Errorf("gerar xlsx: %w", err)
	}
	return email.Message{
		From:     cfg.SMTPUser,
		To:       cfg.MailTo,
		Cc:       cfg.MailCc,
		Subject:  email.Subject(cfg.Tables, site),
		FileName: xlsx.FileName,
		File:     file,
	}, warnings, nil
}
