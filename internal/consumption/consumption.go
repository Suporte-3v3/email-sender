// Package consumption calcula o consumo diário dos medidores (último − primeiro
// valor do dia) e consolida as tabelas numa grade dia × medidor.
package consumption

import (
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
