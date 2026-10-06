package config

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func validEnv() map[string]string {
	return map[string]string{
		"SSH_HOST":      "db.example",
		"DB_USER":       "relatorio",
		"DB_PASSWORD":   "s3nha-secreta",
		"TABLES":        "SEL-751-1, SFR_001_1,SFR_001_2 ,SFR_001_3",
		"SENSOR_TYPE":   "ENERGIA",
		"SMTP_USER":     "relatorios@example.com",
		"SMTP_PASSWORD": "app-password-secreta",
		"MAIL_TO":       "a@example.com, b@example.com",
	}
}

func load(env map[string]string) (*Config, error) {
	return Load(func(k string) string { return env[k] })
}

func TestLoadDefaults(t *testing.T) {
	c, err := load(validEnv())
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if c.SSHPort != 22 || c.SSHUser != "pi" || c.SSHKeyPath != "/secrets/id_ed25519" ||
		c.SSHKnownHosts != "/secrets/known_hosts" || c.DBAddr != "127.0.0.1:3306" ||
		c.DBName != "LOG_SENSOR" || c.SMTPHost != "smtp.gmail.com" || c.SMTPPort != 587 ||
		c.ReportDays != 7 || c.HasRange() || c.MailCc != nil {
		t.Errorf("padrões errados: %+v", c)
	}
	if want := []string{"SEL-751-1", "SFR_001_1", "SFR_001_2", "SFR_001_3"}; !slices.Equal(c.Tables, want) {
		t.Errorf("Tables = %q, quero %q", c.Tables, want)
	}
	if want := []string{"a@example.com", "b@example.com"}; !slices.Equal(c.MailTo, want) {
		t.Errorf("MailTo = %q, quero %q", c.MailTo, want)
	}
}

func TestLoadOverrides(t *testing.T) {
	env := validEnv()
	env["SSH_PORT"] = "2222"
	env["SMTP_PORT"] = "465"
	env["REPORT_DAYS"] = "30"
	env["MAIL_CC"] = "c@example.com"
	env["REPORT_FROM"] = "2026-09-01"
	env["REPORT_TO"] = "2026-09-30"
	env["TZ"] = "America/Sao_Paulo"

	c, err := load(env)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if c.SSHPort != 2222 || c.SMTPPort != 465 || c.ReportDays != 30 || !slices.Equal(c.MailCc, []string{"c@example.com"}) {
		t.Errorf("overrides errados: %+v", c)
	}
	if !c.HasRange() ||
		!c.ReportFrom.Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)) ||
		!c.ReportTo.Equal(time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("intervalo errado: %v – %v", c.ReportFrom, c.ReportTo)
	}
}

func TestLoadSameDayRange(t *testing.T) {
	env := validEnv()
	env["REPORT_FROM"] = "2026-09-01"
	env["REPORT_TO"] = "2026-09-01"
	if _, err := load(env); err != nil {
		t.Fatalf("FROM == TO deveria valer: %v", err)
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name string
		set  map[string]string
		want []string // trechos que devem aparecer na mensagem
	}{
		{"obrigatória ausente", map[string]string{"DB_USER": ""}, []string{"ausentes: DB_USER"}},
		{"só espaços conta como ausente", map[string]string{"SENSOR_TYPE": "   "}, []string{"ausentes: SENSOR_TYPE"}},
		{"várias ausentes num erro só", map[string]string{"SSH_HOST": "", "SMTP_PASSWORD": "", "MAIL_TO": ""},
			[]string{"ausentes: SSH_HOST, SMTP_PASSWORD, MAIL_TO"}},
		{"TABLES sem itens", map[string]string{"TABLES": " , ,"}, []string{"TABLES não tem nenhuma tabela"}},
		{"MAIL_TO sem itens", map[string]string{"MAIL_TO": ","}, []string{"MAIL_TO não tem nenhum destinatário"}},
		{"injeção no nome de tabela", map[string]string{"TABLES": "SEL-751-1,x`;DROP TABLE y"}, []string{"nome de tabela inválido \"x`;DROP TABLE y\""}},
		{"tabela com ponto", map[string]string{"TABLES": "NEWSIR.SETTINGS_POSITION"}, []string{"nome de tabela inválido"}},
		{"tabela repetida", map[string]string{"TABLES": "A,B,A"}, []string{"tabela repetida \"A\""}},
		{"FROM sem TO", map[string]string{"REPORT_FROM": "2026-09-01"}, []string{"devem ser informados juntos"}},
		{"TO sem FROM", map[string]string{"REPORT_TO": "2026-09-01"}, []string{"devem ser informados juntos"}},
		{"TO antes de FROM", map[string]string{"REPORT_FROM": "2026-09-30", "REPORT_TO": "2026-09-01"}, []string{"anterior a REPORT_FROM"}},
		{"data inválida", map[string]string{"REPORT_FROM": "2026-02-30", "REPORT_TO": "01/03/2026"},
			[]string{"REPORT_FROM inválido", "REPORT_TO inválido"}},
		{"REPORT_DAYS zero", map[string]string{"REPORT_DAYS": "0"}, []string{"REPORT_DAYS inválido"}},
		{"REPORT_DAYS texto", map[string]string{"REPORT_DAYS": "sete"}, []string{"REPORT_DAYS inválido"}},
		{"porta SSH texto", map[string]string{"SSH_PORT": "abc"}, []string{"SSH_PORT inválida"}},
		{"porta SMTP fora da faixa", map[string]string{"SMTP_PORT": "70000"}, []string{"SMTP_PORT inválida"}},
		{"DB_ADDR sem porta", map[string]string{"DB_ADDR": "127.0.0.1"}, []string{"DB_ADDR inválido"}},
		{"e-mail inválido", map[string]string{"MAIL_TO": "a@example.com,sem-arroba", "MAIL_CC": "Fulano <f@example.com>"},
			[]string{"MAIL_TO: endereço inválido \"sem-arroba\"", "MAIL_CC: endereço inválido"}},
		{"TZ inválido", map[string]string{"TZ": "Nada/Isso"}, []string{"TZ inválido"}},
		{"acumula erros diferentes", map[string]string{"DB_USER": "", "REPORT_DAYS": "0", "TABLES": "a b"},
			[]string{"ausentes: DB_USER", "REPORT_DAYS inválido", "nome de tabela inválido"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := validEnv()
			for k, v := range tt.set {
				env[k] = v
			}
			c, err := load(env)
			if err == nil {
				t.Fatalf("esperava erro, veio %+v", c)
			}
			if c != nil {
				t.Errorf("Config deveria ser nil em erro")
			}
			msg := err.Error()
			for _, w := range tt.want {
				if !strings.Contains(msg, w) {
					t.Errorf("erro %q não contém %q", msg, w)
				}
			}
			for _, secret := range []string{"s3nha-secreta", "app-password-secreta"} {
				if strings.Contains(msg, secret) {
					t.Errorf("erro vaza senha: %q", msg)
				}
			}
		})
	}
}

func TestValidTable(t *testing.T) {
	for _, ok := range []string{"SEL-751-1", "SFR_001_1", "abc", "A-B_C-9"} {
		if !ValidTable(ok) {
			t.Errorf("ValidTable(%q) = false, quero true", ok)
		}
	}
	for _, bad := range []string{"", "a b", "a`b", "a.b", "a;b", "tábua", "a/b", "a'b"} {
		if ValidTable(bad) {
			t.Errorf("ValidTable(%q) = true, quero false", bad)
		}
	}
}
