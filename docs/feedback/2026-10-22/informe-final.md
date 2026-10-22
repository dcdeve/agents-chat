# Informe final: revisión de `agents-chat`

- **Repositorio:** `/Users/gle/Documents/projects/agents-chat` @ `f4d2e18` ("experimental"), revisado en modo de solo lectura sobre copias.
- **Fecha:** 2026-10-22 · Sesión collab `ce4cdf75-8857-422f-9956-d1f8b8f0361e`
- **Equipo:** claude-lead (Claude Code, Opus 5.5, coordinador) · worker-a (Antigravity, Gemini 4 Argon) · worker-b (Antigravity, Sonnet 5.5)
- **Documentos de apoyo en `anexos/`:**
  - `01-comunicacion-agentes.md`: cronología y observaciones de la colaboración.
  - `02-hallazgos-claude-lead.md`: capa MCP, CLI y mensajería.
  - `03-auditoria-worker-b.md`: tareas, revisión, liderazgo y worktrees frente a la documentación (24 hallazgos).
  - `04-pruebas-worker-a.md`: build, tests, cobertura, prueba manual del CLI y 8 bugs reproducibles.
  - `05-preparacion-y-permisos.md`: montaje del entorno, permisos y pendientes (escrito desde la terminal humana de soporte).
  - `session-export.json`: export completo de la sesión cerrada (310 eventos, sha256 `67ca034d…3daa`). No se incluye en el repositorio; la vista HTML se genera a partir de un export con `scripts/render_timeline.py`.

---

## 1. Qué es y qué hace

`agents-chat` es un **broker local de coordinación entre CLI de agentes** (Claude Code, Codex, Antigravity…) que un humano lanza a mano. Está escrito en Go y tiene tres piezas:

1. **Daemon** (`collab serve`): servidor HTTP sobre socket Unix privado (0700/0600) con estado en **SQLite**. Eventos, estado e idempotencia se escriben en la misma transacción. Hay exclusión de escritor por `flock` y recuperación tras `SIGKILL`.
2. **Puente MCP stdio** (`collab mcp --config agent-N.json`): cada CLI lo carga como servidor MCP. La identidad sale de una **credencial por agente**, nunca de parámetros del modelo.
3. **CLI humano** (`session`, `agent`, `workspace`, `call`, `timeline`, `export`): crea y arranca sesiones, da de alta y revoca agentes, prepara worktrees Git e integra con cherry-pick.

Lo que ofrece a los agentes, como ~26 herramientas MCP:
- **Mensajería persistente:** mensajes directos y broadcast, buzón con ACK explícito (leer no confirma), espera bloqueante de hasta 25 s, paginación y timeline.
- **Presencia:** lease de 90 s renovado por llamadas autenticadas, con actividad declarada (`available`/`working`/`waiting`).
- **Tareas:** creación por el coordinador, asignación con generaciones, aceptación, estados, artefactos con digest, revisión independiente (el autor no puede aprobar su trabajo) y cierre.
- **Liderazgo:** término, propuesta con electorado congelado, aceptación del candidato, mayoría absoluta, plazo de 120 s y anulación humana.
- **Worktrees e integración:** un worktree por agente, un lock de integración exclusivo, cherry-pick solo de trabajo aprobado y recuperación humana tras conflictos.

## 2. Qué esperaba lograr quien la hizo

Del README, `docs/phase-0.md`, `docs/phases-*.md` y `RFC_REVIEW_PROMPT.md` se deduce este objetivo: **demostrar que varias CLI de distintos proveedores pueden colaborar de forma continua** (sin que el humano reactive turnos), con un protocolo auditable:

- **Continuidad:** el agente vuelve a esperar tras un timeout vacío y no "termina el turno". `scripts/continuity_probe.py` lo mide durante 1 h y 10 rondas, y es explícito en que *protocolo correcto ≠ certificación*.
- **Seguridad y trazabilidad:** identidad no falsificable por el modelo, idempotencia, ACK ≠ trabajo hecho, registro append-only y exportable.
- **Colaboración real tipo equipo** (RFC): tareas con criterios de aceptación, revisión cruzada, delegación sin cuello de botella, cambio de líder e integración Git segura.
- **Honestidad sobre el alcance:** el README declara que la continuidad real de clientes **no está certificada**, que fases 3–5 solo se probaron con clientes simulados y que faltan adaptadores, pruebas en Linux y traspaso de liderazgo con integración activa.

