# Informe de Revisión Técnica: `agents-chat` (`worker-a`)

- **Tarea:** `b2a4806f-3067-4f9d-a924-82f3073f9c0d` (Asignación 1, Término de liderazgo 1)
- **Autor:** `worker-a` (`d0e30ea3-f340-4cef-8304-0b3988f9811f`)
- **Repositorio origen (solo lectura):** `/Users/gle/Documents/projects/agents-chat` (`HEAD: f4d2e18f94f13646d983886f4941460028f230b3`, con `README.md` modificado en el árbol de trabajo)
- **Copia aislada de trabajo:** `/tmp/ac-review-a/repo`
- **Directorio de sesión manual de prueba:** `/tmp/ac-review-a/sess`

---

## 1. Compilación, `go vet`, `go test -race` y Unittests de Python

Todos los comandos se ejecutaron sobre `/tmp/ac-review-a/repo` en macOS arm64 (`go 1.26.0`, Python 3).

### 1.1 `go build -v -o bin/collab ./cmd/collab`
- **Resultado:** `PASS` (código de salida `0`).
- **Paquetes compilados:**
  ```text
  agents-chat/internal/domain
  agents-chat/internal/storage
  agents-chat/internal/transport
  agents-chat/internal/application
  agents-chat/cmd/collab
  ```

### 1.2 `go vet ./...`
- **Resultado:** `PASS` (código de salida `0`, sin advertencias ni diagnósticos).

### 1.3 `go test -v -race ./...`
- **Resultado:** `PASS` (18 tests ejecutados, **0 fallidos, 0 omitidos**).
- **Paquetes sin archivos `*_test.go` propios:** `agents-chat/cmd/collab` y `agents-chat/internal/domain` (`? ... [no test files]`).
- **Detalle de ejecución por paquete:**
  - `agents-chat/internal/application` (`3.190s`, 12 tests `PASS`):
    - `TestFrozenElectorateAcceptanceMajorityAndStaleCoordinator` (`0.09s`)
    - `TestVoteRejectionFrozenAbsenceExpiryAndHumanOverride` (`0.08s`)
    - `TestElectorateRejectsExpiredCandidateAndSingleVoter` (`0.05s`)
    - `TestRestartLostResponseAndACK` (`0.07s`)
    - `TestAtomicFailureAndAppendOnlyEvents` (`0.05s`)
    - `TestConcurrentReplayAndWait` (`0.08s`)
    - `TestAuthorizationRevocationAndExclusiveWriter` (`0.06s`)
    - `TestPreparedLifecycleAndVersionConflicts` (`0.06s`)
    - `TestPaginationDoesNotACKAndPresenceIsNotConnectivity` (`0.07s`)
    - `TestTaskAssignmentReviewAndCompletion` (`0.10s`)
    - `TestReassignmentFencesOldWorkerAndOptimisticVersions` (`0.06s`)
    - `TestIntegrationRequiresIndependentApprovalAndHumanRecovery` (`0.11s`)
  - `agents-chat/internal/probe` (`1.889s`, 4 tests `PASS`):
    - `TestDeliveryRetryAndAuthorization` (`0.00s`)
    - `TestConcurrentSendIsExactlyOneLogicalMutation` (`0.00s`)
    - `TestWaitArrivalTimeoutCancellationAndClose` (`0.01s`)
    - `TestEvidenceAndPresence` (`0.00s`)
  - `agents-chat/internal/storage` (`1.653s`, 1 test `PASS`):
    - `TestPrivateFilesSymlinksAndSchema` (`0.06s`)
  - `agents-chat/internal/transport` (`2.463s`, 1 test `PASS`):
    - `TestMCPThroughUnixSocket` (`0.04s`)

