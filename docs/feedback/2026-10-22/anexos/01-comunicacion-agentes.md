# Comunicación con los agentes (registro de la sesión)

Sesión `ce4cdf75-8857-422f-9956-d1f8b8f0361e` · fecha 2026-10-22 · coordinador `claude-lead` (término 1)

| Participante | Producto / modelo | UUID |
|---|---|---|
| claude-lead | Claude Code 2.1.306 (Opus 5.5) | b9cf2d60-c500-4a5e-99b4-db79ec87f6c4 |
| worker-a | Antigravity CLI 1.4.4 · gemini-4-argon-high | d0e30ea3-f340-4cef-8304-0b3988f9811f |
| worker-b | Antigravity CLI 1.4.4 · claude-sonnet-5-5-high | 482d8df2-1ee9-4e2b-8c30-7d4423b4a97a |

## Cronología (UTC, del `timeline` del servidor)

| Hora | Evento |
|---|---|
| 03:34 | claude-lead se conecta; workers `unreachable` (aún sin llamadas autenticadas). |
| 03:35 | Los tres pasan a `recent`; todos en `waiting`. Varias esperas vacías de 25 s sin incidencias. |
| ~03:37 | El usuario cambia la consigna: revisar agents-chat con apoyo de los workers. |
| 03:38 | `create_task` ×2 OK (estado `proposed`, v1). |
| 03:38 | **`assign_task` denegado por el clasificador de modo automático de Claude Code** ("External System Writes" / "Unrequested Commit in a Connected App"). Las tareas quedan creadas sin asignar. |
| 03:38 | Tras autorización explícita del usuario, `assign_task` ×2 OK → v2, generación 1. |
| 03:38:52 | `send_message` a cada worker con instrucciones (seq 78, 80). |
| 03:39:15 | worker-a recibe en un `check_inbox` mi mensaje **y otro ajeno** (`1c6b9b29…`), y hace ACK del ajeno primero. |
| 03:39:20 | worker-b llama a `get_task`. |
| 03:39:29 | worker-b `accept_task` → v3 `accepted`; `set_activity working`. |
| 03:39:36 | worker-a `set_activity working` **sin haber aceptado aún la tarea**. |
| 03:39:55 | worker-a hace ACK de mi mensaje. |
| ~03:40 | worker-a `accept_task` (v3). |
| ~03:41 | worker-a responde por mensaje (seq 116): confirma la aceptación y que puede leer el repo. |
| 03:42:47 | `system` emite `agent_unreachable` para worker-a (lease de 90 s vencido): **estaba trabajando en local** (tests/CLI) sin llamar al MCP. Vuelve a las 03:43:29. |
| 03:43:53 | worker-b hace ACK de un mensaje ajeno (`39a9eb84…`); mi mensaje `35f343f7…` sigue sin ACK aunque ya aceptó la tarea. |

## Observaciones

- **Funciona:** asignación, entrega, ACK, aceptación y mensajes de respuesta en ambos sentidos. Latencia de reacción de los workers: ~20–40 s (ciclo de espera de 25 s).
- **Fricción de permisos en el coordinador:** Claude Code en modo automático trata `assign_task` como escritura externa no solicitada. Para pruebas reproducibles conviene una regla de permisos explícita para `mcp__collab__*` en `.claude/settings.json`.
- **ACK ≠ aceptación:** worker-a se puso `working` antes de `accept_task`. El servidor no lo impide; el coordinador debe comprobar `state=accepted` antes de dar el trabajo por arrancado.
- **Corrección:** los mensajes "ajenos" (`1c6b9b29…` a worker-a, `39a9eb84…` a worker-b) eran **notificaciones automáticas de `assign_task`** ("Task … assigned to you…", tasks.go). Mi mensaje manual era redundante. Esto no está documentado en la descripción de la herramienta. Mejora: decirlo en la descripción de `assign_task` y que la notificación incluya `task_version`/`generation`, para que el worker pueda aceptar sin llamar a `get_task`.
- **Revisión y cierre (03:46–03:50):** worker-b publicó el artefacto `b55d533d` (digest verificado), hizo `request_review` (que también genera un mensaje automático al coordinador) y mandó un resumen. claude-lead revisó, aprobó (la tarea **sigue** `review_requested`, v7) y la completó con `update_task completed`. El ciclo funcionó de punta a punta, pero exige contar versiones a mano (+1 tras cada operación).
- **worker-b aceptó sin responder por mensaje**: el skill pide ACK, no respuesta; si el coordinador quiere confirmación explícita, debe decirlo o mirar el timeline.
- **Falso `unreachable` durante trabajo real:** el lease solo se renueva con llamadas MCP. Un agente que corre tests 90 s parece caído. Mejora: un heartbeat ligero, o que `set_activity working` acepte un lease más largo / ETA.
- **ACK incompleto:** worker-b aceptó la tarea sin hacer ACK del mensaje que se la anunciaba. Los mensajes sin ACK se reentregan en cada espera, lo que añade ruido a su buzón.
- **`timeline` es la herramienta más útil del coordinador** para ver qué hacen los demás (llamadas, ACK, actividad) sin molestarles.

## Cierre de la tarea de worker-a (03:49–03:52)

- 03:49:25 worker-a publica el artefacto `5d11f9e8` (incluye `commit_sha` del repo inspeccionado), hace `request_review` y envía un resumen extenso por mensaje.
- claude-lead verifica el digest, reproduce el bug de `--help`, aprueba (revisión `b1d9253c`) y completa la tarea (v8).
- Duración total desde la asignación: ~11 min (worker-b: ~8 min).

## Resultado

Las 2 tareas se completaron con revisión independiente. Informe consolidado en `00-informe-final.md`.

