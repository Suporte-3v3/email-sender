# Relatório de Medidores em Go — Design

Data: 2026-10-06 · Substitui: `cumulative_meter_reports/` (Python) · Status: aprovado (incluindo variáveis de ambiente)

> Versão pública: host, porta e usuário SSH reais foram trocados por `<SSH_HOST>`, `<SSH_PORT>` e `<SSH_USER>`.

## 1. Problema

O relatório de consumo diário dos medidores (último − primeiro valor do dia) hoje é um script Python interativo: alguém precisa rodar e digitar porta, senhas, tabelas, TYPE, datas e destinatários. Ele precisa rodar sozinho, agendado, numa Raspberry Pi de 4 GB que já roda outros 8 containers — com o menor consumo possível de RAM e disco. A Pi executora **não** é a Pi do banco; o acesso ao MariaDB continua via SSH.

Bug atual resolvido pela reescrita: `FROM {tabela}` sem crases quebra em tabelas com hífen (`SEL-751-1`).

## 2. Objetivos

- Execução 100% não interativa, configurada só por variáveis de ambiente.
- Container one-shot (`docker run --rm`) disparado pelo cron do host: 0 MB em repouso.
- Imagem ≤ 25 MB; pico de RAM ≤ 30 MB.
- Mesma saída do script atual: xlsx com `Data` + uma coluna por medidor; assunto `Relatório Medidores (<tabelas>) <NOME_LOCAL>`; mesmo corpo de e-mail.

## 3. Não-objetivos

Modo interativo · UI web · múltiplas fazendas numa execução · alerta de falha por e-mail · retentativas automáticas · período "mês calendário" · manter a versão Python em paralelo.

## 4. Arquitetura

```
Pi executora (4 GB)                              Pi do banco (<SSH_HOST>)
┌─────────────────────────┐                      ┌──────────────────────┐
│ cron do host            │                      │                      │
│   └─ docker run --rm    │   SSH (chave)        │  sshd :SSH_PORT      │
│       meter-report (Go) ─┼─────────────────────►│    │                 │
│        │                │   1 conexão TCP,     │    └► 127.0.0.1:3306 │
│        │                │   N canais dentro    │        MariaDB       │
│        ▼                │                      └──────────────────────┘
│   xlsx em memória       │
│        │                │   SMTP 587 + STARTTLS
│        └────────────────┼─────────────────────► smtp.gmail.com
│   exit 0 / exit 1       │
└─────────────────────────┘
```

Sequência de uma execução:

1. Lê e valida as variáveis de ambiente; qualquer erro → exit 1 antes de abrir conexão.
2. Calcula o período `[ini, fim)`.
3. Abre o SSH e, por dentro dele, o MariaDB.
4. Busca o nome do local (`NEWSIR.SETTINGS_POSITION`).
5. Para cada tabela de `TABLES`: busca leituras e calcula o consumo diário.
6. Consolida, gera o xlsx em memória (sem arquivo temporário em disco).
7. Monta e envia o e-mail. Fecha conexões. Exit 0.

Um watchdog interno encerra o processo com exit 1 após 3 minutos, cobrindo qualquer travamento (SSH, banco ou SMTP) — sem ele, um container pendurado ficaria ocupando RAM indefinidamente.

## 5. Conexão SSH + MariaDB

**Como funciona.** O Python usa `sshtunnel`: abre SSH, cria uma porta local e o `pymysql` conecta nela. Em Go não há porta local: a conexão SSH é registrada como "rede" do driver MySQL, e cada conexão que o driver abre vira um canal `direct-tcpip` dentro do SSH, que o `sshd` remoto entrega em `127.0.0.1:3306`.

```go
signer, _ := ssh.ParsePrivateKey(chave)               // SSH_KEY_PATH
hostKeys, _ := knownhosts.New(knownHostsPath)          // SSH_KNOWN_HOSTS

client, err := ssh.Dial("tcp", net.JoinHostPort(host, port), &ssh.ClientConfig{
    User:            sshUser,
    Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)}, // só chave
    HostKeyCallback: hostKeys,                                 // nunca InsecureIgnoreHostKey
    Timeout:         15 * time.Second,
})

mysql.RegisterDialContext("ssh", func(ctx context.Context, addr string) (net.Conn, error) {
    return client.DialContext(ctx, "tcp", addr) // addr resolvido no lado remoto
})

cfg := mysql.NewConfig()
cfg.Net, cfg.Addr = "ssh", dbAddr          // DB_ADDR = 127.0.0.1:3306 (visto do servidor)
cfg.User, cfg.Passwd, cfg.DBName = dbUser, dbPassword, dbName
cfg.ParseTime, cfg.Loc = true, time.Local  // DATETIME → time.Time no fuso de TZ

connector, _ := mysql.NewConnector(cfg)
db := sql.OpenDB(connector)
// encerramento: db.Close(); client.Close()
```