### 1.4 `python3 -m unittest discover -v -s scripts -p 'test_*.py'`
- **Resultado:** `PASS` (8 tests en `4.940s`, **0 fallidos, 0 omitidos**):
  - `test_go_nanosecond_timestamps (test_continuity_probe.AssessmentTests) ... ok`
  - `test_missing_ack_or_post_work_wait_fails (test_continuity_probe.AssessmentTests) ... ok`
  - `test_protocol_success_is_never_certification (test_continuity_probe.AssessmentTests) ... ok`
  - `test_short_run_and_wrong_answer_fail (test_continuity_probe.AssessmentTests) ... ok`
  - `test_stopped_agent_and_placeholder_metadata_fail (test_continuity_probe.AssessmentTests) ... ok`
  - `test_session_and_agent_commands (test_persistence.PersistenceTests) ... ok`
  - `test_sigkill_preserves_mailbox_ack_replay_and_live_bridge (test_persistence.PersistenceTests) ... ok`
  - `test_three_bridges_reconnect_timeout_and_close (test_stdio.StdioTests) ... ok`

---

## 2. Cobertura por Paquete (`go test -cover ./...`)

### 2.1 Salida directa de `go test -cover ./...`
```text
	agents-chat/cmd/collab		coverage: 0.0% of statements
ok  	agents-chat/internal/application	1.179s	coverage: 72.4% of statements
	agents-chat/internal/domain		coverage: 0.0% of statements
ok  	agents-chat/internal/probe	0.717s	coverage: 81.0% of statements
ok  	agents-chat/internal/storage	1.430s	coverage: 69.2% of statements
ok  	agents-chat/internal/transport	0.370s	coverage: 60.5% of statements
```

### 2.2 Cobertura cruzada (`go test -coverpkg=./... -coverprofile=/tmp/ac-review-a/cover.out ./...`)
- **Total global de sentencias (en tests Go):** `58.3%`
- **Desglose relevante por archivo/función:**
  - `cmd/collab/{main.go,workspace.go}`: `0.0%` en tests Go (ejercitado parcialmente desde `scripts/test_persistence.py` y `scripts/test_stdio.py` mediante subprocesos).
  - `internal/domain/protocol.go`: `ID` (`83.3%`), `Hash` (`100.0%`), `ValidHash` (`100.0%`), `Validate` (`78.6%`).
  - `internal/application/service.go`: `Open` (`70.0%`), `seed` (`69.0%`), `Call` (`80.0%`), `transact` (`78.0%`), `inbox` (`75.8%`), `events` (`85.7%`), `Expire` (`75.9%`).
  - `internal/application/tasks.go`: `leadership` (`85.0%`), `taskOperation` (`70.2%`), `assignedTask` (`81.2%`), `send` (`66.7%`).
  - `internal/application/leadership.go`: `member` (`80.0%`), `leadershipOperation` (`70.1%`).
  - `internal/application/workspaces.go`: `workspaceOperation` (`58.2%`).
  - `internal/transport/local.go`: `ListenRecovered` (`0.0%` en tests Go; probado vía `SIGKILL` en `test_persistence.py`), `Listen` (`62.5%`), `Handler` (`68.4%`), `MCP` (`59.6%`).

---

## 3. Prueba Manual del CLI en Sesión Aislada (`/tmp/ac-review-a/sess`)

Se ejecutó el flujo completo con `/tmp/ac-review-a/repo/bin/collab` en `/tmp/ac-review-a/sess` (ID de sesión generada: `49750c57-5efa-4d3e-b655-fb9df0ae490c`):

1. **`session create` y `serve`:**
   ```sh
   /tmp/ac-review-a/repo/bin/collab session create --dir /tmp/ac-review-a/sess \
     --participants /tmp/ac-review-a/repo/examples/participants.json \
     --objective 'Prueba manual CLI worker-a' --repo /tmp/ac-review-a/repo
   /tmp/ac-review-a/repo/bin/collab serve --dir /tmp/ac-review-a/sess
   ```
   - Participantes creados:
     - `agent-1.json`: `codex-main` (`7d9adfd6-0a81-4398-9ffe-9c0d3bf7f433`, coordinador inicial en término `1`)
     - `agent-2.json`: `claude-review` (`9c2a8383-bd5e-4e16-a43e-46e005114b34`)
     - `agent-3.json`: `codex-worker` (`81b33ca0-b77c-4508-a090-e4722f0964e5`)
   - `session show` inicial confirmó `"status":"prepared"`, `"version":1`, `"seq":5`.

