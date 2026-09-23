# Molejo Testkit

[English](../../README.md) |
[Español (Argentina)](../es-AR/README.md)

> Projeto experimental em pré-release. O Molejo Testkit é um fixture descartável
> de testes, não uma aplicação de produção.

O Molejo Testkit é uma imagem de contêiner pequena para comprovar alcance de
rede, entrega de aplicações e contratos mínimos de transporte. Execute-a ao lado
do sistema que deseja inspecionar, acione uma das interfaces fixas e valide a
resposta observável.

Um único binário Go não root oferece health checks, REST, GraphQL, Server-Sent
Events (SSE), WebSocket, verificação de volume persistente, monitoramento de peers
configurados, diagnósticos PostgreSQL limitados e um probe HTTP/HTTPS explícito.

## Escolha a imagem

As imagens publicadas usam este repositório:

```text
ghcr.io/molejo-platform/testkit
```

Defina a imagem uma vez antes de seguir os exemplos:

```sh
export TESTKIT_IMAGE=ghcr.io/molejo-platform/testkit:v0.9.0
docker pull "$TESTKIT_IMAGE"
```

`v0.9.0` é a imagem estável mais recente publicada no momento da escrita deste
guia. Uma branch de release pode documentar capacidades ainda não presentes em
uma imagem correspondente. Em ambientes automatizados, substitua a tag pelo
digest imutável publicado pela release:

```sh
export TESTKIT_IMAGE=ghcr.io/molejo-platform/testkit@sha256:COLE_O_DIGEST_PUBLICADO_AQUI
```

O projeto não publica a tag `latest`. Não presuma que a branch padrão, este
README e uma tag antiga da imagem oferecem as mesmas capacidades.

## Início rápido: transporte de entrada

### 1. Inicie o contêiner

A configuração padrão habilita os smokes de transporte na porta `8080` e não
precisa de arquivos de configuração:

```sh
docker run --rm \
  --name molejo-testkit \
  --publish 8080:8080 \
  --read-only \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  "$TESTKIT_IMAGE"
```

Resultado esperado: o processo registra `server.started` e continua executando
como UID/GID `65532:65532`.

### 2. Verifique liveness e readiness

Em outro terminal:

```sh
curl --fail http://localhost:8080/healthz
curl --fail http://localhost:8080/readyz
curl --fail http://localhost:8080/api/status
```

A resposta de status inclui a versão exata incorporada na imagem:

```json
{"status":"ok","version":"v0.9.0"}
```

### 3. Abra o console no navegador

Abra `http://localhost:8080/`. O Testkit detecta o idioma do navegador e
redireciona para `/en/`, `/pt-BR/` ou `/es-AR/`. As páginas REST, GraphQL, SSE e
WebSocket executam cenários guiados e limitados e mostram requisição e resposta.

### 4. Encerre

Pressione `Ctrl+C` no terminal do contêiner. O Testkit trata `SIGTERM`, deixa de
aceitar trabalho, fecha recursos retidos e drena HTTP e WebSocket dentro do
limite de shutdown.

## O que um smoke bem-sucedido comprova

O Testkit responde a uma pergunta focada: este cliente alcança este workload
descartável ou dependência explicitamente configurada, e a troca mínima escolhida
é concluída?

| Superfície | O que o sucesso comprova |
| --- | --- |
| `/healthz`, `/readyz` | O caminho HTTP alcança o processo Testkit em execução. |
| REST/GraphQL | Requisição, resposta, headers e uma troca limitada são concluídos. |
| SSE | Uma resposta HTTP em streaming entrega eventos nomeados. |
| WebSocket | Upgrade, frames bidirecionais, broadcast e fechamento funcionam. |
| `probe URL` | Uma requisição HTTP/HTTPS de saída retorna `2xx`. |
| `/api/peers` | Uma identidade Testkit configurada foi alcançada por este processo. |
| `/api/persistence` | O caminho configurado grava e lê um marcador limitado. |
| Laboratório PostgreSQL | Um destino autorizado aceita uma operação fixa de conexão ou catálogo. |

