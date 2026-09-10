# Testkit — avaliação e plano de consolidação antes dos novos smokes

Data: 10/09/2026. Revisão analisada: `bdc2933cde8e7815f46e9654c207ed3bd65679a4`, versão declarada `v0.8.0`.

## 1. Recomendação

**Consolidar a base atual e estabelecer a fronteira de segurança dos diagnósticos de saída antes de adicionar PostgreSQL.** A evolução proposta é coerente com um produto de smoke tests e debug de infraestrutura, mas muda uma premissa fundamental: hoje uma requisição pública não pode escolher um destino de saída; o novo fluxo passará a receber destino e credenciais pela tela.

O investimento necessário é localizado: corrigir defeitos existentes, separar responsabilidades do backend, uniformizar execução e resultados na UX, centralizar capacidades e implementar controle de acesso, destinos e recursos. A stack atual — Go, templates HTML, módulos JavaScript e CSS — atende a essa evolução. Não há justificativa observada para migrar framework, introduzir banco interno ou criar uma plataforma genérica de plugins.

O caminho recomendado é:

1. Corrigir parser e resultados inconsistentes dos laboratórios atuais.
2. Consolidar configuração, catálogo, execução, resultados e componentes já utilizados.
3. Implementar e testar a proteção da futura superfície de diagnóstico.
4. Entregar PostgreSQL em incrementos: conexão → operação fixa → descoberta limitada.
5. Só então usar a experiência desse provider para definir o próximo.

Este documento é um plano, não uma implementação. Apenas o relatório foi criado; código, configurações operacionais e infraestrutura não foram alterados.

## 2. Premissas de produto

- “URN” foi interpretado como **URI de conexão**, por exemplo `postgresql://...`, também frequentemente chamada de connection string/DSN.
- “Tudo opcional” significa campos omitíveis quando houver padrão explícito ou outra fonte válida. A configuração final ainda precisa ser suficiente para a operação solicitada.
- “Sem SQL arbitrário trafegando” significa que o cliente não envia consultas livres. O backend necessariamente enviará os comandos SQL fixos ao PostgreSQL, protegidos por TLS quando habilitado.
- “Sem escrita” significa não modificar dados de negócio, schema, filas ou recursos externos. Conexões e consultas podem gerar logs, estatísticas e outros efeitos internos do servidor; não é uma promessa de zero escrita física.
- O uso inicial é por operadores autorizados em workloads de diagnóstico. Transformar o servidor anônimo atual em um console público de credenciais não faz parte do caminho recomendado.
- Transportes permanecem disponíveis por padrão. Recursos adicionais são opt-in de implantação; habilitação não equivale a autorização de acesso.

## 3. Estado atual e evidências

### O que já está bem encaminhado

| Área | Base existente que deve ser preservada |
| --- | --- |
| Runtime | Binário Go estático, imagem `scratch`, usuário não root, CA bundle e compatibilidade com filesystem somente leitura: [Dockerfile](/Users/cleidsonoliveira/github/projects/fruto-platform/testkit/Dockerfile:1). |
| Transportes | REST, GraphQL, SSE e WebSocket têm contratos simples e testes; shutdown possui prazo e encerramento de conexões: [main.go](/Users/cleidsonoliveira/github/projects/fruto-platform/testkit/main.go:294). |
| Saída controlada | Peers definidos em arquivo de implantação, timeout, concorrência limitada, conexões novas, TLS validado e ausência de redirects/proxy: [peers.go](/Users/cleidsonoliveira/github/projects/fruto-platform/testkit/peers.go:256). |
| Observabilidade | `slog`, versão, correlação e fatos de ciclo de vida; payloads e credenciais não fazem parte dos logs previstos: [main.go](/Users/cleidsonoliveira/github/projects/fruto-platform/testkit/main.go:502). |
| Frontend | Templates compartilhados, tokens CSS, três idiomas e módulos JavaScript pequenos. Atualizações de conteúdo usam `textContent`: [base.html](/Users/cleidsonoliveira/github/projects/fruto-platform/testkit/templates/base.html:1), [style.css](/Users/cleidsonoliveira/github/projects/fruto-platform/testkit/static/style.css:1). |
| Distribuição | Assets com endereço derivado do conteúdo, versão embutida e publicação multiarch: [main.go](/Users/cleidsonoliveira/github/projects/fruto-platform/testkit/main.go:744), [release.yml](/Users/cleidsonoliveira/github/projects/fruto-platform/testkit/.github/workflows/release.yml:87). |

As integrações externas atualmente implementadas são o probe HTTP/HTTPS via CLI e o monitoramento de peers configurados. Não existem providers de bancos ou brokers. O GraphQL atual é um endpoint do próprio Testkit, não um cliente de GraphQL externo. Persistência é um marcador em arquivo, não um repositório de resultados ou credenciais.

