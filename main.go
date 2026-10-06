// Command meter-report gera o relatório diário de consumo dos medidores
// e o envia por e-mail. Roda uma vez e sai (exit 0 = e-mail enviado).
package main

import (
	"log"
	"os"
	"strings"
	"time"
	_ "time/tzdata" // a imagem scratch não tem /usr/share/zoneinfo
)

func main() {
	// Sem esta checagem, um TZ inválido cai em UTC silenciosamente e o
	// período do relatório fica deslocado.
	if tz := os.Getenv("TZ"); tz != "" {
		if _, err := time.LoadLocation(strings.TrimPrefix(tz, ":")); err != nil {
			log.Fatalf("TZ inválido %q: %v", tz, err)
		}
	}

	name, offset := time.Now().Zone()
	log.Printf("meter-report: fuso %s (%s, UTC%+d)", time.Local, name, offset/3600)
}
