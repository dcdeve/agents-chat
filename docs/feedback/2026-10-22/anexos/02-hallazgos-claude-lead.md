# Hallazgos de claude-lead (capa MCP, CLI y núcleo de mensajería)

Revisado sobre una copia del repo `agents-chat` (commit `f4d2e18`) en el scratchpad; el original no se modificó.

## Build y tests

- `go build`, `go vet ./...`: limpios (Go 1.27.3, darwin/arm64).
- `go test -race ./...`: todos OK. Cobertura: application 72.6%, probe 81.0%, storage 69.2%, transport 60.5%, **cmd/collab 0%**, **domain 0%** (sin tests).
- Python `unittest`: 8 tests OK.

## Problemas en el puente MCP (`internal/transport/local.go`)

| # | Hallazgo | Estado | Impacto |
|---|---|---|---|
| M1 | `get_task`, `assign_task`, `accept_task`, `update_task`, `request_review`, `create_task` comparten el struct `TaskInput`: los 11 campos aparecen en todos los schemas, sin descripciones, y cada handler descarta la mayoría. | CONFIRMADO | El modelo no sabe qué campos importan (p. ej. `create_task` muestra `agent_id`, pero no asigna). |
| M2 | `get_task` (solo lectura) exige `idempotency_key` porque el campo de `TaskInput` no es `omitempty`. | CONFIRMADO (me pasó en sesión) | Fricción y claves sin sentido para lecturas. |
| M3 | `list_tasks` recibe `after`/`limit` y los ignora (`func(Page) … {Op: "list_tasks"}`). | CONFIRMADO | Paginación anunciada que no existe. |
| M4 | `list_agents` y `reconnect_agent` reutilizan la descripción de `get_session_state` ("Inspect this collaboration session…"). `reconnect_agent` no explica qué hace. | CONFIRMADO | El modelo no sabe cuándo usar `reconnect_agent`. |
| M5 | `LeadershipInput.LeadershipTerm` aparece en el schema y ningún handler lo usa. | CONFIRMADO | Ruido en el schema. |
| M6 | `assign_task` muestra `title`/`acceptance`/`generation`, pero los descarta: no se pueden corregir los criterios de una tarea después de crearla. | CONFIRMADO | Falta un "editar tarea". |
| M7 | Las respuestas vacías omiten `messages`/`tasks` en lugar de devolver `[]`. | CONFIRMADO | Ambigüedad menor para el modelo. |

## Núcleo (`internal/application/service.go`, `cmd/collab/main.go`)

| # | Hallazgo | Estado |
|---|---|---|
| S1 | El coordinador inicial es **implícitamente el primer participante registrado** (`UPDATE … coordinator_id=? WHERE coordinator_id=''`). No hay flag ni campo para elegirlo; depende del orden de `participants.json`. | CONFIRMADO |
| S2 | Cada `wait_for_messages` deja 2 eventos (`call` + `wait_timeout`). Con 3 agentes eso son ~430 eventos/h solo esperando, y la retención es indefinida (README). Sumado a `export` cargando todo en memoria, el log crece sin límite. | CONFIRMADO (visto en el timeline) |
| S3 | Un mutex global serializa todas las operaciones (incluidas las lecturas). Correcto para uso local, pero limita la escala. | CONFIRMADO, por diseño |
| S4 | Margen de timeouts ajustado: espera máx. 25 s, `WriteTimeout` del servidor 30 s, timeout del cliente HTTP 30 s. Con un cliente MCP de timeout por defecto menor (p. ej. Antigravity, sin documentar) las esperas pueden cortarse. | PLAUSIBLE |
| S5 | Los mensajes no tienen campo de tarea/hilo ni prioridad; la coordinación compite con el resto del buzón (visto en worker-a). | CONFIRMADO |

_Pendiente: integrar los informes de worker-a (pruebas) y worker-b (auditoría de tareas/liderazgo/worktrees)._