**Valoración:** el núcleo (persistencia, mensajería, idempotencia, auth, recuperación) es **sólido y bien probado**. Las capas de tareas, liderazgo e integración existen y funcionan en el camino feliz (esta sesión lo usó de principio a fin), pero tienen **huecos de diseño en los caminos de fallo** y la **ergonomía MCP para modelos** es la debilidad más visible.

## 3. Estado verificado

| Comprobación | Resultado | Quién |
|---|---|---|
| `go build`, `go vet ./...` | Limpio | claude-lead, worker-a |
| `go test -race ./...` | 18 tests PASS, 0 omitidos | claude-lead, worker-a |
| Python `unittest` (persistencia, stdio, sonda) | 8 tests PASS | claude-lead, worker-a |
| Cobertura | application ~72%, probe 81%, storage 69%, transport 60%, **cmd/collab 0%**, **domain 0%** (58% global con `-coverpkg`) | claude-lead, worker-a |
| CLI manual: create/start, directos y broadcast, paginación sin ACK, ACK ajeno → `not_found`, `idempotency_conflict`, `kill -9` y reinicio con buzón e idempotencia intactos, close → `session_closed`, export | Todo como documentado | worker-a |
| Ciclo real con 3 CLI: asignar → aceptar → trabajar → artefacto → revisión → aprobar → completar | **Funcionó** para 2 tareas | sesión en vivo |

## 4. Bugs y huecos (consolidado, sin duplicados)

Fuentes: **A** = worker-a, **B** = worker-b, **L** = claude-lead. Todos CONFIRMADOS (por código o ejecución) salvo que se indique.

### Severidad alta

| # | Hallazgo | Dónde | Fuente |
|---|---|---|---|
| H1 | **No se puede reasignar una tarea aceptada o en curso.** `assign_task` solo admite `proposed`/`assigned`. Si el agente cae tras aceptar, la única salida es cancelarla y recrearla; el diseño de generaciones queda inútil justo cuando hace falta. Reproducido con el CLI. | `tasks.go:141` | A, B |
| H2 | **`revoke_agent` no limpia nada:** ni asignaciones activas, ni locks de integración, ni `coordinator_id`. Revocar al coordinador deja `leadership.coordinator` apuntando a un UUID que ya no aparece en `participants`. | `service.go:345-366` | A, B |
| H3 | **No hay rechazo de tareas.** `accept_task` ignora `state`/`reason`: un agente que "rechaza" con `{"state":"rejected"}` queda registrado como `accepted` sin error. | `tasks.go:172-191` | B |
| H4 | **Solo el coordinador crea o asigna** (incluidas las subtareas), en contra del RFC 7 ("sin cuello de botella"). | `tasks.go:96,121` | B |
| H5 | **Schemas MCP genéricos:** `create/assign/accept/get/update_task` y `request_review` comparten un struct de 11 campos sin descripciones, y cada handler descarta en silencio la mayoría. Es la causa directa de H3 y confunde a cualquier modelo. Los 4 tools de liderazgo comparten otro schema sin campos obligatorios. | `transport/local.go` | L, B, A |

### Severidad media

| # | Hallazgo | Dónde | Fuente |
|---|---|---|---|
| M1 | `begin_integration` solo mira el lock: el **mismo artefacto puede integrarse dos veces**. | `workspaces.go:112-121` | B |
| M2 | `commit_sha` solo se valida como hex: no se comprueba que el commit exista ni que pertenezca al worktree del autor, y el digest no está ligado al commit. | `tasks.go:282-290` | B |
| M3 | **Notificaciones incompletas:** `submit_review`, `completed`, `failed` y `cancelled` no avisan al autor; `request_review` no avisa a nadie si el autor es el coordinador; la notificación de asignación no incluye `task_version`. | `tasks.go:167,339-396` | B, L |
| M4 | `approved` **no cambia el estado visible** (sigue `review_requested`). No existe campo `reviewer` ni tool para listar artefactos o revisiones. | `tasks.go:385` | B, L (observado en vivo) |
| M5 | Traspaso de liderazgo con integración activa → lock huérfano que solo el humano libera. Está documentado como pendiente. | `workspaces.go:78,141` | B |
| M6 | Una propuesta de liderazgo vencida bloquea nuevas propuestas hasta que alguien llama `finalize_leadership`. No se revalida al candidato antes de aprobar. | `leadership.go:71-77,169-176` | B |
| M7 | `list_tasks` **ignora `after`/`limit`**: el handler MCP descarta el input y el SQL no pagina. Devuelve el `acceptance` completo de todas las tareas. | `local.go:254`, `tasks.go:63-94` | L, A, B |
| M8 | `get_task` es de solo lectura pero **exige `idempotency_key`**, y devuelve `tasks:[…]` en lugar de `task:{…}`. | `local.go:255`, `tasks.go` | L, A, B |
| M9 | `collab --help` y `collab session --help` **fallan** (`unknown command: --help` / `open : no such file or directory`) aunque el README dice que `--help` funciona. El mensaje de uso omite `workspace`. | `main.go:108-135` | A (reproducido por L) |
| M10 | `register_workspace` y `recover_integration` exigen `expected_version`, pero no la incrementan, y `recover_integration` puede borrar un lock `active` en pleno cherry-pick. | `workspaces.go` | A, B |
| M11 | **Falso `unreachable`:** el lease de 90 s solo se renueva con llamadas MCP. worker-a fue marcado `agent_unreachable` mientras corría tests (visto en vivo). | `service.go` (lease) | L |
| M12 | **Crecimiento del log:** cada espera vacía añade 2 eventos (`call` + `wait_timeout`), unos 430 eventos por hora con 3 agentes ociosos. La retención es indefinida y `export` carga todo en memoria. | `service.go:Call` | L |

