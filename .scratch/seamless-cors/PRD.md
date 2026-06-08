Status: ready-for-agent

# seamless-cors PRD

## Problem Statement

Frontend developers and QA engineers often need to run a local frontend against upstream APIs whose authors have not configured CORS for local development. The browser blocks useful API calls before the developer can inspect real upstream behavior, especially when preflight requests fail or response CORS headers are missing, incomplete, duplicated, or incompatible with credentials.

The user wants a local DEV/QA tool that behaves like a seamless-cors for configured upstream domains. Browser application code should keep its original request URLs. The user should only manage an explicit configuration file and a one-domain-per-line Domain List, while the gateway handles selective routing, preflight answers, and response CORS repair.

## Solution

Build a Go-based seamless-cors distributed as platform-specific binaries so users do not need to install an additional runtime. The gateway runs in the foreground, uses a Minimal Command Surface of `start`, `stop`, and `status`, and creates a commented default configuration plus Domain List on first start.

On Managed Platforms, the gateway uses Managed System Proxy with PAC Routing so only Domain List matches route through the gateway and unrelated traffic goes `DIRECT`. macOS and Windows are Managed Platforms for v1. Linux is a Manual Platform for v1, where users can still run the gateway and configure their browser or test tool manually.

For matched HTTP requests, the gateway applies HTTP CORS Scope behavior: Local Preflight Answer, Response Repair, CORS Header Replacement, Concrete Exposed Headers, Credentialed Reflection, Origin Vary Preservation, and Private Network Access Reflection. For matched HTTPS requests, Trusted HTTPS Interception is enabled only when `ca-trusted` is true. CA trust is opt-in by default, uses OS Trust Only, and creates an Ephemeral User CA that is removed during Graceful Cleanup.

The gateway does not rewrite request headers, cookies, auth/session behavior, WebSocket behavior, response bodies, streams, or application logic.

## User Stories