O sucesso não valida regras de negócio, autenticação de produção, autorização,
salas, schemas específicos da aplicação, desempenho, carga, alta disponibilidade,
backup ou dependências não relacionadas.

## Interfaces disponíveis

| Capacidade | Interface |
| --- | --- |
| Console no navegador | `GET /`, `/en/`, `/pt-BR/`, `/es-AR/` |
| Aliases do navegador | `GET /rest`, `GET /graphql-lab`, `GET /sse`, `GET /websocket` |
| Laboratórios no navegador | `GET /<locale>/rest`, `/graphql-lab`, `/sse`, `/websocket` |
| Liveness e readiness | `GET /healthz`, `GET /readyz` |
| Resposta intencionalmente não pronta | `GET /not-ready` |
| REST | `GET /api/status`, `GET /api/items`, `POST /api/echo` |
| GraphQL | `POST /graphql` |
| SSE | `GET /events` |
| WebSocket | `GET /ws` |
| Probe explícito de saída | `testkit probe URL` |
| Identidade de peer configurado | `GET /api/identity`, `GET /api/peers` |
| Marcador persistente | `GET`, `PUT /api/persistence` |
| Capacidades PostgreSQL | `GET /api/diagnostics/postgres/capabilities` |
| Operação PostgreSQL efêmera | `POST /api/diagnostics/postgres` |
| Conexão PostgreSQL retida | `POST /api/diagnostics/postgres/connections` |

Interfaces opcionais só existem quando a configuração correspondente é válida.
Paths e métodos desconhecidos não são tratados como smokes bem-sucedidos.

## Verificações comuns de protocolo

REST echo:

```sh
curl --fail --json '{"hello":"world"}' \
  http://localhost:8080/api/echo
```

GraphQL:

```sh
curl --fail --json '{"query":"{ status version echo(message: \"hello\") }"}' \
  http://localhost:8080/graphql
```

SSE:

```sh
curl --no-buffer http://localhost:8080/events
```

WebSocket com `wscat`:

```sh
wscat --connect ws://localhost:8080/ws
> {"message":"hello"}
```

A resposta WebSocket inclui a mensagem e a versão da imagem. É um sinal de
transporte, não uma implementação de salas ou roteamento da aplicação.

## Probe explícito de saída

O comando de probe é separado do modo servidor. Ele realiza um único `GET`
limitado sem transformar um endpoint HTTP público em proxy genérico:

```sh
docker run --rm "$TESTKIT_IMAGE" probe https://example.com/
```

Ele escreve um resultado JSON e usa códigos de saída seguros para automação:

| Código | Significado |
| --- | --- |
| `0` | O destino retornou HTTP `2xx`. |
| `1` | A requisição de rede/TLS falhou ou a resposta não foi `2xx`. |
| `2` | Os argumentos ou a URL são inválidos. |

Para validar NetworkPolicies do Kubernetes, execute a imagem como um Job curto
no namespace de origem, com os labels e a ServiceAccount cuja identidade de rede
deseja testar. Valide o código do Job e remova-o depois.

## Configuração de runtime

Todas as capacidades são configuradas na inicialização. Valores obrigatórios
inválidos fazem o processo falhar em vez de desabilitar silenciosamente uma
capacidade solicitada.

| Variável | Padrão | Obrigatória quando | Finalidade |
| --- | --- | --- | --- |
| `HTTP_PORT` | `8080` | Nunca | Porta interna, de `1` a `65535`. |
| `SSE_INTERVAL` | `1s` | Nunca | Duração Go positiva entre eventos SSE. |
| `TESTKIT_SMOKES` | `transport` | Nunca | Capacidades: `transport` ou `transport,postgres`. |
| `TESTKIT_PEERS_FILE` | não definida | Monitoramento de peers | Path absoluto do JSON de peers. |
| `TESTKIT_PERSISTENCE_FILE` | não definida | Marcador persistente | Path absoluto e gravável do arquivo marcador. |
| `TESTKIT_DIAGNOSTIC_TOKEN_FILE` | não definida | PostgreSQL | Path absoluto de um token somente leitura com ao menos 16 bytes. |
| `TESTKIT_POSTGRES_DESTINATIONS_FILE` | não definida | PostgreSQL | Path absoluto da política JSON estrita de destinos. |

