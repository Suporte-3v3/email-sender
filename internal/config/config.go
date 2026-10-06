// Package config carrega e valida a configuração a partir de variáveis de
// ambiente. Toda validação acontece aqui, antes de qualquer conexão.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/mail"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// DateLayout é o formato de REPORT_FROM e REPORT_TO.
const DateLayout = "2006-01-02"

type Config struct {
	SSHHost       string
	SSHPort       int
	SSHUser       string
	SSHKeyPath    string
	SSHKnownHosts string

	DBAddr     string
	DBUser     string
	DBPassword string
	DBName     string

	Tables     []string
	SensorType string

	// ReportDays vale quando ReportFrom e ReportTo são zero.
	ReportDays int
	// ReportFrom e ReportTo são datas civis inclusivas (meia-noite UTC;
	// só ano, mês e dia importam). Ambas zero ou ambas preenchidas.
	ReportFrom time.Time
	ReportTo   time.Time

	SMTPHost     string
	SMTPPort     int
	SMTPUser     string
	SMTPPassword string

	MailTo []string
	MailCc []string
}

// HasRange informa se REPORT_FROM/REPORT_TO sobrescrevem REPORT_DAYS.
func (c *Config) HasRange() bool { return !c.ReportFrom.IsZero() }

var tableName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ValidTable informa se name pode ser usado como nome de tabela entre crases.
func ValidTable(name string) bool { return tableName.MatchString(name) }

var required = []string{
	"SSH_HOST", "DB_USER", "DB_PASSWORD", "TABLES", "SENSOR_TYPE",
	"SMTP_USER", "SMTP_PASSWORD", "MAIL_TO",
}

// Load lê a configuração via getenv (normalmente os.Getenv) e devolve todos
// os problemas encontrados num único erro. Mensagens nunca contêm valores de
// senha.
func Load(getenv func(string) string) (*Config, error) {
	get := func(key, def string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return def
	}

	var errs []error
	var missing []string
	for _, k := range required {
		if get(k, "") == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		errs = append(errs, fmt.Errorf("variáveis obrigatórias ausentes: %s", strings.Join(missing, ", ")))
	}

	port := func(key, def string) int {
		v := get(key, def)
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 65535 {
			errs = append(errs, fmt.Errorf("%s inválida: %q (esperado 1–65535)", key, v))
		}
		return n
	}

	c := &Config{
		SSHHost:       get("SSH_HOST", ""),
		SSHPort:       port("SSH_PORT", "22"),
		SSHUser:       get("SSH_USER", "pi"),
		SSHKeyPath:    get("SSH_KEY_PATH", "/secrets/id_ed25519"),
		SSHKnownHosts: get("SSH_KNOWN_HOSTS", "/secrets/known_hosts"),
		DBAddr:        get("DB_ADDR", "127.0.0.1:3306"),
		DBUser:        get("DB_USER", ""),
		DBPassword:    get("DB_PASSWORD", ""),
		DBName:        get("DB_NAME", "LOG_SENSOR"),
		Tables:        splitList(get("TABLES", "")),
		SensorType:    get("SENSOR_TYPE", ""),
		SMTPHost:      get("SMTP_HOST", "smtp.gmail.com"),
		SMTPPort:      port("SMTP_PORT", "587"),
		SMTPUser:      get("SMTP_USER", ""),
		SMTPPassword:  get("SMTP_PASSWORD", ""),
		MailTo:        splitList(get("MAIL_TO", "")),
		MailCc:        splitList(get("MAIL_CC", "")),
	}

	if _, _, err := net.SplitHostPort(c.DBAddr); err != nil {
		errs = append(errs, fmt.Errorf("DB_ADDR inválido: %q (esperado host:porta)", c.DBAddr))
	}

	if get("TABLES", "") != "" && len(c.Tables) == 0 {
		errs = append(errs, errors.New("TABLES não tem nenhuma tabela"))
	}
	for i, t := range c.Tables {
		if !ValidTable(t) {
			errs = append(errs, fmt.Errorf("TABLES: nome de tabela inválido %q (permitido: letras, dígitos, _ e -)", t))
		} else if slices.Contains(c.Tables[:i], t) {
			errs = append(errs, fmt.Errorf("TABLES: tabela repetida %q", t))
		}
	}

	if get("MAIL_TO", "") != "" && len(c.MailTo) == 0 {
		errs = append(errs, errors.New("MAIL_TO não tem nenhum destinatário"))
	}
	errs = append(errs, checkAddrs("MAIL_TO", c.MailTo)...)
	errs = append(errs, checkAddrs("MAIL_CC", c.MailCc)...)
	if c.SMTPUser != "" {
		errs = append(errs, checkAddrs("SMTP_USER", []string{c.SMTPUser})...)
	}

	days := get("REPORT_DAYS", "7")
	if n, err := strconv.Atoi(days); err != nil || n < 1 {
		errs = append(errs, fmt.Errorf("REPORT_DAYS inválido: %q (esperado inteiro ≥ 1)", days))
	} else {
		c.ReportDays = n
	}

	from, to := get("REPORT_FROM", ""), get("REPORT_TO", "")
	switch {
	case from == "" && to == "":
	case from == "" || to == "":
		errs = append(errs, errors.New("REPORT_FROM e REPORT_TO devem ser informados juntos"))
	default:
		f, errF := time.Parse(DateLayout, from)
		t, errT := time.Parse(DateLayout, to)
		if errF != nil {
			errs = append(errs, fmt.Errorf("REPORT_FROM inválido: %q (esperado AAAA-MM-DD)", from))
		}
		if errT != nil {
			errs = append(errs, fmt.Errorf("REPORT_TO inválido: %q (esperado AAAA-MM-DD)", to))
		}
		if errF == nil && errT == nil {
			if t.Before(f) {
				errs = append(errs, fmt.Errorf("REPORT_TO (%s) é anterior a REPORT_FROM (%s)", to, from))
			}
			c.ReportFrom, c.ReportTo = f, t
		}
	}

	// Sem esta checagem, um TZ inválido cai em UTC silenciosamente e o
	// período do relatório fica deslocado.
	if tz := get("TZ", ""); tz != "" {
		if _, err := time.LoadLocation(strings.TrimPrefix(tz, ":")); err != nil {
			errs = append(errs, fmt.Errorf("TZ inválido: %q", tz))
		}
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return c, nil
}

// splitList separa por vírgula, apara espaços e descarta itens vazios.
func splitList(s string) []string {
	var out []string
	for item := range strings.SplitSeq(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func checkAddrs(key string, addrs []string) []error {
	var errs []error
	for _, a := range addrs {
		if p, err := mail.ParseAddress(a); err != nil || p.Address != a {
			errs = append(errs, fmt.Errorf("%s: endereço inválido %q", key, a))
		}
	}
	return errs
}
