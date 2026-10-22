# Estado de implementación y puerta de viabilidad

## Resultado actual

**Condición de avance aprobada por el usuario el 2026-10-21**, mediante la
confirmación «esa condicion quedo aprobada». Se habilita la implementación de
las fases siguientes. Esta aprobación es un dato aportado por el usuario;
no convierte las pruebas simuladas que se detallan abajo en una certificación
observada por este repositorio. Los artefactos de la ejecución real y su
configuración exacta no fueron adjuntados aquí.

| Cliente local observado | Versión obtenida con `--version` | Configuración real de prueba | Continuidad |
| --- | --- | --- | --- |
| Codex CLI | 0.163.0 | Pendiente | No certificada |
| Claude Code | 2.1.92 | Pendiente | No certificada |
| Antigravity | No encontrado en PATH | Pendiente | No evaluada |

Fecha del entorno de implementación: 2026-10-21. Las URL oficiales están en el
ejemplo de participantes. Volver a capturar versiones y configuración al ejecutar
la prueba: no trasladar la certificación entre versiones o modelos por inferencia.

## Evidencia automatizada

- Pruebas Go del buzón: reentrega, idempotencia bajo concurrencia, broadcast,
  aislamiento entre credenciales y sesiones, ACK duplicado, cancelación, timeout,
  carreras envío/espera y cierre que despierta a quienes esperan.
- Prueba del SDK oficial MCP conectado al daemon por socket Unix.
- Prueba del binario y tres puentes MCP `stdio` con clientes **simulados**, incluyendo
  reconexión del puente conservando el buzón, timeout vacío y cierre.
- Pruebas Python del evaluador: una ejecución corta, respuesta equivocada,
  falta de ACK, inactividad al final o metadatos sin completar impiden aprobar.

Los tests de transporte no ejecutan modelos ni prueban continuidad real. `make test`
compila el binario, ejecuta Go con `-race` y las pruebas Python.

Verificación realizada en macOS arm64 con Go 1.27.3 y Python 3.9:
`go test -race ./...`, `go vet ./...`, compilación del binario y seis pruebas
Python aprobadas. El guion completo recorrió dos rondas con tres clientes MCP
simulados durante cuatro segundos; verificó respuestas, ACK y vuelta a espera,
y emitió correctamente `not_passed`. El validador de `skill-creator` aprobó el
skill. Las pruebas de socket necesitaron ejecución fuera del sandbox que bloquea
`bind`; no se ejecutaron CLI de modelos ni se modificó su configuración personal.

## Evidencia necesaria para decidir el avance

Usar el procedimiento de README. Conservar `report.json`, `events.json`, comandos
de arranque sin secretos, configuración exacta y evidencia de las tres terminales.
Registrar explícitamente:

- Inicio y fin, producto, URL oficial, versión, modelo, permisos y timeout efectivo.
- Tres agentes, dos productos distintos, 60 minutos y diez ciclos de trabajo/espera
  por participante, incluyendo esperas que vencen vacías.
- Número de intervenciones para reactivar turnos: debe ser cero.
- Compactación, interrupciones, errores y comportamiento al cerrar la sesión.
- Adaptador usado, si existe, con su configuración exacta y documentación oficial.

El evaluador sólo puede declarar que el protocolo satisface comprobaciones.
La certificación requiere revisar la evidencia externa: no puede deducir ausencia
de intervención humana a partir de llamadas MCP.

## Alcance del prototipo original (histórico)

`internal/probe` contiene el experimento volátil, ahora separado de los paquetes
de dominio y persistencia. `internal/transport` contiene HTTP por socket y MCP; el
puente sólo transforma argumentos y credenciales. No ejecuta shell ni comandos Git.
El administrador crea credenciales aleatorias; el daemon recibe sus hashes.
Los archivos de cliente contienen los secretos necesarios para reconectar.

La fase 0 original no prometía persistencia, reconstrucción, revocación, leases, vigilancia de
conectividad de puentes, transacciones de dominio ni recuperación del servicio.
En ese prototipo las evidencias debían exportarse antes de detener el servicio.
La implementación actual sí conserva estado tras reinicio y revocaciones;
ver [fases 1–2](phases-1-2.md). El registro histórico de pruebas de esta página
corresponde a la fase 0, no a la nueva implementación.

## Hitos posteriores, habilitados por la aprobación del usuario

1. Implementado: núcleo Go con SQLite, sesiones, identidades revocables, eventos
   y estado atómicos, CLI de sesión/agentes y recuperación tras reinicio.
2. Implementado: mensajería persistente, ACK, idempotencia transaccional, long
   polling y pruebas de respuesta perdida después de persistir.
3. Tareas, árbol de subtareas, versiones de asignación, artefactos, revisión por
   otro agente e invalidación de aprobación al cambiar evidencia.
4. Worktree estable por agente e integración separada, referencias verificables,
   cherry-pick de commits aprobados y registro de conflictos.
5. Propuestas, electorado congelado, aceptación de candidatura, mayoría absoluta,
   términos monotónicos y exclusión de integración con traspaso explícito.
6. Timeline completo, exportación, demo de bug real y evaluación individual frente
   a colaboración en cinco tareas, registrando costos ausentes sin inventarlos.

Para futuras configuraciones que fallen, evaluar sólo adaptadores oficiales
compatibles con la prohibición de iniciar/supervisar CLI o automatizar terminales.
