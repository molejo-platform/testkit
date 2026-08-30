# Molejo Testkit

[English](../../README.md) |
[Español (Argentina)](../es-AR/README.md)

> Projeto experimental em pré-release. O Molejo Testkit é uma fixture de testes,
> não uma aplicação de produção.

O Molejo Testkit é uma pequena imagem de contêiner determinística para exercitar
aplicações HTTP, entrega de aplicações no Kubernetes e políticas de rede. Um único
binário Go estático fornece REST, GraphQL, Server-Sent Events (SSE), WebSocket,
endpoints de saúde e um comando explícito de probe de saída.

O servidor HTTP público e o probe arbitrário de rede são intencionalmente
separados. O modo servidor só pode chamar pares declarados em um arquivo read-only
opcional, portanto uma requisição pública não transforma a carga em um proxy de
saída. Diagnósticos pontuais continuam exigindo um comando explícito do contêiner.

## Capacidades

| Capacidade | Interface |
| --- | --- |
| Aliases de detecção de idioma | `GET /`, `GET /websocket`, `GET /rest`, `GET /graphql-lab`, `GET /sse` |
| Páginas localizadas | `GET /en/`, `GET /pt-BR/`, `GET /es-AR/` |
| Laboratórios localizados no browser | `GET /en/rest`, `/en/graphql-lab`, `/en/sse`, `/en/websocket` e caminhos traduzidos correspondentes |
| Asset estático | `GET /static/style.css` |
| Liveness | `GET /healthz` |
| Readiness | `GET /readyz` |
| Resposta indisponível | `GET /not-ready` |
| Status REST | `GET /api/status` |
| Coleção REST | `GET /api/items` |
| Echo REST | `POST /api/echo` |
| GraphQL | `POST /graphql` |
| Server-Sent Events | `GET /events` |
| Echo e broadcast WebSocket | `GET /ws` |
| Probe de rede HTTP/HTTPS | `testkit probe URL` |
| Identidade e estado de pares configurados | `GET /api/identity`, `GET /api/peers` |
| Metadados do marcador persistente opcional | `GET`, `PUT /api/persistence` |

A versão armazenada no arquivo versionado `VERSION` é embutida no binário Go e
exposta nos payloads dos protocolos, nos logs estruturados, no cabeçalho e
rodapé do browser e no header `Testkit-Version` de toda resposta HTTP, inclusive
handshakes WebSocket bem-sucedidos. Isso torna testes de rollout e transporte
observáveis sem exigir argumentos externos de build nem alterar a identidade
lógica da carga.

## Pré-requisitos

- Go 1.26.x.
- Docker Engine ou Docker Desktop com Buildx habilitado.
- `curl` para os exemplos.

## Build e execução local

Construa a imagem para a plataforma local do Docker:

```sh
docker buildx build \
  --load \
  --tag molejo-testkit:dev \
  .
```

Execute-a com o contrato restrito esperado pela Molejo Platform:

```sh
docker run --rm \
  --publish 8080:8080 \
  --read-only \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  molejo-testkit:dev
```

Valide o contrato REST:

```sh
curl --fail http://localhost:8080/api/status
```

Resposta esperada:

```json
{"status":"ok","version":"v0.7.1"}
```

## Console no navegador

Abra `http://localhost:8080/` para detectar o idioma do navegador ou use
diretamente `/en/`, `/pt-BR/` ou `/es-AR/`. Os laboratórios ficam nos caminhos
`/rest`, `/graphql-lab`, `/sse` e `/websocket` correspondentes. REST e GraphQL
usam presets guiados que mostram detalhes da requisição e da resposta. O
laboratório SSE mostra IDs, nomes e dados dos eventos, com ações explícitas de
conectar, desconectar e reconectar. O laboratório WebSocket mostra dois
clientes independentes da mesma origem, `Client A` e `Client B`, permitindo
conectar os dois painéis, enviar uma mensagem por um deles e alternar entre os
eventos JSON brutos e uma visão de chat com horários locais de envio e
recebimento. Os clientes do browser não reconectam automaticamente após erro ou
encerramento.

## Exemplos de protocolos

Echo REST:

```sh
curl --json '{"hello":"world"}' http://localhost:8080/api/echo
```

GraphQL:

```sh
curl --json '{"query":"{ status version echo(message: \"hello\") }"}' \
  http://localhost:8080/graphql
```

SSE:

```sh
curl -N http://localhost:8080/events
```

WebSocket:

```sh
wscat --connect ws://localhost:8080/ws
> {"message":"hello"}
```