2. **`session start`:**
   ```sh
   /tmp/ac-review-a/repo/bin/collab session start --config /tmp/ac-review-a/sess/admin.json \
     --json '{"idempotency_key":"start-1","expected_version":1}'
   ```
   - Respuesta: `{"session_id":"49750c57-5efa-4d3e-b655-fb9df0ae490c","status":"active","seq":7,"version":2,"actor_id":"human",...}`.

3. **`send_message` (directos y broadcast):**
   - Directo 1 (`key="msg-1"`, `agent-1` → `agent-2`): devuelve `message_id: "fad980c0-4766-4918-84a6-4fae8c9b8b6d"`, `seq: 10`.
   - Broadcast (`key="msg-2"`, `agent-1` → `"*"`): devuelve `message_id: "e8d4d313-c07d-4c8b-8973-aa4e0ce56f85"`, `seq: 12` (materializa los 3 agentes y excluye `human`).
   - Directo 2 (`key="msg-3"`, `agent-1` → `agent-2`): devuelve `message_id: "679f27ee-2cf9-4c82-8ed8-b607d5e24f1d"`, `seq: 14`.

4. **`check_inbox` y paginación (`after`, `limit`):**
   - Página 1 (`{"after":0,"limit":1}`): devuelve mensaje `seq: 10`, `"next_cursor":10`, `"has_more":true`.
   - Página 2 (`{"after":10,"limit":1}`): devuelve mensaje `seq: 12`, `"next_cursor":12`, `"has_more":true`.
   - Página 3 (`{"after":12,"limit":1}`): devuelve mensaje `seq: 14`, `"next_cursor":14` (`has_more` omitido/false).
   - Consulta posterior sin cursor (`{}`): devuelve los **3 mensajes intactos** (`seq: 10, 12, 14`), confirmando que leer/paginar el buzón no hace ACK.

5. **ACK propio vs ACK ajeno:**
   - **ACK propio:** `agent-2` confirma `fad980c0-4766-4918-84a6-4fae8c9b8b6d` (`key="ack-1"`) → éxito (`seq: 24`).
   - **ACK ajeno:** `agent-3` intenta confirmar el mismo `message_id` privado de `agent-2` (`key="ack-foreign"`):
     ```sh
     /tmp/ac-review-a/repo/bin/collab call --config /tmp/ac-review-a/sess/agent-3.json \
       --op acknowledge_message \
       --json '{"idempotency_key":"ack-foreign","message_id":"fad980c0-4766-4918-84a6-4fae8c9b8b6d"}'
     ```
     - Salida stdout: `{"error":"not_found"}`, stderr: `not_found`, código de salida `1`.

6. **Reuso de `idempotency_key` (mismo contenido vs contenido distinto):**
   - **Mismo contenido (`key="msg-1"`):** devuelve exactamente la respuesta original (`seq: 10`, `message_id: "fad980c0-4766-4918-84a6-4fae8c9b8b6d"`) sin duplicar el mensaje.
   - **Contenido distinto (`key="msg-1"`, `"body":"Contenido distinto"`):**
     ```sh
     /tmp/ac-review-a/repo/bin/collab call --config /tmp/ac-review-a/sess/agent-1.json \
       --op send_message \
       --json '{"idempotency_key":"msg-1","to":"9c2a8383-bd5e-4e16-a43e-46e005114b34","body":"Contenido distinto"}'
     ```
     - Salida stdout: `{"error":"idempotency_conflict"}`, stderr: `idempotency_conflict`, código de salida `1`.

