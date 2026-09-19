# Extension protocols (`ext/v1` and `ext/v2`)

Extensions are separate processes that exchange one compact JSON object per line over stdin/stdout. This sim-local contract is intentionally narrower than an in-process API: extensions propose effects and never mutate application state. The host owns consent, attribution, sequencing, and lifecycle.

Normative terms **must**, **must not**, **should**, and **may** have their usual RFC 2119 meanings.

## Manifest and identity

Each extension directory contains `manifest.toml`. The format is a deliberately small TOML subset: top-level keys, quoted strings, and arrays of quoted strings only. Unknown and duplicate keys are errors.

```toml
name = "clock"
version = "1.0.0"
protocol = "ext/v1"
executable = "clock"
args = ["--zone", "UTC"]
capabilities = ["events.subscribe", "events.emit"]
```

`name` must match `[a-z][a-z0-9-]{0,62}`. `version`, `protocol`, and `executable` are required; protocol must be exactly `ext/v1` or `ext/v2`. `args` and `capabilities` may be omitted. Capabilities must not be duplicated and are validated against the selected protocol vocabulary.

The consent identity is the manifest name, version, protocol, executable, arguments, complete declared-capability set, and package digest when the extension is managed. Capability order is irrelevant and comparison uses exact set equality. Moving unchanged installed bytes does not create a new identity; changing any identity input invalidates an existing grant and requires consent again. The `ready` name and version must exactly match the manifest; a mismatch is a fatal handshake error.

## Local distribution and managed content

`arxi-sim extensions install <directory>` installs an unpacked local package only. Preflight requires a package-root `manifest.toml`, an explicit contained relative executable path (for example `bin/clock` or `bin/clock.exe`), regular files only, portable non-colliding paths, and bounded depth, file count, and total bytes. The source is snapshotted and hashed before confirmation. The canonical tree digest covers slash-normalized relative paths, file bytes, byte counts, regular-file type, and the executable mode bit.

A confirmed package is copied to an immutable content-addressed generation below the per-user extension store. Publication is atomic and never overwrites a different generation. Registration is a lossless atomic config edit containing the manifest path, `enabled = true`, empty `allow` and `identity`, and the managed digest/generation. Installation is trust to copy and register native code; it is **not** a runtime capability grant. `--yes` automates installation trust only. The first runtime still requires explicit capability consent.

`extensions list` is read-only and must not create an orchestrator or spawn a process. It reports configured entries in name order as disabled, invalid, consent-required, ready, or content-changed. A managed digest mismatch is always content-changed and cannot run or be granted until repaired. Entries without managed metadata remain supported as unmanaged configuration.

There is no update, uninstall, or archive workflow in this phase. Name collisions are refused, existing config is never overwritten, and only a generation newly created by the failed command may be rolled back. WASM packaging/runtime is deferred until an upstream manifest/runtime contract defines executable selection, filesystem/network authority, portability, and content identity without weakening these guarantees.

## Capability vocabulary and permissions

The MVP capability vocabulary is exactly:

- `events.subscribe`: receive the stable event tap (`seq`, `type`, and the opaque event object). Filters use exact event types. An empty filter means all types.
- `events.emit`: propose an attributed event by supplying only `type`, `scope`, and `payload`.
- `inbox.answer`: propose `answer`, `approve`, or `reject` for an inbox item.
- `actions.register`: register explicitly named actions.

`panel.render` is not an ext/v1 capability and ext/v1 continues to reject it. ext/v2 has exactly the four ext/v1 capabilities plus `panel.render`. `overlay.render` is not specified or advertised.

Declaration is not permission. A capability must be present in the manifest, granted by the user for the current consent identity, and requested by `ready` before the extension may use it. Failure is `not_declared` when absent from the manifest and `not_granted` when declared but not consented to. The host must not infer grants from installation, previous execution, or another extension with the same name.

## Envelopes and handshake

Every line has this envelope:

```json
{"type":"ready","id":"1","payload":{"protocol":"ext/v1","name":"clock","version":"1.0.0","capabilities":["events.subscribe"]}}
```

`type` is required. `id` is an optional extension-supplied correlation identifier and grants no authority. `payload` is a typed object. Unknown fields, blank lines, multiple JSON values on one line, and lines over 1 MiB are invalid.

The host must send `hello` first, naming `ext/v1` and the complete capability vocabulary. The extension must answer with exactly one `ready` before any other extension message. `ready` repeats the protocol and manifest name/version and requests a subset of the declared and granted capabilities. The handshake deadline is 5 seconds by default. A timeout, malformed message, identity mismatch, protocol mismatch, or unauthorized request is fatal to that process.

