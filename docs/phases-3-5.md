# Estado de fases 3–5

Implementado en el núcleo local:

- Tareas y subtareas con versiones optimistas, una asignación vigente,
  generaciones que invalidan al responsable anterior y estados de bloqueo.
- Artefactos vinculados a la versión de tarea, revisión independiente y
  aprobación necesaria para completar.
- Propuestas de coordinación con candidatura aceptada, electorado congelado,
  mayoría absoluta, plazo de 120 segundos, voto único y período monotónico.
- Worktrees registrados por sesión, preparación desde CLI, integración mediante
  cherry-pick con una sola exclusión, y recuperación administrativa explícita
  después de conflictos o fallos.
- Skill de colaboración con ciclo de buzón y espera, seguimiento de tareas,
  publicación de evidencia y revisión.

La CLI prepara worktrees fuera del repositorio fuente. El registro de cada uno
retiene su ruta, rama y commit base. `workspace integrate` requiere una tarea
completada con una aprobación independiente vigente; confirma que el worktree
de integración esté limpio, aplica el commit aprobado y registra el SHA
resultante. Ante conflicto conserva el bloqueo y solicita recuperación humana.
La recuperación requiere una confirmación explícita de que el proceso Git previo
se detuvo y el worktree fue inspeccionado.

Ejemplo de preparación humana, una vez activa la sesión:

```sh
./bin/collab workspace prepare --config /tmp/ac-session/admin.json \
  --owner UUID_DEL_PARTICIPANTE --json '{"idempotency_key":"workspace-agent-1"}'
./bin/collab workspace prepare --config /tmp/ac-session/admin.json \
  --integration --json '{"idempotency_key":"workspace-integration-1"}'
```

El coordinador integra con credenciales propias y datos del artefacto revisado:

```sh
./bin/collab workspace integrate --config /tmp/ac-session/agent-1.json \
  --json '{"idempotency_key":"integrate-fix-1","leadership_term":1,"workspace_id":"ID","task_id":"TAREA","artifact_id":"ARTEFACTO"}'
```

El trabajo individual debe verificarse en su worktree antes de publicar el
commit. La CLI de integración comprueba el árbol limpio antes de aplicar el
commit; no ejecuta pruebas del proyecto. Un conflicto deja el bloqueo en estado
de recuperación. Después de inspeccionar y preservar cualquier cambio, el
administrador puede desbloquear con:

```sh
./bin/collab call --config /tmp/ac-session/admin.json --op recover_integration \
  --json '{"idempotency_key":"recover-integration-1","expected_version":VERSION,"confirmed":true,"reason":"Git detenido; worktree inspeccionado"}'
```

Limitaciones pendientes: la continuidad real del cliente MCP todavía requiere
la certificación de fase 0 con productos, versiones y configuraciones concretas;
no se asume que el SDK o el skill reanuden un turno terminado. La exclusión no
detiene comandos Git ejecutados fuera del protocolo. La revisión de seguridad
del Git CLI, pruebas de integración con repositorios reales, traspaso con una
integración activa y evaluación comparativa de fase 6 siguen pendientes. Las
pruebas actuales cubren los invariantes y el registro de éxito/conflicto en
SQLite con clientes simulados; no demuestran compatibilidad continua de modelos.