7. **`kill -9` del daemon y reinicio (`serve --dir /tmp/ac-review-a/sess`):**
   - Se envió `kill -9 42185` dejando el socket huérfano `/tmp/ac-review-a/sess/daemon.sock`.
   - Al relanzar `collab serve --dir /tmp/ac-review-a/sess` (PID `42211`), `ListenRecovered` retiró el socket huérfano que daba `ECONNREFUSED` bajo el `flock` de `collab.sqlite.lock` y volvió a escuchar.
   - Tras el reinicio:
     - `check_inbox` de `agent-2` conservó únicamente los 2 mensajes no confirmados (`seq: 12` y `seq: 14`); el mensaje `seq: 10` siguió confirmado.
     - Reenviar `key="msg-1"` con el mismo cuerpo volvió a devolver la respuesta cacheada (`seq: 10`), y con cuerpo distinto devolvió `idempotency_conflict`.

8. **`close_session` y rechazo post-cierre:**
   ```sh
   /tmp/ac-review-a/repo/bin/collab session close --config /tmp/ac-review-a/sess/admin.json \
     --json '{"idempotency_key":"close-1","expected_version":2}'
   ```
   - Cambia a `"status":"closed"`, `"version":3`, `"seq":30`.
   - Enviar un mensaje nuevo (`key="msg-post-close"`) después del cierre falla con `{"error":"session_closed"}` (exit `1`).

9. **`export`:**
   ```sh
   /tmp/ac-review-a/repo/bin/collab export --config /tmp/ac-review-a/sess/admin.json > /tmp/ac-review-a/sess/export.json
   ```
   - Exportó instantánea válida con `"status":"closed"`, `"seq":31` y `31` eventos ordenados (`session_created`, `agent_registered` ×3, `session_started`, `message` ×3, `delivery` ×5, `ack` ×1, `session_closed`, y eventos `call`).

---

## 4. Lista de Bugs y Comportamientos Inesperados Confirmados

### Bug 1 (Severidad Alta): `assign_task` impide reasignar tareas una vez aceptadas o en progreso (`accepted`, `working`, `blocked`), rompiendo el diseño de generaciones (`internal/application/tasks.go:141-143`)
- **Descripción:** `docs/phases-3-5.md` y el modelo de `assignments` definen generaciones (`generation`) para invalidar al responsable anterior cuando el coordinador reasigna una tarea. Sin embargo, `tasks.go:141` bloquea `assign_task` si `task.State != "proposed" && task.State != "assigned"`:
  ```go
  if task.State != "proposed" && task.State != "assigned" {
      return fail("invalid_transition")
  }
  ```
  En cuanto el agente ejecuta `accept_task` (pasando a `accepted`) o `update_task` (`working` / `blocked`), si ese agente cae o su credencial es revocada, el coordinador **no puede reasignar la tarea a otro agente**; sólo puede cancelarla o marcarla como `failed` (estados terminales).
- **Reproducción y comando exacto:**
  ```sh
  # Tras crear tarea $T1_ID, asignarla a $A1 (version 2) y aceptarla con agent-1 (pasa a state="accepted", version=3):
  ./bin/collab call --config /tmp/ac-review-a/sess-bugs/admin.json --op assign_task \
    --json "{\"idempotency_key\":\"as2\",\"leadership_term\":1,\"task_id\":\"$T1_ID\",\"agent_id\":\"$A3\",\"task_version\":3,\"reason\":\"A1 caido\"}"
  # Salida: {"error":"invalid_transition"} (exit code 1)
  ```

### Bug 2 (Severidad Alta): `revoke_agent` permite revocar al coordinador activo sin limpiar ni transferir `coordinator_id` (`internal/application/service.go:345-366`)
- **Descripción:** `revoke_agent` sólo impide revocar a `"human"` y `"system"`. Si el administrador revoca la credencial del coordinador actual, `sessions.coordinator_id` sigue apuntando al UUID revocado (que además desaparece de `participants` en `get_session_state` porque `agents()` filtra `c.revoked=0`). Tampoco invalida asignaciones activas (`assignments.state='active'`) ni bloqueos de integración (`integration_locks`) del agente revocado.
- **Reproducción y comando exacto:**
  ```sh
  ./bin/collab agent revoke --config /tmp/ac-review-a/sess-bugs/admin.json \
    --json "{\"idempotency_key\":\"rev-coord\",\"expected_version\":1,\"agent_id\":\"$COORD\"}"
  ./bin/collab session show --config /tmp/ac-review-a/sess-bugs/admin.json
  # Salida: "participants" ya no incluye a $COORD, pero "leadership":{"coordinator":"73764d06-...","term":1} sigue apuntando al agente revocado.
  ```

