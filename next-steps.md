# Próximos pasos

Esta lista sale del feedback recabado en la primera sesión real con tres CLI
(2026-10-22): un coordinador Claude Code y dos workers Antigravity CLI, uno con
Gemini 4 Argon y otro con Sonnet 5.5. El detalle está en
[docs/feedback/2026-10-22/](docs/feedback/2026-10-22/informe-final.md). Los
identificadores H*/M* remiten a la sección 4 del informe. Los puntos marcados
como **(nuevo)** son observaciones del usuario tras revisar la sesión.

El orden prioriza lo que bloquea el objetivo del proyecto: demostrar que varias
CLI colaboran de forma continua, sin reactivación humana, con un registro
auditable.

## P0 — bloquea la próxima prueba

1. **Diagnosticar por qué los `agy` se detienen tras la primera espera vacía.**
   La continuidad sigue sin demostrarse. Mirar la terminal antes de reactivar
   nada. Si el cliente termina el turno, documentarlo como limitación y evaluar
   adaptadores (hooks, canales o push; ver P1.9).
2. **Permisos sin bloqueos para la segunda prueba.**
   - Claude Code: regla `allow` para `mcp__collab__*` en el `.claude/settings.json`
     de la carpeta de arranque. Su modo automático bloqueó `assign_task`.
   - Workers: en la segunda prueba, arrancarlos con permisos completos
     (`agy --dangerously-skip-permissions`), **sólo para pruebas**. Hacerlo en
     carpetas de arranque dedicadas, sin secretos ni repositorios reales a su
     alcance, con `--sandbox` si no impide el trabajo, y con el cambio anotado en
     `configuration` de `participants.json`.
   - Para trabajo real, seguir la regla del README ("No desactivar globalmente
     las protecciones del cliente") hasta evaluar el riesgo: los mensajes de
     otros agentes son contenido no confiable.
3. **Repetir la prueba de continuidad en una sesión limpia** (`/tmp/ac-run2`):
   primero corta y después los 3600 s completos, sin cambiar la instrucción a
   mitad de camino. La sesión del 2026-10-22 no cuenta como evidencia.
4. **Script de preparación** que genere participants, carpetas de arranque por
   agente, configuración MCP y skills. Hoy se hace a mano y es fácil cruzar
   identidades (dos instancias de `agy` comparten la configuración MCP global
   si no se usa `.agents/mcp_config.json` por carpeta).

## P1 — el trabajo real se rompe en cuanto algo falla

5. **H1:** no se puede reasignar una tarea aceptada o en curso. Si el agente
   cae, la tarea queda atascada.
6. **H2:** `revoke_agent` no libera asignaciones, locks de integración ni el
   puesto de coordinador.
7. **H5, M7, M8:** las herramientas MCP comparten un formato genérico y
   descartan campos sin avisar. `get_task` pide `idempotency_key` aunque sólo
   lee, y `list_tasks` ignora la paginación. Es la principal fuente de
   confusión para los modelos.
8. **H3:** no existe `decline_task`. Intentar rechazar una tarea la acepta sin
   error.
9. **(nuevo) Coste del sondeo y alternativas push.** Hoy cada agente en espera
   repite el ciclo `wait_for_messages` (long-poll de hasta 25 s) y después
   relee estado, buzón y tareas. La espera en sí no es sondeo activo, pero
   **cada vuelta es un turno del modelo**: consume tokens de contexto y
   razonamiento aunque no haya nada nuevo. En la primera sesión hubo 37 esperas
   vacías en 24 minutos.
   - Medir: tokens por ciclo vacío y por cliente, y coste por hora en espera.
   - Reducir: alargar la espera tanto como permita el timeout MCP de cada
     cliente (Claude Code admite `MCP_TOOL_TIMEOUT`); que el skill no relea
     estado y tareas tras una espera vacía, sólo cuando llegan mensajes; un
     resumen compacto (`my_work`/`next_action`) en lugar de tres lecturas.
   - Push: evaluar los [canales de Claude Code](https://code.claude.com/docs/en/channels)
     y los hooks, que pueden inyectar mensajes sin que el modelo sondee. Las
     notificaciones de servidor MCP existen en el protocolo, pero hay que
     verificar si algún cliente despierta al modelo con ellas. En `agy`,
     revisar `--remote-control` y `--input-format stream-json`.
   - **Arrancar los agentes desde la herramienta es una opción abierta.** Hasta
     ahora el diseño excluía iniciar o supervisar agentes, pero el usuario no
     lo descarta si resulta mejor. La investigación debe comparar con datos:
     - (a) CLI abiertas a mano con sondeo optimizado.
     - (b) CLI abiertas a mano con push (canales, hooks).
     - (c) La herramienta arranca y alimenta cada CLI, por ejemplo con modos de
       entrada en streaming o reanudando la conversación cuando llega trabajo.

     Criterios de comparación: tokens consumidos en espera, latencia de
     reacción, continuidad sin intervención humana, aislamiento de identidad,
     permisos y complejidad por cliente. Si se adopta (c), actualizar el README
     y el RFC: deja de ser cierto que la herramienta "no inicia ni supervisa
     agentes", y la prueba de continuidad tendría que redefinirse, porque
     reactivar al agente pasaría a ser parte del sistema y no una
     intervención humana.
10. **M11:** falsos `unreachable` mientras un agente trabaja en local sin llamar
    al MCP. Hace falta un heartbeat, o que `set_activity working` alargue el
    lease. Ensucia cualquier medición de continuidad.
11. **(nuevo) Idioma de coordinación: siempre inglés.** El coordinador habló con
    los workers en español porque el humano le hablaba en español. El idioma
    entre humano y coordinador no debe condicionar el protocolo: todo mensaje,
    tarea, criterio de aceptación, revisión y artefacto entre agentes va en
    inglés. Fijarlo en el skill y, si hace falta, como campo de la sesión
    (`agent_language: "en"`). El coordinador sigue respondiendo al humano en su
    idioma.

## P2 — robustez, ergonomía y capacidades por rol

12. **(nuevo) Perfiles por participante: para qué sirve cada uno y qué carga.**
    Cada participante declara en la configuración su rol y sus puntos fuertes,
    y la sesión arranca con los skills y MCP adecuados ya instalados en su
    carpeta de arranque. Ejemplos:
    - `code` (p. ej. Codex): skills de implementación, tests, proceso de
      release y convenciones del repo; MCP de Git o de CI si aplica.
    - `research`: skills de investigación, búsqueda web y generación de
      subagentes adversariales que cuestionen las conclusiones.
    - `review`: skills de revisión de código y seguridad, checklist de
      criterios de aceptación.

    El coordinador lee los perfiles (`list_agents`) para asignar según el rol,
    no sólo según la disponibilidad. El script de preparación (P0.4) materializa
    cada perfil en `.claude/skills/`, `.agents/skills/` o la ruta equivalente de
    cada cliente.
13. **(nuevo) ¿Puede el coordinador aprobar permisos de los workers?**
    Investigar si algún cliente permite delegar la aprobación de herramientas a
    otro proceso: hooks de permisos, modo remoto o canales. Así el humano no
    tendría que estar en cada terminal y no haría falta dar permisos completos.
    Riesgos: un coordinador comprometido, o al que otro agente convence, podría
    aprobar acciones peligrosas. La aprobación tendría que quedar registrada en
    el timeline.
14. **M1, M2 (integración):** el mismo artefacto se puede integrar dos veces, y
    no se comprueba que `commit_sha` exista ni que pertenezca al worktree del
    autor.
15. **M3, M4 (ciclo de tarea):** notificar al autor en cada transición, estado
    `approved` visible, campo `reviewer` y una herramienta `my_work`/`next_action`
    para no llevar `task_version` a mano.
16. **H4:** sólo el coordinador puede crear subtareas, en contra del RFC.
    Permitir que cualquier participante proponga subtareas.
17. **M12:** el log crece sin límite. El 75 % de los eventos son `call` y
    `wait_timeout`. Hacen falta retención, compactación y `export` en streaming.
    Relacionado con P1.9.
18. **M9:** `--help` real por subcomando y una opción explícita para elegir el
    coordinador (hoy es el primero de `participants.json`).
19. **Tests:** `cmd/collab` y `internal/domain` tienen 0 % de cobertura. Faltan
    pruebas con Git real y en Linux.
20. **M5, M6, M10 (liderazgo):** traspaso con una integración activa, propuestas
    vencidas que bloquean nuevas y `recover_integration` puede borrar un lock
    activo.

## P3 — detalles

21. **Mensajería:** el broadcast incluye al remitente; las descripciones de
    algunas herramientas están duplicadas o confunden; los mensajes no tienen
    hilo ni `task_id`.
22. **Respuestas inconsistentes:** `stale_term` en lugar de `not_coordinator`,
    fechas vacías como `0001-01-01`, `task_id` vacío en la respuesta de
    `request_review` y el `seq` de un replay idempotente.
23. **Mínimo de participantes configurable** para pruebas con dos agentes, sin
    que esas ejecuciones cuenten para la certificación.
