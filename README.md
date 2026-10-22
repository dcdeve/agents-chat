# agents-chat

Herramienta local Go para comunicar CLI iniciadas manualmente mediante MCP.
No inicia ni supervisa agentes. La condición de avance de fase 0 fue aprobada
por el usuario el 2026-10-21. Están implementados el núcleo persistente y la
mensajería de fases 1–2, tareas/revisión y liderazgo de fase 3/5, y el flujo
principal de worktrees e integración de fase 4. La continuidad real de clientes
y la mejora frente a un agente siguen sin certificarse.

Incluye un daemon sobre socket Unix, puente MCP `stdio` con el SDK oficial fijado
en v1.8.0, identidades preparadas, mensajes directos/broadcast, ACK, idempotencia,
espera cancelable de hasta 25 segundos y exportación del registro. SQLite guarda
eventos, estado y resultados de idempotencia en la misma transacción.
La identidad procede de una credencial del puente, nunca de parámetros del modelo.

## Compilar y verificar

Requiere macOS/Linux, Go 1.26 o posterior y Python 3.9 o posterior:

```sh
make build
make test
```

El test de transporte requiere permiso para crear sockets Unix locales. Los
clientes del test son simulados: prueban el transporte, no continuidad de modelos.
El binario ofrece `init`, `serve`, `mcp`, `call`, `session`, `agent`, `workspace`,
`timeline` y `export`; `--help` muestra sus opciones. Las pruebas incluyen caída con
`SIGKILL`, reinicio con el mismo puente, fallo de escritura y concurrencia.

## Crear y arrancar una sesión persistente

```sh
./bin/collab session create --dir /tmp/ac-session \
  --participants examples/participants.json --objective 'Investigar el bug' \
  --repo /ruta/al/repositorio
./bin/collab serve --dir /tmp/ac-session
```

En otra terminal humana:

```sh
./bin/collab session show --config /tmp/ac-session/admin.json
./bin/collab session start --config /tmp/ac-session/admin.json \
  --json '{"idempotency_key":"start-1","expected_version":1}'
```

La sesión nace `prepared`, admite altas de participantes y requiere al menos tres
identidades vigentes para pasar a `active`. La membresía se congela al iniciar.
`expected_version` debe coincidir con `version` de la sesión; consultar de nuevo
si otra operación administrativa cambió esa versión. `init` conserva el atajo de
la prueba de continuidad: prepara los participantes y arranca la sesión activa.

Si el daemon se cierra o cae, repetir `serve --dir /tmp/ac-session`. Recupera
mensajes pendientes, ACK, credenciales revocadas, versiones e idempotencia.
La exclusión por archivo impide dos escritores y permite retirar el socket
obsoleto tras una caída. No hace falta reiniciar el puente MCP.
El servidor rechaza reutilizar una ejecución antigua de fase 0 con `started`
y sin base: aquel estado volátil no puede recuperarse.

Ver [operaciones y garantías de fases 1–2](docs/phases-1-2.md) para altas,
revocación, paginación, presencia y formato del protocolo.

Ver [tareas, liderazgo y worktrees](docs/phases-3-5.md) para el ciclo de revisión,
preparación de workspaces Git, integración exclusiva y recuperación de conflictos.

## Preparar una prueba real

1. Copiar `examples/participants.json` y reemplazar la configuración de cada
   participante por modelo, permisos, timeout efectivo, sandbox, skill y adaptador
   exactos. Verificar las versiones con `codex --version` / `claude --version`.
   Los valores del ejemplo sólo reflejan los binarios encontrados al implementar.
2. Usar un directorio nuevo y corto para cada ejecución; no debe existir:

   ```sh
   ./bin/collab init --dir /tmp/ac-run1 --participants examples/participants.json
   ./bin/collab serve --dir /tmp/ac-run1
   ```

   `init` imprime la correspondencia entre nombres, UUID y archivos `agent-N.json`.
   Guarda credenciales 0600 dentro del directorio 0700. `admin.json` es sólo humano.
3. Configurar un puente por terminal con **rutas absolutas** al binario y al
   archivo de identidad correspondiente. No compartir una identidad entre CLI.
   Cada entrada MCP ejecuta:

   ```text
   /ruta/agents-chat/bin/collab mcp --config /tmp/ac-run1/agent-1.json
   ```

   Para Codex, configuración por lanzamiento (terminal 1; usar `agent-3.json`
   en la tercera terminal):

   ```sh
   codex -c 'mcp_servers.collab.command="/ruta/agents-chat/bin/collab"' \
     -c 'mcp_servers.collab.args=["mcp","--config","/tmp/ac-run1/agent-1.json"]'
   ```

   Para Claude Code, guardar este JSON en un archivo local y abrir manualmente
   `claude --mcp-config /ruta/claude-probe.json` en la segunda terminal:

   ```json
   {"mcpServers":{"collab":{"command":"/ruta/agents-chat/bin/collab","args":["mcp","--config","/tmp/ac-run1/agent-2.json"]}}}
   ```

   Consultar la configuración admitida por la versión instalada. El timeout
   efectivo debe superar 25 segundos; si es menor, reducir `timeout_ms` en el
   ciclo del skill y registrar el cambio. Resolver los permisos iniciales antes
   de medir. No desactivar globalmente las protecciones del cliente.