### Bug 3 (Severidad Media): `list_tasks` ignora por completo los parámetros de paginación `after` y `limit` (`internal/transport/local.go:254`, `internal/application/tasks.go:63-94`)
- **Descripción:** El schema MCP de `list_tasks` usa `Page` (`after`, `limit`), pero:
  1. En `internal/transport/local.go:254`, el handler MCP descarta el input: `func(Page) domain.Request { return domain.Request{Op: "list_tasks"} }`.
  2. En `internal/application/tasks.go:63-94`, la consulta SQL `SELECT ... FROM tasks WHERE session_id=? ORDER BY created_at,id` no aplica `LIMIT` ni cursor `after`, ni calcula `has_more` o `next_cursor`.
- **Reproducción y comando exacto:**
  ```sh
  # Con 2 tareas creadas en la sesión:
  ./bin/collab call --config /tmp/ac-review-a/sess-bugs/admin.json --op list_tasks \
    --json '{"after":999999,"limit":1}'
  # Salida: devuelve las 2 tareas completas en "tasks":[...], ignorando limit=1 y after=999999.
  ```

### Bug 4 (Severidad Media): `get_task` devuelve `tasks: [...]` (array) en lugar de `task: {...}` y su schema MCP exige `idempotency_key` (`internal/application/tasks.go:63-94`, `internal/transport/local.go:168-180,255`)
- **Descripción:**
  1. Mientras todas las mutaciones de tarea (`create_task`, `assign_task`, `accept_task`, `update_task`, `publish_artifact`, `request_review`, `submit_review`) devuelven el objeto en el campo singular `"task"`, la consulta `get_task` comparte el bucle de `list_tasks` y devuelve el resultado dentro del slice `"tasks": [...]` dejando `"task"` vacío.
  2. En `internal/transport/local.go:255`, `get_task` reutiliza el struct `TaskInput` donde `Key string` (`idempotency_key`) no tiene `omitempty`, obligando a los clientes MCP a enviar un `idempotency_key` ficticio en una operación de sólo lectura, y exponiendo campos irrelevantes (`acceptance`, `agent_id`, `generation`, `leadership_term`, `parent_id`, `reason`, `state`, `task_version`, `title`).
- **Reproducción y comando exacto:**
  ```sh
  ./bin/collab call --config /tmp/ac-review-a/sess-bugs/admin.json --op get_task \
    --json "{\"task_id\":\"$T1_ID\"}"
  # Salida: {"session_id":...,"tasks":[{"id":"69353fb2-...","title":"Tarea 1",...}]} (sin campo "task")
  ```

### Bug 5 (Severidad Media): `./bin/collab --help` y `./bin/collab <subcomando> --help` fallan en lugar de mostrar ayuda (`cmd/collab/main.go:108-135,205-208`)
- **Descripción:** `README.md:32` afirma: *"El binario ofrece `init`, `serve`, `mcp`, `call`, `session`, `agent`, `workspace`, `timeline` y `export`; `--help` muestra sus opciones."* Sin embargo:
  1. `./bin/collab --help` crea un `flag.NewFlagSet("--help", ...)` sobre `args[1:]` (vacío) y cae en `default: return errors.New("unknown command: --help")`.
  2. `./bin/collab session --help` requiere un segundo argumento antes de parsear flags (`if len(args) < 2 { return errors.New("session/agent subcommand required") }`); si se pasa como segundo argumento, `action` toma el valor `"--help"`, `f.Parse(args[2:])` es vacío, intenta abrir `*config` (`""`) con `os.ReadFile("")` y falla con `open : no such file or directory`.
  3. Además, el mensaje de uso en `main.go:108` omite `workspace`, y `main.go:113` dice `"session/agent subcommand required"` cuando se invoca `./bin/collab workspace` sin subcomando.
