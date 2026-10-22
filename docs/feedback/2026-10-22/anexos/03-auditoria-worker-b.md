# Auditoría agents-chat: tareas / revisión / liderazgo / worktrees vs documentación

- Tarea: `eeb68fdc-1975-4bdc-bd29-d50f22215403` (asignación 1, término 1)
- Autor: worker-b (`482d8df2-1ee9-4e2b-8c30-7d4423b4a97a`, Antigravity CLI 1.4.4, Claude Sonnet 5.5)
- Repo auditado (solo lectura): `/Users/gle/Documents/projects/agents-chat` @ `f4d2e18f94f13646d983886f4941460028f230b3`
- Leído completo: README.md, docs/phases-1-2.md, docs/phases-3-5.md, RFC_REVIEW_PROMPT.md, internal/application/{tasks,leadership,workspaces}.go, cmd/collab/workspace.go, internal/domain/protocol.go. Además, por contexto: service.go (dispatch, revoke_agent, lease), los schemas MCP en `~/.gemini/antigravity-cli/mcp/collab/*.json` y llamadas reales de solo lectura (`get_task`, `list_workspaces`, `leadership_state`, `timeline`).
- Etiquetas: **CONFIRMADO-L** = verificado leyendo el código (archivo:línea); **CONFIRMADO-V** = visto en la sesión en vivo; **PLAUSIBLE** = inferido, no ejecutado.
- **No ejecuté** `go test`/`go build` ni el CLI (eso es de la otra tarea). La columna "probado" solo lista tests por nombre (`grep '^func Test'`); no leí sus cuerpos. Por eso no afirmo qué cubren.

## 1. Prometido vs implementado vs probado

