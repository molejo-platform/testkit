# Molejo Testkit

[English](../../README.md) |
[Português (Brasil)](../pt-BR/README.md)

> Proyecto experimental en pre-release. Molejo Testkit es un fixture de pruebas
> descartable, no una aplicación de producción.

Molejo Testkit es una imagen de contenedor pequeña para comprobar alcance de red,
entrega de aplicaciones y contratos mínimos de transporte. Ejecutala junto al
sistema que querés inspeccionar, activá una de sus interfaces fijas y validá la
respuesta observable.

Un único binario Go no root ofrece health checks, REST, GraphQL, Server-Sent
Events (SSE), WebSocket, verificación de volumen persistente, monitoreo de peers
configurados, diagnósticos PostgreSQL limitados y un probe HTTP/HTTPS explícito.

## Elegí la imagen

Las imágenes publicadas usan este repositorio:

```text
ghcr.io/molejo-platform/testkit
```

Definí la imagen una vez antes de seguir los ejemplos:

```sh
export TESTKIT_IMAGE=ghcr.io/molejo-platform/testkit:v0.11.0
docker pull "$TESTKIT_IMAGE"
```

`v0.11.0` es la imagen estable más reciente publicada al escribir esta guía. Una
branch de release puede documentar capacidades todavía no presentes en una
imagen correspondiente. En ambientes automatizados, reemplazá el tag por el
digest inmutable publicado por la release:

```sh
export TESTKIT_IMAGE=ghcr.io/molejo-platform/testkit@sha256:PEGA_EL_DIGEST_PUBLICADO_AQUI
```

El proyecto no publica el tag `latest`. No asumas que la branch predeterminada,
este README y un tag antiguo exponen las mismas capacidades.

## Inicio rápido: transporte de entrada

### 1. Iniciá el contenedor

La configuración predeterminada habilita smokes de transporte en el puerto
`8080` y no necesita archivos de configuración:

```sh
docker run --rm \
  --name molejo-testkit \
  --publish 8080:8080 \
  --read-only \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  "$TESTKIT_IMAGE"
```

Resultado esperado: el proceso registra `server.started` y continúa ejecutando
como UID/GID `65532:65532`.

### 2. Verificá liveness y readiness

En otra terminal:

```sh
curl --fail http://localhost:8080/healthz
curl --fail http://localhost:8080/readyz
curl --fail http://localhost:8080/api/status
```

La respuesta de estado incluye la versión exacta incorporada en la imagen:

```json
{"status":"ok","version":"v0.11.0"}
```

### 3. Abrí la consola en el navegador

Abrí `http://localhost:8080/`. Testkit detecta el idioma del navegador y redirige
a `/en/`, `/pt-BR/` o `/es-AR/`. Las páginas REST, GraphQL, SSE y WebSocket
ejecutan escenarios guiados y limitados y muestran solicitud y respuesta.

### 4. Detenelo

Presioná `Ctrl+C` en la terminal del contenedor. Testkit maneja `SIGTERM`, deja de
aceptar trabajo, cierra recursos retenidos y drena HTTP y WebSocket dentro del
límite de shutdown.

## Qué comprueba un smoke exitoso

Testkit responde una pregunta enfocada: ¿este cliente alcanza este workload
descartable o dependencia configurada explícitamente, y se completa el intercambio
mínimo seleccionado?

| Superficie | Qué comprueba el éxito |
| --- | --- |
| `/healthz`, `/readyz` | El camino HTTP alcanza el proceso Testkit activo. |
| REST/GraphQL | Solicitud, respuesta, headers y un intercambio limitado se completan. |
| SSE | Una respuesta HTTP en streaming entrega eventos nombrados. |
| WebSocket | Upgrade, frames bidireccionales, broadcast y cierre funcionan. |
| `probe URL` | Una solicitud HTTP/HTTPS de salida retorna `2xx`. |
| `/api/peers` | Una identidad Testkit configurada fue alcanzada por este proceso. |
| `/api/persistence` | El path configurado escribe y lee un marcador limitado. |
| Laboratorio PostgreSQL | Un destino autorizado acepta una operación fija de conexión o catálogo. |

El éxito no valida reglas de negocio, autenticación de producción, autorización,
salas, schemas de la aplicación, rendimiento, carga, alta disponibilidad, backup
ni dependencias no relacionadas.

## Interfaces disponibles

