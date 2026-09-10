# Molejo Testkit

[English](../../README.md) |
[Português (Brasil)](../pt-BR/README.md)

> Proyecto experimental en pre-release. Molejo Testkit es una fixture de pruebas,
> no una aplicación de producción.

Molejo Testkit es una imagen de contenedor pequeña y determinista para smoke
tests de conectividad de red y contratos mínimos de transporte HTTP. También puede
ejercitar la entrega de aplicaciones en Kubernetes y las políticas de red. Un
único binario Go estático ofrece REST, GraphQL, Server-Sent Events (SSE),
WebSocket, endpoints de salud y un comando explícito de probe de egreso.

El servidor HTTP público y el probe de red arbitrario están separados
intencionalmente. El modo servidor solo puede llamar a pares declarados en un
archivo opcional de solo lectura, por lo que una solicitud pública no convierte
la carga en un proxy de egreso. Los diagnósticos puntuales siguen requiriendo un
comando explícito del contenedor.

## Objetivo y alcance

Testkit responde una pregunta concreta: ¿el cliente puede alcanzar la carga
descartable y el transporte elegido puede completar su intercambio mínimo? Un
smoke test WebSocket exitoso confirma el camino de red, el upgrade HTTP, el
intercambio bidireccional de frames y el cierre básico de ese camino de despliegue.

No es una implementación completa ni reemplaza las pruebas de la aplicación. Un
smoke test exitoso no valida autenticación, autorización, rooms, ruteo, reglas de
negocio, esquemas completos de mensajes, performance, carga ni dependencias que
no sean ejercitadas por el escenario.

## Superficies de smoke test

| Capacidad | Interfaz |
| --- | --- |
| Aliases de detección de idioma | `GET /`, `GET /websocket`, `GET /rest`, `GET /graphql-lab`, `GET /sse` |
| Páginas localizadas | `GET /en/`, `GET /pt-BR/`, `GET /es-AR/` |
| Laboratorios localizados en el navegador | `GET /en/rest`, `/en/graphql-lab`, `/en/sse`, `/en/websocket` y caminos traducidos equivalentes |
| Assets estáticos | `GET /static/style.css` y branding Molejo incorporado |
| Liveness | `GET /healthz` |
| Readiness | `GET /readyz` |
| Respuesta no disponible | `GET /not-ready` |
| Estado REST | `GET /api/status` |
| Colección REST | `GET /api/items` |
| Echo REST | `POST /api/echo` |
| GraphQL | `POST /graphql` |
| Server-Sent Events | `GET /events` |
| Echo y broadcast WebSocket | `GET /ws` |
| Probe de red HTTP/HTTPS | `testkit probe URL` |
| Identidad y estado de pares configurados | `GET /api/identity`, `GET /api/peers` |
| Metadatos del marcador persistente opcional | `GET`, `PUT /api/persistence` |

La versión almacenada en el archivo versionado `VERSION` se integra en el
binario Go y se expone en los payloads de los protocolos, en los logs
estructurados, en el encabezado y pie de página del navegador y en el header
`Testkit-Version` de toda respuesta HTTP, incluidos los handshakes WebSocket
exitosos. El pie de página también incluye un pequeño enlace localizado de
atribución a Molejo con tracking UTM de la versión del build. Esto hace
observables las pruebas de rollout y transporte sin requerir argumentos externos
de build ni cambiar la identidad lógica de la carga.

## Requisitos previos

- Go 1.26.x.
- Docker Engine o Docker Desktop con Buildx habilitado.
- `curl` para los ejemplos.

## Build y ejecución local

Construí la imagen para la plataforma local de Docker:

```sh
docker buildx build \
  --load \
  --tag molejo-testkit:dev \
  .
```

Ejecutala con el contrato restringido esperado por Molejo Platform:

```sh
docker run --rm \
  --publish 8080:8080 \
  --read-only \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  molejo-testkit:dev
```

Validá el contrato REST:

```sh
curl --fail http://localhost:8080/api/status
```

Respuesta esperada:

