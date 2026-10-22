# Preparación del entorno, permisos y pendientes para la próxima iteración

Escrito desde la terminal humana de soporte (no por un participante de la sesión), a partir de la preparación de la sesión `/tmp/ac-run1` y del `timeline` del servidor.

## Cómo se montó esta iteración

| Pieza | Decisión | Motivo |
|---|---|---|
| Participantes | 1 Claude Code + 2 Antigravity CLI | Sólo hay dos productos disponibles; el servidor exige ≥3 participantes y la prueba de continuidad ≥2 productos distintos. |
| Coordinador | `claude-lead` primero en `participants.json` | El primer registrado es el coordinador (ver S1 en 02); el nombre también es el primero alfabéticamente, que es el criterio de la migración de `sqlite.go`. |
| Modelos de los workers | `gemini-4-argon-high` y `claude-sonnet-5-5-high` | Gastar presupuesto de los dos proveedores. Contra: worker-b y el coordinador son de la misma familia, lo que reduce la independencia de las revisiones. |
| Identidad MCP de Claude | `--mcp-config claude-mcp.json --strict-mcp-config`, `MCP_TOOL_TIMEOUT=60000` | Configuración por lanzamiento, sin tocar la configuración global. |
| Identidad MCP de agy | `.agents/mcp_config.json` en una carpeta de arranque por instancia (`/tmp/ac-launch/worker-a`, `worker-b`); el global `~/.gemini/config/mcp_config.json` vacío | `agy mcp add` escribe en el global y las dos instancias compartirían identidad. Se verificó por proceso que cada puente usa su `agent-N.json`. |
| Skill | Copiado a `.claude/skills/collab-loop/` y `.agents/skills/collab-loop/` en cada carpeta de arranque | Rutas de proyecto de cada cliente. Una copia queda desactualizada si cambia `skills/collab-loop/SKILL.md`. |
| Instrucción inicial | Distinta para el coordinador y los workers | Sin objetivo de sesión, el coordinador tiende a inventar trabajo. A Claude se le dijo que no crease tareas durante la prueba de continuidad. |
| Go | Homebrew (`/opt/homebrew/bin/go`, 1.27.3) | Había una copia en `/private/tmp/agents-chat-toolchain` fuera del PATH; `/tmp` se borra al reiniciar. |

## Incidencias observadas

1. **Los dos agy se detuvieron tras la primera espera vacía.** Cada worker hizo un ciclo completo (estado, buzón, `waiting`, `wait_for_messages`), recibió `wait_timeout` (seq 37 y 38) y no volvió a llamar. El servidor emitió `agent_unreachable` para ambos (seq 47 y 48). claude-lead siguió en bucle sin problema. **Causa sin confirmar:** puede ser un permiso pendiente o que el cliente dio por terminado el turno. Los workers volvieron a llamar (seq 58+) después de un cambio de instrucción, así que esta ejecución **no sirve como evidencia de continuidad**.
2. **Clasificador de modo automático de Claude Code** bloqueó `assign_task` (ver 01). Cada cliente tiene su propia fricción de permisos, y cualquier bloqueo detiene el bucle.
3. **Corrección a 01:** los mensajes "ajenos" que worker-a (`1c6b9b29…`) y worker-b (`39a9eb84…`) confirmaron primero son las **notificaciones automáticas de asignación** que el servidor envía en nombre del coordinador al ejecutar `assign_task` (seq 71 y 75: "Task … assigned to you"), no mensajes de terceros. Por cada asignación el worker recibe dos mensajes: la notificación y el saludo manual. Conviene que el coordinador no duplique la notificación o que el skill lo explique.

## Permisos: propuesta para la próxima iteración

El objetivo es que ningún participante quede bloqueado esperando aprobación, sin dar a un worker capacidad ilimitada ante mensajes de otros agentes, que el skill trata como no confiables.

| Opción | Alcance | Recomendación |
|---|---|---|
| 1. Aprobación permanente sólo de `mcp__collab__*` | Claude: regla `allow` en `.claude/settings.json` de la carpeta de arranque. agy: "allow always" en el diálogo, si existe. | **Primera opción.** Elimina el bloqueo del bucle sin abrir nada más. |
| 2. `agy --mode auto` | Sin verificar qué aprueba exactamente. | Leer la documentación antes de usarlo. |
| 3. `agy --dangerously-skip-permissions --sandbox` | Todo auto-aprobado, terminal restringida. | Aceptable **sólo para la prueba de continuidad** (carpeta en `/tmp`, sin repositorio, desafíos triviales). |
| 4. Todos los permisos y sin sandbox | Acceso completo a la máquina. | **No recomendado.** Contradice el README ("No desactivar globalmente las protecciones del cliente"). Un mensaje del protocolo podría inducir borrados, push o lectura de credenciales. Si se adopta, actualizar README y skill y documentar el riesgo. |

Cualquier cambio de permisos debe quedar en el campo `configuration` de `participants.json`: la prueba exige la configuración exacta.

## Pendientes

- [ ] Diagnosticar por qué se detuvieron los agy: mirar la terminal antes de reactivar, y si es fin de turno, documentarlo como limitación del cliente.
- [ ] Aplicar la opción 1 de permisos y repetir la prueba de continuidad corta en una sesión nueva (`/tmp/ac-run2`) con la instrucción del README, sin cambiarla a mitad de camino.
- [ ] Separar sesiones: una para la prueba de continuidad (sin objetivo) y otra para trabajo real (`--objective`, `--repo`, un worktree por agente).
- [ ] Generar la preparación con un script (participants, carpetas de arranque, configuraciones MCP, skill) en lugar de a mano.
- [ ] Valorar un mínimo de participantes configurable para pruebas con 2 agentes, sin que cuente para la certificación.
- [ ] Medir el timeout efectivo de las llamadas MCP en agy (no documentado).
