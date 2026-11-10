# Contributor notes

Everything a reader needs to *use* this CLI is in [README.md](README.md). This file is only for people changing the binary.

## Layout

| Path | Role |
|---|---|
| `cmd/hypermesh`, `cmd/hm` | entrypoints |
| `internal/cli` | cobra commands |
| `internal/api` | HTTP client, URLs, lease body |
| `internal/route` | local AI tool detect / enable / disable |
| `internal/config` | config + credentials |
| `internal/mcp` | local MCP files |
| `scripts/hypermesh-prompt.ps1` | PowerShell wrapper around `prompt --script` |

## Defaults in code

`DefaultCatalogID` / `DefaultAPIBase` / `DefaultConductorBase` live in `internal/api/urls.go`. They seed flags when omitted — they are not a catalog inventory. Prefer reading ids from `GET /catalog` in examples and tests that care about a specific model.

## Where the paths come from

`https://api.test.hyperme.sh` is controlplane-edge. It forwards the prefix `/api/v1/hypermesh` to hypermesh-plane (`controlplane-edge` `src/controlplane_edge/product_routes.json`, hypermesh-plane `products/hypermesh/edge-routes.json`).

Handlers are `hypermesh-plane` `products/hypermesh/src/hypermesh_service/interfaces/api/routes.py` (router prefix `/hypermesh`, mounted at `/api/v1`):

| CLI | Method and path | Handler |
|---|---|---|
| `catalog` | `GET /api/v1/hypermesh/catalog` | `list_catalog` |
| `classes` | `GET /api/v1/hypermesh/classes` | `list_classes` |
| `hosts` | `GET /api/v1/hypermesh/renter/hosts` | `list_renter_hosts` |
| `checkout` | `POST /api/v1/hypermesh/leases` | `create_lease` |
| `lease list` | `GET /api/v1/hypermesh/leases` | `list_leases` |
| `lease show` | `GET /api/v1/hypermesh/leases/{id}` | `get_lease` |
| `lease complete` | `POST /api/v1/hypermesh/leases/{id}/complete` | `complete_lease` |

`https://chat.test.hyperme.sh` is hypermesh-gateway, served outside that proxy (`edge-routes.json` exposure). `POST /v1/chat/completions` is `openai_router` in `hypermesh-gateway` `src/hypermesh_gateway_service/routes.py`, included with an empty prefix in `app.py`. This CLI does not call that host. Conductor is the chat client. An operator who wants leased capacity points a conductor tier `base_url` at the gateway.

Auth on the API host is `X-Api-Key` plus `X-Tenant-ID`. controlplane-edge `middleware/policy.py` validates the key through api-keys-service and replaces it with an exchange token before the proxy hop. controlplane-identity decodes that token. This process never builds that token.

`POST /leases` on the plane (`lease_auth.py`) accepts the exchange when `purpose` is `service`. Conductor does not change that. `conductor prepare-leases` (`internal/leases/client.go`) execs `hypermesh-orchestrator --prepare-leases` and does not call `POST /leases`. Catalog, classes, hosts, and lease verbs stay on the plane paths above.

The gateway chat door accepts a renter API key (`renter_auth.py` `require_renter_grant`: `Authorization: Bearer` when the token looks like an API key, or `X-Api-Key`, and it rejects the request when both headers are present) plus `lease_id` (`routes.py` `_lease_id`: JSON `lease_id`, else `X-Hypermesh-Lease-Id` or `X-Lease-Id`). `X-Tenant-ID` is optional for an API key (`_optional_tenant_header`); a present header must match the key's tenant.

The lease create body is `LeaseCreate` in `hypermesh_service/domain/models.py`: `kind`, `renter_user_id`, `catalog_id`, `device_id`, `reserved_hours`, `purpose`. The response `Lease` includes `id` and `billing_record_id`. It does not include a checkout URL.

## Conductor