| Área | Prometido (docs/RFC) | Implementado | Probado (test existente por nombre) |
|---|---|---|---|
| Tareas | Versiones optimistas, una asignación vigente, generaciones que invalidan al responsable anterior (phases-3-5) | Sí: `version` en toda transición, `assignments` con `state='active'` y `generation` (tasks.go:121-171, 417-438). **Pero** reasignar solo se permite en `proposed`/`assigned` (tasks.go:141) | `TestReassignmentFencesOldWorkerAndOptimisticVersions`, `TestTaskAssignmentReviewAndCompletion` |
| Tareas | Estados de bloqueo | `blocked` solo desde `working` (tasks.go:251,258); la rama `blocked→accepted` es inalcanzable | sin evidencia |
| Tareas | RFC 6: agentes "aceptan **o rechazan** tareas explicando el motivo" | **No hay rechazo.** `accept_task` ignora `reason`/`state` (tasks.go:172-191) | no |
| Tareas | RFC 7: agentes crean subtareas y delegan sin cuello de botella | Solo coordinador/human crean y asignan (tasks.go:96-99, 121-124) | no |
| Tareas | RFC: reasignar tareas de un agente `unreachable` | No. `revoke_agent` no toca asignaciones (service.go:345-366) y reasignar exige `proposed`/`assigned` | no |
| Revisión | Artefacto ligado a versión de tarea, revisión independiente, aprobación necesaria para completar | Sí: `artifact.task_version`, autor≠revisor (tasks.go:365-367), completar exige aprobación vigente (tasks.go:210-225) | `TestTaskAssignmentReviewAndCompletion` |
| Revisión | Autor no puede aprobar su trabajo | Sí, en `submit_review`, `update_task completed` y `begin_integration` (tasks.go:223, 365; workspaces.go:109) | idem |
| Revisión | Un artefacto cambiado requiere nueva revisión | Sí: `artifact_stale` (tasks.go:375-380) y `review_stale` | idem |
| Revisión | Notificar al revisor | **Parcial:** `request_review` avisa solo al coordinador y solo si no es el autor (tasks.go:339-345). No hay campo `reviewer` | no |
| Liderazgo | Propuesta con candidatura aceptada | `accept_leadership` (leadership.go:89-117) | `TestFrozenElectorateAcceptanceMajorityAndStaleCoordinator` |
| Liderazgo | Electorado congelado, mayoría absoluta, voto único | `electorate_json` + `len/2+1` + `already_voted` (leadership.go:50-70,151-157,169) | `TestVoteRejectionFrozenAbsenceExpiryAndHumanOverride`, `TestElectorateRejectsExpiredCandidateAndSingleVoter` |
| Liderazgo | Plazo 120 s | `deadline = now+120s`; `vote`/`accept` comprueban, `finalize` expira (leadership.go:80,102,131,197) | cubierto por el test de expiración (por nombre) |
| Liderazgo | "Período monotónico" (phases-3-5.md:10) | Los plazos son reloj de pared: `time.Now` guardado como RFC3339 (service.go:36,46; leadership.go:80). El `term` sí crece monótono | sin evidencia |
| Liderazgo | Coordinador obsoleto rechazado | `requireLeader` compara actor y `leadership_term` (tasks.go:57-62; workspaces.go:60,129) | por nombre `...StaleCoordinator` |
| Liderazgo | Humano puede iniciar/confirmar/anular (RFC 10) | Solo `force_leadership` (human, versión=término) (leadership.go:204-226) | `...HumanOverride` |
| Worktrees | Worktrees por sesión, preparación desde CLI, fuera del repo | `prepareWorkspace` (workspace.go:57-191): top-level, symlinks, dir fuera del repo | `TestPrivateFilesSymlinksAndSchema` (storage, no worktrees) |
| Integración | Cherry-pick, una sola exclusión, solo tarea completada con aprobación independiente | `begin_integration` + lock único + `git cherry-pick` (workspaces.go:59-127; workspace.go:231) | `TestIntegrationRequiresIndependentApprovalAndHumanRecovery` |
| Integración | Recuperación humana explícita tras conflicto | `recover_integration` con `confirmed`+`reason` (workspaces.go:179-196) | mismo test |
| Integración | Traspaso con integración activa (pendiente según docs) | No: el lock está atado a owner y término (workspaces.go:78,141) | no (documentado como pendiente) |
| Integración | RFC: workspaces por tarea/agente, reconciliación | Sin deregistro ni ciclo de vida del worktree: `Status` solo puede ser `active` (workspaces.go:44) | no |
| MCP | RFC lista `create_subtask`, `register_agent` | No existen como tools MCP. `begin/finish_integration`, `register_workspace` y `recover_integration` tampoco están en MCP (solo CLI/humano) | n/a |

## 2. Bugs y huecos lógicos

Severidad: A=alta, M=media, B=baja.

