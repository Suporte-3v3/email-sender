// Command meter-report gera o relatório diário de consumo dos medidores
// e o envia por e-mail. Roda uma vez e sai (exit 0 = e-mail enviado).
package main

import (
	"log"
	"os"
	"time"
	_ "time/tzdata" // a imagem scratch não tem /usr/share/zoneinfo

	"github.com/Suporte-3v3/email-sender/internal/config"
	"github.com/Suporte-3v3/email-sender/internal/period"
)

func main() {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		log.Fatalf("configuração inválida:\n%v", err)
	}

	r := period.Compute(cfg, time.Now(), time.Local)
	log.Printf("meter-report: %d tabela(s), período %s, fuso %s", len(cfg.Tables), r, time.Local)
}
