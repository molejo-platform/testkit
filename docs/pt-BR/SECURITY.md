# Política de Segurança

[English](../../SECURITY.md) |
[Español (Argentina)](../es-AR/SECURITY.md)

A versão em inglês é a versão canônica desta política.

## Versões suportadas

O Molejo Testkit é um projeto experimental em pré-release. Não existe uma versão
suportada ou pronta para produção. Correções de segurança são aplicadas à branch
padrão conforme a disponibilidade dos mantenedores.

## Uso pretendido

O Testkit é uma fixture determinística de testes. Ele não é uma aplicação
autenticada, um serviço de autorização ou um backend de produção. Implante-o
somente onde uma carga de teste for apropriada e remova ambientes descartáveis
após a validação.

O servidor HTTP público intencionalmente não expõe o probe de saída. O probe é um
comando explícito do contêiner destinado a Jobs controlados e diagnósticos locais.
Quem puder executar esse comando recebe a identidade de rede do contêiner ou Pod;
portanto, o acesso à criação e execução de cargas permanece uma fronteira de
segurança do cluster.

O monitoramento de pares configurados preserva essa mesma fronteira. Os destinos
vêm somente do arquivo read-only selecionado por `TESTKIT_PEERS_FILE`;
requisições HTTP públicas não podem adicionar, substituir nem acionar destinos.
As verificações ignoram variáveis de proxy, rejeitam redirects, validam
certificados HTTPS e retornam apenas estado sanitizado em memória, sem cache.
Trate acesso de escrita ao arquivo como permissão para originar tráfego de rede
a partir do workload do Testkit.

O endpoint opcional de persistência também não possui autenticação e deve ser
habilitado somente em workloads de teste descartáveis. O caminho é fixado na
inicialização por `TESTKIT_PERSISTENCE_FILE`; requisições não escolhem arquivos.
As respostas expõem apenas tamanho e fingerprint SHA-256, mas qualquer cliente
com acesso pode substituir o marcador. Não armazene credenciais nem dados
sensíveis nele.

## Reportando uma vulnerabilidade

O relato privado de vulnerabilidades do GitHub ainda não está habilitado neste
repositório. Até existir um canal privado, entre em contato com os mantenedores
pelo [perfil da organização Molejo Platform](https://github.com/molejo-platform)
para solicitar um canal privado, sem incluir detalhes da vulnerabilidade na
mensagem inicial. Não divulgue a vulnerabilidade em issue, discussion, pull
request ou log de teste público.

Inclua, quando possível:

- a revisão e o componente afetados;
- passos para reprodução ou uma prova de conceito mínima;
- o impacto de segurança esperado;
- premissas relevantes de implantação;
- qualquer mitigação conhecida.

Os mantenedores confirmarão e avaliarão os relatos conforme sua disponibilidade.
Como o projeto está em pré-release, não há atualmente SLA de resposta ou correção.

## Divulgação

Conceda aos mantenedores uma oportunidade razoável para investigar e preparar uma
correção antes da divulgação pública. O crédito será coordenado com o relator
quando apropriado e desejado.