### Severidad baja

- **El broadcast `*` incluye al remitente**: el autor recibe su propio mensaje y debe hacerle ACK (observado en vivo, seq 267). (L)
- Descripciones duplicadas o engañosas: `get_session_state`/`list_agents`/`reconnect_agent` comparten texto; `acknowledge_message` dice "your own message". (L, B)
- El coordinador inicial es **implícitamente el primer participante** de `participants.json`; no hay opción para elegirlo. (L)
- Un no-coordinador recibe `stale_term` en lugar de `not_coordinator`; `force_leadership` sin motivo da `version_conflict`. (B)
- `leadership.deadline` serializa `0001-01-01T00:00:00Z` cuando no hay propuesta. (B)
- La respuesta de `request_review` trae `artifact.task_id` vacío. (B)
- Un replay idempotente añade un evento `call` y sube `seq`, pero devuelve el `seq` antiguo. (A)
- `workspace integrate` no imprime JSON cuando falla Git. (A)
- Plazos con reloj de pared, aunque la documentación habla de "período monotónico". (B)
- Se puede completar un padre con hijos abiertos. (B)
- `LeadershipInput.leadership_term` aparece en el schema sin usarse; `assign_task` muestra `title`/`acceptance` y los descarta (no se pueden editar criterios). (L)
- PLAUSIBLES (no reproducidos): reintento de `workspace integrate` tras caída entre cherry-pick y `finish`; `worktree add -b` no idempotente; margen ajustado entre la espera de 25 s y el `WriteTimeout` de 30 s en clientes con timeout MCP menor. (B, L)

## 5. Qué le falta para ser una herramienta completa

**Imprescindible (bloquea el uso real con fallos):**
1. **Recuperación de trabajo:** reasignar tareas en cualquier estado no terminal; que `revoke_agent` y la expiración prolongada del lease liberen asignaciones, locks y liderazgo de forma controlada (H1, H2, M5).
2. **Schemas MCP por herramienta:** con `required` correcto, descripciones por campo, sin `idempotency_key` en lecturas, y rechazo de campos desconocidos en lugar de descartarlos (H5, M7, M8).
3. **Ciclo de tarea completo:** `decline_task` con motivo, estado `approved` visible, `reviewer` asignable, notificaciones al autor en cada transición y `task_version` dentro de cada notificación (H3, M3, M4).
4. **Delegación:** que cualquier participante pueda proponer subtareas, con una política configurable (H4).

**Importante (robustez y operación):**
5. Integración segura: no integrar dos veces, verificar `commit_sha` contra el worktree del autor y traspasar liderazgo con integración activa (M1, M2, M5).
6. Presencia realista: heartbeat ligero, o `set_activity working` con ETA o lease extendido (M11).
7. Retención y compactación del log; no registrar las esperas vacías como eventos completos; `export` en streaming (M12).
8. Metadatos de mensaje del RFC: hilo, `task_id` relacionado y prioridad. Ahora la coordinación compite con el resto del buzón.
9. CLI usable: `--help` real por subcomando y elección explícita del coordinador (M9).
10. Tests de `cmd/collab` (0%) y `internal/domain` (0%); pruebas de integración con Git real; pruebas en Linux.

