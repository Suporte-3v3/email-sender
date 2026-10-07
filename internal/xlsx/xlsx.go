// Package xlsx gera, em memória, a planilha do relatório consolidado no mesmo
// formato do to_excel do script Python.
package xlsx

import (
	"fmt"
	"time"

	"github.com/Suporte-3v3/email-sender/internal/consumption"
	"github.com/xuri/excelize/v2"
)

// FileName é o nome do anexo, o mesmo do script Python.
const FileName = "relatorio_consolidado.xlsx"

const sheet = "Sheet1"

// Build devolve o xlsx do relatório: aba Sheet1, cabeçalho Data + uma coluna
// por medidor (na ordem de r.Columns), uma linha por dia. Data vai como data
// do Excel no formato yyyy-mm-dd e os consumos como número, sem arredondar.
func Build(r consumption.Report) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()

	// Mesmo estilo de cabeçalho do pandas: negrito, borda fina, centralizado.
	border := []excelize.Border{
		{Type: "left", Style: 1, Color: "000000"},
		{Type: "top", Style: 1, Color: "000000"},
		{Type: "right", Style: 1, Color: "000000"},
		{Type: "bottom", Style: 1, Color: "000000"},
	}
	headerStyle, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true},
		Border:    border,
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "top"},
	})
	if err != nil {
		return nil, err
	}

	dateFmt := "yyyy-mm-dd"
	dateStyle, err := f.NewStyle(&excelize.Style{CustomNumFmt: &dateFmt})
	if err != nil {
		return nil, err
	}

	header := append([]any{"Data"}, toAny(r.Columns)...)
	if err := f.SetSheetRow(sheet, "A1", &header); err != nil {
		return nil, err
	}
	last, err := excelize.CoordinatesToCellName(len(header), 1)
	if err != nil {
		return nil, err
	}
	if err := f.SetCellStyle(sheet, "A1", last, headerStyle); err != nil {
		return nil, err
	}

	for i, day := range r.Days {
		d, err := time.Parse(time.DateOnly, day)
		if err != nil {
			return nil, fmt.Errorf("dia inválido %q: %w", day, err)
		}
		row := i + 2
		cell, _ := excelize.CoordinatesToCellName(1, row)
		if err := f.SetCellValue(sheet, cell, d); err != nil {
			return nil, err
		}
		if err := f.SetCellStyle(sheet, cell, cell, dateStyle); err != nil {
			return nil, err
		}
		for j, v := range r.Values[i] {
			cell, _ := excelize.CoordinatesToCellName(j+2, row)
			if err := f.SetCellFloat(sheet, cell, v, -1, 64); err != nil {
				return nil, err
			}
		}
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func toAny(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}
