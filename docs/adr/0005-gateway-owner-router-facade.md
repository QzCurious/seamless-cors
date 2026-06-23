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

`PlanStart` is non-mutating. It returns a narrower `StartPlanKind` vocabulary: `ready`, `consent-required`, or `already-running`. `ExecuteStart` recomputes decisive conditions before mutating and activates runtime without blocking until runtime exit. It returns a separate `StartResultKind` vocabulary: `started`, `already-running`, `consent-required`, `start-already-mutating`, or `platform-approval-denied`. The owner run loop waits on OS signals, `/stop` shutdown requests, and fatal runtime errors.

Expected lifecycle blocks are represented as Surface-Neutral Command Results, not as exceptional failures. This includes outcomes such as consent required, already running, start already mutating, uninstall blocked because Gateway Runtime is active, and retryable stop cleanup failure. Result kinds are closed per operation, such as `StartResultKind` and `StopResultKind`, rather than a single global result code set. Unexpected infrastructure failures still surface as errors, such as listener bind failure, filesystem failure, malformed stored state, or platform adapter failure.

Router-hosted start uses `GET /start/plan` followed by `POST /start` for HTTP clients. The owner returns Start Plan details, the HTTP client renders consent, and `ExecuteStart` receives explicit consent input. If decisive conditions changed, `ExecuteStart` returns a structured consent-required result instead of mutating. `serve` never prompts in its terminal for router-hosted start.

`StartPlanKind` and `StartResultKind` both use the same Lifecycle Consent Detail when returning `consent-required`. The envelopes differ, but the consent detail shape is shared so execute-time rechecks report the same kind of user decision a fresh plan would report.

Lifecycle Consent Detail is limited to gateway-owned lifecycle consent. Platform-owned approval, such as the OS trust prompt that may appear during CA Ensure, is not modeled as start consent detail and is reached only during execution after gateway consent has been accepted and rechecked. Denied platform approval is a `platform-approval-denied` start result, not an error. Other platform failures remain errors, including adapter command failure, unexpected unsupported capability, keychain corruption, permission failure, or PAC install failure. Router-hosted start leaves the router-only Gateway Owner alive after `platform-approval-denied`, while direct CLI `start` follows the direct-start failure path that removes owner visibility before exit.

The start plan and consent data shape is:

```go
type StartPlan struct {
	Kind    StartPlanKind          `json:"kind"`
	Consent *LifecycleConsentDetail `json:"consent,omitempty"`
}

type LifecycleConsentDetail struct {
	Requirements []ConsentRequirement `json:"requirements"`
}

type ConsentRequirement struct {
	Kind                  ConsentRequirementKind      `json:"kind"`
	ManagedPACReplacement *ManagedPACReplacementDetail `json:"managedPacReplacement,omitempty"`
}

type ConsentRequirementKind string

const ConsentRequirementManagedPACReplacement ConsentRequirementKind = "managed-pac-replacement"

type ManagedPACReplacementDetail struct {
	CurrentPACState []ManagedPACServiceState `json:"currentPacState"`
	CleanupMode     CleanupMode              `json:"cleanupMode"`
}

type CleanupMode string

const CleanupModeNoPACRestoration CleanupMode = "no-pac-restoration"

type ManagedPACServiceState struct {
	ServiceName string       `json:"serviceName"`
	Enabled     bool         `json:"enabled"`
	URL         string       `json:"url"`
	Ownership   PACOwnership `json:"ownership"`
}

type PACOwnership string

const (
	PACOwnershipEmpty   PACOwnership = "empty"
	PACOwnershipOwned   PACOwnership = "owned"
	PACOwnershipForeign PACOwnership = "foreign"
)
```

`LifecycleConsentDetail.Requirements` is a stable list of uniquely keyed requirements, not top-level booleans. `managed-pac-replacement` is required when a managed service has a non-empty, non-owned PAC URL that start would overwrite, whether that PAC URL is currently enabled or disabled. Owned PAC URLs and empty PAC URLs do not require consent. The detail includes the current PAC state for managed services so surfaces can render the service names, URLs, enabled flags, ownership, and the no-restoration cleanup behavior without embedding facade-authored prompt text.