### Validação executada

| Verificação | Resultado |
| --- | --- |
| `go test -race -cover ./...` | Passou; 81,6% de cobertura de statements. |
| `go vet ./...` | Passou. |
| `go mod tidy -diff`, com módulos disponíveis localmente | Sem diferenças. |
| `gofmt -l *.go` | Nenhum arquivo listado. |
| `npm run test:frontend` | 23 testes passaram. |
| Concorrência e erro GraphQL | Reprodução isolada confirmou resposta antiga substituindo a nova e HTTP 200 com `errors` exibido como sucesso. |
| Parser e persistência | Teste adicional via overlay temporário confirmou: `{"value":"repro-marker"}{}` retorna 400, grava marcador de 12 bytes e concatena JSON de sucesso ao corpo de erro. Nenhum teste ou código do repositório foi alterado. |

Os testes Go precisaram de acesso a listeners temporários em localhost; a tentativa inicial no sandbox falhou por permissão, não por defeito do projeto. O ambiente local usa Go 1.27.1 e Node 24.20.0; o workflow configura Go 1.26.7 e Node 22. O gate da implementação deve repetir a validação nas versões do projeto.

A avaliação de UX foi feita sobre templates, JS, CSS e testes, sem navegador real, leitor de tela ou medição de contraste. Não houve build/execução da imagem nesta avaliação, validação em ECS/Kubernetes, acesso a bancos reais ou varredura completa de CVEs. O resultado não certifica produção nem ausência de vulnerabilidades.

## 4. Achados e prioridades

P1: corrigir antes de ampliar a superfície. P2: consolidar antes de habilitar diagnósticos externos. P3: melhoria localizada ou evolução posterior. Requisitos futuros abaixo não são apresentados como vulnerabilidades já exploráveis no modo atual.

| ID | Prioridade | Achado e impacto | Ação e aceite |
| --- | --- | --- | --- |
| A1 | P1 | [main.go:1122](/Users/cleidsonoliveira/github/projects/fruto-platform/testkit/main.go:1122): segundo JSON válido gera HTTP 400, mas o parser retorna `nil`. [persistence.go:52](/Users/cleidsonoliveira/github/projects/fruto-platform/testkit/persistence.go:52) pode continuar até a escrita. O teste atual verifica apenas status. | Toda rejeição deve interromper o handler. Testar `{"value":"x"}{}` com marcador ausente e existente: 400, nenhuma criação/alteração e nenhum corpo de sucesso anexado. |
| A2 | P1 | [graphql-ui.js:28](/Users/cleidsonoliveira/github/projects/fruto-platform/testkit/static/graphql-ui.js:28): cada seleção executa, sem bloqueio nem descarte de respostas antigas. A tela pode associar request e response de cenários diferentes. | Selecionar → revisar → executar, uma execução ativa, resultado ligado à execução correta. Testar cliques duplicados e respostas fora de ordem. |
| A3 | P1 | [graphql-ui.js:55](/Users/cleidsonoliveira/github/projects/fruto-platform/testkit/static/graphql-ui.js:55): `response.ok` basta para marcar sucesso, mesmo com erros GraphQL. | Separar sucesso HTTP, resultado da operação e expectativa do cenário. Um preset negativo pode passar por receber a rejeição esperada, com esse fato explícito. |
| A4 | P2 | [main.go:383](/Users/cleidsonoliveira/github/projects/fruto-platform/testkit/main.go:383) e [main.go:647](/Users/cleidsonoliveira/github/projects/fruto-platform/testkit/main.go:647): registro de rotas e catálogo de cards são independentes; `main.go` concentra 1.160 linhas de responsabilidades distintas. | Separar arquivos por responsabilidade e usar a mesma configuração resolvida para habilitar handlers e navegação. Preservar contratos dos transportes. |
| A5 | P2 | [rest-ui.js:122](/Users/cleidsonoliveira/github/projects/fruto-platform/testkit/static/rest-ui.js:122) e [graphql-ui.js:46](/Users/cleidsonoliveira/github/projects/fruto-platform/testkit/static/graphql-ui.js:46): requisições finitas sem `AbortSignal`; correlação e estados variam entre labs. | Consolidar execução com deadline, cancelamento, correlação e desbloqueio dos controles. Limite de backend continua obrigatório. |
| A6 | P2 | [peers.go:444](/Users/cleidsonoliveira/github/projects/fruto-platform/testkit/peers.go:444): qualquer timeout de rede vira `connect_timeout`, inclusive espera por resposta após TCP estabelecido. | Usar motivo neutro quando a fase não for conhecida, ou instrumentar a fase. Testar servidor que aceita TCP e não responde; não atribuir automaticamente a SG. |
| A7 | P2 | [probe.go:39](/Users/cleidsonoliveira/github/projects/fruto-platform/testkit/probe.go:39) segue redirects e usa transporte padrão; peers têm política mais restrita. | Definir política própria para a API de diagnóstico. Não expor o probe CLI por simples reutilização de handler; preservar seu contrato separado. |
| A8 | P2 | [labs-ui.test.mjs:4](/Users/cleidsonoliveira/github/projects/fruto-platform/testkit/frontend-tests/labs-ui.test.mjs:4): testes usam DOM montado manualmente. | Acrescentar poucos testes de navegador sobre templates reais, teclado, tela estreita, erro e nova tentativa. Não substituir a suíte rápida existente. |
| A9 | P2 | [release.yml:3](/Users/cleidsonoliveira/github/projects/fruto-platform/testkit/.github/workflows/release.yml:3): o único workflow encontrado roda em tags; verificações não estão definidas para PRs. | Executar o gate em PR, sem publicação. Incluir análise de vulnerabilidades de Go e imagem no processo de release; não tratar auditoria npm sem dependências como evidência sobre Go. |
| A10 | P2 | [persistence.go:74](/Users/cleidsonoliveira/github/projects/fruto-platform/testkit/persistence.go:74): arquivo inteiro é lido antes de verificar o limite de 4 KiB. Um volume pode conter arquivo maior, produzido externamente. | Leitura limitada a tamanho máximo + 1, com teste de arquivo preexistente grande. Não reutilizar este store para segredos. |
| A11 | P3 | [main.go:829](/Users/cleidsonoliveira/github/projects/fruto-platform/testkit/main.go:829): schema GraphQL recriado por requisição, sem contexto HTTP no executor. | Inicializar schema uma vez e propagar contexto ao separar esse módulo; manter os resultados existentes. |

