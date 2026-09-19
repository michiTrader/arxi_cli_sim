# Local extensions

`arxi-sim` can install unpacked native extension directories into a per-user, content-addressed store. Installation copies trusted code; it does not grant runtime capabilities.

## Build and stage an example

The manifests use explicit relative executable paths, so stage the binary under `bin` before installing. A source checkout containing only `main.go` is not directly installable and `go run` is not an installation mechanism.

Unix:

```sh
mkdir -p build/panel-v2/bin
cp examples/extensions/panel-v2/manifest.toml build/panel-v2/
go build -o build/panel-v2/bin/panel-v2 ./examples/extensions/panel-v2
chmod +x build/panel-v2/bin/panel-v2
./arxi-sim extensions install build/panel-v2
```

Windows PowerShell:

```powershell
New-Item -ItemType Directory -Force build\panel-v2\bin | Out-Null
Copy-Item examples\extensions\panel-v2\manifest.toml build\panel-v2\
(Get-Content build\panel-v2\manifest.toml) -replace 'bin/panel-v2"', 'bin/panel-v2.exe"' | Set-Content build\panel-v2\manifest.toml
go build -o build\panel-v2\bin\panel-v2.exe .\examples\extensions\panel-v2
.\arxi-sim.exe extensions install build\panel-v2
```

The same layout applies to `actions` and `event-tap`. On Unix the staged executable must retain an executable mode. Windows packages should explicitly name the `.exe` in their staged manifest.

## Inspect and automate

```sh
arxi-sim extensions list
arxi-sim extensions list --config ./test-config.toml
arxi-sim extensions install ./build/panel-v2 --yes
```

The installer displays source, destination, digest, identity fields, executable, and capabilities before default-no confirmation. `--yes` means only “trust and install these native bytes”; it never grants protocol capabilities. The first interactive runtime asks separately for the complete declared capability set. Native extensions run as your user with no filesystem or network sandbox.

The list command starts no extensions. States are `disabled`, `invalid`, `consent-required`, `ready`, and `content-changed`; the last means managed installed bytes no longer match their registered digest.

Phase 4 supports local install and listing only. Updating, uninstalling, and archiving generations are not implemented. WASM and upstream manifest compatibility remain deferred until their authority, executable-selection, portability, and identity contracts are specified.