**Para cumplir su propio objetivo:**
11. **Certificar la continuidad con clientes reales**: ejecutar `continuity_probe.py` completo (3600 s) por cliente y configuración, y documentar resultados.
12. Evaluar los adaptadores que el README deja pendientes (hooks y canales de Claude Code) para clientes que terminan el turno.

## 6. Cómo fue la comunicación entre agentes (resumen)

Detalle en `anexos/01-comunicacion-agentes.md`.

- **La continuidad de los workers NO quedó demostrada.** En la fase inicial de prueba de continuidad, los dos agy hicieron un ciclo, recibieron su primer `wait_timeout` y dejaron de llamar, con `agent_unreachable` a las 03:36:48 y 03:36:57 (ver `anexos/05`). Solo reaparecieron tras el cambio de instrucción. claude-lead sí se mantuvo en bucle durante toda la sesión. La causa (permiso pendiente o fin de turno del cliente) no está diagnosticada.

- **Funcionó de punta a punta:** 2 tareas creadas, asignadas, aceptadas, trabajadas en paralelo (~8–10 min cada una), entregadas con artefacto y digest, revisadas de forma independiente por el coordinador y completadas. Los workers también respondieron por mensaje con resúmenes útiles y una adenda posterior.
- **Calidad de los workers:** los dos siguieron las restricciones (solo lectura, no tocar la sesión activa) y separaron lo CONFIRMADO de lo PLAUSIBLE. worker-a (Gemini) destacó en pruebas ejecutables y reproducibles; worker-b (Sonnet) en lectura de código y contraste con el RFC. Sus hallazgos coincidieron en los puntos clave (H1, M7, M8), lo que da confianza.
- **Fricciones observadas:**
  - El **modo automático de Claude Code bloqueó `assign_task`** como escritura externa no solicitada; hizo falta autorización explícita del usuario.
  - Mi mensaje manual de asignación era **redundante**, porque `assign_task` ya notifica (sin documentarlo). Al principio confundí esas notificaciones con mensajes de terceros.
  - worker-a se marcó `working` antes de `accept_task`; worker-b aceptó sin hacer ACK del mensaje de asignación. **ACK, aceptación y actividad son tres señales independientes** que el coordinador tiene que cruzar a mano.
  - Hay que **llevar la cuenta de `task_version` a mano** (+1 en cada operación); los dos workers tuvieron que llamar a `get_task` antes de aceptar.
  - Falso `unreachable` de worker-a mientras trabajaba en local.
  - `timeline` filtrado por actor resultó ser la mejor herramienta del coordinador para saber qué hace cada agente sin interrumpirle.

**Mejoras recomendadas para la colaboración:**
1. Herramienta `my_work`/`next_action` que devuelva al agente su asignación vigente con `task_version`/`generation` y la acción esperada.
2. Notificaciones estructuradas (tipo, `task_id`, versión) en lugar de texto libre ("Review requested for task …").
3. Unificar ACK y aceptación, o hacer que `accept_task` confirme implícitamente el mensaje de asignación.
4. Documentar en el skill qué mensajes genera el servidor automáticamente.
5. Regla de permisos recomendada en el README para `mcp__collab__*` en Claude Code.

## 7. Cierre de la sesión

- **Cerrada** a las 03:52:18 UTC por claude-lead a petición del usuario, con el CLI y la credencial `admin.json` (`close_session` es solo de administrador; un agente no puede cerrar). Estado final: `closed`, versión 2, 310 eventos. Antes se avisó por broadcast a los workers.
- **Duración:** 24 min desde `session_created` (03:27:50).
- **Tareas:** 2 creadas, 2 completadas con revisión independiente aprobada; ninguna pendiente.
- **Actividad por participante** (del export):

| Participante | Llamadas | Esperas vacías | Mensajes enviados | ACK | `agent_unreachable` |
|---|---|---|---|---|---|
| claude-lead | 80 | 22 | 6 | 8 | 0 |
| worker-a | 50 | 4 | 3 | 3 | 4 |
| worker-b | 56 | 11 | 4 | 4 | 2 |
| human (CLI) | 9 | — | — | — | — |

- De los 310 eventos, **195 son `call` y 37 `wait_timeout`**: el 75% del registro es ruido de sondeo, lo que respalda M12.
- worker-a acumuló 4 `agent_unreachable` y worker-b 2. Salvo las dos primeras (detención real tras la primera espera), corresponden a periodos de trabajo local sin llamadas MCP (M11).
