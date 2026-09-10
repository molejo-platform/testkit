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
- Node.js 22.x y npm;
- Docker con Buildx para validar el contenedor;
- Make, Git y herramientas comunes de línea de comandos.

Cloná el repositorio y ejecutá las pruebas base antes de modificarlo:

```sh
git clone https://github.com/molejo-platform/testkit.git
cd testkit
npm ci
make test-local
```

Construí una imagen del checkout actual al validar comportamiento todavía no
publicado:

```sh
docker buildx build --load --tag molejo-testkit:dev .
export TESTKIT_IMAGE=molejo-testkit:dev
```

## Realizar cambios

- Mantené la implementación determinista y adecuada para pruebas descartables.
- Evitá abstracciones o dependencias sin un requisito de prueba demostrado.
- Tratá endpoints, payloads, códigos de salida y comportamiento del contenedor
  como contratos.
- Mantené los probes puntuales como comandos explícitos y los pares periódicos en
  una allowlist de solo lectura. Los destinos elegidos en una solicitud requieren
  capacidad opt-in, autenticación, política estricta, operaciones fijas,
  ejecución limitada, logs sin secretos y regresiones de seguridad.
- Conservá el runtime restringido: no root, compatible con filesystem de solo
  lectura, sin capabilities obligatorias y con shutdown limitado.
- Agregá una prueba de regresión antes de corregir un defecto.
- Actualizá la documentación en inglés y las traducciones correspondientes cuando
  cambie el comportamiento.
- Usá el logo oficial y los tokens semánticos de color descritos en
  [docs/BRANDING.md](../BRANDING.md) para cambios en la interfaz del navegador.

Usá el gate más pequeño que compruebe el cambio:

```sh
make test-local      # formato, módulos, vet, race/cobertura unitaria y frontend
make test-browser    # contratos del navegador contra un servidor Testkit local
make test-postgres   # integración con build tag y PostgreSQL 18 descartable
make test-full       # todas las capas locales anteriores
```

`test-local` es el gate esencial y no requiere servicios externos. El runner de
integración PostgreSQL crea y elimina su contenedor Docker y falla si la suite
con build tag no tiene una DSN. Usá `test-full` antes de enviar cambios que
atraviesen backend, navegador y base de datos.

Cuando cambie el contrato del contenedor, ejecutá también la imagen local con los
parámetros restringidos documentados en [README.md](README.md). La configuración
de consumo y las variables de runtime pertenecen al README; las herramientas y
pruebas para contribuidores pertenecen a este documento.

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