| # | Sev | Hallazgo | Escenario que falla | Ubicación | Marca |
|---|---|---|---|---|---|
| 1 | A | Tarea atascada si el responsable cae. Reasignar solo desde `proposed`/`assigned`; `revoke_agent` no supera la asignación activa | Agente acepta, pasa a `working` y su terminal muere o se revoca. `assign_task` devuelve `invalid_transition`. La única salida es `cancelled`/`failed` por el coordinador y recrear la tarea (se pierde historial y versión). Contradice "generaciones que invalidan al responsable anterior" y el RFC de reasignar tareas de un `unreachable` | tasks.go:141; service.go:345-366 | CONFIRMADO-L |
| 2 | A | No se puede rechazar una tarea, y `accept_task` ignora `reason`/`state` | Un agente llama `accept_task` con `{"state":"rejected","reason":"fuera de mi alcance"}`. El servidor lo registra como `accepted`, sin error y sin guardar el motivo. Sin vía de rechazo, el trabajo queda con quien no puede hacerlo | tasks.go:172-191; schema MCP compartido | CONFIRMADO-L |
| 3 | A | Solo el coordinador crea subtareas o asigna, contra el RFC 7 ("sin convertir al orquestador en cuello de botella") | worker-a descubre trabajo adicional y no puede crear una subtarea ni delegarla: `stale_term` | tasks.go:96-99, 121-124 | CONFIRMADO-L |
| 4 | M | Re-integración del mismo artefacto. `begin_integration` solo mira el lock, no la tabla `integrations` | Tras `finish_integration completed` el lock se borra. Un segundo `begin_integration` con el mismo `artifact_id` pasa y vuelve a cherry-pickear el commit (duplicado o conflicto) | workspaces.go:112-121, 172 | CONFIRMADO-L |
| 5 | M | `commit_sha` sin verificación: solo regex hex. El `digest` no está ligado al commit ni a su contenido | Un autor publica `kind=commit` con un SHA de otra rama (p. ej. `main`) y un digest cualquiera. Tras la aprobación (revisando solo el texto) el integrador hace cherry-pick de ese SHA. No se comprueba que el commit pertenezca al worktree del autor ni que exista. Un merge commit falla en cherry-pick sin `-m` | tasks.go:282-290; workspaces.go:90; workspace.go:231 | CONFIRMADO-L (efecto Git: PLAUSIBLE) |
| 6 | M | Notificaciones incompletas del ciclo de revisión | (a) Si el coordinador es el autor, `request_review` no avisa a nadie (`coord != actor`). (b) `submit_review`, `completed`, `failed` y `cancelled` no envían mensaje al autor o asignado: debe sondear `get_task`. (c) La asignación no incluye `task_version`; hay que llamar a `get_task` para aceptar (lo hice así yo) | tasks.go:339-345, 348-396, 193-242, 167 | CONFIRMADO-L / CONFIRMADO-V (c) |
| 7 | M | Traspaso de liderazgo con integración activa deja un bloqueo muerto | Coordinador A abre integración; B gana el voto. A recibe `stale_term`; B no es dueño (`owner != actor`) y además `w.Owner != actor`. Solo `recover_integration` humano lo libera, y el worktree de integración debe recrearse para B (id derivado de `integration-<owner>`). Documentado como pendiente | workspaces.go:78,129-143; workspace.go:103-119 | CONFIRMADO-L |
| 8 | M | Revisor sin asignar ni descubrimiento. No hay `reviewer` en `request_review`, ni tool para leer artefactos o revisiones | El revisor necesita `artifact_id`. Solo aparece en la respuesta al autor, en eventos `artifact_published` de `timeline` (hay que filtrar por tipo) o en un mensaje manual. `list_tasks` no devuelve artefactos | tasks.go:309-347; protocol.go:78-89 | CONFIRMADO-L; `timeline` verificado en vivo |
| 9 | M | La aprobación no cambia de estado visible | Tras `approved`, el estado sigue `review_requested` y solo sube `version`. Un observador no distingue "pendiente" de "aprobado" sin leer revisiones. El coordinador debe adivinar que ya puede completar | tasks.go:385-391 | CONFIRMADO-L |
| 10 | M | Propuesta vencida bloquea nuevas propuestas hasta `finalize_leadership` | Pasan 120 s sin quórum. `vote` devuelve `proposal_expired`, pero el estado sigue `pending`. `propose_leadership` falla con `leadership_pending` y `leadership_state` la muestra como activa hasta que alguien llame `finalize` | leadership.go:71-77,131,197-202; tasks.go:22 | CONFIRMADO-L |
| 11 | M | Al aprobar no se revalida al candidato. Puede votar por sí mismo | Candidato revocado o con lease vencido después de aceptar sigue pudiendo ser elegido. El voto propio cuenta (electorado de 3 → basta 1 voto ajeno) | leadership.go:141-150,169-176 | CONFIRMADO-L |
| 12 | M | La retirada de un candidato tras aceptar elimina la propuesta sin limpiar votos | El candidato puede llamar `accept_leadership` con `confirmed=false` en mitad de la votación; la propuesta pasa a `rejected` y los votos quedan huérfanos. Es un efecto aceptable, pero no está documentado | leadership.go:105-117 | CONFIRMADO-L |
| 13 | B | `force_leadership` con `reason` vacío devuelve `version_conflict`. El resultado del `UPDATE ... rejected` se ignora (`_, _ =`) | Humano sin motivo recibe un código engañoso | leadership.go:208-209, 222 | CONFIRMADO-L |
| 14 | B | `recover_integration` ignora `expected_version` aunque el README/docs lo pasan en el ejemplo | Un humano cree tener control optimista y no lo tiene. Puede borrar un lock `active` en pleno cherry-pick | workspaces.go:179-196; docs/phases-3-5.md (ejemplo `recover`) | CONFIRMADO-L |
| 15 | B | Códigos de error confusos: un no-coordinador recibe `stale_term` | Un agente no coordinador recibe `stale_term` aunque su término sea el correcto; no existe `not_coordinator` | tasks.go:57-62; workspaces.go:60,129 | CONFIRMADO-L |
| 16 | B | `leadership.deadline` serializa `0001-01-01T00:00:00Z` sin propuesta (`omitempty` no omite `time.Time`) | Ruido visible para modelos | protocol.go:124 | CONFIRMADO-V (`get_session_state` y `leadership_state`) |
| 17 | B | Completar un padre no exige hijos terminados. Se permiten subtareas bajo padres terminales | Árbol inconsistente: padre `completed` con hijos `proposed` | tasks.go:104-111, 193-243 | CONFIRMADO-L |
| 18 | B | Reintento de `workspace integrate` tras caída entre cherry-pick y `finish_integration` | Con la misma clave, `begin_integration` repite la respuesta; el worktree está limpio; el segundo cherry-pick queda vacío y falla; `fail()` pone el lock en `recovery` | workspace.go:198-238 | PLAUSIBLE |
| 19 | B | `worktree add -b` no es idempotente si la rama existe pero el directorio no | `workspace prepare` falla con error de Git tras limpiar el directorio a mano | workspace.go:170-174 | PLAUSIBLE |
| 20 | B | `list_tasks` sin filtros ni paginación, devuelve el `acceptance` completo (hasta 8 KiB) de todas y hace una consulta por tarea | Coste en tokens y latencia con muchas tareas | tasks.go:63-94 | CONFIRMADO-L |
| 21 | B | Reloj de pared en plazos y leases, no monotónico como dice la doc | Un salto de reloj acorta o alarga la votación | service.go:36,46; leadership.go:80,102 | CONFIRMADO-L |
| 22 | B | `register_workspace` no valida existencia del dueño, ruta absoluta ni `base_sha` | Solo humano; riesgo bajo | workspaces.go:36-58 | CONFIRMADO-L |
| 23 | — | Latencia observada: mis primeras llamadas en paralelo tardaron ~56 s; un segundo lote, ~2 min; llamadas posteriores, ~0-10 s | Si el daemon serializa lecturas detrás de esperas largas, el lease de 90 s caduca. No sé si la causa es el daemon, el bridge o mi cliente | sesión en vivo, timestamps de `Created At`/`Completed At` | PLAUSIBLE (sin reproducir) |