Há ainda um cuidado de refatoração: [responseLogWriter](/Users/cleidsonoliveira/github/projects/fruto-platform/testkit/main.go:480) não preserva `http.Flusher`/`http.Hijacker`. Aplicá-lo indiscriminadamente ao mux pode quebrar SSE e WebSocket. Manter instrumentação específica desses transportes ou adaptar e testar as interfaces necessárias.

## 5. Consolidação do backend

### Organização e dependências

Primeiro separar arquivos no mesmo `package main`, evitando uma migração estrutural de uma vez:

| Arquivo/responsabilidade proposta | Conteúdo |
| --- | --- |
| `main.go` | Seleção CLI/servidor e composição. |
| `config.go` | Ambiente, padrões explícitos, validação e capacidades habilitadas. |
| `server.go` | Rotas, servidor e shutdown. |
| `pages.go` | View models, templates, catálogo e navegação. |
| `http_helpers.go` e `observability.go` | Parsing, respostas, correlação e logs seguros. |
| `rest.go`, `graphql.go`, `sse.go`, `websocket.go` | Contratos de transporte existentes. |
| Módulos existentes | Preservar `peers.go`, `probe.go`, `persistence.go` e `i18n.go`. |

Para o primeiro provider, criar uma área pequena de diagnóstico com a direção **handler → execução limitada/política → PostgreSQL**. O provider não deve conhecer `http.ResponseWriter`, templates ou traduções. A camada HTTP valida entrada e traduz resultados para o contrato público; o provider trata protocolo, conexão e operações fixas.

Uma função ou interface pequena no ponto de consumo é suficiente para testar a execução. Não criar antecipadamente uma interface com `ListDatabases`, `Publish`, `Query`, `ListTopics` e implementações vazias para todos os produtos futuros. O catálogo descreve capacidades; cada provider mantém entradas e operações próprias.

### Contrato dos novos diagnósticos

Definir structs Go tipadas e operações enumeradas. Não usar `map[string]any` como contrato universal de conexão nem aceitar um campo `sql`, `command` ou `script`. Validar JSON único, tamanho, campos desconhecidos, provider, operação e parâmetros antes de abrir rede.

Exemplo de resultado proposto, exclusivo da nova camada:

```json
{
  "schema_version": 1,
  "check_id": "<uuid>",
  "provider": "postgres",
  "operation": "connect",
  "status": "failed",
  "stage": "authentication",
  "code": "authentication_failed",
  "duration_ms": 83,
  "data": null
}
```

- `status`: sucesso, falha, cancelamento ou timeout; etapas posteriores podem ficar como não executadas.
- `stage`: DNS, TCP, TLS, autenticação ou operação, apenas quando observado; admitir etapa desconhecida.
- `code`: identificador estável e sanitizado. A UI traduz a mensagem; erro bruto do driver não vai para tela ou log.
- `data`: resultado limitado e específico da operação. Não retornar configuração de conexão integral.
- Validação/autorização/limite usam HTTP 4xx; falha interna, 5xx. Uma execução aceita e concluída pode retornar HTTP 200 com `status: failed`: a UI avalia o resultado do diagnóstico, não apenas o HTTP.

