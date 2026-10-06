# meter-report

Relatório diário de consumo dos medidores (último − primeiro valor do dia), enviado por e-mail como xlsx. Binário Go one-shot, empacotado numa imagem `scratch` e disparado pelo cron do host com `docker run --rm`. Substitui o script Python interativo `cumulative_meter_reports/`.

- Andamento: issue #1

## Stack

Go 1.26 (stdlib + `x/crypto/ssh`, `go-sql-driver/mysql`, `excelize`), imagem `scratch` para `linux/arm64`, configuração só por variáveis de ambiente (ver [.env.example](.env.example)).

## Desenvolvimento

```bash
go vet ./... && go test ./...
docker buildx build --platform linux/arm64 -t meter-report .
```

Repositório público: segredos e dados de infra (host, porta, usuário, senhas, chaves) ficam só no `.env` e nos volumes da máquina que executa.
