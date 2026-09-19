# Plan: arxi tui

Documento semilla del proyecto. No es un compromiso de fechas.

## La idea en una frase

Una TUI donde **la interfaz es un documento de datos** que la propia instancia en
ejecución puede reescribir en caliente, dirigida por el usuario. No "un tema" ni "una
extensión a pantalla completa": la interfaz entera, viva y direccionable.

## El objetivo es un espectro, no un look

El estilo de *fx* es una referencia de simplicidad y espacio en blanco, no el
objetivo. Lo que se persigue es que el **mismo motor** pueda vestirse en cualquier
punto de este eje, y que el usuario se mueva por él sin tocar el binario:

| Extremo | Escena | Qué se ve |
|---|---|---|
| **Crudo** | un `input` y un `text` | escribir, respuesta en texto plano o markdown mínimo; nada más |
| **Base (arxi-sim hoy)** | el chrome actual como escena por defecto | transcript, barra de input, filas de estado, paneles; ya familiar |
| **Estilo fx** | un *preset* sobre la base | barra con borde redondeado, shine, mucho aire, pocos adornos |
| **Máximo** | paneles, widgets, overlays, banners, indicadores | chrome rico, montado por partes, extensiones por todas partes |

Las tres cosas que hacen posible el espectro, y que arxi-sim no tenía:

1. **Todo es el mismo tipo de dato.** La escena por defecto *es* el mismo formato que
   el usuario edita: no hay un camino privilegiado para "lo de fábrica".
2. **Se puede quitar tanto como poner.** Bajar a crudo no es "desactivar features",
   es no tener esos nodos: la escena mínima es solo dos nodos.
3. **Los presets son escenas, no código.** "Estilo fx", "denso", "solo chat" son
   documentos de escena que se aplican, se mezclan y se editan como cualquier otro.

## Lo que arxi-sim enseñó, y que aquí se corrige

Lo que funcionó y se reutiliza:

- El **fold puro** de eventos: nada escribe estado directamente, todo propone.
- Los **goldens**: la salida por defecto se fija byte a byte y cualquier cambio de
  píxeles es un evento de revisión, no ruido.
- El **modelo de celda/Frame** y el backend de terminal: dibujar rejillas de celdas
  con estilos y sin desbordar es un problema ya resuelto.
- La disciplina de errores `file:line:` y de "sin extensión configurada, byte a byte
  idéntico".

Lo que ahogó el resultado y aquí **no** se repite:

- Vocabularios cerrados con dueño: cada clave de estilo, cada glifo, cada nombre de
  widget y cada slot es una lista que no se puede ampliar. Es la diferencia entre
  "puedes elegir dentro de lo que yo decidí" y "puedes decidir".
- La composición era fija: `[layout]` solo **reordena y veta** filas existentes; no
  crea widgets, no mueve entre slots, no admite duplicados.
- Las extensiones solo podían pintar **un panel a pantalla completa**. No hay
  `overlay.render`, no se puede montar una fila junto al input, no hay banners.
- El slot `full` existe pero está declarado no componible.

En una frase: arxi-sim congeló el *cómo* para poder gobernar el *qué*; arxi tui abre
el *cómo* y confía en el usuario.

## Decisión central: la interfaz es datos, no código compilado

Tres pilares, en este orden. Cada uno vale por sí solo.

### 1. El árbol de escena (*scene tree*)

La interfaz es un documento: un árbol de nodos tipados con props y estilos.

- Nodos base: `row`, `column`, `box`, `text`, `input`, `spacer`, `list`, `overlay`,
  `spinner`. Abiertos a más: añadir un tipo de nodo no es un cambio de "vocabulario
  cerrado", es una capacidad nueva.
- **Todo nodo tiene un id estable y dirección absoluta** (una ruta tipo
  `root/below_input/status`). Eso es lo que hace que "añade una línea encima del
  input" sea un *patch*, no un fork.
- El chrome por defecto es simplemente el escena por defecto: el mismo formato que
  el usuario edita. No hay un camino privilegiado.

### 2. Reescritura en vivo (*hot reload*)

- La instancia observa su documento de escena. Al cambiar, valida y re-renderiza.
- Un patch inválido **nunca tumba la sesión**: error con `file:line`, se conserva la
  última escena buena en pantalla y se muestra el error donde no estorbe.
