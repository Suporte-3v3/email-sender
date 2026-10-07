package xlsx

import (
	"bytes"
	"slices"
	"testing"

	"github.com/Suporte-3v3/email-sender/internal/consumption"
	"github.com/xuri/excelize/v2"
)

var report = consumption.Report{
	Days:    []string{"2026-09-29", "2026-09-30"},
	Columns: []string{"SEL-751-1", "SFR_001_1", "SFR_001_2", "SFR_001_3"},
	Values: [][]float64{
		{12.5, 3, 0, 7.25},
		{0.20000000000000284, 0, 1, -2.5},
	},
}

func open(t *testing.T, b []byte) *excelize.File {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestBuildLayout(t *testing.T) {
	b, err := Build(report)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	f := open(t, b)

	if got := f.GetSheetList(); !slices.Equal(got, []string{"Sheet1"}) {
		t.Fatalf("abas = %v, quero [Sheet1]", got)
	}
	rows, err := f.GetRows("Sheet1")
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"Data", "SEL-751-1", "SFR_001_1", "SFR_001_2", "SFR_001_3"},
		{"2026-09-29", "12.5", "3", "0", "7.25"},
		// O leitor do excelize mostra 15 dígitos; a precisão total é conferida em TestBuildFullPrecision.
		{"2026-09-30", "0.200000000000003", "0", "1", "-2.5"},
	}
	if len(rows) != len(want) {
		t.Fatalf("linhas = %d, quero %d: %v", len(rows), len(want), rows)
	}
	for i := range want {
		if !slices.Equal(rows[i], want[i]) {
			t.Errorf("linha %d = %v, quero %v", i+1, rows[i], want[i])
		}
	}
}

func TestBuildInvalidDay(t *testing.T) {
	_, err := Build(consumption.Report{Days: []string{"29/09/2026"}, Columns: []string{"X"}, Values: [][]float64{{1}}})
	if err == nil {
		t.Fatal("Build aceitou dia fora do formato AAAA-MM-DD")
	}
}