1. As a frontend developer, I want to call a configured API from my local frontend without changing request URLs, so that I can keep my app code close to production.
2. As a frontend developer, I want failed upstream CORS preflights to be answered locally, so that the real request can reach the upstream API during DEV/QA.
3. As a frontend developer, I want upstream responses to be CORS-repaired, so that browser code can read real API responses.
4. As a frontend developer, I want credentialed CORS responses to work with reflected origins, so that browser-valid credential flows are not blocked by wildcard CORS limitations.
5. As a frontend developer, I want `Origin: null` to be reflected for matched requests, so that local files and sandboxed DEV/QA cases can be tested.
6. As a frontend developer, I want CORS repair to apply to upstream error statuses, so that I can see real `401`, `403`, `404`, or `500` responses instead of browser CORS errors.
7. As a frontend developer, I want gateway-generated errors to be JSON and CORS-readable, so that my frontend can inspect why the gateway failed.
8. As a frontend developer, I want gateway errors to clearly identify the seamless-cors as the source, so that I do not confuse gateway failures with upstream API responses.
9. As a QA engineer, I want Private Network Access preflight support, so that local and LAN API targets can be tested from browsers that enforce PNA.
10. As a QA engineer, I want Local Targets such as localhost, loopback, private IPs, and plain HTTP origins to be valid Domain List entries, so that local services and staging boxes can be tested.
11. As a QA engineer, I want one Domain List Entry per line, so that configuration is quick to edit and review.
12. As a QA engineer, I want Domain List comments, so that I can annotate staging, local, and team-specific entries.
13. As a QA engineer, I want hostname shorthand to match the same host across schemes and ports, so that common DEV/QA entries are easy to write.
14. As a QA engineer, I want full origin entries to match exact scheme, host, and port, so that port-specific behavior can be tested precisely.
15. As a QA engineer, I want exact domain matching by default, so that adding one host does not unexpectedly include subdomains.
16. As a QA engineer, I want Single-Label Wildcard support, so that I can include one level of subdomains explicitly.
17. As a QA engineer, I want IPv6 entries to require full origin syntax, so that IPv6 matching remains unambiguous.
18. As a user, I want Managed System Proxy to use Selective Managed Proxy behavior, so that unrelated browser traffic bypasses the gateway.
19. As a user, I want PAC Routing to send non-matched traffic `DIRECT`, so that unrelated auth, passkey, mobile handoff, banking, and corporate login flows are not disturbed.
20. As a user, I want Trust-Aware PAC Routing, so that matched HTTPS domains are routed through the gateway only when Trusted HTTPS Interception is enabled.
21. As a user, I want Manual Proxy Mode, so that I can opt out of OS proxy changes and configure a browser or tool myself.
22. As a user in Manual Proxy Mode, I want Untouched HTTPS Tunnel behavior for non-intercepted HTTPS traffic, so that browsing does not break when traffic reaches the proxy.
23. As a user, I want a generated PAC served from a PAC Endpoint, so that Domain List routing updates without reinstalling OS proxy settings.
24. As a user, I want the Generated PAC to be derived from Explicit Configuration and the Domain List, so that I do not need to maintain PAC code manually.
25. As a user, I want the gateway to chain an existing proxy where supported, so that corporate proxy settings are preserved.
26. As a user, I want startup to fail clearly when proxy chaining is unsupported, so that my existing proxy setup is not silently replaced.
27. As a user, I want Opt-In CA Trust, so that the first generated configuration does not automatically trust a local CA.
28. As a user, I want `ca-trusted: true` to enable Trusted HTTPS Interception, so that HTTPS CORS repair happens only after an explicit choice.
29. As a user, I want `ca-trusted: false` to disable HTTPS interception completely, so that the gateway does not perform broken or untrusted MITM.
30. As a user, I want OS Trust Only, so that the gateway manages only my operating-system user trust store and not browser profile stores.
31. As a user, I want the Ephemeral User CA to be deleted on stop, so that CA files and trust do not remain after the gateway stops.
32. As a user, I want CA Recovery on start and stop, so that leftover CA trust and files from an unclean shutdown can be cleaned using gateway markers.
33. As a user, I want Proxy Recovery on start and stop, so that leftover OS proxy settings from an unclean shutdown can be restored using gateway markers.
34. As a user, I want Marker-Based Recovery only, so that the gateway never guesses at unrelated proxy or CA state.
35. As a user, I want Ctrl-C to trigger Graceful Cleanup, so that foreground use restores proxy settings and removes CA trust.
36. As a user, I want `stop` to trigger cleanup or recovery, so that I can clean up even when the original terminal is unavailable.
37. As a user, I want `status` to be read-only, so that checking state never changes proxy settings, CA trust, or runtime files.
38. As a user, I want Human Status, so that I can quickly understand whether the gateway is running, which domains are active, and whether lifecycle changes are pending.
39. As a user, I want no JSON status mode in v1, so that the command surface stays simple and interactive.
40. As a user, I want Foreground Start, so that logs and Ctrl-C cleanup are visible.
41. As a user, I want Single User Instance behavior, so that PAC, CA, and recovery state are not corrupted by competing gateway processes.
42. As a user, I want fixed listener ports by default, so that PAC routing and recovery are predictable.
43. As a user, I want startup to fail if listener ports are occupied, so that the gateway does not silently switch ports.
44. As a user, I want Loopback Default behavior, so that proxy, PAC, and control endpoints are not exposed to the LAN unless explicitly configured.
45. As a user, I want warnings for non-loopback listener configuration, so that I understand when I am exposing the gateway beyond my machine.
46. As a user, I want a separate Control Endpoint, so that `status` and `stop` do not mix with browser proxy traffic.
47. As a user, I want listener configuration values to be host-port Listener Addresses, so that config is precise and not confused with URLs.
48. As a user, I want Explicit Configuration with hyphenated keys, so that configuration mirrors CLI flag names.
49. As a user, I want One-Run Override flags, so that I can temporarily override config without editing files.
50. As a user, I want boolean flags to support shorthand true and `--key=false`, so that CLI usage is compact but unambiguous.
51. As a user, I want First-Start Bootstrap, so that missing config and Domain List files are created automatically.
52. As a user, I want Commented Default Config, so that generated settings explain themselves briefly.
53. As a user, I want first bootstrap to print file paths only, so that the gateway does not unexpectedly open editors.
54. As a user, I want the Home Config Directory to store config, domains, and runtime state under `.seamless-cors`, so that files are easy to find across platforms.
55. As a user, I want Path Expansion for path fields, so that `~` and environment variables work in config paths.
56. As a user, I want invalid `config.yaml` at start to fail with a report, so that misconfiguration is obvious.
57. As a user, I want invalid `config.yaml` while running to log a Fatal Config Error and stop cleanly, so that partially applied configuration never happens.
58. As a user, I want invalid Domain List entries at start to fail with line-level reports, so that I can fix the file before lifecycle changes apply.
59. As a user, I want invalid Domain List entries while running to be skipped line-by-line, so that valid entries keep working.
60. As a user, I want an empty Domain List to fail startup, so that the gateway does not start as a no-op managed proxy.
61. As a user, I want an empty Domain List while running to route everything direct, so that live edits do not force shutdown.
62. As a user, I want Full URL Logging for matched requests, so that DEV/QA debugging includes query strings.
63. As a user, I want matched requests logged at info level, so that I can confirm Domain List and PAC routing behavior.
64. As a user, I want unmatched traffic quiet by default, so that unrelated traffic does not fill logs.
65. As a user, I want Authorization, Cookie, Set-Cookie, request bodies, and response bodies omitted from default logs, so that sensitive values are not casually printed.
66. As a frontend developer, I want no request header rewriting, so that upstream application checks are not silently bypassed.
67. As a frontend developer, I want no cookie rewriting, so that session and authentication behavior remains owned by the upstream application.
68. As a frontend developer, I want WebSocket Skip behavior, so that WebSocket origin checks are not confused with CORS repair.
69. As a frontend developer, I want Default Upstream Negotiation, so that Go can use normal upstream protocol negotiation without forcing HTTP/1.1.
70. As a frontend developer, I want upstream TLS verification failures to produce Explicit Gateway Errors, so that invalid upstream certificates are not hidden.
71. As a frontend developer, I want no gateway-imposed request timeout, so that the gateway does not introduce application deadlines.
72. As a frontend developer, I want Client Abort Propagation, so that browser cancellation cleans up upstream work.