Conductor is a loopback proxy. `internal/config/config.go` `DefaultListen` is `127.0.0.1:8787` (`examples/endpoints.json` uses the same value). `ValidateListen` refuses any other address, and `internal/proxy/proxy.go` `ServeListener` requires a loopback TCP socket. `controlplane-edge` `src/controlplane_edge/product_routes.json` has no conductor product and no conductor hostname. `HYPERMESH_CONDUCTOR_BASE` / `--conductor-base` / `conductor_base` default to `http://127.0.0.1:8787`. There is no public conductor URL to guess.

`protocolOf` in `internal/proxy/proxy.go` is the HTTP surface:

| Method and path | Used by this CLI |
|---|---|
| `POST /v1/chat/completions` (also `/chat/completions`) | `chat`, `prompt`, `completions create` |
| `POST /v1/responses` | not called; Codex `wire_api = "responses"` can call it after `route enable codex` |
| `POST /v1/messages` | not called; Claude Code and `ANTHROPIC_BASE_URL` clients append this to the origin |
| `POST /v1/embeddings` | not called |
| `GET /v1/models` | not called |
| `POST /v1/peer/prompt-submit` | not called; optional operator ingress (`internal/cli/peer_ingress.go`) |

`conductor run` (`internal/cli/cli.go` `runCmd`) builds an in-process request. It is not an HTTP route, and this CLI does not shell out to it. `conductor route` prints a tier preview; it is a different command from `hypermesh route`.

Chat request, from `identity`, `clientCredential`, and `applyAuth` in `internal/proxy/proxy.go`:

| Header or field | Why |
|---|---|
| `Authorization: Bearer <renter key>` | OpenAI and openai-compatible passthrough forwards this, and only this, as the client credential |
| `X-Conductor-Class` | `--class`, else `conductor_class` / `HYPERMESH_CONDUCTOR_CLASS`. Empty class plus empty conductor `default_class` is HTTP 400 `job class is empty` |
| `X-Conductor-Suggest` | optional `--suggest`. A higher tier is rejected |
| `X-Conductor-Task` | the lease id when it matches `idPattern` (`^[A-Za-z0-9][A-Za-z0-9._:-]{0,199}$`). Docs call the Visor lease UUID the stable task id |
| `X-Conductor-Token` | forwarded only when `CONDUCTOR_PROXY_TOKEN` is set. Not written to `config.toml` |
| JSON `model`, `messages`, `lease_id` | `lease_id` is kept on a same-protocol forward (`internal/translate/translate.go` `setModelPreserve`) and dropped when the body is translated to another protocol |

Conductor replaces `model` with the tier model (`forceModel` / `setModelPreserve`) before the destination sees it. This client does not send `X-Conductor-Expected-Model` (a mismatch is HTTP 409). It does not send `X-Api-Key`, `X-Tenant-ID`, `X-Hypermesh-Lease-Id`, or `X-Lease-Id` on the conductor call: `applyAuth` does not copy them. The gateway can still read JSON `lease_id` when the tier provider is openai-compatible and the body is not translated.

`CONDUCTOR_ACCOUNTING_URL` makes conductor `PUT {plane}/api/v1/hypermesh/leases/{lease}/token-attempts/{attempt}` (`internal/proxy/plane_reservation.go`) with `Authorization: Bearer` from `CONDUCTOR_ACCOUNTING_TOKEN`. The lease id in that call is `CONDUCTOR_ACCOUNTING_LEASE` on the conductor process, not the CLI flag.

The plane path `/api/v1/hypermesh/renter/chat/completions` always returns 409 (`renter_chat_completions`). This client refuses to build it (`PathRenterChatStub`).

## Redirecting other tools

`hypermesh route` is opt-in. `detect` and `status` only read. `enable` writes the tool's own config and stores the previous bytes under `{config dir}/route-backups` (directory `0700`, files `0600`). `disable` restores those bytes. `--dry-run` prints a diff and writes nothing. A second enable that already matches is a no-op. The renter key is never copied into a config file. References are `printenv HYPERMESH_API_KEY`, `{env:HYPERMESH_API_KEY}`, `${HYPERMESH_API_KEY}`, `env_key = "HYPERMESH_API_KEY"`, or a Continue secret placeholder. Diffs redact other key-looking values.

