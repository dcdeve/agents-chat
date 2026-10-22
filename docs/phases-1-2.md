# Sesiones, identidad y mensajería persistentes

La condición de fase 0 fue aprobada por el usuario el 2026-10-21. Este incremento
implementa las fases 1–2; no implementa todavía el dominio de tareas o liderazgo.

## Arquitectura y almacenamiento

- `domain`: contrato y tipos compartidos; el transporte no decide autorización.
- `application`: operaciones, identidad, estados, versiones, idempotencia y espera.
- `storage`: migración de esquema 1, SQLite y exclusión del daemon escritor.
- `transport`: HTTP por socket Unix, puente MCP del SDK oficial y cliente común.
- `cmd/collab`: CLI humana y arranque del daemon; no lanza agentes.
- `probe`: modelo volátil original, conservado para sus pruebas de fase 0.

`collab.sqlite` contiene `sessions`, `agents`, `credentials`, `messages`,
`message_receipts`, `events` e `idempotency`. Se habilitan claves foráneas, WAL y
`synchronous=FULL`; las migraciones usan una transacción y rechazan esquemas de
una versión futura. El directorio debe ser 0700 y la base/archivo de exclusión
0600; no se siguen symlinks en esos archivos.

Cada operación guarda sus cambios, eventos y resultado de idempotencia en la
misma transacción. Se responde sólo después del commit. La notificación de nuevos
mensajes ocurre después de persistir; una espera nunca mantiene abierta una
transacción. Las claves de idempotencia tienen ámbito sesión + autor autenticado.
La clave repetida devuelve la respuesta original, incluso después de reiniciar;
otro contenido produce `idempotency_conflict`. `request_id` es por intento y no
participa del hash lógico.

Los eventos tienen secuencia por sesión, UUID y versión de esquema; no pueden
actualizarse o borrarse mediante SQL ordinario. No contienen tokens ni hashes de
credenciales. El daemon usa una identidad `system` sin posibilidad de autenticación
para observaciones automáticas. La cuenta `human` autoriza operaciones administrativas.

El lock del escritor permanece abierto durante toda la ejecución y nunca se
elimina. Otro daemon sobre la misma base se rechaza antes de tocar el socket.
Tras una caída, sólo se elimina un socket Unix que rechaza conexiones, bajo ese
lock. Un archivo normal, symlink o listener activo no se reemplaza.

## Ciclo de sesión

`session create` prepara archivos de configuración y credenciales; el primer
`serve` crea la sesión en SQLite. La base pasa a ser la autoridad y posteriores
arranques no recargan altas o revocaciones desde `server.json`.

`prepared → active → closed`; también puede cerrarse una sesión preparada.
Iniciar exige al menos tres identidades vigentes. Al iniciar se congela la
membresía; las revocaciones siguen permitidas. No se reabre una sesión cerrada.
Una reconexión consulta la sesión existente y conserva el UUID de la identidad.

`session show`, `agent list` y `agent reconnect` son consultas. `session start`,
`session close`, `agent register` y `agent revoke` requieren credencial humana,
`idempotency_key` y `expected_version`. La versión cambia por estado o membresía,
no por cada mensaje o consulta.

Para agregar un participante a una sesión preparada:

```sh
./bin/collab agent register --config /tmp/ac-session/admin.json \
  --output /tmp/ac-session/reviewer.json \
  --json '{"idempotency_key":"register-reviewer","expected_version":1,"participant":{"name":"reviewer","product":"Claude Code","version":"VERSION_REAL","official_url":"https://code.claude.com/docs/en/mcp","configuration":"CONFIGURACION_REAL"}}'
```

La CLI genera una credencial aleatoria y un UUID en `--output` **antes** de llamar
al daemon. Si la llamada falla, el archivo queda disponible para repetir el mismo
comando: no genera otra credencial. El daemon recibe sólo su hash. El archivo no
significa que el registro haya sido aceptado; comprobar la respuesta de la CLI.
Guardar ese archivo en un directorio privado y no compartirlo entre participantes.

```sh
./bin/collab agent revoke --config /tmp/ac-session/admin.json \
  --json '{"idempotency_key":"revoke-reviewer","expected_version":2,"agent_id":"UUID"}'
```

Revocar invalida llamadas nuevas y esperas abiertas, sin borrar el historial ni
mensajes existentes. No hay rotación/reemisión de credenciales ni reincorporación
de miembros en esta versión. Nunca se permite revocar la identidad humana por API.

