package period

import (
	"testing"
	"time"

	"github.com/Suporte-3v3/email-sender/internal/config"
)

// civil monta uma data civil (meia-noite UTC), a convenção do pacote.
func civil(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestComputeLastDays(t *testing.T) {
	sp := mustLoc(t, "America/Sao_Paulo")
	tests := []struct {
		name      string
		now       time.Time
		days      int
		wantStart time.Time
		wantEnd   time.Time
	}{
		{"exemplo da spec", time.Date(2026, 10, 6, 7, 0, 0, 0, sp), 7, civil(2026, 9, 29), civil(2026, 10, 6)},
		{"um dia = só ontem", time.Date(2026, 10, 6, 7, 0, 0, 0, sp), 1, civil(2026, 10, 5), civil(2026, 10, 6)},
		{"virada de mês", time.Date(2026, 10, 1, 7, 0, 0, 0, sp), 7, civil(2026, 9, 24), civil(2026, 10, 1)},
		{"virada de ano", time.Date(2027, 1, 1, 7, 0, 0, 0, sp), 3, civil(2026, 12, 29), civil(2027, 1, 1)},
		{"ano bissexto", time.Date(2028, 3, 1, 7, 0, 0, 0, sp), 1, civil(2028, 2, 29), civil(2028, 3, 1)},
		{"logo após a meia-noite local", time.Date(2026, 10, 6, 0, 0, 1, 0, sp), 1, civil(2026, 10, 5), civil(2026, 10, 6)},
		// 22:30 em São Paulo já é 01:30 do dia 6 em UTC; o "hoje" tem que ser o dia 5.
		{"hoje vem do fuso, não de UTC", time.Date(2026, 10, 5, 22, 30, 0, 0, sp), 1, civil(2026, 10, 4), civil(2026, 10, 5)},
		// O mesmo instante, visto de UTC: o "hoje" passa a ser o dia 6.
		{"mesmo instante em UTC", time.Date(2026, 10, 5, 22, 30, 0, 0, sp).UTC(), 1, civil(2026, 10, 5), civil(2026, 10, 6)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loc := sp
			if tt.now.Location() == time.UTC {
				loc = time.UTC
			}
			got := Compute(&config.Config{ReportDays: tt.days}, tt.now, loc)
			if !got.Start.Equal(tt.wantStart) || !got.End.Equal(tt.wantEnd) {
				t.Errorf("Compute = [%s, %s), quero [%s, %s)",
					got.Start.Format(time.DateOnly), got.End.Format(time.DateOnly),
					tt.wantStart.Format(time.DateOnly), tt.wantEnd.Format(time.DateOnly))
			}
		})
	}
}