```json
{"status":"ok","version":"v0.8.0"}
```

Para ejecutar el servidor en otro puerto, definí `HTTP_PORT`. El valor
predeterminado es `8080`:

```sh
HTTP_PORT=2020 go run .
curl --fail http://localhost:2020/api/status
```

En el contenedor, configurá el mismo puerto dentro y fuera del contenedor:

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

Después de iniciar el servidor, verificá readiness y ejercitá el intercambio
mínimo de WebSocket:

Si configuraste `HTTP_PORT`, reemplazá `8080` por su valor en las dos URLs.

```sh
curl --fail http://localhost:8080/readyz
wscat --connect ws://localhost:8080/ws
> {"message":"smoke"}
```

La respuesta de readiness, el handshake WebSocket exitoso y el mensaje JSON
recibido confirman la conectividad básica y el camino de transporte. No validan
funcionalidades específicas de WebSocket de la aplicación.

## Consola del navegador

Abra `http://localhost:8080/` para detectar el idioma del navegador o use
directamente `/en/`, `/pt-BR/` o `/es-AR/`. Los laboratorios están disponibles
en los caminos `/rest`, `/graphql-lab`, `/sse` y `/websocket` correspondientes.
Los laboratorios son verificaciones pequeñas y deterministas de conectividad y
contrato, no suites de funcionalidades. REST y GraphQL usan presets guiados que
muestran detalles de solicitud y respuesta. El laboratorio SSE muestra IDs,
nombres y datos de eventos, con acciones explícitas para conectar, desconectar y
reconectar. El laboratorio WebSocket muestra dos clientes independientes del
mismo origen, `Client A` y `Client B`, para observar un intercambio mínimo de
mensajes y su señal de broadcast. Los clientes del navegador no se reconectan
automáticamente después de un error o cierre.

## Ejemplos mínimos de protocolo

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

El handshake exitoso y el mensaje recibido son la señal del smoke test de
WebSocket. La fixture transmite el mismo mensaje determinista a los clientes
conectados para hacer visible el resultado de transporte; esto no prueba rooms,
autenticación ni ruteo a nivel de la aplicación.

```json
{"message":"hello","version":"v0.8.0"}
```

## Probe de red

Ejecutá diagnósticos de red como un comando explícito de la misma imagen:

```sh
docker run --rm molejo-testkit:dev probe https://example.com/
```

El probe realiza una única solicitud HTTP o HTTPS `GET`, con límites, y emite un
resultado JSON. Está separado de los smoke tests de transporte entrante del
servidor: valida alcance HTTP/HTTPS explícito de egreso, no conectividad
WebSocket. Sus códigos de salida forman el contrato de automatización:

| Código | Significado |
| --- | --- |
| `0` | El destino respondió con HTTP `2xx`. |
| `1` | La solicitud falló o devolvió una respuesta distinta de `2xx`. |
| `2` | Los argumentos del comando o la URL son inválidos. |

Para validar NetworkPolicies de Kubernetes, ejecutá el probe como un Job de corta
duración en el namespace bajo prueba. Aplicá las labels y la ServiceAccount cuya
identidad de red querés validar y comprobá el código de salida del Job. Así, el
origen, el destino y el resultado esperado de permiso o denegación quedan
explícitos.

## Monitoreo de pares configurados

El modo servidor puede verificar continuamente una allowlist fija de otras
instancias de Testkit. La funcionalidad queda deshabilitada salvo que
`TESTKIT_PEERS_FILE` apunte a un archivo JSON de solo lectura:

El monitoreo de pares valida la conectividad del transporte y el endpoint de identidad
configurado de Testkit. No valida compatibilidad de la aplicación ni
funcionalidades de negocio entre los pares.

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

Cada verificación abre una conexión directa nueva, ignora las variables de
proxy HTTP, no sigue redirects y solicita el camino fijo `/api/identity`. HTTPS
usa el trust store del sistema sin modo inseguro. Las respuestas se limitan a 4
KiB y como máximo cuatro pares se verifican en paralelo. La primera verificación
es inmediata y las siguientes usan `check_interval`; `timeout` debe ser positivo
menor o igual a 30 segundos y menor que ese intervalo.

