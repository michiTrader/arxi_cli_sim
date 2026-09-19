# Escenas de prueba — el ejercicio que valida el formato

El plan dice: antes de programar el motor, escribir a mano las escenas de
referencia con el formato propuesto para el usuario. Si alguna escena no cabe, el
formato está mal y lo cambiamos aquí, en papel, gratis.

Este documento es ese ejercicio. Formato y primitivas son **candidatos, no
contratos**; cada escena va seguida de las preguntas que expone.

## Formato candidato: JSON (por ahora)

- El que edita en caliente es en buena parte un agente → JSON es el lenguaje que
  mejor escribe sin alucinar, y hay parsers en cualquier lenguaje de extensión.
- El humano edita por `/ui` y por presets aplicados; el archivo crudo es la
  serialización, no la interfaz principal.
- Con `offset → file:line` en el validador, los errores siguen llevando `file:line`.
- Alternativas descartadas por ahora: TOML (árboles anidados incómodos), DSL de
  indentación propia (hay que escribir parser, validador y errores a mano; caro y
  sin ganar expresividad). Volveremos a mirar esto cuando el conjunto de
  propiedades se congele.

## Primitivas candidatas (v0)

Contenedores: `stack` (columna), `row`, `box` (frame opcional), `overlay`
(anclado, flota sobre todo).
Contenido: `text`, `markdown`, `input`, `spinner`, `marquee` (texto que rueda hacia
la izquierda, el thinking de arxi-sim), `list` (filtrable, con teclas al pie).
Común a todos: `id`, `bind` (campo del estado doblado), `when` (visibilidad
condicional), `style` (token de estilo), `grow`/`weight` (reparto de espacio),
`children`.

Estilos: tokens nombrados con definición abierta (el usuario puede definir
tokens nuevos — vocabulario *abierto*, al revés que arxi-sim; la validación
comprueba referencias, no inventario).

## Escena 1 — CRUDA (escribir y responder, nada más)

```json
{ "root": { "type": "stack", "children": [
  { "id": "chat",   "type": "markdown", "bind": "chat.history", "grow": 1 },
  { "id": "prompt", "type": "input",    "bind": "user.input", "placeholder": "> " }
]}}
```

```
Hola, en qué te ayudo?
> quiero que revises el módulo de pagos_
```

¿Qué prueba?: que la escena mínima es legal y arranca; que `when`/frames/overlays
son opcionales de verdad.
Preguntas que expone: ¿el markdown *es* un nodo o es el render natural de `text`
con bind a mensajes? (Asumí nodo propio.) ¿Dónde vive el cursor del input si la
escena lo pinta todo? (Debe vivir en el nodo `input`.)

## Escena 2 — SOBRIA (el look fx, con las reglas del plan)

```json
{ "root": { "type": "stack", "children": [
  { "type": "text", "style": "header",
    "text": "Δr×i v0.1.0 · Run /help for commands" },

  { "id": "chat", "type": "markdown", "bind": "chat.history", "grow": 1 },

  { "id": "thinking", "type": "marquee", "when": "agent.working",
    "bind": "thinking.text", "prefix": { "text": "• Thinking · ", "style": "dim" },
    "suffix": { "bind": "usage.delta", "style": "dim" } },

  { "id": "prompt", "type": "input", "bind": "user.input",
    "prefix": "┃ ", "placeholder": "ask anything, or / for commands" },

  { "id": "menu", "type": "overlay", "anchor": "bottom", "when": "slash.active",
    "children": [
      { "type": "rule" },
      { "id": "cmds", "type": "list", "bind": "slash.matches",
        "filter_by": "typed", "categories": ["All","General","Session","Account",
        "Model","Appearance","Security","Workspace","Media","Extensions","Product"],
        "count": true },
      { "type": "text", "style": "dim",
        "text": "↑↓ navigate · tab category · enter open · esc close" },
      { "type": "rule" } ] },

  { "id": "status", "type": "row", "style": "dim", "children": [
    { "type": "text", "bind": "agent.mode" },
    { "type": "text", "text": " · " },
    { "type": "text", "bind": "model.name" },
    { "type": "text", "text": " · ⚡︎" } ] }
]}}
```

