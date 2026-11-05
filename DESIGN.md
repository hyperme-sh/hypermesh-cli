# Contributor notes

Everything a reader needs to *use* this CLI is in [README.md](README.md). This file is only for people changing the binary.

## Layout

| Path | Role |
|---|---|
| `cmd/hypermesh`, `cmd/hm` | entrypoints |
| `internal/cli` | cobra commands |
| `internal/api` | HTTP client, URLs, lease body |
| `internal/config` | config + credentials |
| `internal/mcp` | local MCP files |
| `scripts/hypermesh-prompt.ps1` | PowerShell wrapper around `prompt --script` |

## Defaults in code

`DefaultCatalogID` / `DefaultAPIBase` / `DefaultChatBase` live in `internal/api/urls.go`. They seed flags when omitted — they are not a catalog inventory. Prefer reading ids from `GET /catalog` in examples and tests that care about a specific model.

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

`https://chat.test.hyperme.sh` is hypermesh-gateway, served outside that proxy (`edge-routes.json` exposure). `POST /v1/chat/completions` is `openai_router` in `hypermesh-gateway` `src/hypermesh_gateway_service/routes.py`, included with an empty prefix in `app.py`.

Auth on the API host is `X-Api-Key` plus `X-Tenant-ID`. controlplane-edge `middleware/policy.py` validates the key through api-keys-service and replaces it with an exchange token before the proxy hop. controlplane-identity decodes that token. This process never builds that token.

`POST /leases` on the plane (`lease_auth.py`) accepts the exchange when `purpose` is `service`. The gateway chat door accepts a renter API key (`renter_auth.py`) plus `lease_id` (`X-Hypermesh-Lease-Id` or `X-Lease-Id`, or the JSON field).

The lease create body is `LeaseCreate` in `hypermesh_service/domain/models.py`: `kind`, `renter_user_id`, `catalog_id`, `device_id`, `reserved_hours`, `purpose`. The response `Lease` includes `id` and `billing_record_id`. It does not include a checkout URL.

## Chat path

Router chat is `{HYPERMESH_CHAT_BASE}/v1/chat/completions`. The plane path `/api/v1/hypermesh/renter/chat/completions` always returns 409 (`renter_chat_completions` in the same `routes.py`). This client refuses to build it (`PathRenterChatStub`).

## Not in this client

Host, router, gateway, and site secrets (`hm_dev_`, `hm_rtr_`, `hm_gw_`, `hm_site_`) are rejected in `ValidateRenterKey`. No host addresses, no shell access, and no browser login. Do not add a measured-rate field to CLI output.

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