Cada cliente WebSocket conectado recebe a mensagem de broadcast com a versão de
build atual:

```json
{"message":"hello","version":"v0.7.1"}
```

## Probe de rede

Execute diagnósticos de rede como um comando explícito da mesma imagem:

```sh
docker run --rm molejo-testkit:dev probe https://example.com/
```

O probe executa uma única requisição HTTP ou HTTPS `GET`, com limites, e emite um
resultado JSON. Seus códigos de saída formam o contrato de automação:

| Código | Significado |
| --- | --- |
| `0` | O destino retornou HTTP `2xx`. |
| `1` | A requisição falhou ou retornou uma resposta diferente de `2xx`. |
| `2` | Os argumentos do comando ou a URL são inválidos. |

Para validar NetworkPolicies do Kubernetes, execute o probe como um Job de curta
duração no namespace testado. Aplique as labels e a ServiceAccount cuja identidade
de rede deseja validar e verifique o código de saída do Job. Assim, origem, destino
e resultado esperado de permissão ou negação permanecem explícitos.

## Monitoramento de pares configurados

O modo servidor pode verificar continuamente uma allowlist fixa de outras
instâncias do Testkit. A funcionalidade fica desabilitada a menos que
`TESTKIT_PEERS_FILE` aponte para um arquivo JSON somente leitura:

```json
{
  "schema_version": 1,
  "instance_id": "testkit-a",
  "check_interval": "30s",
  "timeout": "3s",
  "peers": [
    {
      "name": "testkit-b",
      "scheme": "http",
      "host": "testkit-b.namespace-b",
      "port": 8080,
      "expected_instance_id": "testkit-b"
    }
  ]
}
```

Cada verificação abre uma conexão direta nova, ignora variáveis de proxy HTTP,
não segue redirects e solicita o caminho fixo `/api/identity`. HTTPS usa o trust
store do sistema sem modo inseguro. As respostas são limitadas a 4 KiB e no
máximo quatro pares são verificados em paralelo. A primeira verificação é
imediata e as seguintes usam `check_interval`; `timeout` deve ser positivo e
menor ou igual a 30 segundos e menor que esse intervalo.

`GET /api/identity` retorna a identidade lógica configurada e um `boot_id`
UUIDv7 local ao processo. `GET /api/peers` retorna somente fatos sanitizados em
memória sobre as últimas verificações. Os dois endpoints existem apenas quando
o arquivo está configurado. Uma instância sem pares de saída pode usar
`"peers": []` para servir sua identidade. Nenhum endpoint aceita um destino ou
inicia uma verificação sob demanda, e ambas as respostas usam
`Cache-Control: no-store`.

Os outcomes são `reachable`, `unreachable` e `unknown`. Reasons distinguem
falhas de DNS, conexão, TLS, HTTP, resposta e identidade. Esses são fatos de
transporte observados: o Testkit nunca afirma que uma falha foi causada por uma
NetworkPolicy. Um Service com múltiplas réplicas comprova alcance ao Service,
não a um Pod específico.

## Contrato do contêiner

- Escuta na porta TCP `8080`.
- Executa como UID/GID `65532:65532`.
- Suporta filesystem raiz somente leitura.
- Não exige capabilities Linux nem elevação de privilégios.
- Inclui CA bundle para probes HTTPS.
- Incorpora todos os assets estáticos no binário.
- Compila de forma reproduzível para plataformas alvo do BuildKit, incluindo
  `linux/amd64` e `linux/arm64`.
- Trata `SIGTERM` e drena conexões HTTP, SSE e WebSocket com shutdown limitado.

O servidor aceita conexões WebSocket de mesma origem e clientes que omitem o
header `Origin`, como ferramentas de linha de comando. Conexões cross-origin de
navegadores são rejeitadas.

## Configuração

| Variável | Padrão | Finalidade |
| --- | --- | --- |
| `SSE_INTERVAL` | `1s` | Intervalo entre eventos de status SSE. |
| `TESTKIT_PEERS_FILE` | não definido | Configuração somente leitura; ausente desabilita o monitor e seus endpoints. |
| `TESTKIT_PERSISTENCE_FILE` | não definido | Caminho absoluto do marcador; ausente desabilita o endpoint de persistência. |

Valores inválidos ou não positivos de `SSE_INTERVAL` usam o padrão.
Quando a persistência está habilitada, `PUT /api/persistence` aceita
`{"value":"..."}` com 1–4096 bytes e grava de forma atômica. `GET` e `PUT`
retornam somente existência, tamanho em bytes e fingerprint SHA-256; o valor
nunca é devolvido. Monte um volume persistente gravável no diretório pai do
arquivo ao usar filesystem raiz somente leitura.

