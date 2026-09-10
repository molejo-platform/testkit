# Contribuindo com o Molejo Testkit

[English](../../CONTRIBUTING.md) |
[Español (Argentina)](../es-AR/CONTRIBUTING.md)

Obrigado por ajudar a melhorar o Molejo Testkit. O projeto é experimental e possui
releases de pré-lançamento, portanto as mudanças devem ser pequenas, reproduzíveis
e diretamente relacionadas a uma necessidade de teste.

## Antes de começar

- Verifique issues e pull requests existentes antes de duplicar trabalho.
- Abra uma issue para alterações que introduzam protocolo, dependência, endpoint
  público ou comportamento incompatível.
- Reporte suspeitas de vulnerabilidade de forma privada conforme
  [SECURITY.md](SECURITY.md).

## Ambiente de desenvolvimento

Você precisa de:

- Go 1.26.x;
- Docker com Buildx para validar o contêiner;
- Git e ferramentas comuns de linha de comando.

Clone o repositório e execute os testes de base antes de modificá-lo:

```sh
git clone https://github.com/molejo-platform/testkit.git
cd testkit
go test -race -cover ./...
go vet ./...
go mod tidy -diff
```

## Realizando alterações

- Mantenha a implementação determinística e adequada a testes descartáveis.
- Evite abstrações ou dependências sem um requisito de teste demonstrado.
- Trate endpoints, payloads, códigos de saída e comportamento do contêiner como
  contratos.
- Mantenha probes pontuais como comandos explícitos e pares periódicos em uma
  allowlist read-only. Destinos de diagnóstico escolhidos na requisição exigem
  capacidade opt-in, autenticação, política estrita, operações fixas, execução
  limitada, logs sem segredos e regressões de segurança.
- Preserve o runtime restrito: não root, compatível com filesystem somente
  leitura, sem capabilities obrigatórias e com shutdown limitado.
- Adicione um teste de regressão antes de corrigir um defeito.
- Atualize a documentação em inglês e as traduções correspondentes quando o
  comportamento mudar.
- Use o logo oficial e os tokens semânticos de cor descritos em
  [docs/BRANDING.md](../BRANDING.md) nas alterações da interface do browser.

Formate e valide o código:

```sh
gofmt -w *.go
go test -race -cover ./...
go vet ./...
go mod tidy -diff
```

Quando o contrato do contêiner mudar, também construa e execute a imagem com os
parâmetros restritos documentados no [README.md](README.md).

## Mensagens de commit

Use [Conventional Commits](https://www.conventionalcommits.org/) com resumo em
inglês. Adicione um corpo quando o commit alterar mais de três arquivos ou quando
o motivo não estiver evidente no resumo.

Exemplos:

```text
feat(rest): add deterministic headers endpoint
fix(websocket): close clients during shutdown
docs: document Kubernetes probe usage
```

## Pull requests

Um pull request deve:

- explicar o problema de teste resolvido;
- descrever a alteração observável do contrato;
- incluir cobertura de regressão;
- listar os comandos usados na validação;
- evitar limpezas não relacionadas ou ruído gerado;
- atualizar a documentação quando aplicável.

Os mantenedores podem solicitar uma mudança menor quando comportamentos não
relacionados estiverem acoplados.

## Licença da contribuição

Ao contribuir, você concorda que suas contribuições sejam licenciadas sob a
[Apache License 2.0](../../LICENSE), conforme a seção 5 da licença.