Core message types are `hello`, `ready`, `events.subscribe`, `event`, `events.dropped`, `events.emit`, `inbox.answer`, `actions.register`, `actions.invoke`, `error`, and `shutdown`. Error codes are `unknown_type`, `not_declared`, `not_granted`, `invalid_message`, and `protocol_mismatch`.

Message direction is strict. Extensions send `ready`, `events.subscribe`, `events.emit`, `inbox.answer`, `actions.register`, and `error`; hosts send `hello`, `event`, `events.dropped`, `actions.invoke`, `error`, and `shutdown`. Receiving a message in the wrong direction is a fatal protocol error.

## Ordering and event delivery

NDJSON line order is protocol order in each direction. The host sends `hello` before event traffic. After `ready`, messages from an extension are evaluated in receive order. Host event messages preserve accepted run-log sequence order; `seq` is the authoritative order, not arrival time or an extension correlation ID.

`events.subscribe` replaces the process's current subscription. `after` excludes events with `seq <= after`; omitted or zero starts at the beginning of the available live tap. Type filters are exact strings. Subscription delivery must never block the application fold. The default outbound queue is 64 messages. When full, the host drops event deliveries and sends `events.dropped` with the dropped count and the sequence after which the gap began. An extension must treat that notice as loss; the protocol does not promise replay.

An `event` carries positive `seq`, non-empty `type`, and the complete opaque event object. Extensions must not depend on simulator fold heuristics or payload interpretations not specified by the event contract.

## Event proposals and attribution

`events.emit` contains exactly non-empty `type`, non-empty `scope`, and `payload`. `seq`, `source`, and `actor` are host-owned and are rejected if supplied. Possession of `events.emit` does not authorize every event: the host must accept only event types and payloads exposed by its proposal gate, validate them through the same closed vocabulary as first-party input, and reject proposals that cannot be represented without weakening that vocabulary.

For every accepted extension effect, the host assigns sequence and timestamp, chooses the existing event `source` required by the receiving gate, and sets `actor` to the manifest name. The extension cannot select or override attribution. `inbox.answer` follows the same rule and the host/v1 `Answer`/`Approve`/`Reject` decision shapes. Rejected proposals do not enter the log as successful effects.

## Actions

`actions.register` contains one or more actions with unique names matching `[a-z][a-z0-9-]{0,62}` and non-empty descriptions. The host constructs the public identifier `ext:<manifest-name>:<action-name>`; the extension cannot register outside that namespace and supplies no default key binding.

Registration is process-scoped. Re-registering a name already owned by that process replaces its description; collision with a core action, another extension's namespace, or an existing incompatible identifier must be rejected rather than shadowed. All actions registered by a process are removed when that process exits, fails protocol validation, or begins shutdown. Invocation after removal must fail as unavailable rather than target a replacement process implicitly.

`actions.invoke` is host-to-extension only and carries the registered local `action` name plus an optional opaque `args` string. Its envelope `id` is a non-empty host correlation identifier. A dynamic key sends empty args; `/ext:<name> <action> [args...]` separates the first argument as the local action and preserves the remainder. The host may reject invocation when its bounded outbound control queue is full; it must never block the app loop.

## Shutdown, supervision, and restart

On an orderly host close, the host sends `shutdown` with an empty object payload and asks the process group to terminate gracefully. The extension should stop accepting work, flush only already-produced protocol output, and exit with status zero. The default grace period is 2 seconds; after it expires the host kills the complete process tree. EOF, protocol failure, write failure, or unexpected process exit also ends the session and removes its process-scoped registrations.

The host owns the process group and must not leave descendants behind. A protocol or handshake failure is fatal and disables the extension for that supervisor session; it must not be restarted automatically. Other start failures and unexpected exits may be retried only when supervision explicitly sets `MaxRestarts > 0`. The default `MaxRestarts` is zero, so the default is no automatic restart. Negative values are treated as zero.

Retries are bounded by `MaxRestarts` in addition to the initial start. Backoff starts at `InitialBackoff` (100 milliseconds by default), doubles after each failed attempt, and is capped by `MaxBackoff` (5 seconds by default). Cancellation stops the backoff and no further process is started. Every retry is a new session with a new `hello`/`ready` handshake and fresh subscriptions and action registrations; grants are reused only when the consent identity is unchanged.

## Executable and environment

The host invokes `executable` directly with `args`; it must not pass either through a command shell. Relative or bare executable resolution follows the host operating system's direct executable lookup at spawn time. The working directory is not a stable API and extensions must not rely on it. Installers should use a deterministic executable path where portability permits.