`POST /start` sends consent input by accepted requirement kind, not by plan ID:

```go
type StartRequest struct {
	Consent *LifecycleConsentInput `json:"consent,omitempty"`
}

type LifecycleConsentInput struct {
	AcceptedRequirements []ConsentRequirementKind `json:"acceptedRequirements"`
}
```

There is still no plan ID, plan token, fingerprint, or stale-plan protocol. `ExecuteStart` recomputes current consent requirements and proceeds only when the accepted requirement kinds cover every current requirement. If they do not, it returns `consent-required` with the current Lifecycle Consent Detail. Extra accepted requirement kinds are ignored by the facade; HTTP JSON validation may reject unknown strings before calling the facade.

Successful `ExecuteStart` returns `StartResultKind` `started` with Start Guidance Detail:

```go
type StartResult struct {
	Kind     StartResultKind      `json:"kind"`
	Consent  *LifecycleConsentDetail `json:"consent,omitempty"`
	Guidance *StartGuidanceDetail `json:"guidance,omitempty"`
}

type StartGuidanceDetail struct {
	ConfigPath         string `json:"configPath"`
	DomainListPath     string `json:"domainListPath"`
	ManagedPACActive   bool   `json:"managedPacActive"`
	CATrusted          bool   `json:"caTrusted"`
	InstalledCAChanged bool   `json:"installedCaChanged,omitempty"`
}
```

Start Guidance Detail is data for surfaces to render; it does not include proxy, PAC, or router listener endpoints. `ManagedPACActive` is true after successful activation because PAC Routing is installed independently of current Domain List contents.
`InstalledCAChanged` is true only when CA Ensure added or replaced Installed User CA trust during this start; surfaces may use it to render a success acknowledgement without re-inspecting CA state.

`already-running` start plan and start execution results remain minimal and carry no status or guidance detail. Clients that need runtime details call `Status`; start results do not smuggle runtime status into planning or execution.

`start-already-mutating` is also minimal. It carries no retry-after value, queue position, progress state, or current mutation detail; it only reports that another start execution is already mutating lifecycle state.

`Stop` returns a closed `StopResultKind` vocabulary: `stopped` or `cleanup-failed`. Runtime close failures are warnings on the stop result because Gateway Runtime resources are Process-Bound Cleanup Subjects and process termination is the backstop. A `stopped` result means Blocking Cleanup Subjects were removed and owner shutdown is allowed, even if warnings are present. A `cleanup-failed` result means at least one Blocking Cleanup Subject remains, so the Gateway Owner stays alive for retry and the result carries blocking cleanup failure detail.

Stopping a router-only Gateway Owner also returns `stopped` when Blocking Cleanup Subjects are removed. There is no separate `router-only-stopped` result kind; stop semantics are owner-oriented, not runtime-state-branded.

Stop cleanup detail is typed by Blocking Cleanup Subject kind:

```go
type StopResult struct {
	Kind            StopResultKind         `json:"kind"`
	Warnings        []CommandWarning       `json:"warnings,omitempty"`
	CleanupFailures []CleanupFailureDetail `json:"cleanupFailures,omitempty"`
}

type CleanupFailureDetail struct {
	Subject    CleanupSubjectKind `json:"subject"`
	Diagnostic string             `json:"diagnostic,omitempty"`
}

type CleanupSubjectKind string

const (
	CleanupSubjectManagedPAC        CleanupSubjectKind = "managed-pac"
	CleanupSubjectGatewayStateCache CleanupSubjectKind = "gateway-state-cache"
)

type CommandWarning struct {
	Kind       CommandWarningKind `json:"kind"`
	Diagnostic string            `json:"diagnostic,omitempty"`
}

type CommandWarningKind string

const CommandWarningRuntimeCloseFailed CommandWarningKind = "runtime-close-failed"
```

