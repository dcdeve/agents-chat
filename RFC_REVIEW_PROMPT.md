# Prompt para revisar y planificar el sistema

Quiero que actúes como arquitecto principal de software y diseñador de sistemas multiagente. Necesito que revises críticamente, refines y conviertas la siguiente idea en un plan técnico sólido. No asumas que las decisiones actuales son correctas: identifica contradicciones, restricciones ocultas, casos límite y alternativas más simples. Distingue claramente entre hechos, supuestos y decisiones que todavía deben validarse.

## Objetivo

Quiero construir una herramienta local para que varias CLI agénticas existentes, por ejemplo Codex CLI, Claude Code y Agy CLI, puedan colaborar en una misma sesión.

No quiero crear nuevos agentes ni modificar internamente esas herramientas. Cada agente será una CLI abierta manualmente por el usuario en una terminal. La nueva herramienta proporcionará infraestructura de comunicación, coordinación, descubrimiento, persistencia y observabilidad.

La implementación se imagina inicialmente como una aplicación CLI en Go, posiblemente acompañada por un servidor MCP local.

## Modelo operativo previsto

1. El humano inicia manualmente cada CLI agéntica en su terminal.
2. Cada CLI tiene configurado un MCP y un skill de colaboración.
3. El agente se registra en una sesión, descubre los demás agentes y puede enviar y recibir mensajes.
4. El humano conversa principalmente con un agente orquestador.
5. El orquestador divide el objetivo y asigna tareas.
6. Los demás agentes pueden aceptar o rechazar tareas, explicar el motivo, investigar, ejecutar cambios, revisar resultados, cuestionar decisiones y proponer otros enfoques.
7. Los agentes pueden comunicarse directamente, crear subtareas y delegarlas sin convertir al orquestador en un cuello de botella.
8. El orquestador coordina e integra, pero no es una autoridad incuestionable.
9. Si el plan o el desempeño del orquestador no es satisfactorio, cualquier agente puede proponer reemplazarlo.
10. El cambio de orquestador se aprueba por mayoría simple de los agentes disponibles. El humano puede iniciar, confirmar o anular un cambio.
11. Todo lo que pase por el protocolo queda registrado para poder reconstruir cómo se investigó, debatió, ejecutó, revisó e iteró la solución.

## Separación conceptual

Quiero separar cuatro capas:

- Infraestructura: identidad, sesiones, presencia, mensajería, entrega, persistencia, tareas y artefactos.
- Política de colaboración: orquestador, contributors, reviewers, debate, delegación y cambio de liderazgo.
- Integración con cada CLI: MCP, skills, comandos locales o adaptadores.
- Workspace y Git: aislamiento, ramas, worktrees, revisión e integración.

El skill y el MCP cumplen funciones distintas:

- El MCP expone operaciones al agente.
- El skill enseña al agente cómo comportarse y usar esas operaciones.
- Si alguna CLI no soporta MCP, podría existir una interfaz equivalente mediante comandos de shell.

## Decisiones provisionales

### Identidad

Cada agente tendría:

- Un UUID inmutable generado por el sistema.
- Un nombre legible, como `codex-main` o `claude-review`.
- El runtime o producto que utiliza.
- Un rol mutable: `coordinator`, `contributor` o `reviewer`.
- Capacidades declaradas.
- Estado de presencia: `available`, `working`, `waiting` o `unreachable`.

El humano elegiría inicialmente el nombre y el rol. Las capacidades podrían actualizarse.

### Mensajería

Inicialmente, el contenido de los mensajes sería texto. Los agentes pueden compartir rutas, archivos, commits, diffs y otros artefactos haciendo referencia a ellos desde el texto.

Aunque el payload sea texto, cada mensaje necesita metadatos estructurados: ID, sesión, remitente, destinatario o broadcast, conversación o thread, tarea relacionada, timestamp, estado de entrega y procesamiento, y período de liderazgo cuando corresponda.

Operaciones preliminares:

- `register_agent`
- `list_agents`
- `send_message`
- `check_inbox`
- `wait_for_messages`
- `acknowledge_message`
- `create_task`
- `assign_task`
- `update_task`
- `create_subtask`
- `publish_artifact`
- `propose_leadership_change`
- `vote_leadership_change`
- `get_session_state`

