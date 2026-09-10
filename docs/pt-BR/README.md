# Molejo Testkit

[English](../../README.md) |
[Español (Argentina)](../es-AR/README.md)

> Projeto experimental em pré-release. O Molejo Testkit é uma fixture de testes,
> não uma aplicação de produção.

O Molejo Testkit é uma pequena imagem de contêiner determinística para smoke
tests de conectividade de rede e contratos mínimos de transporte HTTP. Ele também pode
exercitar entrega de aplicações no Kubernetes e políticas de rede. Um único
binário Go estático fornece REST, GraphQL, Server-Sent Events (SSE), WebSocket,
endpoints de saúde e um comando explícito de probe de saída.

O servidor HTTP público e o probe arbitrário de rede são intencionalmente
separados. O modo servidor só pode chamar pares declarados em um arquivo read-only
opcional, portanto uma requisição pública não transforma a carga em um proxy de
saída. Diagnósticos pontuais continuam exigindo um comando explícito do contêiner.

## Objetivo e escopo

O Testkit responde a uma pergunta objetiva: o cliente consegue alcançar a carga
descartável e o transporte selecionado consegue concluir sua troca mínima? Um
smoke test WebSocket bem-sucedido confirma o caminho de rede, o upgrade HTTP, a
troca bidirecional de frames e o encerramento básico desse caminho de implantação.

Ele não é uma implementação completa nem substitui os testes da aplicação. Um
smoke test bem-sucedido não valida autenticação, autorização, rooms, roteamento,
regras de negócio, schemas completos de mensagens, performance, carga ou
dependências não exercitadas pelo cenário.

## Superfícies de smoke test

| Capacidade | Interface |
| --- | --- |
| Aliases de detecção de idioma | `GET /`, `GET /websocket`, `GET /rest`, `GET /graphql-lab`, `GET /sse` |
| Páginas localizadas | `GET /en/`, `GET /pt-BR/`, `GET /es-AR/` |
| Laboratórios localizados no browser | `GET /en/rest`, `/en/graphql-lab`, `/en/sse`, `/en/websocket` e caminhos traduzidos correspondentes |
| Assets estáticos | `GET /static/style.css` e branding Molejo incorporado |
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
handshakes WebSocket bem-sucedidos. O rodapé também inclui um pequeno link de
atribuição localizado do Molejo com rastreamento UTM da versão do build. Isso
torna testes de rollout e transporte observáveis sem exigir argumentos externos
de build nem alterar a identidade lógica da carga.

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
{"status":"ok","version":"v0.8.0"}
```

Para executar o servidor em outra porta, defina `HTTP_PORT`. O padrão é `8080`:

```sh
HTTP_PORT=2020 go run .
curl --fail http://localhost:2020/api/status
```

No contêiner, configure a mesma porta dentro e fora do contêiner:

```sh
docker run --rm \
  --env HTTP_PORT=2020 \
  --publish 2020:2020 \
  --read-only \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  molejo-testkit:dev
```

## Smoke test rápido

Depois de iniciar o servidor, verifique o readiness e exercite a troca mínima do
WebSocket:

Se `HTTP_PORT` estiver configurada, substitua `8080` pelo valor definido nas duas
URLs.

```sh
curl --fail http://localhost:8080/readyz
wscat --connect ws://localhost:8080/ws
> {"message":"smoke"}
```

A resposta de readiness, o handshake WebSocket bem-sucedido e a mensagem JSON
recebida confirmam a conectividade básica e o caminho de transporte. Eles não validam
funcionalidades específicas de WebSocket da aplicação.

## Console no navegador

Abra `http://localhost:8080/` para detectar o idioma do navegador ou use
diretamente `/en/`, `/pt-BR/` ou `/es-AR/`. Os laboratórios ficam nos caminhos
`/rest`, `/graphql-lab`, `/sse` e `/websocket` correspondentes. Os laboratórios
são verificações pequenas e determinísticas de conectividade e contrato, não
suítes de funcionalidades. REST e GraphQL usam presets guiados que mostram
detalhes da requisição e da resposta. O laboratório SSE mostra IDs, nomes e
dados dos eventos, com ações explícitas de conectar, desconectar e reconectar. O
laboratório WebSocket mostra dois clientes independentes da mesma origem,
`Client A` e `Client B`, permitindo observar uma troca mínima de mensagens e
seu sinal de broadcast. Os clientes do browser não reconectam automaticamente
após erro ou encerramento.

## Exemplos mínimos de protocolo

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

O handshake bem-sucedido e a mensagem recebida são o sinal de smoke test do
WebSocket. A fixture transmite a mesma mensagem determinística aos clientes
conectados para tornar o resultado de transporte visível; isso não testa rooms,
autenticação nem roteamento no nível da aplicação.

```json
{"message":"hello","version":"v0.8.0"}
```

## Probe de rede

Execute diagnósticos de rede como um comando explícito da mesma imagem:

```sh
docker run --rm molejo-testkit:dev probe https://example.com/
```

O probe executa uma única requisição HTTP ou HTTPS `GET`, com limites, e emite um
resultado JSON. Ele é separado dos smoke tests de transporte de entrada do
servidor: valida alcance HTTP/HTTPS explícito de saída, não conectividade
WebSocket. Seus códigos de saída formam o contrato de automação:

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

O monitoramento de pares valida conectividade de transporte e o endpoint de identidade
configurado do Testkit. Ele não valida compatibilidade da aplicação nem
funcionalidades de negócio entre os pares.

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

- Escuta na porta TCP `8080` por padrão; `HTTP_PORT` pode substituí-la em runtime.
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
| `HTTP_PORT` | `8080` | Porta TCP usada pelo servidor HTTP; deve ser um inteiro entre `1` e `65535`. |
| `SSE_INTERVAL` | `1s` | Intervalo entre eventos de status SSE. |
| `TESTKIT_PEERS_FILE` | não definido | Configuração somente leitura; ausente desabilita o monitor e seus endpoints. |
| `TESTKIT_PERSISTENCE_FILE` | não definido | Caminho absoluto do marcador; ausente desabilita o endpoint de persistência. |

`HTTP_PORT` ausente ou vazio usa o padrão. Valores inválidos fazem o servidor
falhar durante a inicialização. Valores inválidos ou não positivos de
`SSE_INTERVAL` usam o padrão.
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
ghcr.io/molejo-platform/testkit:v0.8.0
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

As regras de identidade visual e a origem dos assets estão documentadas no
[contrato de branding do Molejo Testkit](../BRANDING.md).

## Contribuindo

Leia [CONTRIBUTING.md](CONTRIBUTING.md) antes de propor uma alteração.

## Segurança

Não reporte vulnerabilidades em issues públicas. Siga [SECURITY.md](SECURITY.md).

## Licença

Licenciado sob a [Apache License 2.0](../../LICENSE).