## 3. Funciones MCP con schema ambiguo o confuso

1. **Un solo schema genérico de 11 propiedades** (`acceptance, agent_id, generation, idempotency_key, leadership_term, parent_id, reason, state, task_id, task_version, title`), con solo `idempotency_key` obligatorio, compartido por `create_task`, `assign_task`, `accept_task`, `get_task`, `update_task` y `request_review`. El modelo no sabe qué enviar a cada una ni cuáles son obligatorias. Los extras se ignoran en silencio (hallazgo 2).
2. **`get_task` exige `idempotency_key`** aunque es solo lectura. Me falló en vivo con `missing properties: ["idempotency_key"]`.
3. **`propose/accept/vote/finalize_leadership` comparten otro schema de 7 propiedades sin ninguna obligatoria**, y `vote_leadership` no obliga `proposal_id` ni `choice`.
4. **Descripciones duplicadas y engañosas:** `get_session_state`, `list_agents` y `reconnect_agent` dicen "Inspect this collaboration session. Inbox reads never acknowledge messages". Las tres devuelven casi lo mismo y `reconnect_agent` no describe qué reconecta. `list_agents` además repite el payload completo (visto en vivo).
5. **`update_task` está sobrecargada:** la descripción habla solo de progreso/bloqueo, pero el coordinador también la usa para `completed`/`failed`/`cancelled`, con reglas distintas (término, autor). El modelo no puede deducirlo.
6. **`acknowledge_message`: "Confirm receipt of your own message".** Es el destinatario quien confirma un mensaje que recibió. "Your own" confunde.
7. **Paginación de `check_inbox` y `wait_for_messages`:** `check_inbox` dice "Cursor paginates; omit it on the next inbox pass", `wait_for_messages.after` dice "omit to redeliver all pending". El campo cambia de nombre (`after`/`next_cursor`) y su semántica no queda clara.
8. **`submit_review`/`request_review`:** no explican que `task_version` debe ser el vigente tras publicar. Hay que sumar `+1` tras `publish_artifact` y tras `request_review`.
9. **Faltan tools MCP** que el skill necesita implícitamente: leer artefactos y revisiones, rechazar una tarea, crear subtareas (hallazgo 3).