## Implementation Decisions

- Build the seamless-cors in Go and ship one Gateway Distribution per operating system and CPU architecture. Users should not need to install an additional runtime.
- Provide only Three Commands in v1: `start`, `stop`, and `status`.
- `start` uses Foreground Start. It does not launch an official daemon or service in v1.
- Use hyphenated configuration keys and matching CLI flags.
- Support One-Run Override flags. Flags override Explicit Configuration only for the current process and must be shown in status.
- Boolean flags support shorthand true and explicit false, such as `--ca-trusted` and `--ca-trusted=false`.
- Use Home Config Directory for user files and Runtime State Directory for runtime markers, control tokens, recovery snapshots, and ephemeral CA files.
- First-Start Bootstrap creates a Commented Default Config and Domain List file, prints their paths, and does not open an editor.
- Generated config defaults:
  - `proxy-listen: 127.0.0.1:8080`
  - `pac-listen: 127.0.0.1:8079`
  - `control-listen: 127.0.0.1:8078`
  - `managed-system-proxy: true`
  - `domain-list: ~/.seamless-cors/domains.txt`
  - `log-level: info`
  - `ca-trusted: false`
- Listener Address values are host-port strings, not URLs.
- Listener settings, `managed-system-proxy`, and `ca-trusted` are Lifecycle Operations. Changes while running become Pending Lifecycle Changes and require Restart-Applied Lifecycle.
- `domain-list`, Domain List contents, and `log-level` are Live Configuration. Config reload is All-Or-Nothing Config.
- Invalid config at start fails startup. Invalid config while running is a Fatal Config Error that logs the problem, performs Graceful Cleanup, and stops.
- Domain List validation is line-level. Invalid entries fail startup, but while running valid entries continue to serve and invalid entries are skipped and reported.
- Active Domain Requirement requires at least one valid Domain List Entry at startup, even if current lifecycle settings make every entry non-repairable.
- Domain List supports blank lines, full-line comments, and inline comments.
- Domain List supports Hostname Shorthand matching any scheme and port for the exact host.
- Domain List supports full origins for exact scheme, host, and port matching.
- Domain List uses Exact Domain Match by default. Subdomains require Single-Label Wildcard entries.
- Local Targets are allowed.
- IPv6 targets require IPv6 Full Origin syntax.
- Managed System Proxy uses PAC Routing and Generated PAC served from PAC Endpoint.
- PAC Routing is Trust-Aware PAC Routing: HTTPS matches route through the gateway only when Trusted HTTPS Interception is enabled.
- Managed Platform v1 support is macOS and Windows. Linux is a Manual Platform in v1.
- Existing proxy settings should use Proxy Chaining when supported. Unsupported existing proxy settings should not be silently replaced.
- Manual Proxy Mode exposes the Proxy Listener without configuring OS proxy settings.
- Manual Proxy Mode uses Untouched HTTPS Tunnel for HTTPS traffic not eligible for Trusted HTTPS Interception.
- The Control Endpoint is separate from Proxy Listener and PAC Endpoint, loopback by default, and protected by runtime state such as a random control token.
- Enforce Single User Instance.
- Use Graceful Cleanup on Ctrl-C, `stop`, and normal termination.
- Use Marker-Based Recovery on `start` and `stop` only. `status` is Read-Only Status.
- Proxy Recovery restores only proxy state proven by runtime markers.
- CA Recovery removes only Ephemeral User CA state proven by runtime markers.
- Use OS Trust Only. Do not manage or diagnose browser-specific trust stores in v1.
- Use Ephemeral User CA when `ca-trusted` is true. Generate and trust it at start, then remove trust and local files during Graceful Cleanup.
- If `ca-trusted` is false, do not intercept HTTPS at all.
- If upstream TLS verification fails, produce a JSON Gateway Error with Gateway Error Status Mapping and make it a CORS-Readable Gateway Error for matched Origin-gated requests.
- Use Default Upstream Negotiation for gateway-to-upstream requests. Do not promise HTTP/2-specific behavior.
- Apply HTTP CORS Scope only. WebSocket Skip leaves WebSocket upgrade behavior untouched.
- Do not rewrite request headers except ordinary proxy transport mechanics.
- Cookie Out of Scope: do not rewrite cookie attributes, values, sessions, or authentication behavior.
- Do not rewrite response bodies, streaming payloads, or protocol frames.
- Use Origin-Gated Rewriting. Requests without `Origin` do not receive CORS repair.
- Local Preflight Answer handles matched `OPTIONS` requests that include `Origin` and `Access-Control-Request-Method`.
- Preflight responses use Requested Method Reflection and Requested Header Reflection.
- Preflight responses use Fixed Preflight Cache with `Access-Control-Max-Age: 600`.
- Preflight responses include Private Network Access Reflection when requested.
- Response Repair uses CORS Header Replacement before writing the Reflective DEV/QA Policy.
- Response Repair uses Global CORS Policy for every Domain List match.
- Response Repair uses Credentialed Reflection and Null Origin Reflection.
- Response Repair uses Origin Vary Preservation.
- Response Repair uses Concrete Exposed Headers, listing actual upstream response headers except CORS headers.
- Response Repair uses All-Status Repair for upstream statuses.
- Do not impose a gateway request timeout by default. Use only transport and idle cleanup timeouts.
- Propagate client aborts upstream.
- Full URL Logging includes query strings for matched request logs.
- Default logs must not include Authorization headers, Cookie headers, Set-Cookie headers, request bodies, or response bodies.
- Matched Request Logging logs every matched request routed through the gateway.
- Unmatched Quiet Mode keeps unrelated traffic out of normal logs.