`GET /api/identity` devuelve la identidad lógica configurada y un `boot_id`
UUIDv7 local al proceso. `GET /api/peers` devuelve únicamente hechos sanitizados
en memoria sobre las últimas verificaciones. Ambos endpoints existen solo
cuando el archivo está configurado. Una instancia sin pares de egreso puede usar
`"peers": []` para servir su identidad. Ningún endpoint acepta un destino ni
inicia una verificación bajo demanda, y ambas respuestas usan
`Cache-Control: no-store`.

Los outcomes son `reachable`, `unreachable` y `unknown`. Los reasons distinguen
fallas de DNS, conexión, TLS, HTTP, respuesta e identidad. Son hechos de
transporte observados: Testkit nunca afirma que una falla fue causada por una
NetworkPolicy. Un Service con múltiples réplicas demuestra alcance al Service,
no a un Pod específico.

## Contrato del contenedor

- Escucha en el puerto TCP `8080` de forma predeterminada; `HTTP_PORT` puede
  sobrescribirlo en runtime.
- Se ejecuta como UID/GID `65532:65532`.
- Admite un filesystem raíz de solo lectura.
- No requiere capabilities Linux ni escalamiento de privilegios.
- Incluye un CA bundle para probes HTTPS.
- Incorpora todos los assets estáticos en el binario.
- Se compila de manera reproducible para plataformas objetivo de BuildKit,
  incluyendo `linux/amd64` y `linux/arm64`.
- Maneja `SIGTERM` y drena conexiones HTTP, SSE y WebSocket con un shutdown
  limitado.

El servidor acepta conexiones WebSocket del mismo origen y clientes que omiten el
header `Origin`, como las herramientas de línea de comandos. Las conexiones
cross-origin de navegadores son rechazadas.

## Configuración

| Variable | Valor predeterminado | Finalidad |
| --- | --- | --- |
| `HTTP_PORT` | `8080` | Puerto TCP usado por el servidor HTTP; debe ser un entero entre `1` y `65535`. |
| `SSE_INTERVAL` | `1s` | Intervalo entre eventos de estado SSE. |
| `TESTKIT_PEERS_FILE` | no definido | Configuración de solo lectura; ausente deshabilita el monitor y sus endpoints. |
| `TESTKIT_PERSISTENCE_FILE` | no definido | Ruta absoluta del marcador; ausente deshabilita el endpoint de persistencia. |

Si `HTTP_PORT` no está definido o está vacío, se usa el valor predeterminado.
Los valores inválidos hacen que el servidor falle durante el inicio. Los valores
inválidos o no positivos de `SSE_INTERVAL` usan el valor predeterminado.
Cuando la persistencia está habilitada, `PUT /api/persistence` acepta
`{"value":"..."}` con 1–4096 bytes y lo escribe de forma atómica. `GET` y `PUT`
devuelven únicamente existencia, tamaño en bytes y fingerprint SHA-256; el valor
nunca se devuelve. Monte un volumen persistente escribible en el directorio padre
del archivo al usar un filesystem raíz de solo lectura.

## Logs estructurados

El modo servidor escribe logs JSON delimitados por línea en la salida estándar.
Cada registro incluye `service` y `version`. Los principales valores de `event`
son:

- `server.started`, `server.stopped` y eventos de falla del servidor;
- `http.request.completed` para `/api/status`, `/api/items` y `/api/echo`, con
  método, ruta estable, código de estado, duración en milisegundos y bytes de
  respuesta;
- `connection.opened` y `connection.closed` para WebSocket y SSE, con las
  cantidades de conexiones activas del protocolo y totales y la duración al cerrar;
- `connections.snapshot` cada 15 minutos mientras haya al menos una conexión
  activa.
- `peer.identity.requested` para solicitudes de identidad entre pares;
- `peer.state.changed` cuando cambia el outcome, reason o `boot_id` remoto de un
  par;