| Capacidad | Interfaz |
| --- | --- |
| Consola del navegador | `GET /`, `/en/`, `/pt-BR/`, `/es-AR/` |
| Alias del navegador | `GET /rest`, `GET /graphql-lab`, `GET /sse`, `GET /websocket` |
| Laboratorios del navegador | `GET /<locale>/rest`, `/graphql-lab`, `/sse`, `/websocket` |
| Liveness y readiness | `GET /healthz`, `GET /readyz` |
| Respuesta intencionalmente no lista | `GET /not-ready` |
| REST | `GET /api/status`, `GET /api/items`, `POST /api/echo` |
| GraphQL | `POST /graphql` |
| SSE | `GET /events` |
| WebSocket | `GET /ws` |
| Probe explícito de salida | `testkit probe URL` |
| Identidad de peer configurado | `GET /api/identity`, `GET /api/peers` |
| Marcador persistente | `GET`, `PUT /api/persistence` |
| Capacidades PostgreSQL | `GET /api/diagnostics/postgres/capabilities` |
| Operación PostgreSQL efímera | `POST /api/diagnostics/postgres` |
| Conexión PostgreSQL retenida | `POST /api/diagnostics/postgres/connections` |

Las interfaces opcionales solo existen cuando su configuración es válida. Paths
y métodos desconocidos no cuentan como smokes exitosos.

## Verificaciones comunes de protocolo

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

WebSocket con `wscat`:

```sh
wscat --connect ws://localhost:8080/ws
> {"message":"hello"}
```

La respuesta WebSocket incluye el mensaje y la versión de la imagen. Es una
señal de transporte, no una implementación de salas o routing de la aplicación.

## Probe explícito de salida

El comando de probe está separado del modo servidor. Realiza un único `GET`
limitado sin convertir un endpoint HTTP público en un proxy genérico:

```sh
docker run --rm "$TESTKIT_IMAGE" probe https://example.com/
```

Escribe un resultado JSON y usa códigos de salida seguros para automatización:

| Código | Significado |
| --- | --- |
| `0` | El destino retornó HTTP `2xx`. |
| `1` | La solicitud de red/TLS falló o la respuesta no fue `2xx`. |
| `2` | Los argumentos o la URL son inválidos. |

Para validar NetworkPolicies de Kubernetes, ejecutá la imagen como un Job corto
en el namespace de origen, con los labels y la ServiceAccount cuya identidad de
red querés probar. Validá el código del Job y eliminalo después.

## Configuración de runtime

Todas las capacidades se configuran al iniciar. Los valores obligatorios
inválidos hacen fallar el proceso en vez de deshabilitar silenciosamente una
capacidad solicitada.

| Variable | Predeterminado | Obligatoria cuando | Propósito |
| --- | --- | --- | --- |
| `HTTP_PORT` | `8080` | Nunca | Puerto interno, de `1` a `65535`. |
| `SSE_INTERVAL` | `1s` | Nunca | Duración Go positiva entre eventos SSE. |
| `TESTKIT_SMOKES` | `transport` | Nunca | Capacidades: `transport` o `transport,postgres`. |
| `TESTKIT_PEERS_FILE` | no definida | Monitoreo de peers | Path absoluto del JSON de peers. |
| `TESTKIT_PERSISTENCE_FILE` | no definida | Marcador persistente | Path absoluto y escribible del marcador. |
| `TESTKIT_DIAGNOSTIC_TOKEN_FILE` | no definida | PostgreSQL | Path absoluto a un token read-only de al menos 16 bytes. |
| `TESTKIT_POSTGRES_DESTINATIONS_FILE` | no definida | PostgreSQL | Path absoluto a la política JSON estricta de destinos. |

Los archivos de configuración se leen desde el filesystem del contenedor.
Montá cada archivo en un path absoluto y usá el mismo path en la variable. Los
archivos de diagnóstico y peers deben ser read-only. El directorio padre de la
persistencia debe ser escribible por UID/GID `65532:65532`.

### Puerto HTTP e intervalo SSE personalizados

Configurá el mismo puerto dentro del contenedor y en el mapeo publicado:

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

## Receta: validá persistencia escribible

`TESTKIT_PERSISTENCE_FILE` habilita un pequeño contrato de marcador. Testkit
guarda como máximo 4096 bytes y retorna solo existencia, tamaño y SHA-256; nunca
devuelve el valor del marcador.

Primero creá un volumen Docker escribible por el usuario de runtime de Testkit:

```sh
docker volume create molejo-testkit-data
docker run --rm \
  --user 0:0 \
  --mount type=volume,source=molejo-testkit-data,target=/data \
  alpine:3.22 chown 65532:65532 /data
```

Después iniciá Testkit:

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

Escribí y leé los metadatos del marcador:

```sh
curl --fail --request PUT \
  --header 'Content-Type: application/json' \
  --data '{"value":"volume-smoke"}' \
  http://localhost:8080/api/persistence

curl --fail http://localhost:8080/api/persistence
```

Resultado esperado: `exists` es `true`, `size` es mayor que cero y `sha256`
permanece igual después de reiniciar Testkit con el mismo volumen.

## Receta: monitoreá peers Testkit configurados

El monitoreo de peers es una verificación periódica y read-only entre instancias
de Testkit. Cada instancia que sirve `/api/identity` necesita un archivo de peers;
una instancia que solo recibe verificaciones usa un array `peers` vacío.

Ejemplo de `/config/peers.json`:

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

Montá el archivo y definí su path absoluto en el contenedor:

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

Inspeccioná el estado sanitizado y local al proceso:

```sh
curl --fail http://localhost:8080/api/identity
curl --fail http://localhost:8080/api/peers
```

La primera verificación es inmediata. Las siguientes usan `check_interval`.
Testkit no sigue redirects ni variables de proxy HTTP; HTTPS usa el trust store
del sistema sin modo inseguro.

## Receta: ejecutá diagnósticos PostgreSQL

Los diagnósticos PostgreSQL son opt-in y requieren dos archivos: un token
operativo de la API y una allowlist de destinos. El token del deployment está
separado de la credencial de base usada en el diagnóstico.

Esta capacidad está incluida en `v0.9.0` y permanece deshabilitada hasta que el
token y la política de destinos requeridos se configuren explícitamente.

### 1. Creá los archivos de configuración locales

Usá un token descartable para este ejemplo y reemplazá el destino por el hostname
o CIDR que Testkit debe alcanzar:

```sh
mkdir -p config
printf '%s\n' 'reemplaza-con-al-menos-16-bytes' > config/diagnostic-token
printf '%s\n' \
  '{"destinations":[{"host":"db.internal.example","ports":[5432]}]}' \
  > config/postgres-destinations.json
chmod 0444 config/diagnostic-token config/postgres-destinations.json
```

No hagas commit del token. En ambientes compartidos, creá el archivo mediante el
gestor de secretos de la plataforma, no junto a los manifiestos de deployment.

### 2. Iniciá Testkit con PostgreSQL habilitado

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

Resultado esperado: `http://localhost:8080/es-AR/postgres` existe y
`/api/diagnostics/postgres/capabilities` describe campos, tipos de credencial,
modos TLS, ciclos de vida y operaciones fijas soportadas.

### 3. Ejecutá un diagnóstico

La página del navegador es el cliente más simple. Ingresá el token del
deployment, destino, identidad de base, credencial, TLS y ciclo de vida. La base
inicial es opcional; cuando se omite, PostgreSQL la selecciona con sus reglas de
inicio.

| Parte de la conexión | Contrato |
| --- | --- |
| `target` | Host obligatorio; puerto predeterminado `5432`. |
| `database` | Opcional. Omitirla delega a PostgreSQL la selección de la base inicial. |
| `identity` | Usuario de base obligatorio. |
| `credential` | Tipo obligatorio: `password`, `token` o `none`. Password/token requieren `secret`; none lo prohíbe. |
| `tls_config` | Predeterminado `verify-full`; CA privada opcional solo para TLS verificado. |
| `lifecycle` | `ephemeral` cierra tras una operación; `retained` queda en memoria hasta delete o shutdown. |

`token` es un token generado por el proveedor y enviado mediante el campo de
password de PostgreSQL; Testkit no genera credenciales IAM, OAuth o de proveedores
cloud. También se acepta una URI como alternativa a los campos estructurados,
pero ambos modos no se pueden combinar.

Para automatización, ejecutá una operación efímera directamente:

```sh
curl --fail --json '{
  "operation": "connect",
  "connection": {
    "target": {"host": "db.internal.example", "port": 5432},
    "identity": {"user": "testkit"},
    "credential": {"type": "password", "secret": "reemplaza"},
    "tls_config": {"mode": "verify-full"},
    "lifecycle": {"mode": "ephemeral"}
  }
}' \
  --header 'Authorization: Bearer reemplaza-con-al-menos-16-bytes' \
  http://localhost:8080/api/diagnostics/postgres
```

Las operaciones disponibles son `connect`, `arithmetic_check`,
`controlled_delay`, `list_databases` y `list_schemas`. No se acepta SQL
arbitrario. La operación `controlled_delay` ejecuta un `pg_sleep` controlado por
el servidor durante 500 ms fijos; valida la instrumentación de tiempos sin
trabajo sintético de CPU o E/S y ocupa una conexión durante la espera. Ejecutala
en una conexión retenida para observar el recorrido hasta la base sin abrir una
nueva conexión física.