Major modules to build:

- CLI and command lifecycle module: parses commands/flags, applies One-Run Override, starts foreground runtime, invokes stop/status control calls, and prints human output.
- Configuration module: loads Explicit Configuration, applies Path Expansion for path fields, validates all-or-nothing config, tracks live vs lifecycle settings, and detects Pending Lifecycle Changes.
- Domain List module: parses comments and one-line entries, validates entries with line numbers, supports Hostname Shorthand, full origins, Single-Label Wildcard, Local Targets, and IPv6 Full Origin.
- Domain matching module: exposes a small matcher interface used by PAC generation and proxy request handling.
- PAC module: generates Generated PAC from the current Domain List and lifecycle state, serves the PAC Endpoint, and implements Trust-Aware PAC Routing.
- Proxy core module: implements HTTP proxying, CONNECT handling, Untouched HTTPS Tunnel, Trusted HTTPS Interception, Default Upstream Negotiation, Client Abort Propagation, and no request timeout behavior.
- CORS repair module: implements Local Preflight Answer, Reflective DEV/QA Policy, CORS Header Replacement, Concrete Exposed Headers, Origin Vary Preservation, All-Status Repair, and CORS-Readable Gateway Error.
- CA module: creates Ephemeral User CA, generates leaf certificates, manages OS Trust Only user trust, performs Full CA Removal, and supports CA Recovery using markers.
- Platform adapter module: manages Managed Platform OS proxy/PAC configuration, Proxy Chaining, Proxy Recovery, and loopback listener warnings.
- Control module: owns Control Endpoint, runtime token, Single User Instance state, `stop`, and Read-Only Status.
- Logging/status module: implements Human Status, Full URL Logging, Matched Request Logging, Unmatched Quiet Mode, sensitive-value exclusion, and pending lifecycle reporting.