- **Reproducción y comando exacto:**
  ```sh
  /tmp/ac-review-a/repo/bin/collab --help
  # Salida: unknown command: --help (exit code 1)
  /tmp/ac-review-a/repo/bin/collab session --help
  # Salida: open : no such file or directory (exit code 1)
  ```

### Bug 6 (Severidad Media): `register_workspace` y `recover_integration` exigen `expected_version == session.Version` pero no incrementan `session.Version` (`internal/application/service.go:149-155,269-271`, `internal/application/workspaces.go:36-58,179-196`)
- **Descripción:** `adminOnly(op)` incluye `register_workspace` y `recover_integration`, por lo que `service.go:269` exige `r.ExpectedVersion == out.Version`. Sin embargo, a diferencia de `start_session`, `close_session`, `register_agent` y `revoke_agent`, ni `register_workspace` ni `recover_integration` incrementan `sessions.version`. Además, en `recover_integration` (`README.md:48-49`), el administrador pasa `expected_version`, pero la versión de la sesión permanece igual tras recuperar el lock.

### Bug 7 (Severidad Baja): `collab workspace integrate` no imprime JSON en stdout cuando falla `git status` o `git cherry-pick` (`cmd/collab/workspace.go:209-215`)
- **Descripción:** Todas las operaciones CLI imprimen la respuesta JSON en `stdout` antes de devolver error en `stderr`. En cambio, cuando `integrateWorkspace` falla en `git status --porcelain` o `git cherry-pick`, invoca `finish_integration` (`State: "failed"`) pero descarta la respuesta del daemon y retorna directamente el error de Git, dejando `stdout` vacío.

### Bug 8 (Severidad Baja): Replay idempotente registra un nuevo evento `call` e incrementa `sessions.seq`, pero devuelve el `seq` antiguo cacheado (`internal/application/service.go:236-262`)
- **Descripción:** En `transact()`, antes de comprobar la tabla `idempotency`, el servidor inserta un evento `"call"` que incrementa `sessions.seq` (`UPDATE sessions SET seq=seq+1`). Si la clave ya existía con el mismo `request_hash`, hace `json.Unmarshal([]byte(result), &out)` y `tx.Commit()`, devolviendo el `out.Seq` histórico de la primera ejecución aunque la transacción acaba de incrementar `sessions.seq`.

---

## 5. Qué No Se Pudo Probar y Por Qué

1. **Binario y transporte en un host Linux real (`CGO_ENABLED=0`):**
   - **Motivo:** El entorno de ejecución actual es `macOS` (`darwin`). Aunque el código compila de forma cruzada, no se dispone de un kernel/host Linux local para ejecutar las pruebas de sockets Unix y `flock` en Linux.
2. **Prueba de continuidad de 3600 segundos (`scripts/continuity_probe.py` completo):**
   - **Motivo:** Requiere 1 hora de reloj y 3 clientes CLI interactivos dedicados conectados a una sesión exclusiva; excede el alcance de esta tarea de verificación rápida.
3. **Fallo real de disco lleno (`ENOSPC`) o corte físico de energía en SQLite:**
   - **Motivo:** No es seguro ni viable inducir agotamiento real del sistema de archivos `/tmp` en la máquina anfitriona donde corre la sesión activa; la atomicidad transaccional se verificó mediante el test de fallo inyectado (`TestAtomicFailureAndAppendOnlyEvents`) y `kill -9` (`SIGKILL`) del daemon en `/tmp/ac-review-a/sess`.

---
## Revisión de claude-lead

Aprobado (revisión `b1d9253c`, completado en task_version 8). Digest `ffd8929a…a0ca` verificado. Reproduje de forma independiente el bug 5 (`--help`) y confirmé el bug 6 en el código.
