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

// parityFixture é a mesma entrada usada para gerar a saída de referência com o
// main.py (pandas 3.0.6). Não altere sem regerar os valores esperados.
func parityFixture() map[string][]Reading {
	return map[string][]Reading{
		"SEL-751-1": {
			at("2026-09-29 00:00:10", 1000.0), at("2026-09-29 12:00:00", 1010.25), at("2026-09-29 23:59:59", 1012.5),
			at("2026-09-30 08:00:00", 1013.0),
			at("2026-10-01 00:01:00", 1020.0), at("2026-10-01 10:00:00", 2.0), at("2026-10-01 23:00:00", 5.5),
		},
		"SFR_001_1": {
			at("2026-09-29 18:00:00", 100.3), at("2026-09-29 06:00:00", 100.1),
			at("2026-10-01 06:00:00", 200.7), at("2026-10-01 18:00:00", 201.0),
		},
		"SFR_001_2": nil,
	}
}

func TestBuildParityWithPython(t *testing.T) {
	tables := []string{"SEL-751-1", "SFR_001_1", "SFR_001_2"}
	got, warnings, err := Build(tables, parityFixture())
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}

	if want := []string{"2026-09-29", "2026-09-30", "2026-10-01"}; !slices.Equal(got.Days, want) {
		t.Errorf("Days = %q, quero %q", got.Days, want)
	}
	if !slices.Equal(got.Columns, tables) {
		t.Errorf("Columns = %q, quero %q", got.Columns, tables)
	}
	want := [][]float64{
		{12.5, 0.20000000000000284, 0},
		{0, 0, 0},
		{-1014.5, 0.30000000000001137, 0},
	}
	if !slices.EqualFunc(got.Values, want, slices.Equal) {
		t.Errorf("Values = %v, quero %v", got.Values, want)
	}

	wantWarnings := []string{
		"SFR_001_2: sem dados no período; coluna preenchida com 0",
		"SEL-751-1: consumo negativo em 2026-10-01 (-1014.5); o contador pode ter reiniciado",
	}
	if !slices.Equal(warnings, wantWarnings) {
		t.Errorf("warnings = %q, quero %q", warnings, wantWarnings)
	}
}

func TestBuildKeepsTableOrder(t *testing.T) {
	data := map[string][]Reading{
		"B": {at("2026-09-29 06:00:00", 0), at("2026-09-29 18:00:00", 2)},
		"A": {at("2026-09-29 06:00:00", 0), at("2026-09-29 18:00:00", 1)},
	}
	tables := []string{"B", "A"}
	got, _, err := Build(tables, data)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Columns, []string{"B", "A"}) || !slices.Equal(got.Values[0], []float64{2, 1}) {
		t.Errorf("ordem errada: %q %v", got.Columns, got.Values)
	}
	tables[0] = "X"
	if got.Columns[0] != "B" {
		t.Errorf("Columns compartilha memória com o argumento tables")
	}
}