Arquivos de configuração são lidos do filesystem do contêiner. Monte cada
arquivo em um path absoluto e use o mesmo path na variável. Arquivos de
diagnóstico e peers devem ser somente leitura. O diretório pai da persistência
precisa ser gravável pelo UID/GID `65532:65532`.

### Porta HTTP e intervalo SSE personalizados

Configure a mesma porta dentro do contêiner e no mapeamento publicado:

```sh
docker run --rm \
  --name molejo-testkit \
  --env HTTP_PORT=2020 \
  --env SSE_INTERVAL=2s \
  --publish 2020:2020 \
  --read-only \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  "$TESTKIT_IMAGE"
```

URL esperada: `http://localhost:2020/readyz`.

## Receita: valide persistência gravável

`TESTKIT_PERSISTENCE_FILE` habilita um pequeno contrato de marcador. O Testkit
grava no máximo 4096 bytes e retorna somente existência, tamanho e SHA-256; o
valor do marcador nunca é devolvido.

Primeiro crie um volume Docker gravável pelo usuário de runtime do Testkit:

```sh
docker volume create molejo-testkit-data
docker run --rm \
  --user 0:0 \
  --mount type=volume,source=molejo-testkit-data,target=/data \
  alpine:3.22 chown 65532:65532 /data
```

Depois inicie o Testkit:

```sh
docker run --rm \
  --name molejo-testkit \
  --publish 8080:8080 \
  --env TESTKIT_PERSISTENCE_FILE=/var/lib/testkit/marker \
  --mount type=volume,source=molejo-testkit-data,target=/var/lib/testkit \
  --read-only \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  "$TESTKIT_IMAGE"
```

Grave e leia os metadados do marcador:

```sh
curl --fail --request PUT \
  --header 'Content-Type: application/json' \
  --data '{"value":"volume-smoke"}' \
  http://localhost:8080/api/persistence

curl --fail http://localhost:8080/api/persistence
```

Resultado esperado: `exists` é `true`, `size` é maior que zero e `sha256`
permanece igual depois de reiniciar o Testkit com o mesmo volume.

## Receita: monitore peers Testkit configurados

O monitoramento de peers é uma verificação periódica e somente leitura entre
instâncias do Testkit. Toda instância que serve `/api/identity` precisa de um
arquivo de peers; uma instância que apenas recebe verificações usa `peers` vazio.

Exemplo de `/config/peers.json`:

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

Monte o arquivo e defina seu path absoluto no contêiner:

```sh
docker run --rm \
  --name testkit-a \
  --env TESTKIT_PEERS_FILE=/etc/testkit/peers.json \
  --mount type=bind,source="$PWD/config/peers.json",target=/etc/testkit/peers.json,readonly \
  --publish 8080:8080 \
  --read-only \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  "$TESTKIT_IMAGE"
```

Inspecione o estado sanitizado e local ao processo:

```sh
curl --fail http://localhost:8080/api/identity
curl --fail http://localhost:8080/api/peers
```

A primeira verificação é imediata. As seguintes usam `check_interval`. O Testkit
não segue redirects ou variáveis de proxy HTTP; HTTPS usa o trust store do
sistema sem modo inseguro.

## Receita: execute diagnósticos PostgreSQL

Os diagnósticos PostgreSQL são opt-in e exigem dois arquivos: um token
operacional da API e uma allowlist de destinos. O token da implantação é
separado da credencial de banco usada no diagnóstico.

Essa capacidade está incluída na `v0.9.0` e permanece desabilitada até o token e
a política de destinos exigidos serem configurados explicitamente.

### 1. Crie os arquivos de configuração locais

Use um token descartável neste exemplo e substitua o destino pelo hostname ou
CIDR que o Testkit deve alcançar:

```sh
mkdir -p config
printf '%s\n' 'substitua-por-ao-menos-16-bytes' > config/diagnostic-token
printf '%s\n' \
  '{"destinations":[{"host":"db.internal.example","ports":[5432]}]}' \
  > config/postgres-destinations.json
chmod 0444 config/diagnostic-token config/postgres-destinations.json
```

Não faça commit do token. Em ambientes compartilhados, crie o arquivo pelo
gerenciador de segredos da plataforma, não junto dos manifestos de implantação.

### 2. Inicie o Testkit com PostgreSQL habilitado

```sh
docker run --rm \
  --name molejo-testkit \
  --publish 8080:8080 \
  --env TESTKIT_SMOKES=transport,postgres \
  --env TESTKIT_DIAGNOSTIC_TOKEN_FILE=/run/testkit/diagnostic-token \
  --env TESTKIT_POSTGRES_DESTINATIONS_FILE=/etc/testkit/postgres-destinations.json \
  --mount type=bind,source="$PWD/config/diagnostic-token",target=/run/testkit/diagnostic-token,readonly \
  --mount type=bind,source="$PWD/config/postgres-destinations.json",target=/etc/testkit/postgres-destinations.json,readonly \
  --read-only \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  "$TESTKIT_IMAGE"
```

Resultado esperado: `http://localhost:8080/pt-BR/postgres` existe e
`/api/diagnostics/postgres/capabilities` descreve campos, tipos de credencial,
modos TLS, ciclos de vida e operações fixas suportadas.

### 3. Execute um diagnóstico

A página do navegador é o cliente mais simples. Informe o token da implantação,
destino, identidade do banco, credencial, TLS e ciclo de vida. O banco inicial é
opcional; quando omitido, o PostgreSQL o seleciona pelas regras de inicialização.

| Parte da conexão | Contrato |
| --- | --- |
| `target` | Host obrigatório; porta padrão `5432`. |
| `database` | Opcional. A omissão delega ao PostgreSQL a seleção do banco inicial. |
| `identity` | Usuário de banco obrigatório. |
| `credential` | Tipo obrigatório: `password`, `token` ou `none`. Senha/token exigem `secret`; none o proíbe. |
| `tls_config` | Padrão `verify-full`; CA privada opcional vale somente para TLS verificado. |
| `lifecycle` | `ephemeral` fecha após uma operação; `retained` fica em memória até delete ou shutdown. |

`token` é um token gerado pelo provedor e enviado pelo campo de senha do
PostgreSQL; o Testkit não gera credenciais IAM, OAuth ou de provedores cloud. Uma
URI também é aceita como alternativa aos campos estruturados, mas os dois modos
não podem ser combinados.

Para automação, execute uma operação efêmera diretamente:

```sh
curl --fail --json '{
  "operation": "connect",
  "connection": {
    "target": {"host": "db.internal.example", "port": 5432},
    "identity": {"user": "testkit"},
    "credential": {"type": "password", "secret": "substitua"},
    "tls_config": {"mode": "verify-full"},
    "lifecycle": {"mode": "ephemeral"}
  }
}' \
  --header 'Authorization: Bearer substitua-por-ao-menos-16-bytes' \
  http://localhost:8080/api/diagnostics/postgres
```

As operações disponíveis são `connect`, `arithmetic_check`, `list_databases` e
`list_schemas`. SQL arbitrário não é aceito.

Todas as requisições de conexão, operação, inspeção e remoção exigem o bearer
token da implantação. O endpoint de capacidades é somente leitura e não exige.
Um diagnóstico processado pode retornar HTTP `200` com `status: "failed"`; a
automação deve validar `status` e `code` no JSON, não apenas o status HTTP.

Use o lifecycle `retained` com
`POST /api/diagnostics/postgres/connections` para manter uma conexão física na
memória do processo. Execute operações fixas em
`POST /api/diagnostics/postgres/connections/<id>/operations`, inspecione com
`GET /api/diagnostics/postgres/connections/<id>` e sempre finalize com
`DELETE /api/diagnostics/postgres/connections/<id>`.