- El render es un diff de escena → celdas. El estado (transcripción, foco, scroll)
  no vive en la escena; la escena dice *cómo* se ve, no *qué* hay.

### 3. Una superficie de control para la instancia viva

Aquí está el "construir desde dentro". Dos niveles, y conviven:

- **Determinista (comandos).** `/ui add node below_input …`, `/ui move …`,
  `/ui style …`. Predecible, validado, testeable.
- **Dirigido por el usuario/agente (documento).** El usuario pide "pon un indicador
  encima de la barra y un popup flotante"; la instancia escribe el parche en su
  documento y se ve al instante. El agente no pinta píxeles: **propone escena**,
  igual que propone eventos.

Esto es lo que en arxi-sim era imposible: allí el usuario elegía *valores* de una
lista; aquí el usuario **añade estructura**.

## Extensiones: el modelo que sí queremos

El error de arxi-sim fue limitar la extensión a "un panel a pantalla completa con
filas y spans de rol cerrado". El modelo de arxi tui:

- Una extensión es **un proveedor de fragmentos de escena**. Devuelve un subárbol de
  nodos y dice *dónde* montarlo (por id de nodo), no "pinta mi ventana".
- **Overlays, banners y filas junto al input son nodos**, no capacidades prohibidas.
  Flotar sobre el chat es tan normal como apilar.
- **Dos niveles de extensión**, elegibles por separado:
  1. *Declarativo*: escena + tokens de estilo. Cero código, cero riesgo, cero
     dependencias. Cubre "cambiar la carga", "otro borde", "un banner".
  2. *Con comportamiento*: un proceso (NDJSON) que devuelve fragmentos de escena y
     recibe input del nodo que le pertenece. Aislamiento de fallos, cualquier
     lenguaje. El protocolo es de **árbol**, no de panel: aquí no hay un
     `panel.render` que sea el techo.
- **Escapatoria opcional, diferida:** un intérprete embebido en proceso (estilo pi)
  solo si de verdad hace falta lógica dentro del render. Se decide con un ADR que
  nombre su coste (una dependencia pesada, un runtime que gobernar). No entra por
  la puerta de atrás.

Lo que **no** se repite: no hay una lista de cinco capacidades y ya. El árbol es la
capacidad.

## El caso concreto: la barra de input

Lo que pediste —borde redondeado y un barrido de luz cada 5 segundos— es, en este
modelo, puro dato:

```jsonc
{
  "id": "input",
  "type": "input",
  "placeholder": "ask anything, or / for commands",
  "border": { "shape": "rounded", "style": "frame" },
  "shine":   { "period": "5s", "width": 12, "travel": "loop", "style": "shine" }
}
```

En arxi-sim esto se aproxima a golpe de glifos (`frame.tl/tr/…`) y `[anim]
period/travel/width`, y ese mismo glifo lo usan también las tablas y los overlays,
así que no puedes tocar solo el input. En arxi tui el borde es del nodo, el estilo
es un token, y no hay colisión global.

## Stack propuesto

- **Go**, binario único, sin dependencias para el núcleo. El objetivo —del crudo al
  exagerado— no necesita más.
- **Proyecto nuevo** (`arxi-tui`) con su propio módulo: mezclar dos filosofías de
  diseño en un mismo repo envicia a la vieja.
- **Se reutiliza** de arxi-sim: el backend de terminal, el modelo de celda/Frame, el
  fold de eventos y la disciplina de goldens. **Se reconstruye**: composición,
  layout y extensiones. La copia es deliberada, no un import entre repos: son dos
  productos con dos contratos distintos.

## Cuándo escribir código, cuándo datos

| Quiero… | Herramienta |
|---|---|
| Colores, glifos, tipografía, opacidades | tokens de estilo (datos) |
| Borde del input, barrido de luz, spinner | props de nodo (datos) |
| Reordenar / vetar / duplicar filas | escena (datos) |
| Una fila nueva encima del input | nodo nuevo en la escena (datos) |
| Un popup flotante, un banner | nodo `overlay` (datos) |
| Lógica: calcular algo, reaccionar a eventos | extensión con comportamiento (proceso) |
| Un render a medida, no expresable como árbol | intérprete embebido — **ADR pendiente** |

