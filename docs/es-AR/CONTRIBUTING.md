# Contribuir a Molejo Testkit

[English](../../CONTRIBUTING.md) |
[Português (Brasil)](../pt-BR/CONTRIBUTING.md)

Gracias por ayudar a mejorar Molejo Testkit. El proyecto es experimental y tiene
releases de pre-lanzamiento, por lo que los cambios deben ser pequeños,
reproducibles y directamente relacionados con una necesidad de prueba.

## Antes de comenzar

- Revisá las issues y pull requests existentes antes de duplicar trabajo.
- Abrí una issue para cambios que introduzcan un protocolo, dependencia, endpoint
  público o comportamiento incompatible.
- Reportá sospechas de vulnerabilidad de forma privada según
  [SECURITY.md](SECURITY.md).

## Entorno de desarrollo

Necesitás:

- Go 1.26.x;
- Docker con Buildx para validar el contenedor;
- Git y herramientas comunes de línea de comandos.

Cloná el repositorio y ejecutá las pruebas base antes de modificarlo:

```sh
git clone https://github.com/molejo-platform/testkit.git
cd testkit
go test -race -cover ./...
go vet ./...
go mod tidy -diff
```

## Realizar cambios

- Mantené la implementación determinista y adecuada para pruebas descartables.
- Evitá abstracciones o dependencias sin un requisito de prueba demostrado.
- Tratá endpoints, payloads, códigos de salida y comportamiento del contenedor
  como contratos.
- Mantené los probes puntuales como comandos explícitos y los pares periódicos en
  una allowlist de solo lectura. Nunca permitas que una solicitud HTTP pública
  seleccione o active un destino de egreso.
- Conservá el runtime restringido: no root, compatible con filesystem de solo
  lectura, sin capabilities obligatorias y con shutdown limitado.
- Agregá una prueba de regresión antes de corregir un defecto.
- Actualizá la documentación en inglés y las traducciones correspondientes cuando
  cambie el comportamiento.
- Usá el logo oficial y los tokens semánticos de color descritos en
  [docs/BRANDING.md](../BRANDING.md) para cambios en la interfaz del navegador.

Formateá y verificá el código:

```sh
gofmt -w *.go
go test -race -cover ./...
go vet ./...
go mod tidy -diff
```

Cuando cambie el contrato del contenedor, también construí y ejecutá la imagen con
los parámetros restringidos documentados en [README.md](README.md).

## Mensajes de commit

Usá [Conventional Commits](https://www.conventionalcommits.org/) con un resumen en
inglés. Agregá un cuerpo cuando el commit modifique más de tres archivos o cuando
el motivo del cambio no resulte evidente en el resumen.

Ejemplos:

```text
feat(rest): add deterministic headers endpoint
fix(websocket): close clients during shutdown
docs: document Kubernetes probe usage
```

## Pull requests

Un pull request debe:

- explicar el problema de prueba que resuelve;
- describir el cambio observable del contrato;
- incluir cobertura de regresión;
- enumerar los comandos usados para la validación;
- evitar limpiezas no relacionadas o ruido generado;
- actualizar la documentación cuando corresponda.

Los mantenedores pueden solicitar un cambio más pequeño si se acoplan
comportamientos no relacionados.

## Licencia de la contribución

Al contribuir, aceptás que tus contribuciones se licencien bajo la
[Apache License 2.0](../../LICENSE), según la sección 5 de la licencia.