## Logs estruturados

O modo servidor escreve logs JSON delimitados por linha na saída padrão. Cada
registro inclui `service` e `version`. Os principais valores de `event` são:

- `server.started`, `server.stopped` e eventos de falha do servidor;
- `http.request.completed` para `/api/status`, `/api/items` e `/api/echo`, com
  método, rota estável, código de status, duração em milissegundos e bytes da
  resposta;
- `connection.opened` e `connection.closed` para WebSocket e SSE, com as
  quantidades de conexões ativas no protocolo e no total e a duração ao fechar;
- `connections.snapshot` a cada 15 minutos enquanto houver pelo menos uma
  conexão ativa.
- `peer.identity.requested` para solicitações de identidade entre pares;
- `peer.state.changed` quando o outcome, reason ou `boot_id` remoto de um par
  muda;
- `peers.snapshot` a cada 15 minutos enquanto houver um par configurado.

Eventos de ciclo de vida e snapshots incluem `connection_sequence`, que aumenta
a cada transição de estado de conexão no processo. Consumidores podem usá-la para
reconstruir a ordem das transições quando registros concorrentes chegam fora de
ordem.

Requisições REST concluídas e conexões WebSocket e SSE aceitas incluem um
`correlation_id` UUIDv7. Clientes REST podem fornecê-lo por
`X-Testkit-Correlation-ID`; clientes WebSocket e SSE podem usar o parâmetro de
query `correlation_id`. Valores ausentes ou inválidos são substituídos por um ID
gerado pelo servidor. Respostas REST devolvem o ID efetivo no mesmo header.
Os labs REST, WebSocket e SSE incluídos geram e exibem esses IDs automaticamente.
Snapshots de conexões permanecem agregados e não incluem IDs de correlação.

As quantidades representam conexões aceitas observadas atualmente por um processo
do servidor e zeram no restart. Atualizações de página, fechamentos normais e
erros de transporte diminuem a quantidade quando o servidor observa a
desconexão. Heartbeats WebSocket limitam a detecção de falhas silenciosas a cerca
de 60 segundos; no SSE, a detecção é best effort quando o caminho de rede
desaparece sem fechar o stream HTTP. O shutdown gracioso aguarda os handlers
WebSocket aceitos emitirem seus fatos de fechamento. Um crash ou encerramento
forçado não consegue emitir fatos de fechamento, portanto consumidores devem
tratar cada início do servidor como uma nova época local ao processo.

Os logs não incluem endereços de clientes, headers brutos, query strings brutas,
payloads das requisições, mensagens WebSocket nem dados SSE. São fatos de teste
observáveis, não métricas duráveis ou globais.

Fatos de pares usam nomes lógicos e reasons estáveis. Não registram hosts
configurados, IPs resolvidos, configuração de proxy, corpos de resposta nem
erros brutos. O estado dos pares zera no restart; `boot_id` identifica a época
local e `observed_boot_id` identifica a última época remota. Verificações
canceladas pelo shutdown não substituem o último estado observado do par.

## Distribuição da imagem

Releases versionadas publicam imagens multiplataforma no GitHub Container
Registry:

```text
ghcr.io/molejo-platform/testkit:v0.7.1
```

As tags existem para descoberta. Testes automatizados devem consumir o digest
imutável informado pela pipeline de release:

```text
ghcr.io/molejo-platform/testkit@sha256:<digest>
```

O projeto não publica uma tag `latest`. Assinatura, SBOMs e attestations adicionais
de proveniência estão fora do contrato atual de release.

## Desenvolvimento

Execute o gate local de qualidade:

```sh
gofmt -w *.go
go test -race -cover ./...
go vet ./...
go mod tidy -diff
```

Construa e exercite o contêiner final sempre que runtime, assets incorporados,
probes ou Dockerfile forem alterados.

## Documentação

Inglês é o idioma canônico da documentação. Traduções disponíveis:

- [English](../../README.md)
- [Español (Argentina)](../es-AR/README.md)

As traduções preservam comandos, caminhos, endpoints, campos e identificadores de
protocolo em inglês. Em caso de divergência, a versão em inglês define o contrato
atual.

## Contribuindo

Leia [CONTRIBUTING.md](CONTRIBUTING.md) antes de propor uma alteração.

## Segurança

Não reporte vulnerabilidades em issues públicas. Siga [SECURITY.md](SECURITY.md).

## Licença

Licenciado sob a [Apache License 2.0](../../LICENSE).