OpenAI-compatible clients get `{origin}/v1`. Anthropic clients get the origin, because they append `/v1/messages` and conductor serves that path. `hypermesh-vscode` `hypermesh.apiBase` is the origin: the extension appends `/v1/chat/completions` and sends `Authorization: Bearer` (`hypermesh-vscode` `README.md`). It does not send `X-Conductor-Class`, so the conductor process needs `default_class` or the smoke command gets HTTP 400.

| Tool | Detection | Config key | Protocol | Supported |
|---|---|---|---|---|
| Claude Code | `claude` on `PATH`, `~/.claude/settings.json`, `~/.claude.json` | `env.ANTHROPIC_BASE_URL` | anthropic | yes |
| OpenAI Codex CLI | `codex` on `PATH`, `~/.codex/config.toml` | `model_providers.hypermesh.base_url` (`wire_api = responses`) | openai | yes |
| Gemini CLI | `gemini` on `PATH`, `~/.gemini/` | no base URL key | none | no |
| Aider | `aider` on `PATH`, `~/.aider.conf.yml` | `openai-api-base` | openai | yes |
| OpenCode | `opencode` on `PATH`, `~/.config/opencode/opencode.json` | `provider.hypermesh.options.baseURL` | openai | yes |
| Continue | `~/.continue/config.yaml` or `config.json` | `models` `apiBase` | openai | yes (`config.ts` is reported and not edited) |
| Cline | extension dir or `cline.*` in VS Code `settings.json` | `cline.openAiBaseUrl` | openai | yes (key stays in secret storage) |
| Roo Code | `rooveterinaryinc.roo-cline` or `roo-cline.*` | `roo-cline.openAiBaseUrl` | openai | yes (key stays in secret storage) |
| Cursor | `cursor` on `PATH`, `~/.cursor` | UI state, no documented file | none | no |
| VS Code Copilot | `github.copilot` settings or extension | BYOK secret storage | none | no |
| Hypermesh for VS Code | `fyberlabs.hypermesh-vscode` or `hypermesh.apiBase` | `hypermesh.apiBase` | openai | yes |
| Zed | `zed` on `PATH`, `~/.config/zed/settings.json` | `language_models.openai.api_url` and `language_models.anthropic.api_url` | openai+anthropic | yes (no invented context length) |
| JetBrains AI | `~/.config/JetBrains` or a product config dir | no documented base URL file | none | no |
| LM Studio | `~/.lmstudio` | local server | none | no |
| Ollama | `ollama` on `PATH`, `~/.ollama` | `OLLAMA_HOST` is the listen address | none | no |
| OpenAI env | `OPENAI_BASE_URL` or `OPENAI_API_BASE` in the environment or a shell rc | both exports, plus `OPENAI_API_KEY=${HYPERMESH_API_KEY}` | openai | yes |
| Anthropic env | `ANTHROPIC_BASE_URL` | that export, plus `ANTHROPIC_API_KEY=${HYPERMESH_API_KEY}` | anthropic | yes |

Cline, Roo, and the VS Code extension share `settings.json`. The first enable snapshots the original bytes. Disabling one tool replays the remaining edits onto that snapshot. Disabling the last tool writes the snapshot back unchanged.

## Not in this client

Host, router, gateway, and site secrets (`hm_dev_`, `hm_rtr_`, `hm_gw_`, `hm_site_`) are rejected in `ValidateRenterKey`. No host addresses, no shell access, and no browser login. Do not add a measured-rate field to CLI output. `route enable` does not install a proxy, a certificate, or an edit of another app's binary.

`GET /api/v1/hypermesh/renter/hosts` also accepts `protection_requirements`. This client sends only `catalog_id`. An empty qualified set on the plane returns no rows for that filter.

`GET /api/v1/hypermesh/renter/catalog` exists and is authenticated. `hypermesh catalog` stays on the public `GET /catalog`.

Card checkout prefixes `/api/v1/checkouts` and `/api/v1/payments/stripe/checkout-sessions` answer 410 from controlplane-edge. No public checkout URL was found on the lease response, so `checkout` does not open one.

## Tests

```bash
make vet
make test
make build
```

GitHub Actions (`.github/workflows/test.yml`) runs the same on push and pull request.
