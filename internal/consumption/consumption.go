// Package consumption calcula o consumo diário dos medidores (último − primeiro
// valor do dia) e consolida as tabelas numa grade dia × medidor.
package consumption

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"
)

// Reading é uma leitura de medidor. Time deve estar no fuso do relatório; o
// driver MySQL já a devolve em time.Local.
type Reading struct {
	Time  time.Time
	Value float64
}

// Daily devolve o consumo por dia (AAAA-MM-DD no fuso de cada Time): a última
// leitura do dia menos a primeira. Dia com uma leitura só vale 0. Ordena uma
// cópia por Time (estável); a entrada não é alterada.
func Daily(readings []Reading) map[string]float64 {
	sorted := slices.Clone(readings)
	slices.SortStableFunc(sorted, func(a, b Reading) int { return a.Time.Compare(b.Time) })

	first := map[string]float64{}
	out := map[string]float64{}
	for _, r := range sorted {
		day := r.Time.Format(time.DateOnly)
		f, ok := first[day]
		if !ok {
			f = r.Value
			first[day] = f
		}
		out[day] = r.Value - f
	}
	return out
}

// Report é a grade consolidada: Values[i][j] é o consumo do dia Days[i] no
// medidor Columns[j].
type Report struct {
	Days    []string // AAAA-MM-DD, em ordem crescente
	Columns []string // tabelas, na ordem de TABLES
	Values  [][]float64
}

// ErrNoData indica que nenhuma tabela teve leitura no período; nesse caso o
// e-mail não deve ser enviado.
var ErrNoData = errors.New("nenhuma tabela tem dados no período")

// Build consolida o consumo diário das tabelas, na ordem dada. Os dias são a
// união dos dias com dado em qualquer tabela; tabela sem dado num dia vale 0.
// Os avisos (tabela sem dados, consumo negativo) não impedem o relatório.
func Build(tables []string, readings map[string][]Reading) (Report, []string, error) {
	var warnings []string
	perTable := make([]map[string]float64, len(tables))
	daySet := map[string]bool{}
	for j, t := range tables {
		perTable[j] = Daily(readings[t])
		if len(perTable[j]) == 0 {
			warnings = append(warnings, fmt.Sprintf("%s: sem dados no período; coluna preenchida com 0", t))
		}
		for d := range perTable[j] {
			daySet[d] = true
		}
	}
	if len(daySet) == 0 {
		return Report{}, warnings, ErrNoData
	}

	days := slices.Sorted(maps.Keys(daySet))
	values := make([][]float64, len(days))
	for i, d := range days {
		values[i] = make([]float64, len(tables))
		for j, t := range tables {
			v := perTable[j][d] // dia ausente → 0
			if v < 0 {
				warnings = append(warnings, fmt.Sprintf("%s: consumo negativo em %s (%g); o contador pode ter reiniciado", t, d, v))
			}
			values[i][j] = v
		}
	}
	return Report{Days: days, Columns: slices.Clone(tables), Values: values}, warnings, nil
}
