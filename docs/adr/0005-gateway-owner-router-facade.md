# ADR 0005: Gateway Owner, Router, Facade, and State Cache

## Status

Proposed

## Context

The current gateway lifecycle surface is overloaded. `internal/managedgateway` is both a command-facing lifecycle boundary and a runtime engine: it decides start/stop/status behavior, constructs proxy and PAC listeners, serves the runtime control endpoint, owns live reload, writes coordination state, installs PAC state, and performs cleanup. `internal/control` mixes the runtime HTTP protocol with client-side terminal formatting, and `internal/runtimecoord` imports `control`, so coordination knows too much about the transport protocol.

This makes it difficult to support both CLI and HTTP command surfaces without duplicating lifecycle decisions or letting one surface call the other. It also keeps the coordination file too close to runtime status, even though status should come from the owning process.

## Decision

Split the gateway lifecycle into explicit concepts:

- `gatewayfacade`: the surface-neutral command boundary for start, stop, status, check, install, and uninstall. It returns structured command results and owns lifecycle decisions, but does not render terminal text or HTTP responses.
- `gatewayrouter`: the HTTP command surface. It exposes command routes such as `GET /start/plan`, `POST /start`, `/stop`, `/status`, `/install`, and `/uninstall`, then renders facade results as HTTP responses. `check` remains local and footprint-free.
- `gatewayruntime`: the live traffic-serving engine. It owns the proxy listener, PAC listener, proxy server, PAC server, live configuration reload, fatal runtime errors, and runtime close behavior. It does not install or unset OS PAC.
- `gatewaycoord`: gateway coordination. It owns the Gateway State Cache and owner verification/claiming semantics.
- `cli`: the terminal surface. It parses arguments, prompts, renders text, and either becomes a Gateway Owner or acts as a client to an existing owner.

The foreground process that hosts the Gateway Router is the Gateway Owner. When Gateway Runtime is active, the owner also owns Gateway Footprint Cleanup. `cmd/seamless-cors/main.go` remains a tiny call into `cli.Run`.

The Gateway State Cache is a client discovery cache and owner lease identity, not runtime status. Its intended shape is:

```go
type GatewayStateCache struct {
	HTTPRouterListen string `json:"httpRouterListen"`
	Token            string `json:"token"`
}
```

The token is a local coordination nonce generated per Gateway Owner. The cache is stored as `gateway-state-cache.json`, written with `0600` permissions, and removed when the owner exits. This is a clean-break cache: the implementation does not read, migrate, or preserve the old `control-state.json` filename or `controlListen` schema. Runtime details such as proxy listener, PAC listener, domain count, CA state, and pending lifecycle information come from `/status`, not from the cache.

The Gateway Owner continuously watches the cache as a lease. If the cache disappears or its `HTTPRouterListen` or `Token` no longer match the owner identity, the owner shuts down immediately. If Gateway Runtime is active, shutdown includes full Gateway Footprint Cleanup.

## Consequences

CLI and HTTP become sibling surfaces over the same facade rather than one surface calling the other.

`serve` can host a router without implying Gateway Runtime is active. If a valid Gateway Owner already exists, `serve` fails clearly because it cannot become the owner. CLI `start` with no existing owner becomes a Gateway Owner and activates Gateway Runtime while hosting the Gateway Router. CLI `start` with an existing owner fails clearly instead of delegating activation. HTTP `/start` remains available for HTTP clients to activate Gateway Runtime on an existing router-only owner. `/stop` stops everything owned by the process; a separate runtime-only stop route is intentionally out of scope.

A new Gateway Owner has a synchronous prepare phase before it is discoverable: generate token, construct facade, construct router, and bind the HTTP router listener. Activation then starts HTTP serving, writes the Gateway State Cache, starts the lease watcher, optionally runs direct `start` activation, and enters the owner run loop. If cache write fails after serving starts, the server shuts down and the process exits without becoming owner.

`PlanStart` is non-mutating. `ExecuteStart` recomputes decisive conditions before mutating and activates runtime without blocking until runtime exit. The owner run loop waits on OS signals, `/stop` shutdown requests, and fatal runtime errors.

Router-hosted start uses `GET /start/plan` followed by `POST /start` for HTTP clients. The owner returns Start Plan details, the HTTP client renders consent, and `ExecuteStart` receives explicit consent input. If decisive conditions changed, `ExecuteStart` returns a structured consent-required result instead of mutating. `serve` never prompts in its terminal for router-hosted start.

The cache must be written atomically and claimed carefully: claim missing ownership with exclusive create, update owned cache with identity guard and atomic replace, and replace stale cache only after verification says the recorded owner is unreachable.

## Open Questions

Finalize exact structured result types, status vocabulary, and route response codes.
