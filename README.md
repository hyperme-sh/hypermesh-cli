# hypermesh-cli

Thin **Hypermesh** CLI for [Hyperme.sh](https://hyperme.sh): browse the catalog, create a lease, send chat through [Conductor](https://github.com/hyperme-sh/hypermesh-conductor), and manage local MCP config for the [desktop visor](https://github.com/hyperme-sh/hypermesh-visor).

Binaries: `hypermesh` and `hm`.

```bash
git clone https://github.com/hyperme-sh/hypermesh-cli.git
cd hypermesh-cli
make build    # bin/hypermesh and bin/hm
make test
make install  # PREFIX=/usr/local
```

Or: `go install github.com/hyperme-sh/hypermesh-cli/cmd/hypermesh@latest` (and the same for `cmd/hm`). Needs Go 1.22+.

Pass Terms acceptance through to the installed binary. `--license accept` and `HYPERMESH_LICENSE=accept` record it. Host install consent (`--yes`) is a separate choice.

```bash
hypermesh --license accept auth whoami
HYPERMESH_LICENSE=accept hypermesh auth whoami
```

## Terms

Commands other than `--help`, `help`, `version`, and `terms` require the Hypermesh Terms before they do any network or local work. `hypermesh terms` prints the Terms, the version id, and the SHA-256 of that text.

```bash
hypermesh terms
hypermesh terms --accept
hypermesh --license accept catalog
HYPERMESH_LICENSE=accept hypermesh catalog
```

In a terminal the CLI prints the Terms and asks `Accept the Hypermesh Terms? [y/N]`. `y` or `yes` records acceptance. Any other `--license` value is an error.

The stamp is `terms-accepted.json` under the user config directory (`UserConfigDir()/hypermesh`). It stores `version`, `sha256`, and `accepted_at` (UTC). On Linux that directory is `$XDG_CONFIG_HOME/hypermesh` when `XDG_CONFIG_HOME` is set, and `~/.config/hypermesh` otherwise. A stamp whose version or hash differs from the embedded Terms asks again. Non-interactive runs without acceptance exit `1` and name `--license accept`, `HYPERMESH_LICENSE=accept`, and the version id.

## Auth

Use an org API key and tenant id. The same headers go to the API host and the chat host. This CLI stores the key locally. It does not mint a token.

```bash
hypermesh auth login \
  --api-key "$HYPERMESH_API_KEY" \
  --tenant-id "$HYPERMESH_TENANT_ID" \
  --renter-user-id "$HYPERMESH_RENTER_USER_ID"

hypermesh auth whoami
hypermesh auth logout
```

Authenticated requests send:

| Header | Value |
|---|---|
| `X-Api-Key` | org API key (`prefix.secret` from api-keys-service) |
| `X-Tenant-ID` | tenant id |

`api.test.hyperme.sh` checks that pair and exchanges the key before the request reaches hypermesh-plane. Catalog, hosts, and leases use those headers. Chat goes to Conductor on `http://127.0.0.1:8787` with `Authorization: Bearer` plus the lease id in the JSON body. Conductor does not forward `X-Api-Key` or the lease headers. `hm_dev_`, `hm_rtr_`, `hm_gw_`, and `hm_site_` are rejected before any request is sent.

Lease create on the plane accepts the exchanged key when its purpose is `service`. Chat accepts a renter key whose fingerprint was published for that tenant, plus a paid lease. api-keys-service publishes that renter fingerprint at mint for every key purpose.

Config directory: `~/.config/hypermesh` (or `HYPERMESH_CONFIG_DIR`).

| File | Purpose |
|---|---|
| `config.toml` | API base, conductor origin, and preferences |
| `credentials` | API key (mode `0600`) |
| `mcp.json` | MCP servers |
| `mcp-profiles.json` | named MCP profiles |
| `mcp-bindings.json` | focused-app → server bindings |
| `terms-accepted.json` | Terms stamp (`version`, `sha256`, `accepted_at`) in `UserConfigDir()/hypermesh` |

## Usage

```bash
hypermesh catalog
hypermesh catalog show "$CATALOG_ID"
hypermesh classes
hypermesh hosts
hypermesh hosts --catalog-id "$CATALOG_ID"

hypermesh checkout \
  --device-id "$DEVICE_ID" \
  --catalog-id "$CATALOG_ID" \
  --renter-user-id "$HYPERMESH_RENTER_USER_ID" \
  --wait

hypermesh lease list
hypermesh lease show "$LEASE_ID"

hypermesh prompt --script --class code --lease-id "$LEASE_ID" "hello"
hypermesh chat --script --class code --lease-id "$LEASE_ID" --message "hello"

text=$(hypermesh prompt --script --class code --lease-id "$LEASE_ID" "hello")
HYPERMESH_LICENSE=accept ./scripts/hypermesh-prompt.ps1 -LeaseId "$LEASE_ID" "hello"
./scripts/hypermesh-prompt.ps1 -License accept -LeaseId "$LEASE_ID" "hello"

hypermesh lease complete "$LEASE_ID"
```

Pick `CATALOG_ID` from `hypermesh catalog` and `DEVICE_ID` from `hypermesh hosts`. `checkout` prints the lease id as soon as create returns. `--wait` polls until the lease is `active`, `ended`, `failed`, or `refunded`.

With `--script`, stdout is the assistant text and failures exit `1`. Use `--json` when you want the raw response (not with `--script`).

## API map

Defaults: `HYPERMESH_API_BASE=https://api.test.hyperme.sh`, `HYPERMESH_CONDUCTOR_BASE=http://127.0.0.1:8787`.

The API host is controlplane-edge. It forwards `/api/v1/hypermesh` to hypermesh-plane. Chat is Conductor on loopback. Conductor is not in `product_routes.json` and only binds a loopback port (`127.0.0.1:8787`). The gateway at `https://chat.test.hyperme.sh` is a destination an operator can put in Conductor's tier config. This CLI does not call it.

| Command | Method | Path |
|---|---|---|
| `catalog` / `catalog show` | `GET` | `/api/v1/hypermesh/catalog` |
| `classes` | `GET` | `/api/v1/hypermesh/classes` |
| `hosts` | `GET` | `/api/v1/hypermesh/renter/hosts` |
| `checkout` | `POST` | `/api/v1/hypermesh/leases` |
| `lease list` | `GET` | `/api/v1/hypermesh/leases` |
| `lease show` | `GET` | `/api/v1/hypermesh/leases/{id}` |
| `lease complete` | `POST` | `/api/v1/hypermesh/leases/{id}/complete` |
| `chat` / `prompt` / `completions create` | `POST` | `{conductor origin}/v1/chat/completions` |

`catalog` and `classes` are public. The rest need the API key and tenant id.

`catalog show` filters the public catalog list on the client. There is no `GET /catalog/{id}` on the plane.

### Checkout body

```json
{
  "kind": "p2_loaded_model",
  "renter_user_id": "<uuid>",
  "catalog_id": "<from catalog>",
  "reserved_hours": 1,
  "purpose": "renter",
  "device_id": "<from hosts>"
}
```

`device_id` is the UUID plane id from `hosts` (not `public_label`). The plane opens the USDC lease payment and returns the lease id on that POST. The lease document has `billing_record_id` when the payment row exists. It has no checkout URL. This CLI does not call `/api/v1/hypermesh/renter/chat/completions` (that route always answers 409).

`renter_user_id` must match the user bound to the exchanged key. A mismatch is refused by the plane.

Lease status moves `offered` → `paid` → `starting` → `active`, then `ended` / `failed` / `refunded`. `suspended` is also a plane status; `--wait` keeps polling it.

### Chat

`POST {HYPERMESH_CONDUCTOR_BASE}/v1/chat/completions` with OpenAI-shaped `model` + `messages`, plus:

- `lease_id` in the JSON body (Conductor keeps it when the destination speaks the same protocol)
- `Authorization: Bearer` set to the renter key
- `X-Conductor-Class` when `--class` or `HYPERMESH_CONDUCTOR_CLASS` is set
- `X-Conductor-Task` set to the lease id
- `X-Conductor-Token` when `CONDUCTOR_PROXY_TOKEN` is set in the environment

Conductor chooses the tier and replaces `model`. With no class on the request and no `default_class` in the conductor config, Conductor returns HTTP 400. `conductor run` is a local conductor command, not an HTTP route, so this CLI does not call it. Prompt bodies are not logged.

`hypermesh route detect` lists other AI tools. `route enable` and `route disable` are opt-in and restore from a backup. See [Redirect other tools](#redirect-other-tools).

## Local MCP

Config for the [hypermesh-visor](https://github.com/hyperme-sh/hypermesh-visor) companion (visor attaches the active profile on session open):

```bash
hypermesh mcp catalog ls
hypermesh mcp import cursor
hypermesh mcp import project
hypermesh mcp import docker
hypermesh mcp profile ls
hypermesh mcp profile create frontend
hypermesh mcp profile use frontend
hypermesh mcp profile add filesystem
hypermesh mcp profile add chrome          # also writes catalog matchers into bindings
hypermesh mcp profile gateway on          # attach only Docker MCP gateway for this profile
hypermesh mcp bindings add filesystem --wm-class Code
hypermesh mcp bindings ls
hypermesh mcp doctor
hypermesh mcp list
```

Project import merges `.hypermesh/mcp.json` or `.cursor/mcp.json` from a path (or cwd parents). Catalog entries such as `chrome` and `code` carry desktop matchers that become `mcp-bindings.json` rows when you `profile add` them. `profile gateway on` asks the visor to attach only `docker-gateway`.

| File | Shape |
|---|---|
| `mcp.json` | `{ "mcpServers": { … } }` |
| `mcp-profiles.json` | `{ "active", "profiles": { …, "gateway"?: bool } }` |
| `mcp-bindings.json` | `{ "bindings": [ { "server", "wm_class"?, … } ] }` |
| project `.hypermesh/mcp.json` | same servers shape; merged at attach time / import |

## Environment

| Env | Default |
|---|---|
| `HYPERMESH_API_BASE` | `https://api.test.hyperme.sh` |
| `HYPERMESH_CONDUCTOR_BASE` | `http://127.0.0.1:8787` |
| `HYPERMESH_CONDUCTOR_CLASS` | unset; Conductor then uses its own `default_class` or refuses |
| `HYPERMESH_LICENSE` | unset; `accept` records the Hypermesh Terms |

Also: `HYPERMESH_API_KEY`, `HYPERMESH_TENANT_ID`, `HYPERMESH_RENTER_USER_ID`, `HYPERMESH_LEASE_ID`, `HYPERMESH_CONDUCTOR_SUGGEST`, `HYPERMESH_CONFIG_DIR`, `HYPERMESH_VISOR_URL`. `CONDUCTOR_PROXY_TOKEN` is forwarded as `X-Conductor-Token` and is not stored.

`HYPERMESH_LICENSE=accept` is the same acceptance as `--license accept`. Pass either one through scripts that exec `hypermesh` or `hm`.

## Commands

| Command | Role |
|---|---|
| `terms` | print the Terms; `--accept` records acceptance |
| `version` | build version (no Terms stamp required) |
| `auth …` | local config |
| `catalog` / `classes` | public catalog |
| `hosts` | renter hosts |
| `checkout` / `lease …` | leases |
| `chat` / `prompt` / `completions create` | Conductor chat |
| `route detect` / `enable` / `disable` / `status` | local AI tool endpoints |
| `mcp …` | local MCP config |

## Redirect other tools

```bash
hypermesh route detect
hypermesh route detect --json
hypermesh route enable aider --dry-run
hypermesh route enable aider
hypermesh route enable --all
hypermesh route status
hypermesh route disable aider
hypermesh route disable --all
```

`detect` is read-only. It reports whether each tool has a custom OpenAI-compatible or Anthropic-compatible base URL. `enable` writes that config, keeps a backup, and does not paste the renter key. `disable` restores the backup. Tools without a documented base URL (Gemini CLI, Cursor, Copilot BYOK, JetBrains AI, LM Studio, Ollama) are listed and not modified.

[Hypermesh for VS Code](https://github.com/hyperme-sh/hypermesh-vscode) contributes **Hypermesh: smoke** (`hypermesh.smoke`). Its window setting `hypermesh.apiBase` defaults to the gateway origin and the command posts `{apiBase}/v1/chat/completions`. `hypermesh route enable hypermesh-vscode` sets `hypermesh.apiBase` to the Conductor origin. The renter key stays in VS Code SecretStorage (`hypermesh.key`). The extension does not send a job class, so Conductor needs `default_class` for that smoke command. The extension is not a substitute for `hypermesh checkout`.

Contributor notes (same repo): [DESIGN.md](DESIGN.md).