```
Δr×i v0.1.0 · Run /help for commands
┃ hi

● Usé tres llamadas: listar, leer y verificar.
  8s (↑8 ↓444)
• Thinking (3s) · el usuario pide ver cómo genero códi   ← rueda en gris
┃ /
──────────────────────────────────────────────────────────
Commands 35   All [General] Session Account Model …
  /help       show available slash commands
  /clear      start a fresh conversation while keeping…
──────────────────────────────────────────────────────────
↑↓ navigate · tab category · enter open · esc close

auto · kimi-k3 · ⚡︎
```

¿Qué prueba?: el rango medio completo —condicionales, overlay de menú con
categorías y contador, marquee con prefijo/sufijo en gris, pie segmentado, el
`┃` como prefijo del input (¡no es un nodo!), la detección claro/oscuro heredada
de fx (sería un comportamiento del host, no de la escena).
Preguntas que expone:
1. **El marquee con prefix y suffix** — ¿el nodo `marquee` rueda solo el texto
   bindeado y el prefijo/sufijo quedan fijos? En el ejemplo sí; hay que decirlo.
2. **El menú es un overlay anclado a `bottom`** — ¿o es "una fila que aparece en
   el stack"? El overlay gana porque el transcript no debe saltar; decisión.
3. **`(↑8 ↓444)` es un bind compuesto** — `usage.delta` no es un campo del fold,
   es una cuenta derivada. Falta decir cómo se declaran los derivados del estado,
   o si el motor expone campos calculados. (Problema real: los binds no pueden ser
   solo espejos del evento crudo.)
4. **`count: true` + `categories` en `list`** — ¿es una primitiva `list` o es el
   slash-menú propio del host? Si cada app tiene listas así, conviene que `list`
   las traiga; si no, mejor un `stack` de `text` generado por el host.

## Escena 3 — MÁXIMA (escena 2 + panel lateral + banner + overlay de tokens)

```json
{ "root": { "type": "stack", "children": [
  { "id": "banner", "type": "box", "style": "banner",
    "children": [ { "type": "text", "text": "Δr×i OS v1.0 ── Sesión de Trading" } ] },

  { "type": "row", "grow": 1, "children": [
    { "type": "stack", "weight": 3, "children": [
      { "id": "chat", "type": "markdown", "bind": "chat.history", "grow": 1 },
      { "id": "thinking", "type": "marquee", "when": "agent.working",
        "bind": "thinking.text",
        "prefix": { "text": "• Thinking · ", "style": "dim" } } ] },
    { "id": "tasks", "type": "box", "weight": 1, "title": "Tareas",
      "border": "single",
      "children": [ { "type": "list", "bind": "agent.todos" } ] } ] },

  { "id": "prompt", "type": "input", "prefix": "❯ ", "grow": 0 },

  { "id": "tokens", "type": "overlay", "anchor": "top-right",
    "border": { "shape": "single", "style": "warn" },
    "children": [ { "type": "text", "bind": "session.tokens_used" } ] }
]}}
```

```
Δr×i OS v1.0 ── Sesión de Trading              ╭ tokens ╮
┃ · transcripción que ocupa el 75% ancho       │ 41.2k  │
┃                                              ╰────────╯
┃ Tareas pendientes → panel derecho 25%
┃ /quiero que revises el módulo_
```

¿Qué prueba?: pesada de verdad —anidado mixto (`stack` dentro de `row` dentro de
`stack`), reparto por peso, overlay montado por *otro* (una extensión vía B, o el
usuario por `/ui add`), y coexistencia de los dos overlays (menú + tokens) con
reglas de foco.
Preguntas que expone:
5. **Dos overlays visibles a la vez** — ¿quién tiene el teclado? Falta un modelo
   de foco entre overlays (apilados por orden de aparición, esc cierra el de
   arriba: regla provisional).
6. **`overlay` + `weight` + `grow: 0`** — el reparto de espacio entre contenido
   fijo (input, banner, status) y elástico (chat) tiene que ser determinista; el
   chat se contrae primero, y hay que decir el orden exacto de contracción.