La regla: si se puede decir como datos, se dice como datos. El código es la última
opción, no la primera.

## La escena por defecto: el minimalismo fx, hecho con las herramientas del usuario

El primer producto visible de arxi tui es una escena por defecto copiada del lenguaje
visual de fx, **escrita íntegramente con el formato de escena que tendrá el
usuario** — esa es la prueba de fuego: si la escena de fábrica no puede expresarse
con las herramientas del usuario, las herramientas están incompletas.

Reglas del look, extraídas de la exploración de fx (2026-09):

- **Sin color por defecto.** Todo en el gris/blanco natural de la terminal. El
  énfasis se hace **iluminando el texto** (el ítem seleccionado se aclara), jamás
  pintando un fondo. Los colores serán una decisión posterior, capa encima.
- **Encabezado de una línea:** `Δr×i v0.1.0 · Run /help for commands`.
- **El prompt es una barra vertical `┃`**, no una caja. El input vive a su derecha.
- **Separadores de una sola línea `────`** en lugar de marcos de caja para
  paneles/menús: la zona de menú es texto entre dos reglas horizontales.
- **Pie de estado mínimo:** `auto · kimi-k3 · ⚡︎` — modo, modelo, señal, separados
  por puntos medios, sin recuadro.
- **Menús fx-style:** lista plana filtrable por tipeo, categorías navegables con
  tab (`All · General · Session · Account · Model · Appearance · Security · …`),
  cabecera con contador (`Commands 35`), y una línea de teclas al pie
  (`↑↓ navigate · tab category · enter open · esc close`).
- **Del shell de fx hacia adelante, se conserva lo nuestro:** el transcript, el
  markdown, las herramientas, el fold de arxi-sim siguen siendo la base; fx aporta
  el andamiaje visual, no un reemplazo del fondo.

### El Thinking: la marquee de arxi-sim, con la forma de fx

fx muestra una línea terminante:

```
• Thinking (3s) (↑4 ↓11)
```

donde `(↑4 ↓11)` —tokens de entrada y salida— va en gris. arxi-sim ya tiene la
pieza que falta: `thinkingMarquee` (`internal/ui/block.go:167`), el scroll
horizontal que hace fluir el pensamiento visible hacia la izquierda. La línea por
defecto de arxi tui combina las dos cosas:

```
• Thinking (3s) (↑4 ↓11) El usuario pregunta cómo implementar la barra de…
```

con el resumen de tiempo y contadores al frente y **la marquee en gris a
continuación**. Estado: `•` mientras piensa (marquee viva); al terminar, la línea
colapsa a la forma fx. Es la primera escena de muestra del motor: un nodo `text` con
bind a `thinking.text`, animado por el reloj del host, dos spans con dos estilos.

## Instalación: un solo comando, también en Termux

Regla de producto: **el usuario no instala dependencias, instala arxi.** La
experiencia de pi (primero Node/Bun y npm, luego pi) no se acepta; la de fx (un
binario, cero prerequisites) es el estándar.

- Go compila **todo dentro del binario**: runtime incluido, enlace estático con
  `CGO_ENABLED=0`. Las dependencias existen solo para nosotros al construir; en
  casa del usuario hay un archivo. Las que ya usamos (`charmbracelet/x/ansi`) y las
  candidatas (`wazero` para wasm —puro Go, cero dependencias nativas—) son Go puro:
  compilan hacia adentro, nunca se instalan "por separado".
- Matriz de artefactos: linux x86_64/arm64, macos x86_64/arm64, windows, y
  **Termux (`GOOS=android GOARCH=arm64`)** — arxi-sim ya mantiene code de Termux,
  la experiencia se hereda.
- Instalador: `curl -fsSL https://arxi.sh/install.sh | sh` — detecta SO/arq, baja el
  tarball, deja el binario en `~/.local/bin` (o `$PREFIX/bin` en Termux), nada más.
  Ese script es la única cosa que un usuario necesita tocar.
- El modelo de plugins no rompe la regla: un plugin declarativo es **datos montados
  por el motor** (nada que instalar), uno de subproceso es su propio binario
  estático, y uno wasm es un `.wasm` interpretado por wazero dentro del binario
  madre. Ninguno añade dependencias al sistema del usuario.

