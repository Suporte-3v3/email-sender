// Command meter-report gera o relatório diário de consumo dos medidores
// e o envia por e-mail. Roda uma vez e sai (exit 0 = e-mail enviado).
package main

import (
	"fmt"
	"log"
	"os"
	"time"
	_ "time/tzdata" // a imagem scratch não tem /usr/share/zoneinfo

	"github.com/Suporte-3v3/email-sender/internal/config"
	"github.com/Suporte-3v3/email-sender/internal/consumption"
	"github.com/Suporte-3v3/email-sender/internal/email"
	"github.com/Suporte-3v3/email-sender/internal/period"
	"github.com/Suporte-3v3/email-sender/internal/xlsx"
)

func main() {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		log.Fatalf("configuração inválida:\n%v", err)
	}

	r := period.Compute(cfg, time.Now(), time.Local)
	log.Printf("meter-report: %d tabela(s), período %s, fuso %s", len(cfg.Tables), r, time.Local)
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