Bibliotecas: `golang.org/x/crypto/ssh` (+ `ssh/knownhosts`), `github.com/go-sql-driver/mysql`.

Por que assim: nenhuma porta aberta na Pi executora, nenhum processo `ssh` auxiliar, nenhum binário extra na imagem; se o SSH cair, o driver devolve erro imediatamente.

## 6. Preparação (uma vez)

**Pi executora** — chave dedicada e host key do servidor:

```bash
sudo mkdir -p /opt/meter-report && cd /opt/meter-report
ssh-keygen -t ed25519 -f id_ed25519 -N "" -C meter-report
ssh-copy-id -i id_ed25519.pub -p <SSH_PORT> <SSH_USER>@<SSH_HOST>
ssh-keyscan -p <SSH_PORT> <SSH_HOST> > known_hosts
ssh-keygen -lf known_hosts            # conferir fingerprint com o da Pi do banco
sudo chown 65534:65534 id_ed25519 && sudo chmod 400 id_ed25519   # container roda como nobody
chmod 600 .env
```

**MariaDB (Pi do banco)** — usuário só de leitura, no lugar do `root`. Pelo túnel a conexão chega como local, por isso `127.0.0.1`:

```sql
CREATE USER 'relatorio'@'127.0.0.1' IDENTIFIED BY '<senha forte>';
GRANT SELECT ON LOG_SENSOR.* TO 'relatorio'@'127.0.0.1';
GRANT SELECT ON NEWSIR.SETTINGS_POSITION TO 'relatorio'@'127.0.0.1';
FLUSH PRIVILEGES;
```

Se o login falhar com "Access denied for 'relatorio'@'localhost'", criar o mesmo usuário também em `@'localhost'` (depende de `skip-name-resolve`).

**sshd da Pi do banco** precisa de `AllowTcpForwarding yes` (padrão; já funciona com o Python).

**Verificações de premissa antes de codar:**

```sql
SELECT VERSION();
DESCRIBE LOG_SENSOR.`SEL-751-1`;                 -- TIME é DATETIME/TIMESTAMP? VALUE é numérico?
SELECT DISTINCT TYPE FROM LOG_SENSOR.`SEL-751-1`; -- repetir para SFR_001_1..3: mesmo TYPE?
```

## 7. Consultas

```sql
-- nome do local (falha não aborta: usa "LOCAL DESCONHECIDO" e registra aviso)
SELECT NAME FROM NEWSIR.SETTINGS_POSITION LIMIT 1;

-- leituras, uma consulta por tabela
SELECT TIME, VALUE FROM `<tabela>`
WHERE TYPE = ? AND TIME >= ? AND TIME < ?
ORDER BY TIME ASC;
```

- **Nome de tabela**: validado contra `^[A-Za-z0-9_-]+$` na carga da config e de novo antes de montar o SQL; sempre entre crases. Corrige o bug do hífen e impede injeção.
- **TYPE e datas**: sempre parâmetros `?`, nunca interpolados.
- **Datas** enviadas como texto `AAAA-MM-DD HH:MM:SS` no horário local, evitando conversão de fuso pelo driver.
- **Intervalo meio aberto** `[ini, fim)`, com `fim` = meia-noite do dia seguinte ao último dia — não perde leituras em `23:59:59.5`.

## 8. Período

- Padrão: últimos `REPORT_DAYS` dias completos, terminando ontem. Ex.: execução em 2026-10-06 com `REPORT_DAYS=7` → `[2026-09-29 00:00, 2026-10-06 00:00)`.
- Reprocessamento: `REPORT_FROM` e `REPORT_TO` (ambos, inclusivos) sobrescrevem `REPORT_DAYS`. Ex.: `2026-09-01`/`2026-09-30` → `[2026-09-01 00:00, 2026-10-01 00:00)`.
- O fuso vem de `TZ`; o binário embute `time/tzdata` porque a imagem `scratch` não tem `/usr/share/zoneinfo`.

## 9. Cálculo