## Fases

- **Fase 0 — Motor de escena.** Documento, validación, diff a celdas, hot reload,
  escena por defecto reproducible. Es la pieza nueva; todo lo demás la presupone.
- **Fase 0.5 — El vocabulario de binds.** Inventario cerrado de campos que el
  estado expone a la escena (chat.history, agent.working, usage.in/out,
  ui.focus…) con sus derivados calculados por el host, más el namespace abierto
  para plugins (`tick.price`). Sin esto, las escenas no tienen qué bindear.
- **Fase 1 — El espectro.** Tokens de estilo, nodos base, y **tres escenas de
  referencia**: crudo (input + texto), **la escena por defecto minimalista
  fx-style** (encabezado `Δr×i v0.1.0`, barra `┃`, reglas `────`, pie de estado,
  menús filtrables y la línea Thinking con marquee — todo sin color, énfasis por
  brillo de texto), y la base arxi-sim completa con paneles. Aquí ya se ve el rango
  entero, y la escena de fábrica demuestra que el formato del usuario puede con
  todo.
- **Fase 2 — Mutación desde dentro.** Comandos `/ui …` + propuesta de parche por
  parte del agente, con validación, `file:line` y goldens. Aquí ya se construye
  desde la propia instancia, en cualquier dirección del espectro.
- **Fase 3 — Montaje de terceros.** Extensión = fragmento de escena montado en un
  id; overlays, banners y filas junto al input, con foco e input por nodo.
- **Fase 4 — Comportamiento.** Extensiones con lógica, y la decisión de scripting
  embebido (ADR con coste explícito).

## Invariantes que no se pueden romper

1. Sin personalización, la escena por defecto se dibuja byte a byte igual.
2. La escena dice forma; el fold dice contenido. El fold no espera a nadie.
3. Un parche inválido no tumba la sesión: se conserva la última escena buena.
4. Todo error lleva `file:line`.
5. Si se puede expresar como dato, no se exige código.
6. **La escena nunca captura la salida.** El motor conserva un gesto de pánico
   inamovible (`Ctrl-C` dos veces / flag de arranque `-scene ""`): restaura la
   escena cruda aunque la escena activa esté rota o secuestre la pantalla. Es lo
   que hace viable el modo comunidad.

## Decisiones tomadas

0. **Las 17 preguntas de `docs/SCENES.md` quedan firmadas con su provisional**
   (2026-09-14), con dos enmiendas: (a) `overlay anchor:"full"` es legal pero
   ninguna escena puede capturar la tecla de escape del motor (invariante 6);
   (b) el inventario de campos de bind pasa a ser trabajo de diseño propio:
   **Fase 0.5**.

1. **Comportamiento: declarativo + subproceso primero; intérprete diferido.** El
   árbol de escena cubre "crudo ↔ exagerado" sin una línea de código de usuario, así
   que la Fase 4 no bloquea el producto. El scripting embebido queda como ADR con
   coste explícito, y solo si aparece un caso que el árbol no exprese.
2. **El agente edita por comandos y por documento.** El comando (`/ui ...`) para lo
   determinista, testeable y auditable; el documento/patch para lo libre y
   exploratorio. Los dos escriben la misma escena y se validan igual.
3. **Se copia y diverge de arxi-sim, no se importa.** Módulo y repo propios. Se
   reutilizan backend de terminal, modelo de celda/Frame y fold; se reconstruyen
   composición, layout y extensiones.

## Lo que el espectro exige del motor (no negociable)

- **La escena mínima debe ser legal.** Dos nodos (`input`, `text`) es un documento
  válido y arranca; no hay nodos obligatorios que impidan bajar a crudo.
- **Todo nodo es opcional y montable por id.** Lo que la base trae de fábrica se
  puede quitar, mover o duplicar con las mismas operaciones que cualquier otra cosa.
- **Un preset es una escena completa, no un parche acumulativo.** Aplicar un preset
  es poner una escena; los presets no se pisan entre sí por orden de carga.
- **Nada del fondo depende del look.** El fold, el scroll, el input y la respuesta
  funcionan igual con dos nodos que con treinta.