7. **Anclar un overlay con contenido que crece** — el de tokens nunca baja de 1
   fila porque el host sabe medir; el formato debería declarar `min-width`.

## Escena 4 — ANIMACIONES (brillo de foco, marquee con ritmo, reveal escalonado)

```json
{ "id": "prompt", "type": "input", "prefix": "┃ ",
  "focus_glow": { "style": "bright", "period": "1.2s", "shape": "pulse" },
  "transition": { "appear": "fade", "duration": "180ms" } }
```
```json
{ "id": "thinking", "type": "marquee",
  "scroll": { "speed": "40cps", "pause_when": "no_new_text" } }
```
```json
{ "id": "tasks", "type": "box",
  "reveal": { "when": "todos.count>0", "shape": "slide_left" },
  "children": [ { "type": "list", "bind": "agent.todos",
                  "enter": { "row": "fade", "stagger": "40ms" } } ] } }
```
```
┃ ❯_                     ← el ┃ pulsa suavemente mientras el input tiene foco
• Thinking · …            ← rueda a 40 cps, se pausa si no llega texto nuevo
  ▸ tarea nueva           ← las filas del panel entran escalonadas
```

¿Qué prueba?: que las animaciones son **props de nodo con timing del reloj del
host**, no código. Decisiones expuestas:
8. **¿El timing vive en la escena o en tokens globales `[anim]` que la escena
   referencia?** Provisional: ambos — token global por defecto, override por nodo.
9. **¿`stagger` sobre filas que inyecta el host es mérito de la escena o del
   host?** Provisional: el host informa "fila nueva"; la escena declara la
   animación. Separación forma/contenido intacta.

## Escena 5 — CONFIGURACIÓN (la pantalla /config como escena, no como código)

```json
{ "id": "config", "type": "overlay", "anchor": "full", "when": "surface==config",
  "children": [
  { "type": "row", "grow": 1, "children": [
    { "type": "list", "bind": "settings.categories", "style": "nav" },
    { "type": "stack", "grow": 1, "children": [
      { "type": "list", "bind": "settings.visible",
        "row_template": { "type": "row", "children": [
          { "type": "text", "bind": "row.label", "width": 20 },
          { "type": "switch", "bind": "row.value", "when": "row.kind==bool" },
          { "type": "input",  "bind": "row.value", "when": "row.kind==string" } ] } },
      { "type": "text", "style": "dim", "bind": "settings.provenance" } ] } ] },
  { "type": "text", "style": "dim",
    "text": "↑↓ navigate · enter edit · esc back · escribir filtra" } ] }
```

¿Qué prueba?: **dogfooding total** — en arxi-sim `/config` son 730 líneas de Go;
aquí es la escena del modo config, escrita con el formato del usuario. Decisiones:
10. **`row_template` con binds relativos** (`row.label` se resuelve contra el
    ítem, no contra el estado global). El formato necesita distinguir bind
    absoluto de bind relativo.
11. **¿`switch`/`slider` son primitivas o un `input` tipado?** Provisional:
    primitivas propias — el corazón del modo settings las pide. **(Única adición
    al vocabulario v0 que surgió del ejercicio.)**
12. **¿`overlay anchor:"full"` es legal?** La escena 3 usaba anclas de esquina;
    config necesita cubrir todo. A diferencia del `full` no-componible de
    arxi-sim, aquí el motor debe poder decir "esta superficie es de este nodo".

## Escena 6 — PLUGIN AVANZADO COMPARTIDO POR LINK (ticker con su propio binario)

La historia: alguien publicó `tick` (binario Zig/Go estático, autocontenido). Otro
lo instala pasándole el link a arxi:

```
❯ /ui plugin add https://github.com/ana/tick/releases/latest/download/tick
```

El plugin trae un `manifest.json` y **declara su UI como fragmentos montados**:

```json
{ "name": "tick", "version": "1.0.0", "executable": "tick",
  "capabilities": ["events.subscribe", "panel.render"],
  "consent_required": true,
  "mounts": {
    "ticker": { "type": "overlay", "anchor": "top-right", "when": "tick.open",
      "children": [ { "type": "row", "children": [
        { "type": "sparkline", "bind": "tick.history", "width": 20 },
        { "type": "text", "bind": "tick.price", "style": "tick.price" },
        { "type": "text", "bind": "tick.delta",  "style": "tick.delta" } ] } ] } } }
```

El proceso emite frames NDJSON (`{"tick":{"price":41.2,"delta":0.3}}`) y el motor
los sube al namespace de binds `tick.*`. **El `sparkline` que usa es primitiva del
sistema madre**: el plugin compone nuestros nodos, no trae código de render.

¿Qué prueba?: que un tercero publica UI completa (estructura + estilo + datos)
para instalar por link sin romper la regla de un-comando: el artefacto es **su**
binario estático, no una dependencia del sistema del usuario. Decisiones:
13. **El overlay del plugin ¿pide foco propio y recibe teclado?** Provisional:
    sí, con la regla de pila (pregunta 5).
14. **¿Y si el plugin quiere un render que no es ninguno de nuestros nodos?**
    El techo honesto: necesita el hueco wazero de la Fase 4 (ADR). Que esto
    aparezca aquí es un resultado, no un fallo: el plan ya diseñó la salida.
15. **Confianza**: `/ui plugin add <url>` dispara el gate de consentimiento del
    plan madre (digest, capacidades, nombre/version exactos) — el link no es una
    excepción: descargar y confiar son dos pasos, correr y otorgar capacidades
    son un tercero.

## Escena 7 — COMUNIDAD (listar, previsualizar e instalar presets desde la TUI)

```json
{ "id": "community", "type": "overlay", "anchor": "full", "when": "surface==community",
  "children": [
    { "type": "row", "grow": 1, "children": [
      { "type": "list", "bind": "community.entries",
        "row_template": { "type": "row", "children": [
          { "type": "text", "bind": "e.name", "width": 22 },
          { "type": "text", "bind": "e.author", "style": "dim", "width": 12 },
          { "type": "text", "bind": "e.kind", "style": "tag" },
          { "type": "spinner", "bind": "e.state", "when": "e.busy" } ] } },
      { "type": "markdown", "bind": "community.preview", "width": 40, "grow": 0 } ] },
    { "type": "input", "prefix": "❯ buscar: ", "bind": "community.filter" },
    { "type": "text", "style": "dim",
      "text": "enter preview · i instalar · esc cerrar" } ] }
```
```
  minimal-dark   @ana      preset     ┊ Vista previa:
▸ trading-grid   @luis     preset     ┊   escenas 2/3 combinadas
  tick           @ana      plugin     ┊   (rendersin estado real)
  ❯ buscar: trad                       ┊ [i] instalar
```

¿Qué prueba?: que **el instalador de comunidad es él mismo una escena** (nada de
la pantalla de instalar está codificada) y que puede previsualizar una escena
ajena renderizándola con los binds insatisfechos. Decisiones:
16. **Previsualizar exige un modo de render sin estado**: el motor pinta
    placeholders donde el bind no resuelve, en vez de reventar. (Esto además
    sirve para validar escenas del usuario en un sandbox visual.)
17. **El índice de comunidad es un formato, no un servicio**: un JSON en un repo
    (nombre, autor, tipo, url, digest, manifiesto). Cero infraestructura.

## Escena 8 — BOTONES INTERACTIVOS (chips de respuesta rápida y aprobación)

Un botón es un nodo con `on_press` que invoca una acción del vocabulario del host.
Clicable con mouse, alcanzable con tab.

```json
{ "id": "quick-reply", "type": "row", "when": "chat.waiting_for_choice",
  "children": [
    { "type": "button", "label": "Sí, hazlo",   "on_press": "answer:approve" },
    { "type": "button", "label": "No, explica", "on_press": "answer:reject" },
    { "type": "button", "label": "Otro…",       "on_press": "focus:prompt" } ] }
```
```
┃ ¿refactorizo el módulo de pagos?
  [ Sí, hazlo ]  [ No, explica ]  [ Otro… ]     ← tab recorre, enter/clic activan
```

