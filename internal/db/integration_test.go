package db

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Suporte-3v3/email-sender/internal/config"
	"github.com/Suporte-3v3/email-sender/internal/period"
)

// Integração com um MariaDB de verdade: o CI sobe um em service container. Sem
// MARIADB_TEST_ADDR o teste é pulado. O schema abaixo é a premissa da spec
// (§15, a confirmar na #2): TIME DATETIME local, TYPE texto, VALUE numérico.

var sp = mustLoc("America/Sao_Paulo")

func mustLoc(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

func testDBConfig(t *testing.T) *config.Config {
	t.Helper()
	addr := os.Getenv("MARIADB_TEST_ADDR")
	if addr == "" {
		// No CI pular seria silencioso demais: sem o service container, falha.
		if os.Getenv("CI") != "" {
			t.Fatal("CI sem MARIADB_TEST_ADDR: confira o service container do MariaDB no ci.yml")
		}
		t.Skip("defina MARIADB_TEST_ADDR (e MARIADB_TEST_PASSWORD) para testar contra o MariaDB")
	}
	return &config.Config{DBAddr: addr, DBUser: "root", DBPassword: os.Getenv("MARIADB_TEST_PASSWORD"), DBName: "LOG_SENSOR"}
}

// setupDB recria o schema e os dados de teste e devolve uma conexão em LOG_SENSOR.
func setupDB(t *testing.T) (*config.Config, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	c := testDBConfig(t)

	admin := *c
	admin.DBName = ""
	a, err := Open(ctx, &admin, sp, nil)
	if err != nil {
		t.Fatalf("Open admin: %v", err)
	}
	defer a.Close()
	for _, q := range []string{
		"CREATE DATABASE IF NOT EXISTS LOG_SENSOR",
		"CREATE DATABASE IF NOT EXISTS NEWSIR",
		"DROP TABLE IF EXISTS LOG_SENSOR.`SEL-751-1`, LOG_SENSOR.SFR_001_1, NEWSIR.SETTINGS_POSITION",
		"CREATE TABLE LOG_SENSOR.`SEL-751-1` (TIME DATETIME NOT NULL, TYPE VARCHAR(32) NOT NULL, VALUE DECIMAL(14,3) NOT NULL)",
		"CREATE TABLE LOG_SENSOR.SFR_001_1 (TIME DATETIME NOT NULL, TYPE VARCHAR(32) NOT NULL, VALUE DECIMAL(14,3) NOT NULL)",
		"CREATE TABLE NEWSIR.SETTINGS_POSITION (NAME VARCHAR(100))",
		"INSERT INTO NEWSIR.SETTINGS_POSITION VALUES ('Fazenda São João')",
		// Fora de ordem de propósito: a consulta tem que ordenar por TIME.
		"INSERT INTO LOG_SENSOR.`SEL-751-1` VALUES " +
			"('2026-09-30 23:59:59', 'kWh', 1030)," + // último instante do período: entra
			"('2026-09-28 23:59:59', 'kWh', 999)," + // antes do início: fora
			"('2026-09-29 23:30:00', 'kWh', 1012.5)," + // já é 30/09 em UTC: tem que voltar como 29/09 local
			"('2026-10-01 00:00:00', 'kWh', 1031)," + // fim exclusivo: fora
			"('2026-09-30 12:00:00', 'kW', 1020.25)," + // outro TYPE: fora
			"('2026-09-29 00:00:00', 'kWh', 1000)", // início inclusivo: entra
		"INSERT INTO LOG_SENSOR.SFR_001_1 VALUES ('2026-09-29 08:00:00', 'kWh', 3)",
	} {
		if _, err := a.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}

	db, err := Open(ctx, c, sp, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return c, db
}

var testRange = period.Range{
	Start: time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC),
	End:   time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
}

func checkReadings(t *testing.T, db *sql.DB) {
	t.Helper()
	got, err := Readings(context.Background(), db, "SEL-751-1", "kWh", testRange)
	if err != nil {
		t.Fatalf("Readings: %v", err)
	}
	want := []struct {
		at    time.Time
		value float64
	}{
		{time.Date(2026, 9, 29, 0, 0, 0, 0, sp), 1000},
		{time.Date(2026, 9, 29, 23, 30, 0, 0, sp), 1012.5},
		{time.Date(2026, 9, 30, 23, 59, 59, 0, sp), 1030},
	}
	if len(got) != len(want) {
		t.Fatalf("Readings = %v, quero %d leituras", got, len(want))
	}
	for i, w := range want {
		if !got[i].Time.Equal(w.at) || got[i].Value != w.value {
			t.Errorf("leitura %d = %v %v, quero %v %v", i, got[i].Time, got[i].Value, w.at, w.value)
		}
		if got[i].Time.Location() != sp {
			t.Errorf("leitura %d no fuso %v, quero %v", i, got[i].Time.Location(), sp)
		}
	}
}

func TestReadingsMariaDB(t *testing.T) {
	_, db := setupDB(t)
	checkReadings(t, db)
}

func TestReadAllMariaDB(t *testing.T) {
	_, db := setupDB(t)
	ctx := context.Background()

	all, err := ReadAll(ctx, db, []string{"SEL-751-1", "SFR_001_1"}, "kWh", testRange)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(all["SEL-751-1"]) != 3 || len(all["SFR_001_1"]) != 1 {
		t.Errorf("ReadAll = %v", all)
	}

	_, err = ReadAll(ctx, db, []string{"SEL-751-1", "NAO_EXISTE"}, "kWh", testRange)
	if err == nil || !strings.Contains(err.Error(), "NAO_EXISTE") {
		t.Errorf("ReadAll com tabela inexistente = %v, quero erro citando a tabela", err)
	}
}

func TestSiteNameMariaDB(t *testing.T) {
	_, db := setupDB(t)
	ctx := context.Background()

	if name, err := SiteName(ctx, db); err != nil || name != "Fazenda São João" {
		t.Errorf("SiteName = %q, %v", name, err)
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM NEWSIR.SETTINGS_POSITION"); err != nil {
		t.Fatal(err)
	}
	if name, err := SiteName(ctx, db); err == nil || name != UnknownSite {
		t.Errorf("SiteName com tabela vazia = %q, %v; quero %q e erro", name, err, UnknownSite)
	}
}

func TestOpenWrongPasswordMariaDB(t *testing.T) {
	c := testDBConfig(t)
	c.DBPassword = "errada"
	if _, err := Open(context.Background(), c, sp, nil); err == nil {
		t.Fatal("Open aceitou senha errada")
	}
}

// Caminho de produção inteiro: driver → canal SSH → MariaDB.
func TestReadingsThroughSSHMariaDB(t *testing.T) {
	c, _ := setupDB(t)

	hostKey, _ := newSigner(t)
	client, clientPriv := newSigner(t)
	sshAddr := sshServer(t, hostKey, client.PublicKey())
	sc := sshConfig(t, sshAddr, clientPriv, hostKey.PublicKey())

	ctx := context.Background()
	tunnel, err := DialSSH(ctx, sc)
	if err != nil {
		t.Fatalf("DialSSH: %v", err)
	}
	defer tunnel.Close()

	db, err := Open(ctx, c, sp, tunnel.DialContext)
	if err != nil {
		t.Fatalf("Open via SSH: %v", err)
	}
	defer db.Close()
	checkReadings(t, db)

	// Com o túnel fechado o driver tem que falhar, nunca cair em TCP direto.
	tunnel.Close()
	if err := db.PingContext(ctx); err == nil {
		t.Fatal("Ping funcionou com o túnel SSH fechado")
	}
}