## Protocolo y entrega

Cada request HTTP lleva `protocol_version`, `session_id`, `request_id`, operación
y parámetros. Las mutaciones agregan `idempotency_key`. El bridge usa versión 1;
se acepta versión 0 para continuidad del experimento anterior. La credencial va
en el encabezado de autorización; el autor se deriva en el servidor.

Operaciones MCP: `get_session_state`, `list_agents`, `reconnect_agent`,
`send_message`, `check_inbox`, `wait_for_messages`, `acknowledge_message`,
`set_activity` y `timeline`. Las operaciones administrativas sólo se exponen en
la CLI/API con autorización humana, no en la lista MCP del agente.

Mensajes de hasta 16 KiB, directos a UUID, a `human` o broadcast `*`. El broadcast
materializa los participantes vigentes al enviar y no incluye la identidad humana.
Lectura al menos una vez: los mensajes reaparecen hasta un ACK del destinatario.
Los recibos registran primera entrega y ACK por separado. Un ACK duplicado no
duplica el hecho. ACK y cumplimiento de trabajo siguen siendo conceptos distintos.

`after` pagina por secuencia; no confirma mensajes. En la siguiente consulta de
pendientes omitir `after` para recuperar también mensajes anteriores sin ACK.
`limit` admite 1–500 (por defecto 100). La respuesta incluye `has_more` y
`next_cursor`. Las esperas van de 1 a 25000 ms (0 usa 25000), se cancelan con la
conexión y despiertan al enviar, cerrar o revocar la identidad.

```sh
./bin/collab timeline --config /tmp/ac-session/admin.json \
  --json '{"after":0,"limit":100,"type_filter":"message"}'
./bin/collab export --config /tmp/ac-session/admin.json > /tmp/ac-export.json
```

El timeline admite filtros por tipo y autor. Cualquier participante vigente puede
consultar los eventos de su sesión; observar eventos no confirma mensajes ajenos.
`export` produce una instantánea completa; se recomienda `timeline` paginado para
registros grandes. El log registra el protocolo, no las conversaciones privadas.

Errores implementados incluyen `unauthorized`, `not_found`, `invalid_request`,
`invalid_transition`, `version_conflict`, `idempotency_conflict`, `session_closed`,
`session_not_active`, `membership_frozen`, `insufficient_members`, `already_exists`,
`invalid_participant`, `invalid_timeout`, `invalid_body`, `invalid_activity`,
`cancelled` y `storage_error`. Este último no declara éxito; reintentar con la
misma clave resuelve también una respuesta perdida tras el commit.

## Presencia y verificación

Las llamadas autenticadas aceptadas actualizan `last_agent_seen` y un lease de
90 segundos. `available`, `working` y `waiting` son actividad declarada; `recent`
y `unreachable` son salud observada. Tener un puente conectado no renueva el lease.
La expiración genera una sola observación por período de ausencia; no prueba el
cierre de una terminal ni reasigna trabajo. Una llamada posterior restablece salud.

Pruebas: reinicio preservando mensajes, ACK y respuestas idempotentes; fallo de
escritura a mitad de operación con rollback completo; concurrencia, autorización,
revocación persistente, versiones, paginación, cancelación, lease y exclusión de
escritores. Las pruebas del binario matan el daemon con `SIGKILL`, mantienen el
puente MCP abierto y comprueban recuperación después de reiniciar.

Pendiente: reconstrucción de proyecciones a partir del log, pruebas reales de disco
lleno y de fallos de alimentación, retención/backup, múltiples sesiones en un solo
daemon, y fases 3–6 (tareas, revisión, Git, liderazgo y evaluación de mejora).
El fallo de escritura inyectado comprueba atomicidad; no certifica hardware ni
reemplaza una prueba de disco lleno real. El esquema incorpora relaciones de sesión
en todas las claves, pero la CLI usa un directorio y daemon por sesión por ahora.

Verificación de este incremento en macOS arm64: `go test -race ./...`, `go vet
./...`, ocho pruebas Python, compilación del binario y validación del skill
aprobados. También se compiló para Linux amd64 con `CGO_ENABLED=0`; no se ejecutó
ese binario en un host Linux.

Referencias: [driver SQLite puro Go](https://pkg.go.dev/modernc.org/sqlite) fijado
en v1.61.0 y [pragmas de SQLite](https://www.sqlite.org/pragma.html).