Manter os payloads públicos de transporte compatíveis. Logs da nova camada registram provider, operação, resultado, duração e correlação, sem DSN, senha, conteúdo de certificado, SQL ou nomes descobertos. A correlação é por operação, não uma sessão de banco.

Executar operações curtas, com conexão nova e encerramento ao final. Não usar pool compartilhado entre usuários ou manter credenciais no servidor entre cliques no primeiro corte. Isso evita misturar identidades e torna a nova tentativa uma validação real de conexão. O navegador pode manter configuração apenas na memória da página e reenviá-la por HTTPS a cada ação explícita.

## 6. Feature flags e catálogo

Uma configuração enumerada é suficiente para começar. Proposta de nomes, ainda não existentes:

```text
TESTKIT_SMOKES=transport
TESTKIT_SMOKES=transport,postgres
```

Categorias são organização de UX; habilitação é por provider implementado. Assim, habilitar Postgres não libera silenciosamente todo futuro banco adicionado à imagem.

Regras:

1. Valor ausente usa `transport`; transportes e health checks permanecem como base.
2. Identificador desconhecido ou configuração inválida interrompe a inicialização com mensagem clara.
3. Provider desabilitado não tem handler de execução registrado; acesso direto responde 404 e não abre rede.
4. Cards, páginas e operações são derivados das capacidades resolvidas pelo backend. A configuração do navegador nunca habilita uma API.
5. Habilitar diagnóstico de saída exige controle de acesso e política de destinos válidos na inicialização.
6. `TESTKIT_PEERS_FILE` e `TESTKIT_PERSISTENCE_FILE` mantêm o opt-in atual. O marcador de persistência continua separado da promessa de leitura dos novos providers.
7. Não adicionar flags ou cards para MySQL, Kafka etc. antes de implementar seus contratos.

A home deve apresentar **Transportes**, **Bancos de dados**, **Brokers** e **Sistemas externos**, mostrando uma categoria adicional somente quando houver provider habilitado. REST/GraphQL do próprio Testkit devem ficar claramente identificados como transportes; HTTP/GraphQL para outro destino pertencem a Sistemas externos.

## 7. Threat model mínimo antes da API de saída

### Fronteiras e controle de acesso

O contrato atual documenta servidor sem autenticação e proíbe destinos escolhidos por requisição pública: [SECURITY.md:16](/Users/cleidsonoliveira/github/projects/fruto-platform/testkit/SECURITY.md:16) e [CONTRIBUTING.md:41](/Users/cleidsonoliveira/github/projects/fruto-platform/testkit/CONTRIBUTING.md:41). Essa regra precisa ser revisada explicitamente na evolução, preservando o modo de transporte padrão.

Ativos: credenciais digitadas, identidade de rede/IAM do workload, serviços alcançáveis, metadados descobertos e disponibilidade. Atores: operador autorizado, visitante sem autorização, página maliciosa no navegador, destino remoto malicioso e processo com acesso privilegiado ao host. Não prometer proteger memória contra administrador do host nem impedir abuso de alguém que controla a implantação e sua política.

Para o primeiro corte independente de cloud, recomendo **um segredo de acesso operacional por implantação**, recebido pelo processo via arquivo montado somente leitura e informado pelo operador à UI, mantido apenas em memória e enviado em header para APIs de diagnóstico. A comparação ocorre no servidor. Sem cadastro, RBAC ou banco de usuários. Esse segredo não é a senha do banco e sua granularidade de auditoria é por implantação, não por pessoa.

Em implantação compartilhada, autenticação no gateway/ALB é uma alternativa, desde que impeça acesso direto aos targets e valide corretamente a identidade encaminhada. Não aceitar headers de identidade arbitrários de qualquer cliente. A escolha do mecanismo pode mudar; o gate obrigatório é que nenhum cliente não autorizado consiga executar diagnósticos.

### Ameaças, controles e prova exigida

