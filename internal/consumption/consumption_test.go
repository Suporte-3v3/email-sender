package consumption

import (
	"maps"
	"slices"
	"testing"
	"time"
)

var sp = mustLoc("America/Sao_Paulo")

func mustLoc(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// at monta uma leitura em horário de São Paulo a partir de "AAAA-MM-DD HH:MM:SS".
func at(ts string, v float64) Reading {
	t, err := time.ParseInLocation(time.DateTime, ts, sp)
	if err != nil {
		panic(err)
	}
	return Reading{Time: t, Value: v}
}

func TestDaily(t *testing.T) {
	tests := []struct {
		name string
		in   []Reading
		want map[string]float64
	}{
		{"vazio", nil, map[string]float64{}},
		{"último menos primeiro", []Reading{
			at("2026-09-29 00:00:10", 1000), at("2026-09-29 12:00:00", 1010.25), at("2026-09-29 23:59:59", 1012.5),
		}, map[string]float64{"2026-09-29": 12.5}},
		{"uma leitura só vale 0", []Reading{at("2026-09-30 08:00:00", 1013)}, map[string]float64{"2026-09-30": 0}},
		{"fora de ordem é ordenado", []Reading{
			at("2026-09-29 18:00:00", 30), at("2026-09-29 06:00:00", 10), at("2026-09-29 12:00:00", 20),
		}, map[string]float64{"2026-09-29": 20}},
		{"dias separados", []Reading{
			at("2026-09-29 06:00:00", 1), at("2026-09-29 18:00:00", 3),
			at("2026-09-30 06:00:00", 3), at("2026-09-30 18:00:00", 10),
		}, map[string]float64{"2026-09-29": 2, "2026-09-30": 7}},
		{"delta negativo é mantido", []Reading{
			at("2026-10-01 00:01:00", 1020), at("2026-10-01 23:00:00", 5.5),
		}, map[string]float64{"2026-10-01": -1014.5}},
		// 23:30 em São Paulo já é 02:30 do dia seguinte em UTC: tem que contar no dia local.
		{"dia é o do fuso da leitura, não UTC", []Reading{
			at("2026-09-29 00:30:00", 1), at("2026-09-29 23:30:00", 9),
		}, map[string]float64{"2026-09-29": 8}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Daily(tt.in); !maps.Equal(got, tt.want) {
				t.Errorf("Daily = %v, quero %v", got, tt.want)
			}
		})
	}
}

func TestDailyDoesNotMutateInput(t *testing.T) {
	in := []Reading{at("2026-09-29 18:00:00", 30), at("2026-09-29 06:00:00", 10)}
	orig := slices.Clone(in)
	Daily(in)
	if !slices.Equal(in, orig) {
		t.Errorf("Daily alterou a entrada: %v", in)
	}
}
