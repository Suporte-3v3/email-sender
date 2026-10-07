package xlsx

import (
	"archive/zip"
	"bytes"
	"io"
	"slices"
	"strconv"
	"strings"
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

func TestBuildDateIsExcelDate(t *testing.T) {
	b, err := Build(report)
	if err != nil {
		t.Fatal(err)
	}
	f := open(t, b)

	raw, err := f.GetCellValue("Sheet1", "A2", excelize.Options{RawCellValue: true})
	if err != nil {
		t.Fatal(err)
	}
	if raw != "46294" { // 2026-09-29 no sistema de datas 1900 do Excel
		t.Errorf("A2 bruto = %q, quero o serial 46294", raw)
	}
	id, err := f.GetCellStyle("Sheet1", "A2")
	if err != nil {
		t.Fatal(err)
	}
	st, err := f.GetStyle(id)
	if err != nil {
		t.Fatal(err)
	}
	if st.CustomNumFmt == nil || *st.CustomNumFmt != "yyyy-mm-dd" {
		t.Errorf("formato de A2 = %v, quero yyyy-mm-dd", st.CustomNumFmt)
	}
}

func TestBuildValuesAreNumbers(t *testing.T) {
	b, err := Build(report)
	if err != nil {
		t.Fatal(err)
	}
	f := open(t, b)

	for _, cell := range []string{"B2", "C2", "D2", "E2", "B3", "E3"} {
		// Célula numérica não tem atributo t no XML; o excelize a reporta como Unset.
		typ, err := f.GetCellType("Sheet1", cell)
		if err != nil {
			t.Fatal(err)
		}
		if typ != excelize.CellTypeUnset && typ != excelize.CellTypeNumber {
			t.Errorf("%s tem tipo %v, quero número", cell, typ)
		}
		raw, _ := f.GetCellValue("Sheet1", cell, excelize.Options{RawCellValue: true})
		if _, err := strconv.ParseFloat(raw, 64); err != nil {
			t.Errorf("%s = %q não é número: %v", cell, raw, err)
		}
	}
}

// O Python grava o float completo; o excelize também, mas só lendo o XML dá
// para ver, porque o leitor dele arredonda para 15 dígitos.
func TestBuildFullPrecision(t *testing.T) {
	b, err := Build(report)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	for _, zf := range zr.File {
		if zf.Name != "xl/worksheets/sheet1.xml" {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer rc.Close()
		xml, err := io.ReadAll(rc)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(xml), "<v>0.20000000000000284</v>") {
			t.Errorf("0.20000000000000284 foi arredondado no XML")
		}
		return
	}
	t.Fatal("xl/worksheets/sheet1.xml não encontrado")
}