- Por tabela, sobre as leituras já ordenadas por `TIME`: para cada dia, `último VALUE − primeiro VALUE`. Dia com uma leitura só → 0.
- Feito em Go (função pura, testável sem banco), não em SQL: independe da versão do MariaDB e o volume é pequeno (~1.440 linhas/dia/tabela com leitura por minuto).
- Consolidação: dias = união dos dias com dado em qualquer medidor, ordenados; medidor sem dado naquele dia → 0 (paridade com o `fillna(0)` do Python).
- Delta negativo (contador reiniciou) → mantém o valor e registra aviso no log.

## 10. Saída

**xlsx** (`github.com/xuri/excelize/v2`), gerado em memória, nome `relatorio_consolidado.xlsx`, aba `Sheet1`:

| Data | SEL-751-1 | SFR_001_1 | SFR_001_2 | SFR_001_3 |
|---|---|---|---|---|
| 2026-09-29 | 12.5 | 3.0 | 0 | 7.25 |

A coluna `Data` vai como texto ISO (`AAAA-MM-DD`), que ordena corretamente.

**E-mail** (`net/smtp` + `mime/multipart`, stdlib):

- Remetente: `SMTP_USER`. Para: `MAIL_TO`. Cc: `MAIL_CC`.
- Assunto: `Relatório Medidores (SEL-751-1, SFR_001_1, SFR_001_2, SFR_001_3) <NOME_LOCAL>` (codificado em UTF-8/Q-encoding).
- Corpo HTML: `Segue em anexo o relatório dos equipamentos. E-mail automático gerado pela 3v3 Tecnologia.` (Arial 14px, #333, igual ao atual).
- Anexo: xlsx em base64, `Content-Type: application/vnd.openxmlformats-officedocument.spreadsheetml.sheet`.
- Envio: `smtp.gmail.com:587`, STARTTLS, autenticação PLAIN com senha de app.

## 11. Variáveis de ambiente (aprovadas)

| Variável | Obrigatória | Padrão | Descrição |
|---|---|---|---|
| `SSH_HOST` | sim | — | Host da Pi do banco |
| `SSH_PORT` | não | `22` | Porta SSH |
| `SSH_USER` | não | `pi` | Usuário SSH |
| `SSH_KEY_PATH` | não | `/secrets/id_ed25519` | Chave privada (volume read-only) |
| `SSH_KNOWN_HOSTS` | não | `/secrets/known_hosts` | Host key esperada (volume read-only) |
| `DB_ADDR` | não | `127.0.0.1:3306` | Endereço do MariaDB visto do servidor SSH |
| `DB_USER` | sim | — | Usuário só leitura (`relatorio`) |
| `DB_PASSWORD` | sim | — | Senha do usuário do banco |
| `DB_NAME` | não | `LOG_SENSOR` | Banco das leituras |
| `TABLES` | sim | — | Tabelas separadas por vírgula; viram as colunas e o assunto |
| `SENSOR_TYPE` | sim | — | Valor de `TYPE` filtrado |
| `REPORT_DAYS` | não | `7` | Janela em dias completos até ontem (≥ 1) |
| `REPORT_FROM` | não | — | Início `AAAA-MM-DD`; exige `REPORT_TO` |
| `REPORT_TO` | não | — | Fim inclusivo `AAAA-MM-DD`; exige `REPORT_FROM` |
| `SMTP_HOST` | não | `smtp.gmail.com` | Servidor SMTP |
| `SMTP_PORT` | não | `587` | Porta SMTP (STARTTLS) |
| `SMTP_USER` | sim | — | Conta Gmail; também é o remetente |
| `SMTP_PASSWORD` | sim | — | Senha de app do Gmail |
| `MAIL_TO` | sim | — | Destinatários, separados por vírgula |
| `MAIL_CC` | não | — | Cópias, separadas por vírgula |
| `TZ` | não | UTC | Fuso; usar `America/Sao_Paulo` |

Regras de validação: obrigatória vazia → erro listando todas as ausentes; `TABLES`/`MAIL_TO` sem nenhum item após o split → erro; nome de tabela fora da whitelist → erro; `REPORT_FROM` sem `REPORT_TO` (ou vice-versa) → erro; `REPORT_TO` antes de `REPORT_FROM` → erro.

Modelo do `/opt/meter-report/.env`: [`.env.example`](../../../.env.example).

## 12. Empacotamento e execução

**Imagem** — build multi-stage, binário estático, imagem final `scratch` só com binário + certificados CA: ver [`Dockerfile`](../../../Dockerfile).

Build direto na Pi executora (`docker build -t meter-report .`) e `docker image prune` depois, para remover o estágio `golang` (~250 MB temporários). Alternativa: build `linux/arm64` em outra máquina e `docker save | ssh <pi-executora> docker load`.

**Agendamento** — crontab do host (exemplo semanal, segunda 07:00):

```cron
0 7 * * 1  docker run --rm --memory=64m --env-file /opt/meter-report/.env -v /opt/meter-report/id_ed25519:/secrets/id_ed25519:ro -v /opt/meter-report/known_hosts:/secrets/known_hosts:ro meter-report >> /opt/meter-report/run.log 2>&1
```

`--memory=64m` é um teto de segurança para não afetar os outros 8 containers. Reprocessar um período: mesmo comando com `-e REPORT_FROM=... -e REPORT_TO=...`.

## 13. Códigos de saída e log

Todo log vai para stdout/stderr (acaba em `run.log`). Exit 0 = e-mail enviado. Exit 1 = qualquer falha:

| Situação | Comportamento |
|---|---|
| Variável inválida/ausente | exit 1, lista o que falta, nada é conectado |
| SSH inacessível / timeout 15 s | exit 1 |
| Host key diferente do `known_hosts` | exit 1 (atualização manual intencional) |
| Login no banco falhou | exit 1 |
| Erro na consulta de qualquer tabela | exit 1, sem relatório parcial |
| Nome do local indisponível | aviso, segue com `LOCAL DESCONHECIDO` |
| Tabela sem dados no período | aviso, coluna com zeros, segue |
| Nenhum dado em nenhuma tabela | exit 1, e-mail não enviado |
| Delta diário negativo | aviso, valor mantido |
| Falha no SMTP | exit 1 |
| Execução passou de 3 min | watchdog: exit 1 |

## 14. Decisões

| Decisão | Escolha | Descartadas e por quê |
|---|---|---|
| Linguagem | Go, binário estático em `scratch` | Python+pandas (~400 MB imagem); Python slim (~70 MB); Bun (~130 MB, túnel via `ssh2` frágil); n8n (não existe instância; subir uma só pra isso custa mais que entrega) |
| Agendamento | Cron do host → `docker run --rm` | Cron dentro do container (processo residente ocupando RAM) |
| Túnel | `x/crypto/ssh` como dialer do driver | `ssh -L` no entrypoint (binário ssh na imagem); porta local tipo `sshtunnel` |
| Auth SSH | Chave + `known_hosts` | Senha (segredo em texto); ignorar host key (MITM) |
| Usuário do banco | `relatorio`, só `SELECT` | `root` |
| Cálculo | Em Go, função pura | Window functions (versão do MariaDB, teste difícil); `MAX−MIN` (erra se o contador zerar) |
| Dias sem medição | 0 só nos dias com dado em algum medidor | Todos os dias do período (muda a saída; dia todo zero mascara Pi offline) |
| Falha parcial | Aborta, sem e-mail | Relatório parcial (destinatário não percebe o buraco) |
| Período | Janela de N dias + override FROM/TO | Datas fixas no env (editar a cada execução) |

## 15. Premissas

1. Um único `TYPE` serve para todas as tabelas (verificar na seção 6).
2. `TIME` é `DATETIME`/`TIMESTAMP` em horário local; `VALUE` é numérico (verificar na seção 6).
3. Medidores: `SEL-751-1`, `SFR_001_1`, `SFR_001_2`, `SFR_001_3`.
4. Frequência e janela definidas no deploy (cron + `REPORT_DAYS`).
5. Pi executora com SO 64 bits (`linux/arm64`) e Docker instalado.
6. Gmail com senha de app continua sendo o remetente.

## 16. Divergências

Nenhuma. O usuário encerrou a entrevista antes do gate formal; pontos não discutidos viraram premissas (seção 15).

## 17. Critérios de aceite

1. `go test ./...` passa (config, período, cálculo, nome de tabela, xlsx, MIME).
2. Execução real com `SEL-751-1` incluso gera e envia o e-mail com o xlsx anexo.
3. Para `SFR_001_*` num mesmo período, valores iguais aos do script Python.
4. Imagem ≤ 25 MB (`docker image ls`); pico de RAM ≤ 30 MB (`/usr/bin/time -v` no binário).
5. Sem dados no período → exit 1 e nenhum e-mail.
6. Nenhum segredo no código ou na imagem; só via `--env-file` e volumes read-only.

## 18. Questões abertas

Nenhuma bloqueante.