| Ameaça | Controle mínimo | Teste de aceite |
| --- | --- | --- |
| Visitante usa a identidade de rede do workload | Autorização em todas as APIs de diagnóstico, inclusive descoberta; HTTPS no caminho que transporta credenciais. | Token ausente/inválido não abre conexão; backend direto não contorna o controle. |
| SSRF, varredura ou acesso a serviços internos indevidos | Política de implantação somente leitura por provider, hosts/CIDRs e portas; sem destino irrestrito. Limites de execução e egress na infraestrutura. | Destino fora da política é rejeitado antes de conectar. |
| DNS rebinding e IPv6 contornam a política | Canonicalizar endereços, validar respostas A/AAAA e discar somente endereço validado; preservar hostname esperado no TLS. Revalidar cada tentativa. | DNS alternando destino, IPv4 mapeado em IPv6 e endereço proibido não contornam a regra. |
| Roubo de credenciais de metadata/cloud | Bloquear destinos de metadata, loopback, link-local, multicast e endereços não especificados na API de diagnóstico, inclusive variantes IPv6. | Requisições a metadata de EC2/ECS são negadas mesmo com feature habilitada. |
| Vazamento de senha/URI/certificado | Corpo HTTPS, sem eco integral, `Cache-Control: no-store`, logs por campos permitidos, sem armazenamento local/persistente, sem capturar payload no gateway/APM. | Segredos sentinela ausentes de logs, respostas, preview, erros e exportações, inclusive com URI inválida. |
| Certificado ou URI induz leitura de arquivos/alteração de destino | Upload limitado, parsing em memória, sem paths fornecidos pelo usuário; aceitar somente parâmetros de URI conhecidos. | `passfile`, `servicefile`, paths de certificado, opções de runtime e parâmetros extras são rejeitados antes do driver. |
| Saturação por conexões ou destino lento | Deadline total e por fase quando possível, cancelamento, sem retry automático, concorrência e taxa limitadas no servidor. | Excesso retorna erro previsível; timeout/cancelamento/shutdown fecham recursos e liberam capacidade. |
| CSRF, XSS ou clickjacking | Origem autorizada, JSON estrito, sem CORS aberto, token fora da URL, CSP compatível com os assets locais e proteção contra framing; DOM por texto. | Origem indevida é rejeitada e conteúdo remoto nunca executa HTML/JS. |
| Mistura de credenciais/resultados entre operadores | Estado de execução por request; nada de pool global com credencial variável nem broadcast de resultados. | Execuções simultâneas com configurações diferentes não compartilham resultado ou segredo. |