Los mensajes pueden tener un destinatario específico, pero provisionalmente serían visibles para todos los participantes de la sesión para favorecer observabilidad y cuestionamiento. El humano puede observar todos los mensajes y enviar mensajes, preferiblemente al orquestador.

### Recepción de mensajes

Como la herramienta no inicia ni supervisa las CLI, no puede introducir mágicamente un mensaje en el razonamiento de un agente inactivo. La hipótesis actual es que cada agente siga un ciclo enseñado por el skill:

1. Consultar o esperar mensajes.
2. Aceptar o rechazar tareas.
3. Ejecutar trabajo.
4. Reportar hallazgos, decisiones, bloqueos y objeciones.
5. Publicar resultados o artefactos.
6. Volver a esperar hasta que termine la sesión.

Se contemplan `check_inbox`, no bloqueante, y `wait_for_messages(timeout)`, mediante long polling.

Evalúa si esto es viable en las CLI objetivo, qué límites de tiempo o del tool loop pueden existir y qué alternativas hay. No afirmes compatibilidad sin verificarla. Si tienes acceso a documentación o Internet, valida las capacidades actuales de Codex CLI, Claude Code y Agy CLI usando fuentes oficiales. Si Agy CLI resulta ambiguo, señálalo y formula la pregunta exacta que debe responderse.

### Tareas

Además de mensajes, existirían tareas persistentes. Flujo preliminar:

`proposed → assigned → accepted → working → review_requested → completed`

También existirían `blocked`, `rejected`, `cancelled` y `failed`.

Un agente puede rechazar una tarea con una explicación. Puede crear subtareas y delegarlas dejando registro para el orquestador.

### Liderazgo

El sistema usaría un modelo híbrido:

- Hay un orquestador responsable de coordinar, integrar y determinar cuándo la solución está lista.
- Los demás agentes pueden debatir, cuestionar, pedir revisiones y explorar alternativas.
- Cualquier agente puede proponer un cambio de orquestador indicando motivo y candidato.
- La transferencia voluntaria requiere aceptación del nuevo orquestador.
- Una sustitución disputada se decide por mayoría simple de agentes disponibles.
- El humano puede intervenir.
- El servidor local es la autoridad sobre quién ocupa el rol.
- Cada período de liderazgo queda registrado para que instrucciones antiguas no parezcan vigentes.

Evalúa empates, agentes `unreachable`, votación estratégica, sesiones con sólo dos agentes, mensajes enviados durante el cambio y tareas pertenecientes al orquestador anterior. No quiero diseñar un protocolo bizantino innecesario: todo corre en una máquina controlada por un solo usuario.

### Workspace y Git

La hipótesis actual es usar un Git worktree por agente y otro de integración para el orquestador.

Ventajas esperadas:

- Evitar que dos agentes se pisen archivos.
- Explorar enfoques en paralelo.
- Asociar cambios con ramas y commits.
- Revisar antes de integrar.
- Reconstruir qué agente produjo cada resultado.

Costos y preguntas:

- Los agentes no ven automáticamente cambios sin commit de otros worktrees.
- Hay que decidir quién crea y elimina worktrees y ramas.
- Hay que resolver integración, conflictos y cambios en archivos compartidos.
- Algunas tareas sólo requieren investigación y quizá no necesitan worktree.
- La herramienta no debería iniciar ni supervisar las CLI durante el MVP.

Compara como mínimo: workspace compartido, worktree por agente, worktree por tarea, sólo el orquestador modificando código y una estrategia híbrida según el tipo de tarea.

### Persistencia y observabilidad

Todo evento del protocolo debería persistirse localmente, probablemente en SQLite, idealmente como un log append-only del cual se derive el estado actual.

Quiero poder inspeccionar:

- El objetivo dado por el humano y cómo se dividió.
- Quién aceptó o rechazó cada tarea.
- Qué investigación produjo cada agente.
- Qué alternativas se debatieron y qué objeciones surgieron.
- Qué decisiones se tomaron y por qué.
- Qué artefactos, archivos, diffs o commits se produjeron.
- Qué revisiones e iteraciones ocurrieron.
- Cuándo cambió el orquestador.
- Cómo se llegó al resultado final.

