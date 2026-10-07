// Package db acessa o MariaDB através de SSH: a conexão SSH é a "rede" do
// driver MySQL, sem porta local nem binário ssh.
package db

import (
	"fmt"

	"github.com/Suporte-3v3/email-sender/internal/config"
)

// readingsQuery monta a consulta de uma tabela. O nome é revalidado aqui,
// imediatamente antes de entrar no SQL, e vai sempre entre crases.
func readingsQuery(table string) (string, error) {
	if !config.ValidTable(table) {
		return "", fmt.Errorf("nome de tabela inválido %q", table)
	}
	return "SELECT TIME, VALUE FROM `" + table + "` WHERE TYPE = ? AND TIME >= ? AND TIME < ? ORDER BY TIME ASC", nil
}