O servidor limita conexões retidas a oito e requisições de diagnóstico
concorrentes a quatro. Operações têm prazo de 10 segundos e não fazem retry.
Segredos não são retornados nem registrados. Loopback só é permitido quando
`localhost` ou um IP de loopback literal aparece em uma regra `host` exata com a
porta solicitada; uma regra CIDR não pode liberá-lo. Endereços link-local,
multicast, não especificados e de metadata cloud permanecem bloqueados. A
política é carregada uma vez na inicialização; reinicie o Testkit após editar o
arquivo.

## Kubernetes: fixture mínimo de transporte

Este exemplo implanta a imagem padrão somente com transporte. Fixe `image` no
digest validado pelo seu processo de release:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: molejo-testkit
spec:
  replicas: 1
  selector:
    matchLabels:
      app: molejo-testkit
  template:
    metadata:
      labels:
        app: molejo-testkit
    spec:
      securityContext:
        runAsNonRoot: true
        runAsUser: 65532
        runAsGroup: 65532
        seccompProfile:
          type: RuntimeDefault
      containers:
        - name: testkit
          image: ghcr.io/molejo-platform/testkit:v0.9.0
          ports:
            - name: http
              containerPort: 8080
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop: ["ALL"]
          livenessProbe:
            httpGet:
              path: /healthz
              port: http
          readinessProbe:
            httpGet:
              path: /readyz
              port: http
---
apiVersion: v1
kind: Service
metadata:
  name: molejo-testkit
spec:
  selector:
    app: molejo-testkit
  ports:
    - name: http
      port: 8080
      targetPort: http
```

Use ConfigMaps para arquivos não secretos de peers e política de destinos,
Secrets para o token de diagnóstico e um volume gravável com `fsGroup: 65532`
para persistência.

## Comportamento operacional e de segurança

- A imagem não exige capabilities Linux ou elevação de privilégio e aceita
  filesystem raiz somente leitura.
- Todos os assets do navegador estão no binário; nenhum volume é necessário.
- Requisições cross-origin são rejeitadas nos diagnósticos sensíveis.
- Rotas HTTP públicas não escolhem destinos arbitrários de saída.
- Destinos PostgreSQL e de peers são restringidos antes da conexão.
- Logs excluem headers, query strings, payloads, credenciais, mensagens
  WebSocket e dados SSE brutos.
- Fatos de peers, contadores, boot IDs e conexões de banco retidas são locais ao
  processo e reiniciam junto dele.

A versão incorporada aparece em respostas, logs, páginas e no header
`Testkit-Version`. Use-a para diferenciar a imagem em execução dos manifestos ou
checkout que esperava implantar.

## Solução de problemas na inicialização

| Sintoma | Verificação |
| --- | --- |
| Processo encerra imediatamente | Leia o log estruturado `server.configuration_failed`. |
| Porta publicada não responde | Confirme que porta externa, interna e `HTTP_PORT` coincidem. |
| Endpoint opcional retorna `404` | Confirme variável e arquivo montado na inicialização. |
| Path de configuração é rejeitado | Paths internos do contêiner devem ser absolutos. |
| Destino PostgreSQL é proibido | Confira hostname/CIDR, porta, DNS e restrições de endereços especiais. |
| Persistência retorna `500` | Confirme que o diretório pai é gravável por UID/GID `65532`. |
| Peer permanece `unknown` | Confira DNS, timeout, instance ID esperado e arquivo remoto. |

## Contribuição e desenvolvimento

Este README é o guia do consumidor para executar a imagem. Build do código,
gates locais, mudanças de contrato e preparação de pull requests estão
documentados separadamente no [CONTRIBUTING.md](../../CONTRIBUTING.md).

Problemas de segurança não devem ser relatados em issues públicas. Siga o
[SECURITY.md](../../SECURITY.md). Identidade visual e proveniência dos assets
estão em [docs/BRANDING.md](../BRANDING.md).

Licenciado sob a [Apache License 2.0](../../LICENSE).