## 4. Qué falta para una herramienta completa

1. Reasignación y recuperación de tareas de agentes revocados o `unreachable` (hallazgo 1), con efecto visible en la asignación.
2. Rechazo explícito de tareas con motivo (`decline_task`) y que `accept_task` falle ante parámetros inválidos (hallazgos 2 y 3 de schema).
3. Delegación y subtareas por cualquier participante, con política (hallazgo 3).
4. Notificaciones al autor, asignado y revisor, y un campo `reviewer` o cola de revisión (hallazgos 6, 8).
5. Estado `approved` explícito o proyección visible (hallazgo 9).
6. Schemas MCP propios por herramienta, con `required` correcto y sin `idempotency_key` en lecturas.
7. Tools de lectura de artefactos, revisiones y workspaces propios del agente. Operaciones de integración accesibles al coordinador (o un paso documentado) y ciclo de vida de worktrees (retirar/cerrar).
8. Validación de `commit_sha` contra el workspace del autor, enlace commit-digest, y protección contra doble integración (hallazgos 4, 5).
9. Traspaso de liderazgo con integración activa (hallazgo 7), y finalización automática de propuestas vencidas (hallazgo 10).
10. Plazos con reloj monotónico, o corregir la doc (hallazgo 21).
11. Metadatos de mensaje del RFC (hilo, tarea relacionada, estado de procesamiento): `Message` solo tiene `id, sender, recipients, body, seq` (protocol.go:71-77). Roles y capacidades mutables del RFC tampoco existen.
12. Pruebas de integración reales con Git y de continuidad con clientes reales (la propia doc lo marca como pendiente).

## Qué no pude probar

- No compilé ni ejecuté tests ni el CLI; no leí los cuerpos de los tests. La columna "probado" es solo por nombre.
- No ejecuté escenarios de revocación, cherry-pick real ni votación completa: los hallazgos 4, 5, 7, 11 y 18 vienen de lectura de código.
- No conozco el modelo de latencia del daemon; el hallazgo 23 es solo una observación.

---
## Adenda (mensaje de worker-b, seq 171; no forma parte del artefacto aprobado)

- **24 · B · CONFIRMADO-V:** la respuesta de `request_review` devuelve `artifact.task_id` vacío (`''`) porque la consulta de `tasks.go:321` no lee `task_id`.

## Revisión de claude-lead

Aprobado (revisión `f8bde64e`, task_version 6 → 7; completada en v8). Verifiqué el digest y los hallazgos 1, 2, 3, 4 y 9 en el código de `f4d2e18`.
