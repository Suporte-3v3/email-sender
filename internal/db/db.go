// Package db acessa o MariaDB através de SSH: a conexão SSH é a "rede" do
// driver MySQL, sem porta local nem binário ssh.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/Suporte-3v3/email-sender/internal/config"
	"github.com/Suporte-3v3/email-sender/internal/consumption"
	"github.com/Suporte-3v3/email-sender/internal/period"
	"github.com/go-sql-driver/mysql"
)

// DialFunc abre a conexão de rede do driver; ssh.Client.DialContext serve.
type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// dbTimeout limita a abertura de cada conexão com o banco.
const dbTimeout = 15 * time.Second

// Open conecta no MariaDB em DB_ADDR usando dial (nil = TCP direto) e confere o
// login. DATETIME volta como time.Time no fuso loc, com o mesmo relógio de
// parede gravado no banco.
func Open(ctx context.Context, c *config.Config, loc *time.Location, dial DialFunc) (*sql.DB, error) {
	mc := mysql.NewConfig()
	mc.Net, mc.Addr = "tcp", c.DBAddr
	mc.User, mc.Passwd, mc.DBName = c.DBUser, c.DBPassword, c.DBName
	mc.ParseTime, mc.Loc = true, loc
	mc.Timeout = dbTimeout
	mc.DialFunc = dial
	connector, err := mysql.NewConnector(mc)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(connector)
	// As consultas são sequenciais; uma conexão = um canal SSH.
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("conectar no banco %s como %s: %w", c.DBAddr, c.DBUser, err)
	}
	return db, nil
}

// UnknownSite é o nome usado quando NEWSIR.SETTINGS_POSITION não responde.
const UnknownSite = "LOCAL DESCONHECIDO"

// SiteName busca o nome do local. Em qualquer falha (erro, tabela vazia, nome
// vazio) devolve UnknownSite junto com o erro: quem chama registra o aviso e
// segue.
func SiteName(ctx context.Context, db *sql.DB) (string, error) {
	var name sql.NullString
	err := db.QueryRowContext(ctx, "SELECT NAME FROM NEWSIR.SETTINGS_POSITION LIMIT 1").Scan(&name)
	switch {
	case err != nil:
		return UnknownSite, fmt.Errorf("nome do local: %w", err)
	case strings.TrimSpace(name.String) == "":
		return UnknownSite, fmt.Errorf("nome do local: NEWSIR.SETTINGS_POSITION.NAME vazio")
	}
	return strings.TrimSpace(name.String), nil
}

// readingsQuery monta a consulta de uma tabela. O nome é revalidado aqui,
// imediatamente antes de entrar no SQL, e vai sempre entre crases.
func readingsQuery(table string) (string, error) {
	if !config.ValidTable(table) {
		return "", fmt.Errorf("nome de tabela inválido %q", table)
	}
	return "SELECT TIME, VALUE FROM `" + table + "` WHERE TYPE = ? AND TIME >= ? AND TIME < ? ORDER BY TIME ASC", nil
}

// Readings devolve as leituras de uma tabela no período [r.Start, r.End),
// filtradas por TYPE, em ordem de TIME.
func Readings(ctx context.Context, db *sql.DB, table, sensorType string, r period.Range) ([]consumption.Reading, error) {
	q, err := readingsQuery(table)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, q, sensorType, r.SQLStart(), r.SQLEnd())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", table, err)
	}
	defer rows.Close()
	var out []consumption.Reading
	for rows.Next() {
		var rd consumption.Reading
		if err := rows.Scan(&rd.Time, &rd.Value); err != nil {
			return nil, fmt.Errorf("%s: %w", table, err)
		}
		out = append(out, rd)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", table, err)
	}
	return out, nil
}

// ReadAll lê todas as tabelas, na ordem dada. Erro em qualquer uma aborta tudo:
// não existe relatório parcial.
func ReadAll(ctx context.Context, db *sql.DB, tables []string, sensorType string, r period.Range) (map[string][]consumption.Reading, error) {
	out := make(map[string][]consumption.Reading, len(tables))
	for _, t := range tables {
		rs, err := Readings(ctx, db, t, sensorType, r)
		if err != nil {
			return nil, err
		}
		out[t] = rs
	}
	return out, nil
}