The extension receives only the environment explicitly assembled by the host. The host must not implicitly copy its complete environment. Secrets, credentials, proxy settings, and unrelated application variables must not be inherited by default. Stdin and stdout are reserved for ext/v1 NDJSON. Diagnostic output belongs on stderr; the host retains at most the final 64 KiB by default.

## Limits and failure policy

The default limits are: 1 MiB per NDJSON line, 5 seconds for handshake, 2 seconds for graceful shutdown, 64 queued outbound events, and 64 KiB retained stderr. Exceeding the line limit, sending malformed JSON, unknown fields, an unexpected message direction, or using an unrequested capability is a fatal protocol error. Limits apply to each process independently. Extensions should bound their own input, memory, and work because host limits are containment measures, not scheduling guarantees.

## Threat boundary and sandboxing

An extension is native code running as the current user. ext/v1 provides protocol authorization, consent, attribution, bounded transport, and process cleanup; it is **not** an operating-system security sandbox. There is explicitly no filesystem sandbox and no network sandbox. A granted capability controls only host protocol effects and does not prevent an extension from reading user-accessible files, opening network connections, spawning children, or using inherited operating-system authority.

Users must treat an extension binary as installed executable code. The host must not describe a capability grant as granting filesystem or network safety. Strong isolation requires an external OS sandbox, container, restricted account, or a future separately specified runtime. Process-group termination limits orphaning but does not make hostile native code safe.

## Relationship to `host/v1`

This transport does not invent a parallel authority model. `events.subscribe` mirrors host/v1 `Subscribe` and `EventFilter` while pinning only `seq` and `type`; the event body remains opaque. `events.emit` is a proposal seam: it accepts event content but reserves sequence and attribution for the host. `inbox.answer` mirrors host/v1 `Answer`/`Approve`/`Reject` and `DecisionKind`. Grants correspond to a host/v1 principal authorization, with the extension consent identity identifying the principal.

Approval binding, call IDs, digests, and single-use rules are inherited when host/v1 defines them; this sim-local protocol does not weaken or predict them. A host/v1 shape change requires this specification and codec to change in the same commit.

## ext/v2 declarative panels

ext/v2 carries every ext/v1-equivalent message with the same semantics, replacing only the handshake protocol string and capability vocabulary. It adds extension-to-host `view.update` and `view.close`, and host-to-extension `view.resize`, `view.focus`, `view.blur`, and `view.input`. Direction is strict. All view messages require a panel `id` matching `[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}`. The `panel.render` capability gates extension view effects.

`view.update` is a complete replacement for one panel's cached declarative view, not a patch. It carries target `width` and `height` plus rows. A row has a unique `id` and spans; a span has text and one closed role: `text`, `muted`, `emphasis`, `success`, `warning`, `error`, `title`, or `code`. Styling is host-owned. `view.close` removes that panel and its cache. A process exit, protocol failure, or shutdown removes all its panels and cache. A new session starts empty. `view.resize` requests a fresh replacement at the new dimensions; the previous view may remain cached until update or close. Focus/blur are notifications. Hosts may coalesce redundant resize/focus notifications but must preserve close and the latest dimensions.

`view.input` contains the panel id and an input with its current target width/height. Kinds are `key`, `action`, `text`, `paste`, `click`, `drag`, `release`, and `wheel`. Key and action carry exactly their respective string; text and paste carry non-empty text. Pointer events carry zero-based panel-local `x`,`y`; wheel also requires a non-zero `dx` or `dy`. Coordinates must be inside the supplied dimensions. The reserved key/action values `quit` and `interrupt` are invalid; lifecycle control remains host-owned.

Semantic maxima are 16 panels per extension session, 512 rows per panel, 64 spans per row, 64 KiB UTF-8 span text per panel, and width/height 1..4096. Session code must enforce the panel-count limit; each message independently enforces the other limits. Text must be valid UTF-8 and contain no ANSI escape or Unicode control characters. Width overflow is measured by the protocol-local deterministic method: combining marks occupy zero cells, code points below U+1100 occupy one, and other code points occupy two. This deliberately conservative method is independent of application/UI rendering.

All ext/v2 envelope and payload decoders reject unknown fields and trailing values. The framing limit remains 1 MiB per NDJSON line.

## Compatibility

ext/v1 behavior, message shapes, error vocabulary, four capabilities, and rejection rules remain unchanged. A manifest selects one protocol and its closed vocabulary. ext/v2 does not imply transport negotiation: `hello` and `ready` must exactly name the manifest-selected revision. Optional payload fields, message types, roles, input kinds, or capabilities still require a future protocol revision. Event type strings and opaque event bodies retain ext/v1 compatibility behavior.