4. En cada CLI, dar **una sola instrucción inicial**:

   > Lee `/ruta/agents-chat/skills/collab-loop/SKILL.md` y participa en la prueba
   > usando el servidor MCP collab hasta que la sesión cierre. Procesa los
   > desafíos y vuelve a esperar incluso tras una espera vacía.

5. Cuando las tres estén esperando, ejecutar en una terminal humana:

   ```sh
   python3 scripts/continuity_probe.py --config /tmp/ac-run1/admin.json \
     --output /tmp/ac-evidence1
   ```

   Durante 3600 segundos envía diez rondas de trabajo separadas por períodos de
   espera. No inicia CLI, no simula respuestas ni reanuda turnos. Verifica
   respuestas, ACK y vuelta a espera por participante; también exige timeouts
   vacíos y no más de 90 segundos entre llamadas para este trabajo trivial.
   Cierra la sesión y exporta eventos y resultados al terminar. Una ejecución
   corta con `--duration 30 --rounds 2` sirve para depurar y **nunca aprueba**.
6. Adjuntar evidencia de las terminales y completar la declaración humana de
   intervenciones en un documento separado; no editar los eventos originales.
   `pending_human_attestation` no certifica clientes. Un turno finalizado que
   requiere intervención falla la prueba; registrar producto y configuración.

## Inspección manual

```sh
./bin/collab call --config /tmp/ac-run1/admin.json --op get_session_state
./bin/collab call --config /tmp/ac-run1/admin.json --op send_message \
  --json '{"idempotency_key":"hello-1","to":"*","body":"Mensaje de prueba"}'
./bin/collab export --config /tmp/ac-run1/admin.json > /tmp/ac-events.json
./bin/collab call --config /tmp/ac-run1/admin.json --op close_session \
  --json '{"idempotency_key":"close-manual","expected_version":1}'
```

Cada mutación requiere una clave estable: repetir contenido devuelve el resultado
original y cambiar contenido con la misma clave devuelve `idempotency_conflict`.
Un ACK ajeno se rechaza. Leer el registro no confirma mensajes. `*` resuelve los
participantes al enviar. Los mensajes a `human` son separados del broadcast.
`last_agent_seen` cambia sólo por llamadas autenticadas, no por tener un puente
abierto. El lease de 90 segundos genera un evento de sospecha al vencer; no
implica que la terminal se haya cerrado ni reasigna trabajo.

## Límites y condición de avance

El estado del servicio está en SQLite. `internal/probe` conserva exclusivamente el
modelo volátil de las pruebas originales. Todavía no se implementaron tareas,
artefactos, revisiones, votos, worktrees, integración Git ni adaptadores de
reactivación. Tampoco se ejecutó una prueba real de modelos en Linux.

Un bridge puede seguir conectado con el agente inactivo. Un ACK sólo prueba
recepción declarada. El registro no captura conversaciones privadas, comandos
externos ni intervención humana: necesita evidencia complementaria. Las
credenciales separan identidades lógicas, no procesos del mismo usuario del SO.
La prueba usa mensajes pequeños. El buzón y el timeline tienen paginación; una
exportación completa carga el registro en memoria. La retención del historial es
indefinida y aún no hay reconstrucción de proyecciones desde los eventos.

Para nuevas configuraciones que fallen la prueba, documentar el fallo y evaluar
un adaptador oficial de hooks o canales. La aprobación recibida no sustituye
resultados por cliente que no se hayan observado. Ver [registro de aprobación](docs/phase-0.md).

## Fuentes de integración

- [SDK oficial MCP Go](https://github.com/modelcontextprotocol/go-sdk), usado con
  una versión fijada y `go.sum`.
- [MCP en Codex](https://learn.chatgpt.com/docs/extend/mcp?surface=cli).
- [MCP en Claude Code](https://code.claude.com/docs/en/mcp).
- [Canales oficiales de Claude Code](https://code.claude.com/docs/en/channels),
  alternativa pendiente de evaluar si el ciclo termina.

La documentación de conexión no demuestra continuidad de ejecución.