Deep modules to extract and test in isolation:

- Domain List parser and matcher.
- PAC generator.
- CORS repair engine.
- Config loader/validator with live/lifecycle classification.
- Gateway error builder.
- Recovery marker reader/writer.
- Platform adapter contracts with fake adapters.

## Testing Decisions

- Tests should verify external behavior, not implementation details. A good test should describe observable gateway behavior: generated PAC output, response headers, status messages, cleanup calls to platform adapters, parsed Domain List matches, and gateway error payloads.
- Domain List parser tests should cover comments, blank lines, inline comments, hostname shorthand, full origins, exact matching, Single-Label Wildcard, invalid wildcard positions, Local Targets, IPv6 Full Origin, startup validation failure, and running-session line-level invalid handling.
- Config tests should cover generated defaults, hyphenated keys, path expansion for path fields, One-Run Override precedence, All-Or-Nothing Config, Fatal Config Error behavior, live setting changes, lifecycle pending changes, and listener address validation.
- PAC generator tests should cover managed mode, manual mode, Trust-Aware PAC Routing, `DIRECT` for unmatched traffic, HTTP matches when `ca-trusted` is false, HTTPS matches when `ca-trusted` is true, exact host matching, wildcard matching, and custom ports.
- CORS repair tests should cover Local Preflight Answer, Requested Header Reflection, Requested Method Reflection, Fixed Preflight Cache, Private Network Access Reflection, CORS Header Replacement, Credentialed Reflection, Null Origin Reflection, Origin Vary Preservation, Concrete Exposed Headers, and All-Status Repair.
- Gateway error tests should cover JSON Gateway Error shape, Explicit Gateway Error source fields, CORS-Readable Gateway Error headers, and Gateway Error Status Mapping.
- Proxy core tests should use local upstream test servers to verify HTTP forwarding, no request header rewriting, response repair, WebSocket Skip, Untouched HTTPS Tunnel behavior, upstream TLS failure handling, and Client Abort Propagation.
- CA tests should use fake trust-store adapters for OS Trust Only behavior, Ephemeral User CA creation, Full CA Removal, and CA Recovery marker behavior. Avoid mutating the real OS trust store in automated tests.
- Platform adapter tests should use fake macOS/Windows adapters for PAC installation, Proxy Chaining, Proxy Recovery, existing proxy conflict handling, and unsupported adapter behavior. Real OS adapter tests should be manual or explicitly gated.
- Control endpoint tests should cover Single User Instance, token-protected status/stop calls, Read-Only Status, and stop-triggered cleanup.
- Logging tests should verify matched request logging includes full URLs with query strings while excluding Authorization, Cookie, Set-Cookie, request bodies, and response bodies.
- There is no existing application code or prior test suite in the repo. New implementation should establish the first test structure, with unit tests for deep modules before integration tests for proxy behavior.

## Out of Scope

- Browser extension implementation.
- VPN/TUN routing.
- Linux Managed System Proxy in v1.
- Browser-specific certificate trust stores or browser profile diagnostics.
- Persistent user CA.
- Per-domain CORS policies.
- Cookie repair, cookie diagnostics, session rewriting, or authentication bypass.
- Request header rewriting, including `Origin`, `Referer`, `Authorization`, or `Cookie`.
- Request or response body rewriting.
- WebSocket CORS/origin repair or frame rewriting.
- HTTPS fronting for HTTP APIs.
- Insecure upstream TLS mode.
- Machine-readable JSON status.
- Background daemon/service mode.
- Config editing commands.
- `init`, `trust-ca`, `untrust-ca`, or `apply-lifecycle` commands.
- Automatic port selection.
- Multi-instance operation for a single user.

## Further Notes

- The repo has ADR-0001 for Ephemeral User CA and ADR-0002 for PAC-Based Selective Managed Proxy. Implementations should respect both.
- `status` is read-only and must never perform recovery or cleanup.
- `start` and `stop` may perform Marker-Based Recovery.
- `ca-trusted: false` is the generated default, so first-run HTTPS Domain List entries may be valid but non-repairable until CA trust is explicitly enabled through config or a One-Run Override.
- If generated files are missing, First-Start Bootstrap should create them and print the paths. If the Domain List has no active entries, startup should fail with instructions to add entries.
- The project name in the glossary is seamless-cors, but the canonical product term is seamless-cors.