- `peers.snapshot` cada 15 minutos mientras haya un par configurado.

Los eventos de ciclo de vida y snapshots incluyen `connection_sequence`, que
aumenta con cada transición de estado de conexión en el proceso. Los consumidores
pueden usarla para reconstruir el orden de las transiciones cuando los registros
concurrentes llegan fuera de orden.

Las solicitudes REST completadas y las conexiones WebSocket y SSE aceptadas
incluyen un `correlation_id` UUIDv7. Los clientes REST pueden proporcionarlo
mediante `X-Testkit-Correlation-ID`; los clientes WebSocket y SSE pueden usar el
parámetro de query `correlation_id`. Los valores ausentes o inválidos se
reemplazan por un ID generado por el servidor. Las respuestas REST devuelven el
ID efectivo en el mismo header. Los labs REST, WebSocket y SSE incluidos generan
y muestran estos IDs automáticamente. Los snapshots de conexiones permanecen
agregados y no incluyen IDs de correlación.

Las cantidades representan conexiones aceptadas observadas actualmente por un
proceso del servidor y se reinician junto con él. Las actualizaciones de página,
los cierres normales y los errores de transporte disminuyen la cantidad cuando
el servidor observa la desconexión. Los heartbeats WebSocket limitan la detección
de fallas silenciosas a cerca de 60 segundos; en SSE, la detección es best effort
cuando la ruta de red desaparece sin cerrar el stream HTTP. El apagado controlado
espera que los handlers WebSocket aceptados emitan sus hechos de cierre. Un crash
o una terminación forzada no puede emitir hechos de cierre, por lo que los
consumidores deben tratar cada inicio del servidor como una nueva época local al
proceso.

Los logs no incluyen direcciones de clientes, headers sin procesar, query strings
sin procesar, payloads de requests, mensajes WebSocket ni datos SSE. Son hechos
de prueba observables, no métricas durables o globales.

Los hechos de pares usan nombres lógicos y reasons estables. No registran hosts
configurados, IPs resueltas, configuración de proxy, cuerpos de respuesta ni
errores crudos. El estado de pares se reinicia junto con el proceso; `boot_id`
identifica la época local y `observed_boot_id` identifica la última época remota.
Las verificaciones canceladas por el shutdown no reemplazan el último estado
observado del par.

## Distribución de la imagen

Las releases versionadas publican imágenes multiplataforma en GitHub Container
Registry:

```text
ghcr.io/molejo-platform/testkit:v0.8.0
```

Los tags existen para descubrimiento. Las pruebas automatizadas deben consumir el
digest inmutable informado por el pipeline de release:

```text
ghcr.io/molejo-platform/testkit@sha256:<digest>
```

El proyecto no publica un tag `latest`. La firma, los SBOMs y las attestations
adicionales de procedencia quedan fuera del contrato actual de release.

## Desarrollo

Ejecutá el gate local de calidad:

```sh
gofmt -w *.go
go test -race -cover ./...
go vet ./...
go mod tidy -diff
```

Construí y ejercitá el contenedor final siempre que cambien el runtime, los assets
incorporados, los probes o el Dockerfile.

## Documentación

El inglés es el idioma canónico de la documentación. Traducciones disponibles:

- [English](../../README.md)
- [Português (Brasil)](../pt-BR/README.md)

Las traducciones conservan en inglés los comandos, paths, endpoints, campos e
identificadores de protocolo. Si existe una divergencia, la versión en inglés
define el contrato vigente.

Las reglas de identidad visual y el origen de los assets están documentados en el
[contrato de branding de Molejo Testkit](../BRANDING.md).

## Contribuir

Leé [CONTRIBUTING.md](CONTRIBUTING.md) antes de proponer un cambio.

## Seguridad

No reportes vulnerabilidades en issues públicas. Seguí
[SECURITY.md](SECURITY.md).

## Licencia

Licenciado bajo la [Apache License 2.0](../../LICENSE).
