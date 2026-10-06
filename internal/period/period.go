// Package period calcula o intervalo de datas do relatório.
package period

import (
	"time"

	"github.com/Suporte-3v3/email-sender/internal/config"
)

// Range é o intervalo meio aberto [Start, End) de datas civis: time.Time à
// meia-noite UTC, onde só ano, mês e dia importam. A coluna TIME do banco é
// horário local, então essas datas são comparadas com ela como texto, sem
// conversão de fuso.
type Range struct {
	Start time.Time // primeiro dia, inclusivo
	End   time.Time // dia seguinte ao último, exclusivo
}

// Compute devolve o período do relatório. Com REPORT_FROM/REPORT_TO usa esses
// dias (inclusivos); senão, os últimos ReportDays dias completos até ontem,
// sendo "hoje" a data de now no fuso loc.
func Compute(c *config.Config, now time.Time, loc *time.Location) Range {
	if c.HasRange() {
		return Range{Start: c.ReportFrom, End: c.ReportTo.AddDate(0, 0, 1)}
	}
	y, m, d := now.In(loc).Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return Range{Start: today.AddDate(0, 0, -c.ReportDays), End: today}
}