El navegador informa la duración total y la duración del diagnóstico. La
duración total se mide en el navegador e incluye el intercambio HTTP y la
lectura de la respuesta. El campo `duration_ms` de la respuesta se mide en el
servidor alrededor del diagnóstico; para una conexión efímera, incluye la
apertura, la verificación y el cierre de la conexión a la base de datos, no solo
la operación SQL fija.

Todas las solicitudes de conexión, operación, inspección y eliminación requieren
el bearer token del deployment. El endpoint de capacidades es read-only y no lo
requiere. Un diagnóstico procesado puede retornar HTTP `200` con
`status: "failed"`; la automatización debe validar `status` y `code` en el JSON,
no solo el estado HTTP.

Usá lifecycle `retained` con
`POST /api/diagnostics/postgres/connections` para mantener una conexión física en
memoria del proceso. Ejecutá operaciones fijas en
`POST /api/diagnostics/postgres/connections/<id>/operations`, inspeccionala con
`GET /api/diagnostics/postgres/connections/<id>` y finalizá siempre con
`DELETE /api/diagnostics/postgres/connections/<id>`.

El servidor limita las conexiones retenidas a ocho y las solicitudes de
diagnóstico concurrentes a cuatro. Las operaciones tienen un plazo de 10
segundos y no reintentan. Los secretos no se retornan ni registran. Loopback se
permite solo cuando `localhost` o una IP loopback literal aparece en una regla
`host` exacta con el puerto solicitado; una regla CIDR no puede habilitarlo. Las
direcciones link-local, multicast, no especificadas y de metadata cloud siguen
bloqueadas. La política se carga una vez al iniciar; reiniciá Testkit después de
editar el archivo.

## Kubernetes: fixture mínimo de transporte

Este ejemplo despliega la imagen predeterminada solo con transporte. Fijá
`image` al digest validado por tu proceso de release:

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
          image: ghcr.io/molejo-platform/testkit:v0.11.0
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

Usá ConfigMaps para archivos no secretos de peers y política de destinos,
Secrets para el token de diagnóstico y un volumen escribible con
`fsGroup: 65532` para persistencia.

## Comportamiento operativo y de seguridad

- La imagen no requiere capabilities Linux ni elevación de privilegio y admite
  filesystem raíz read-only.
- Todos los assets del navegador están incorporados; no requieren volumen.
- Las solicitudes cross-origin se rechazan en diagnósticos sensibles.
- Las rutas HTTP públicas no eligen destinos arbitrarios de salida.
- Los destinos PostgreSQL y de peers se restringen antes de conectar.
- Los logs excluyen headers, query strings, payloads, credenciales, mensajes
  WebSocket y datos SSE crudos.
- Estados de peers, contadores, boot IDs y conexiones de base retenidas son
  locales al proceso y reinician con él.

La versión incorporada aparece en respuestas, logs, páginas y en el header
`Testkit-Version`. Usala para distinguir la imagen en ejecución de los
manifiestos o checkout que esperabas desplegar.

## Solución de problemas al iniciar

| Síntoma | Verificación |
| --- | --- |
| El proceso termina inmediatamente | Leé el log estructurado `server.configuration_failed`. |
| El puerto publicado no responde | Confirmá que puerto externo, interno y `HTTP_PORT` coincidan. |
| Un endpoint opcional retorna `404` | Confirmá variable y archivo montado al iniciar. |
| Se rechaza un path de configuración | Los paths internos deben ser absolutos. |
| El destino PostgreSQL está prohibido | Revisá hostname/CIDR, puerto, DNS y direcciones especiales. |
| Persistencia retorna `500` | Confirmá que el directorio padre sea escribible por UID/GID `65532`. |
| Un peer permanece `unknown` | Revisá DNS, timeout, instance ID esperado y archivo remoto. |

## Contribución y desarrollo

Este README es la guía del consumidor para ejecutar la imagen. El build del
código, los gates locales, los cambios de contrato y la preparación de pull
requests están documentados por separado en
[CONTRIBUTING.md](../../CONTRIBUTING.md).

Los problemas de seguridad no deben reportarse en issues públicas. Seguí
[SECURITY.md](../../SECURITY.md). La identidad visual y la procedencia de los
assets están en [docs/BRANDING.md](../BRANDING.md).

Licenciado bajo [Apache License 2.0](../../LICENSE).