**Não bloquear toda rede privada:** o objetivo inclui acessar RDS e serviços internos. Permitir explicitamente os destinos privados necessários; bloquear metadata e destinos especiais separadamente. Preferir destinos exatos e portas específicas a CIDRs amplos. Redirects e proxies de ambiente ficam desabilitados na nova API, evitando um segundo caminho não validado. Essas decisões seguem os controles de validação de destino e DNS descritos pela [OWASP para SSRF](https://cheatsheetseries.owasp.org/cheatsheets/Server_Side_Request_Forgery_Prevention_Cheat_Sheet.html).

No ECS, metadata não é abstrato: existem endpoints locais como `169.254.170.2`. O cliente de diagnóstico não deve oferecê-los como destino. No futuro, um provider AWS pode usar a cadeia de credenciais do SDK por configuração confiável, sem expor esses endpoints ao usuário. Referência: [metadata de tasks ECS](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/task-metadata-endpoint-v2.html).

Ponto de partida para limites, a calibrar com testes: 5 s para conectar, 10 s por operação, quatro operações concorrentes por processo, uma por laboratório na UI, 100 itens e 64 KiB por resposta, 64 KiB para CA PEM e 128 KiB por requisição. Não adicionar filas para acumular trabalho. Informar truncamento e limitar taxa de tentativas; em múltiplas réplicas, limites por processo se multiplicam e precisam de limite agregado no gateway se necessário.

Manter UID não root, root filesystem read-only, capabilities removidas, limites de CPU/memória, egress mínimo e remoção do workload após o diagnóstico. Feature flag não substitui esses controles.

## 8. Consolidação de componentes e UX

### Componentes a extrair agora

| Componente | Responsabilidade e limite |
| --- | --- |
| Cabeçalho do laboratório | Título, categoria, breadcrumbs e explicação do que o smoke comprova. Reutilizar templates existentes. |
| Seleção e execução | Cenário selecionado, ação explícita de executar, cancelar, nova tentativa e bloqueio durante execução. |
| Painel de resultado | Status do diagnóstico separado de HTTP/protocolo; duração, correlação, erro e detalhes. |
| Executor de requisições finitas | `fetch`, correlação, deadline, cancelamento e descarte de resposta obsoleta. Não forçar SSE/WS nesse ciclo. |
| Formatação segura | Dados remotos como texto; visualização de JSON e resumo sanitizado de entrada. |

Campos de formulário, segredo, TLS/upload e lista de bancos devem nascer **junto do PostgreSQL**. Extrair para componentes compartilhados quando houver reutilização concreta. Não construir um gerador de formulários orientado por schema para todos os providers futuros.

O preview atual mostra o payload completo em [rest-ui.js:118](/Users/cleidsonoliveira/github/projects/fruto-platform/testkit/static/rest-ui.js:118) e [graphql-ui.js:36](/Users/cleidsonoliveira/github/projects/fruto-platform/testkit/static/graphql-ui.js:36). Isso atende aos presets existentes, mas não pode ser copiado para formulários com senha/URI. O resumo do diagnóstico deve ser produzido por lista de campos permitidos, não por tentativa de esconder algumas chaves depois.

### Padrão de interação e acessibilidade

- Operações finitas: ocioso → executando → sucesso/falha/timeout/cancelado. Conexões SSE/WS mantêm estados próprios.
- Alterar conexão ou banco invalida visualmente os resultados dependentes. Uma resposta atrasada não os revalida.
- Sem execução automática ao trocar campos, categoria, provider ou preset. Descobertas também exigem ação explícita.
- Label, ajuda e erro associados aos campos; seleção com semântica acessível; `aria-busy` durante execução e região de status com anúncios moderados.
- Navegação por teclado, foco previsível, resultado compreensível sem depender de cor e layout funcional em tela estreita.
- Manter os três idiomas e os tokens CSS existentes. Não é necessário reorganizar todo o stylesheet para adicionar essa camada.
- “Limpar credenciais e resultados” remove referências da página. Não usar URL, `localStorage`, `sessionStorage`, IndexedDB ou histórico persistido para segredos; não prometer apagamento seguro de memória gerenciada.

## 9. Primeiro provider: PostgreSQL

### Configuração sem ambiguidades

Dois modos mutuamente exclusivos: **URI** ou **campos separados**. Ambos viram a mesma configuração validada no backend. Não combinar silenciosamente host da URI com usuário do formulário. Antes de executar, apresentar o destino e as opções efetivas, com senha ocultada e sem reconstruir URI sensível no preview.

| Entrada | Semântica proposta |
| --- | --- |
| Host | Precisa existir na configuração resolvida. Nunca assumir localhost silenciosamente. |
| Porta | Omitível; padrão visível `5432`; validar 1–65535 e política do destino. |
| Usuário | Precisa ser resolvido para autenticação; não herdar implicitamente o usuário do SO. |
| Senha | Pode ser omitida conforme autenticação do destino. Omitida não significa buscar segredo oculto no ambiente/arquivo. |
| Banco inicial | Padrão visível `postgres`, editável; não é garantido que exista ou seja acessível. Não testar vários nomes automaticamente. |
| TLS | Padrão proposto `verify-full`; sem fallback automático para texto claro. |
| CA do servidor | Upload PEM opcional para CA privada. Sem upload, usar trust store do runtime. |
| Certificado/chave do cliente | Requisito distinto de CA. Adiar mTLS ao incremento seguinte, salvo necessidade concreta inicial; se incluído, tratar o par como segredo e validar correspondência. |

“SSL sim/não” é insuficiente para comunicar confiança. No corte inicial, oferecer **TLS com validação do servidor** e **sem TLS**, este último por escolha explícita e identificado no resultado. Se depois houver modo criptografado sem validar identidade, rotulá-lo como tal; não chamá-lo de conexão segura. `verify-full` verifica cadeia e hostname; CA e certificado de cliente têm funções diferentes. Referência: [SSL no PostgreSQL](https://www.postgresql.org/docs/18/libpq-ssl.html).

Aceitar apenas esquema PostgreSQL e campos de URI explicitamente suportados. Inicialmente, um host; sem Unix socket, multi-host, `servicefile`, `passfile`, paths locais de certificados ou opções livres de sessão. Defaults do driver não podem introduzir credenciais, arquivos ou fallback TLS implícitos. A documentação do [pgconn/pgx](https://pkg.go.dev/github.com/jackc/pgx/v5/pgconn#ParseConfig) registra leitura de ambiente/arquivos e interação entre TLS e fallbacks; esse comportamento precisa de teste ao escolher e fixar a versão do driver. `pgx/v5` é candidato adequado a avaliar, não dependência já presente.

### Operações permitidas

| Operação | UX | Execução e evidência |
| --- | --- | --- |
| `connect` | Testar conexão | Conexão física nova, negociação TLS quando configurada e autenticação; fecha ao terminar. Abrir handle/pool sem conectar não conta como sucesso. |
| `arithmetic_check` | Executar `SELECT 1 + 1` | SQL constante no backend, resposta esperada `2`. Nenhum editor SQL ou texto de query na requisição da UI. |
| `list_databases` | Listar bancos disponíveis para tentativa | Consulta fixa ao catálogo, limitada, considerando `datallowconn` e privilégio `CONNECT`; informar que são candidatos, não conexões já comprovadas. |
| `list_schemas` | Selecionar banco e listar schemas | Nova conexão ao banco selecionado e consulta fixa/limitada de schemas com `USAGE`; exibir a política sobre schemas de sistema. Isso não comprova leitura de todas as tabelas. |

O PostgreSQL expõe funções para consultar privilégios de banco e schema, mas visibilidade/privilégio de catálogo não substitui uma tentativa real de conexão. Referência: [funções de privilégios](https://www.postgresql.org/docs/current/functions-info.html). A seleção de outro banco deve provocar nova conexão; a interface não deve simular troca de banco na sessão atual.

Proteções contra escrita e execução livre:

1. Lista fechada de operações; queries constantes mantidas no código e parâmetros como dados. Não usar regex para decidir se SQL livre é “somente leitura”.
2. Transações `READ ONLY` para as consultas, com timeout e término garantido. Preferir usuário de privilégios mínimos.
3. Catálogos/funções qualificados com `pg_catalog`, sem resolução de nomes controlada por schemas graváveis ou execução de funções fornecidas pelo usuário.
4. Sem DML, DDL, `COPY`, extensões, funções arbitrárias, criação de objetos de smoke ou acesso a tabelas de negócio.
5. Não inferir segurança apenas porque o comando começa com `SELECT`. O modo read-only é defesa adicional, não sandbox SQL universal; o próprio PostgreSQL esclarece que ele não impede toda escrita física. Referência: [transações read-only](https://www.postgresql.org/docs/current/sql-set-transaction.html).

O fluxo dinâmico inicial pode ser stateless no backend: configurar → testar → executar operação fixa → listar bancos → selecionar → listar schemas. A página mantém os passos e metadados apenas em memória; cada ação é independente. Isso funciona com várias réplicas atrás de ALB sem afinidade de sessão ou armazenamento central. Resultados refletem instantes distintos e devem carregar sua própria correlação.

## 10. Evolução para outras famílias

| Família | Evolução proposta | Limite a preservar |
| --- | --- | --- |
| Bancos | PostgreSQL primeiro; depois MySQL/SQL Server conforme demanda. Redis e DynamoDB têm operações próprias. | Não impor modelo banco/schema/SQL a todos. Nenhum provider recebe comando arbitrário. |
| Brokers | Começar por um provider com operação fixa de conexão/autenticação ou metadata. | Publicar, consumir, confirmar, declarar filas/tópicos ou fazer commit de offset ficam fora do contrato somente leitura. Cada operação exige análise de efeitos antes de habilitação. |
| HTTP externo | Destino autorizado e presets revisados de método/caminho; resultado limitado. | GET/HEAD não garantem ausência de efeitos em qualquer serviço. Não prometer “sem escrita” para URLs arbitrárias. |
| GraphQL externo | Operações predefinidas e revisadas para um destino autorizado. | Bloquear mutations não garante ausência de efeitos de resolvers. Consulta livre não entra no primeiro corte. |
| TCP externo | Estabelecer conexão e fechar, porta/destino autorizados, sem payload arbitrário. | Chamar de “conexão TCP”, distinguindo de ping ICMP. Não é scanner de sub-rede/portas. |
| AWS: DynamoDB/SQS/SNS | Endpoints/região e operações fixas por provider, identidade IAM explícita no resultado sanitizado. | A identidade da task faz parte do teste. Não habilitar acesso genérico a APIs AWS nem confundir metadata SDK com destino diagnosticável. |

SQS/SNS podem ficar juntos na categoria de mensageria por conveniência, mas SNS é publicação/assinatura e não deve herdar uma interface de fila. O reuso deve estar em execução, política, resultados e UX, não em fingir equivalência entre protocolos.

## 11. Plano de execução e gates

### Etapa 1 — corrigir a base

Escopo: A1–A3 e A10. Criar regressões para os defeitos, corrigir parsing/ausência de efeitos, concorrência GraphQL, interpretação de resultados e leitura limitada do marcador.

**Aceite:** requisições rejeitadas não produzem efeitos; a tela sempre relaciona request/response corretos; cenários positivos e negativos reportam a expectativa corretamente; gates existentes passam. Não adicionar provider nesta etapa.

### Etapa 2 — consolidar backend e laboratórios atuais

Escopo: separar responsabilidades sem alterar APIs públicas; resolver configuração/catálogo em um lugar; consolidar executor finito e painel de resultado; corrigir taxonomia de timeout; manter interfaces de streaming e correlação. A11 pode acompanhar a extração do GraphQL.

Adicionar verificação de PR com Go/frontend e poucos testes reais de navegador. Se arquivos migrarem para subdiretórios, atualizar o gate de formatação, hoje restrito a `*.go` na raiz.

**Aceite:** mesmas rotas, payloads, versão, cache, idiomas e comportamento SSE/WS; cancelamento e resposta obsoleta cobertos; navegador valida templates reais e navegação por teclado.

### Etapa 3 — fechar a superfície de diagnóstico

Escopo: documentar o threat model; implementar capacidades opt-in, controle de acesso, política de destinos, limites e contrato de resultado. Testar com destinos locais controlados, sem banco real e sem endpoint genérico liberado em produção.

Atualizar `SECURITY.md`, `CONTRIBUTING.md` e README canônicos e traduções. Documentar transporte de credenciais, exposição de resultados, proteção no ingress e descarte da implantação. Nenhuma flag transforma automaticamente o servidor em proxy público.

**Aceite:** recursos desabilitados não executam; acesso não autorizado e destinos proibidos não abrem rede; limites, cancelamento, redaction e isolamento entre execuções têm regressões. Essa etapa é bloqueadora da habilitação do provider.

### Etapa 4 — PostgreSQL incremental

1. **4A: conexão.** URI/campos, defaults explícitos, TLS e CA em memória, erros observáveis e connection lifecycle limitado.
2. **4B: operação fixa.** `SELECT 1 + 1`, somente leitura, resultado `2`, ausência de SQL na entrada do navegador.
3. **4C: descoberta.** Listagem limitada de candidatos a banco e schemas; seleção explícita e nova conexão.

**Aceite:** testes de integração com PostgreSQL descartável cobrem sucesso, senha incorreta, banco inexistente, privilégio insuficiente, TLS ausente/CA inválida/hostname divergente, CA enviada, sem TLS explícito, timeout e cancelamento. Testar URI contra campos, parâmetros proibidos, ambiente contaminado por `PG*`, resultado truncado e preservação de segredos. Usar logs de banco ou instrumentação em ambiente de teste para comprovar somente o conjunto de comandos fixos autorizado.

### Etapa 5 — validar distribuição e uso operacional

Escopo: repetir gates nas versões do projeto; construir e testar imagem com filesystem read-only, UID não root, capabilities removidas e shutdown. Rodar análise de vulnerabilidades da aplicação e imagem antes da release. A escolha de assinatura/SBOM/proveniência pode ser uma entrega de distribuição separada: hoje `sbom` e `provenance` estão desabilitados em [release.yml:95](/Users/cleidsonoliveira/github/projects/fruto-platform/testkit/.github/workflows/release.yml:95).

Depois, executar uma validação operacional autorizada em ECS ou no ambiente alvo, com destino de teste e evidência sanitizada. Esta avaliação não autoriza nem executa deploy.

**Aceite:** imagem funciona no runtime restrito; documentação explica configuração e limites; resultado permite correlacionar a execução ao workload e à versão. Só então selecionar o segundo provider.

### Checklist para iniciar PostgreSQL

- [ ] Parser rejeita completamente entradas inválidas e testes comprovam ausência de efeitos.
- [ ] Concorrência e resultado GraphQL corrigidos.
- [ ] Responsabilidades separadas sem regressão dos transportes.
- [ ] Catálogo e flags resolvidos pelo backend; opt-in funciona também na API.
- [ ] Controle de acesso e política de destinos implementados e testados.
- [ ] Segredos, certificados, limites e cancelamento têm contrato definido.
- [ ] Componentes atuais de execução/resultado consolidados e verificados em navegador.
- [ ] CI em PR preserva o gate e contempla os novos testes de segurança.
- [ ] Threat model e documentação refletem a nova fronteira de confiança.

## 12. Como interpretar o smoke no cenário ECS

O teste deve produzir fatos separados: acesso do navegador ao Testkit; resolução do destino pelo workload; conexão TCP; TLS; autenticação; execução da operação fixa; privilégios de descoberta. `/healthz` e `/readyz` continuam indicando a saúde do Testkit, sem depender do Postgres que se está investigando.

Para comparar com a aplicação, registrar ou conferir fora dos logs sensíveis: VPC/subnets, Security Groups efetivos, modo de rede, resolução DNS, destino/porta, configuração TLS, usuário/banco e identidade IAM quando pertinente. No ECS, o diagnóstico parte da task; a execution role e a task role têm papéis diferentes e a identidade relevante depende da operação.

Um sucesso mostra que **aquele workload, naquele instante, com aquela configuração, conseguiu executar aquela operação**. Isso ajuda a separar hipóteses de aplicação e infraestrutura. Não comprova automaticamente que todas as réplicas, rotas do ALB, permissões IAM, tabelas ou configurações da aplicação estão corretas. Um timeout isolado também não identifica SG como causa.

O produto deve encerrar cada resultado com uma descrição objetiva do que foi observado e do que ainda não foi testado. Essa precisão é tão importante para um Testkit de debug quanto aumentar a lista de providers.