Decisiones expuestas:
18. **El vocabulario de `on_press` es un conjunto cerrado** — `cmd:/<slash>`,
    `ext:<plugin>:<accion>`, `answer:<kind>`, `focus:<nodo>` — ampliable solo por
    acciones registradas, igual que los ActionKeys de arxi-sim pero con espacio de
    nombres para plugins.
19. **El orden de tabulación deriva del orden de escena**; el `input` puede
    negarse (`tab: false`) para no perder el flujo de escritura.

## Escena 9 — SUBAGENTES DEBAJO DEL INPUT (el equipo, como escena)

```json
{ "id": "squad", "type": "list", "bind": "team.members", "grow": 0,
  "row_template": { "type": "row", "on_press": "cmd:/agent {m.id}", "children": [
    { "type": "spinner", "bind": "m.tick", "when": "m.state==working" },
    { "type": "text", "bind": "m.glyph",   "when": "m.state!=working" },
    { "type": "text", "bind": "m.name", "style": "member.name", "width": 10 },
    { "type": "text", "bind": "m.now",  "style": "dim" } ] } }
```
```
┃ quiero el informe de trading listo
❯ _
  ◉ scout      leyendo pkg/auth…
  ⧗ builder    esperando a scout
  ○ reviewer
```

¿Qué prueba?: el monitor de equipo de arxi-sim (hoy: Go puro, `internal/app/team.go`)
se expresa como documento; cada fila es clicable y lleva a ese agente. Decide:
20. **Interpolación de binds en las acciones** (`{m.id}` dentro de `on_press` se
    resuelve contra el ítem de la fila — mismo bind relativo de la pregunta 10).

## Escena 10 — DASHBOARD (grid 2×2 con maximizar por clic)

Sin primitiva `grid`: anidado `row`/`column` con `weight`, que es lo que ya hay.

```json
{ "type": "column", "grow": 1, "children": [
  { "type": "row", "grow": 1, "children": [
    { "id": "p-chat", "type": "box", "weight": 1, "title": "Chat",
      "when": "ui.max=='' || ui.max=='chat'", "on_press": "cmd:/max chat",
      "children": [ { "type": "markdown", "bind": "chat.history" } ] },
    { "id": "p-cost", "type": "box", "weight": 1, "title": "Costo",
      "when": "ui.max=='' || ui.max=='cost'", "on_press": "cmd:/max cost",
      "children": [ { "type": "sparkline", "bind": "cost.series", "width": 30 },
                    { "type": "text", "bind": "cost.today" } ] } ] },
  { "type": "row", "grow": 1, "children": [
    { "id": "p-tasks", "type": "box", "weight": 1, "title": "Tareas",
      "when": "ui.max=='' || ui.max=='tasks'", "on_press": "cmd:/max tasks",
      "children": [ { "type": "list", "bind": "agent.todos" } ] },
    { "id": "p-log", "type": "box", "weight": 1, "title": "Trayecto",
      "when": "ui.max=='' || ui.max=='log'", "on_press": "cmd:/max log",
      "children": [ { "type": "text", "bind": "log.tail", "scroll": true } ] } ] } ] }
```
```
╭ Chat ────────────────╮╭ Costo ──────────── [max] ╮
│ ┃ informe listo…     ││ ▁▂▅▇▃▂▄  $1.42 hoy
├──────────────────────┤├──────────────────────────┤
│ Tareas ▸ 3 abiertas  ││ Trayecto: scout→build→…
╰──────────────────────╯╰──────────────────────────╯
```

¿Qué prueba?: paneles por peso + clic para maximizar, todo como dato. Decide:
21. **`ui.*` es el espacio de estado de la interfaz que posee el host** (`ui.focus`,
    `ui.max`, `ui.surface`); las escenas lo leen con `when` y lo escriben solo vía
    `cmd:` registrado. La escena no mutada nunca toca `ui.*` directamente.

## Escena 11 — BANNER ANIMADO E INTERACTIVO