Surfaces branch on `CleanupFailureDetail.Subject`, not on diagnostic text. Diagnostics are supporting human or log detail only.

`Install` returns a closed `InstallResultKind` vocabulary: `installed` or `already-usable`. `installed` means Installed User CA material or trust changed; `already-usable` means the current Installed User CA state was usable and no change was needed. Both results carry Installed User CA expiry when known:

```go
type InstallResult struct {
	Kind               InstallResultKind `json:"kind"`
	InstalledCAExpires time.Time         `json:"installedCAExpires,omitempty"`
	Advisories         []InstallAdvisory `json:"advisories,omitempty"`
}

type InstallResultKind string

const (
	InstallResultInstalled     InstallResultKind = "installed"
	InstallResultAlreadyUsable InstallResultKind = "already-usable"
)

type InstallAdvisory struct {
	Kind                    InstallAdvisoryKind             `json:"kind"`
	ConfigCATrustedDisabled *ConfigCATrustedDisabledAdvisory `json:"configCaTrustedDisabled,omitempty"`
}

type InstallAdvisoryKind string

const InstallAdvisoryConfigCATrustedDisabled InstallAdvisoryKind = "config-ca-trusted-disabled"

type ConfigCATrustedDisabledAdvisory struct {
	ConfigPath    string `json:"configPath"`
	Setting       string `json:"setting"`
	CurrentValue  bool   `json:"currentValue"`
	RequiredValue bool   `json:"requiredValue"`
}
```

Surfaces decide how to render `InstalledCAExpires`; CLI may keep the current date-only display while HTTP can expose the timestamp.

If an existing readable config has `ca-trusted: false`, `InstallResult.Advisories` includes `config-ca-trusted-disabled` with `Setting` `ca-trusted`, `CurrentValue` `false`, and `RequiredValue` `true`. This is advisory data, not a third install result kind. Missing, unreadable, or invalid config does not make install fail and does not produce this advisory, preserving Config-Independent CA Install.

`Uninstall` returns a closed `UninstallResultKind` vocabulary: `uninstalled`, `already-absent`, or `blocked-runtime-active`. `uninstalled` means seamless-cors-owned current-user CA trust or local CA material was present and Complete CA Uninstall removed it. `already-absent` means both owned trust and local material were already absent before mutation. `blocked-runtime-active` means Gateway Runtime is active, so uninstall did not mutate CA trust or local material. Router-only Gateway Owners may return `uninstalled` or `already-absent`.

```go
type UninstallResult struct {
	Kind UninstallResultKind `json:"kind"`
}

type UninstallResultKind string

const (
	UninstallResultUninstalled          UninstallResultKind = "uninstalled"
	UninstallResultAlreadyAbsent        UninstallResultKind = "already-absent"
	UninstallResultBlockedRuntimeActive UninstallResultKind = "blocked-runtime-active"
)
```

Removal errors, post-removal inspection errors, or a post-removal state that is not missing are errors, not additional result kinds.

`Status` returns a read-only snapshot. Its top-level gateway status vocabulary is `not-running`, `stale-cache`, `router-only`, or `running`. Cleanup-needed state and Installed User CA health are separate details, not gateway status kinds:

```go
type StatusResult struct {
	Kind        GatewayStatusKind       `json:"kind"`
	Owner       *OwnerStatusDetail       `json:"owner,omitempty"`
	Runtime     *RuntimeStatusDetail     `json:"runtime,omitempty"`
	Cleanup     CleanupStatusDetail      `json:"cleanup"`
	InstalledCA InstalledCAStatusDetail  `json:"installedCA"`
}

type GatewayStatusKind string

const (
	GatewayStatusNotRunning GatewayStatusKind = "not-running"
	GatewayStatusStaleCache GatewayStatusKind = "stale-cache"
	GatewayStatusRouterOnly GatewayStatusKind = "router-only"
	GatewayStatusRunning    GatewayStatusKind = "running"
)

type OwnerStatusDetail struct {
	RouterListen string `json:"routerListen"`
}

type RuntimeStatusDetail struct {
	ProxyListen      string                       `json:"proxyListen"`
	PACListen        string                       `json:"pacListen"`
	DomainListPath   string                       `json:"domainListPath"`
	DomainCount      int                          `json:"domainCount"`
	CATrusted        bool                         `json:"caTrusted"`
	PendingLifecycle []PendingLifecycleChangeKind `json:"pendingLifecycle,omitempty"`
}

type PendingLifecycleChangeKind string

const PendingLifecycleChangeCATrusted PendingLifecycleChangeKind = "ca-trusted"

type CleanupStatusDetail struct {
	Needed   bool                 `json:"needed"`
	Subjects []CleanupSubjectKind `json:"subjects,omitempty"`
}

type InstalledCAStatusDetail struct {
	Health  CAHealthStatus `json:"health"`
	Expires time.Time      `json:"expires,omitempty"`
}

type CAHealthStatus string

const (
	CAHealthUsable             CAHealthStatus = "usable"
	CAHealthMissing            CAHealthStatus = "missing"
	CAHealthExpired            CAHealthStatus = "expired"
	CAHealthExpiringSoon       CAHealthStatus = "expiring-soon"
	CAHealthInvalid            CAHealthStatus = "invalid"
	CAHealthMultiple           CAHealthStatus = "multiple"
	CAHealthMismatchedMaterial CAHealthStatus = "mismatched-material"
	CAHealthUnsupported        CAHealthStatus = "unsupported"
	CAHealthUnknown            CAHealthStatus = "unknown"
)
```

`running` includes both `OwnerStatusDetail` and `RuntimeStatusDetail`. `router-only` includes `OwnerStatusDetail` and omits runtime detail. `not-running` and `stale-cache` omit both owner and runtime detail. `CleanupStatusDetail.Subjects` reuses `CleanupSubjectKind`; `stale-cache` includes `gateway-state-cache`, and owned active PAC state that is not part of an active Gateway Runtime includes `managed-pac`. Status does not mutate these subjects.

`Check` does not use a result kind. It returns the Detailed Capability Report directly:

```go
type CheckResult struct {
	Platform          string           `json:"platform"`
	Supported         bool             `json:"supported"`
	PACManagement     CapabilityStatus `json:"pacManagement"`
	CATrustManagement CapabilityStatus `json:"caTrustManagement"`
	RuntimeCleanup    CapabilityStatus `json:"runtimeCleanup"`
}

type CapabilityStatus string

const (
	CapabilitySupported   CapabilityStatus = "supported"
	CapabilityUnsupported CapabilityStatus = "unsupported"
	CapabilityLimited     CapabilityStatus = "limited"
	CapabilityUnknown     CapabilityStatus = "unknown"
)
```

Gateway Router HTTP status codes do not duplicate facade result semantics. Expected command results return `200 OK` with their structured result body, including `consent-required`, `already-running`, `start-already-mutating`, `platform-approval-denied`, `cleanup-failed`, `blocked-runtime-active`, and status snapshots such as `not-running` or `stale-cache`.

The router reserves non-2xx responses for HTTP/request failures and unexpected server failures:

- `401 Unauthorized` when the router token is missing or invalid.
- `400 Bad Request` when request JSON is malformed or fails HTTP-layer validation, such as an unknown enum string.
- `404 Not Found` for unknown routes.
- `405 Method Not Allowed` for known routes with the wrong method.
- `500 Internal Server Error` when facade execution returns an unexpected error rather than a structured command result.

For successful `POST /stop`, the router writes the `200 OK` stop result before triggering owner shutdown. If the result is `cleanup-failed`, the owner remains alive for retry.

The cache must be written atomically and claimed carefully: claim missing ownership with exclusive create, update owned cache with identity guard and atomic replace, and replace stale cache only after verification says the recorded owner is unreachable.

## Open Questions

None.