La herramienta sólo puede registrar lo que los agentes publiquen mediante el protocolo. No captura automáticamente toda la conversación privada de la CLI ni cada comando ejecutado. Evalúa cómo obtener observabilidad útil sin depender del razonamiento privado del modelo ni convertir el sistema en vigilancia exhaustiva.

### Membresía y presencia

Para el MVP, el conjunto de agentes se prepara al inicio y permanece disponible durante la sesión. No necesitamos una experiencia centrada en agentes entrando y saliendo continuamente.

Aun así, una terminal puede cerrarse, fallar o quedar bloqueada. El sistema debe poder marcar un agente como `unreachable` y permitir reasignar sus tareas. Evalúa heartbeats, leases, timeouts y detección de presencia teniendo en cuenta que el servidor no controla los procesos.

## Demostración exitosa del MVP

La prueba se realizará probablemente sobre un problema real de software:

1. Se abren y registran al menos tres CLI agénticas.
2. Se descubren entre sí.
3. El humano entrega un objetivo al orquestador hablando con su CLI.
4. El orquestador divide el trabajo en investigación, implementación y revisión.
5. Los agentes intercambian hallazgos y cuestionan al menos una decisión.
6. Uno o más agentes producen cambios aislados.
7. Otro agente revisa los cambios y solicita al menos una corrección relevante.
8. Se itera sobre la solución.
9. El orquestador integra y verifica el resultado.
10. Si corresponde, se prueba un cambio de enfoque o de orquestador.
11. La herramienta muestra un timeline completo y comprensible de mensajes, tareas, decisiones, revisiones, artefactos y cambios de liderazgo.

El éxito no consiste solamente en resolver la tarea. También debe poder observarse cómo colaboraron los agentes y si la colaboración produjo una solución mejor.

## Lo que necesito de ti

Trabaja primero como crítico y después como arquitecto. No escribas código todavía.

Entrega, en este orden:

1. Una reformulación breve y precisa del problema.
2. Las fortalezas de la idea.
3. Los supuestos más débiles, contradicciones y riesgos técnicos o de producto.
4. Las preguntas críticas aún abiertas, priorizadas, explicando qué decisión depende de cada una.
5. Dos o tres arquitecturas alternativas, incluida una deliberadamente mínima.
6. Una recomendación de arquitectura para el MVP y su justificación.
7. Los límites explícitos del MVP y lo que debe quedar fuera.
8. Un modelo de dominio inicial con `session`, `agent`, `message`, `task`, `artifact`, `leadership term`, `vote` y `event`; incluye relaciones e invariantes.
9. El protocolo mínimo: operaciones, envelopes, estados, acknowledgements, errores, idempotencia, ordenamiento y semántica de entrega.
10. Una propuesta concreta para recibir mensajes y mantener activo el tool loop en las CLI.
11. Una estrategia para workspaces, worktrees, ramas, commits e integración.
12. El modelo de liderazgo, debate, rechazo de tareas, revisión y cambio de orquestador, incluyendo empates y agentes no disponibles.
13. La arquitectura interna de la aplicación Go: componentes, límites entre paquetes, persistencia, MCP, CLI y observabilidad.
14. Un esquema inicial de SQLite o al menos sus tablas y campos principales.
15. Consideraciones de seguridad local: aislamiento entre sesiones, permisos, ejecución de comandos, exposición de rutas y confianza.
16. Un plan incremental de implementación dividido en hitos pequeños con resultados demostrables.
17. Criterios de aceptación verificables para el MVP.
18. Un plan de pruebas con escenarios normales, concurrencia, fallos y recuperación.
19. Un guion detallado para demostrarlo sobre un problema real.
20. Métricas o evidencia para decidir si la colaboración multiagente mejora realmente los resultados.
21. Un borrador del índice de un RFC posterior.

Usa tablas o diagramas Mermaid sólo cuando aclaren relaciones complejas. Señala qué partes recomiendas y cuáles siguen siendo experimentos. Favorece el diseño más pequeño que permita aprender sobre la hipótesis central: si varias CLI agénticas pueden coordinarse, cuestionarse y producir un resultado observable y mejor mediante una infraestructura local compartida.