```json
{ "id": "promo", "type": "box", "style": "banner",
  "when": "session.new_milestone",
  "shine":   { "period": "5s", "style": "banner.shine" },
  "marquee": { "speed": "20cps" },
  "on_press": "cmd:/milestones",
  "children": [ { "type": "text", "bind": "session.milestone_line" } ] }
```
```
╭ ✦ ganaste 200 créditos esta semana · mira tus hitos ✦ ╮  ← la luz cruza cada 5s
```

¿Qué prueba?: **animación + interacción + condicional en un mismo nodo sin ninguna
primitiva nueva** — es la composición de las escenas 2, 4 y 8. Decide:
22. El hit-test del `on_press` sobre un nodo animado usa las celdas finales del
    frame (el motor resuelve el choque marquee/cursor; no es cosa de la escena).

## Veredicto provisional (tras las 11 escenas)

- Once escenas caben con ~13 primitivas: las 11 de v0 más `switch`/`slider`
  (escena 5) y `button` (escena 8). `grid` no hizo falta (escena 10). Nada de
  recompilar el binario madre en ninguna escena.
- Las decisiones de formato pasaron de 7 a 22. Ninguna cambió la arquitectura:
  todas fueron vocabulario o reglas del host — exactamente lo que debía probar el
  ejercicio.
- El techo honesto sigue donde estaba: un render que no sea ninguno de nuestros
  nodos, o una animación que no pueda expresarse como prop con reloj del host,
  quiere el hueco wazero (Fase 4, ADR).

## Pregunta 23 (firmada): cuánto del pipeline se expone a los plugins

El núcleo arxi (`D:/projects/arxi`) ya tiene por dentro las cuatro puertas:
`internal/tool`+`toolrun` (tools), `internal/provider` (providers/HTTP),
`internal/blueprint` (system prompt, modelo, grants de tools por agente) y
`host/v1` (capacidades a procesos externos: Submit/Wait/Approve/Answer/
Subscribe). Lo decidido era cuáles se abren hacia la TUI y en qué orden.
Firmado (2026-09-14):

- **A — solo UI**: no basta. Es el techo de arxi-sim.
- **B — + tools del agente**: un plugin instalado por link registra un tool
  (schema + descripción) que el agente pasa a poder llamar; corre como proceso
  propio, si se cuelga el run no muere. **Primero.**
- **C — + hooks de comportamiento**: gate de tool calls, ajuste del system
  prompt, compactación propia. **Segundo**, con orden de pila por identidad.
- **D — + providers**: modelos nuevos al árbol por link (`arxi provider add`
  ya existe por dentro; es exponerlo). **Tercero**, casi gratis.
- **Autoextensión**: el agente usa exactamente las puertas B/C/D que el usuario
  le haya abierto — y solo cuando el usuario lo pide. Sobre las tres.

**Regla de UX firmada por el dueño del producto: el gate es la excepción, no el
ritmo.** El consentimiento se pide al **otorgar un poder** (cambio persistente:
"este tool queda registrado", "este hook queda montado"), nunca en cada uso de
un poder ya concedido. Cuando el usuario manda al agente autoextenderse, el
agente hace el cambio y muestra el **diff de lo que cambió** con atribución en
el log — no abre un menú bloqueante por paso. Dos patrones se heredan de
arxi-sim: *remember* (la respuesta a un gate se guarda para la misma identidad
de manifiesto+digest) y el log atribuido como auditoría posterior (`run why`
dice quién pidió qué). gobernable no significa interrogado: significa que
después siempre se sabe.

## Cómo sigue el ejercicio (para el dueño del producto)

1. Responde las 17 preguntas con un "sí, sería así" (o un veto con motivo) por
   cada una; eso congela el vocabulario v0.
2. Escribe una octava escena que *tú* quieras y que no esté aquí, lo más rara
   posible (grid multi-agente, dashboard, pantalla de inicio sin input). Si cabe
   con las primitivas, el diseño es honesto.
3. Con eso, la Fase 0 arranca con el formato cerrado y siete (u ocho) escenas de
   oro listas para ser los primeros goldens del motor.
