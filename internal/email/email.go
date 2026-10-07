// Package email monta a mensagem do relatório (MIME, só stdlib) e a envia por
// SMTP com STARTTLS obrigatório.
package email

import (
	"fmt"
	"slices"
	"strings"
)

// ContentTypeXLSX é o tipo MIME do anexo.
const ContentTypeXLSX = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// Subject monta o assunto como o script Python: tabelas separadas por ", ".
func Subject(tables []string, site string) string {
	return fmt.Sprintf("Relatório Medidores (%s) %s", strings.Join(tables, ", "), site)
}

// Message é o e-mail do relatório. Os endereços devem vir validados pelo
// config (endereço puro, sem nome de exibição).
type Message struct {
	From     string
	To       []string
	Cc       []string
	Subject  string
	FileName string // nome do anexo
	File     []byte // conteúdo do anexo (xlsx)
}

// Recipients é o envelope RCPT TO: To + Cc.
func (m Message) Recipients() []string { return slices.Concat(m.To, m.Cc) }
