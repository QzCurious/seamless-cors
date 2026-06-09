# seamless-cors

seamless-cors is a DEV/QA context for controlled browser-origin testing across configured upstream domains.

## Language

**seamless-cors**:
A local DEV/QA network tool that sits between the browser and configured upstream domains so browser requests can be tested under adjusted cross-origin behavior without changing application request URLs.
_Avoid_: seamless-cors, generic proxy, CORS middleware

**Managed System Proxy**:
A traffic capture approach where the gateway configures the operating system or browser proxy settings on behalf of the user, so application requests keep their original URLs and no manual proxy setup is required.
_Avoid_: VPN, manual proxy, browser-only workaround

**Selective Managed Proxy**:
A Managed System Proxy behavior where only Domain List matches are routed through the gateway and all other traffic bypasses it.
_Avoid_: whole-system proxy, blanket proxying

**PAC Routing**:
A Selective Managed Proxy approach that uses a proxy auto-configuration file to route Domain List matches through the gateway while returning `DIRECT` for other traffic.
_Avoid_: global proxy, pass-through proxying

**Trust-Aware PAC Routing**:
A PAC Routing behavior where matched HTTPS traffic is routed through the gateway only when Trusted HTTPS Interception is enabled.
_Avoid_: routing unrepaired HTTPS, unnecessary HTTPS proxying

**Generated PAC**:
A runtime proxy auto-configuration artifact derived from Explicit Configuration and the Domain List, not edited directly by the user.
_Avoid_: user-authored PAC, manual PAC rules

**PAC Route Set**:
The Generated PAC data buckets derived inside the PAC Routing module from normalized Domain List Entries and the current Trusted HTTPS Interception state, keeping the PAC JavaScript mostly static.
_Avoid_: hand-built JavaScript rules, duplicated Domain List parsing, PAC-owned Domain List syntax

**PAC Endpoint**:
A local HTTP endpoint served by the gateway that returns the current Generated PAC.
_Avoid_: file PAC, static PAC file

**Default Upstream Negotiation**:
A gateway transport behavior where upstream HTTP protocol selection uses the Go runtime's normal negotiation instead of forcing a specific upstream protocol.
_Avoid_: forced HTTP/1.1 upstream, guaranteed HTTP/2 feature

**Explicit Gateway Error**:
A gateway-generated failure response that clearly identifies seamless-cors as the source and explains the upstream or lifecycle problem.
_Avoid_: disguised upstream error, silent failure

**CORS-Readable Gateway Error**:
An Explicit Gateway Error that includes the Reflective DEV/QA Policy headers when the original request reaching the Proxy Listener is Origin-gated.
_Avoid_: hidden gateway error, browser-masked failure

**JSON Gateway Error**:
An Explicit Gateway Error encoded as JSON for frontend API debugging.
_Avoid_: HTML error page, plain-text API error

**Gateway Error Status Mapping**:
The HTTP status convention where upstream failures use `502`, upstream timeouts use `504`, and gateway internal failures use `500`.
_Avoid_: all-500 errors, disguised upstream status

**No Request Timeout**:
A gateway behavior where requests are not failed by a gateway-imposed application timeout; only transport and idle cleanup timeouts apply.
_Avoid_: gateway request deadline, forced API timeout

**Client Abort Propagation**:
A gateway behavior where client-side request cancellation closes or cancels the corresponding upstream work.
_Avoid_: orphaned upstream request, ignored client disconnect

**Gateway Distribution**:
The installable form of seamless-cors for a specific operating system and CPU architecture.
_Avoid_: cross-platform binary

**Managed Platform**:
A supported operating system where the gateway can configure PAC Routing and user trust on behalf of the user.
_Avoid_: manual platform, manual proxy fallback, all-platform parity without adapters

**User-Trusted Development CA**:
A local certificate authority trusted only in the current user's trust store so the gateway can inspect HTTPS traffic for configured upstream domains during DEV/QA work.
_Avoid_: system-wide CA, production CA, shared CA

**OS Trust Only**:
A trust model where the gateway manages only the current user's operating-system trust store and does not inspect or manage browser-specific trust stores.
_Avoid_: browser trust management, profile-specific trust diagnostics

**Ephemeral User CA**:
A local development certificate authority generated for a gateway run, trusted only in the current user's operating-system trust store, and removed with its local files when the gateway stops.
_Avoid_: persistent CA, system-wide CA, retained CA key

**Trusted HTTPS Interception**:
A lifecycle behavior where HTTPS traffic that reaches the Proxy Listener is intercepted only when `ca-trusted` is enabled and the Ephemeral User CA is trusted for the current user.
_Avoid_: untrusted HTTPS interception, broken MITM, Domain List gated interception

**Opt-In CA Trust**:
A lifecycle default where generated configuration starts with `ca-trusted: false` so HTTPS interception requires an explicit user choice.
_Avoid_: default CA trust, implicit HTTPS interception

**Live Configuration**:
A gateway behavior where request handling uses the newest available request policy without requiring the user to manually restart or reload the program.
_Avoid_: manual reload, restart requirement, stale configuration

**All-Or-Nothing Config**:
A configuration behavior where a valid config file is applied as a whole and an invalid config file is rejected without partially changing gateway behavior.
_Avoid_: partial config application, half-applied config

**Lenient Configuration Shape**:
A clean-break configuration behavior where only current settings affect gateway behavior and unknown settings are ignored without aliases, migration, or old-version-specific handling.
_Avoid_: backward-compatible alias, migration, obsolete-setting special case

**Fatal Config Error**:
A configuration behavior where an invalid config file causes the gateway to report the validation problem, perform Runtime Cleanup, and stop.
_Avoid_: silent config fallback, stale config after invalid edit

**Lifecycle Operation**:
A gateway operation chosen explicitly through a command or start-time flag because it can affect OS proxy settings or certificate authority identity.
_Avoid_: live OS reconfiguration, config-triggered permission prompt

**Automatic Listeners**:
A lifecycle behavior where the gateway chooses available loopback ports for its proxy, PAC, and control endpoints at startup, then wires dependent runtime state in sequence.
_Avoid_: user-selected listener ports, fixed listener ports, manual listener addresses

**Loopback Default**:
A listener behavior where gateway endpoints bind to loopback.
_Avoid_: LAN-exposed proxy, user-selected bind address

**Control Endpoint**:
A separate loopback endpoint used by gateway commands such as stop and status, protected by runtime state rather than mixed into browser proxy traffic.
_Avoid_: proxy-path admin command, public control API

**Proxy Listener**:
A local proxy endpoint where traffic that reaches it is eligible for CORS repair, normally reached through PAC Routing for Domain List matches.
_Avoid_: manual proxy endpoint, browser setup address, generic listen, gatewayListen

**CORS Proxy**:
The gateway module behind the Proxy Listener that owns CORS repair, Local Preflight Answer, Response Repair, Explicit Gateway Error responses, and Trusted HTTPS Interception behavior for traffic that reaches it.
_Avoid_: Domain List admission module, PAC Routing module, generic proxy

**Diagnostic Proxy Listener Use**:
An allowed troubleshooting behavior where a local user may point a browser or tool directly at the Proxy Listener, while normal Start Guidance still presents PAC Routing as the managed setup path.
_Avoid_: supported manual setup path, listener-first start output, domain-list admission fallback

**Explicit Configuration**:
The complete user-editable gateway configuration, including live request policy and lifecycle settings but excluding runtime-selected listener addresses.
_Avoid_: hidden user settings, flag-only configuration, listener address configuration

**Path Expansion**:
A configuration behavior where path fields support home-directory and environment-variable expansion.
_Avoid_: arbitrary string expansion

**Home Config Directory**:
The default user configuration location at `.seamless-cors` under the user's home directory.
_Avoid_: platform-native app config directory

**Runtime State Directory**:
The durable location under the Home Config Directory for runtime coordination state and product-owned cleanup files.
_Avoid_: temp runtime state, volatile cleanup files

**Runtime State File**:
A durable runtime record that signals a possible active gateway instance, including the automatic Control Endpoint needed by `stop` and `status`.
_Avoid_: pid-only lock file, configured control address, in-memory instance registry

**Runtime State Verification**:
A lifecycle behavior where an existing Runtime State File is checked against its Control Endpoint before the gateway treats another instance as active.
_Avoid_: file-exists-is-running, port-only lock, stale state as active instance

**Runtime-Owned File Cleanup**:
A cleanup behavior where known seamless-cors runtime files under the Runtime State Directory may be removed as product-owned leftovers.
_Avoid_: deleting user files, broad home-directory cleanup, record-required file cleanup

**Footprint-Based Cleanup**:
A cleanup behavior where the gateway scans current machine state and removes resources carrying a stable seamless-cors ownership footprint.
_Avoid_: per-run PAC cleanup, previous-state restoration, guessed ownership

**Managed PAC Footprint**:
The stable loopback HTTP PAC URL shape whose path ends in `seamless-cors.pac`, proving a current managed PAC setting belongs to seamless-cors without depending on a run-specific port.
_Avoid_: run-specific PAC identity, port-based ownership, full-URL ownership, non-loopback PAC ownership

**CA Footprint**:
The strict seamless-cors-owned current-user CA trust identity used to identify Ephemeral User CA trust for cleanup.
_Avoid_: name-contains matching, system-wide CA cleanup, user-authored CA identity

**Cleanup Retry Guidance**:
A user-facing cleanup behavior where failed cleanup explains that gateway-owned state remains and tells the user to run `seamless-cors stop` again after resolving the OS or permission problem.
_Avoid_: silent cleanup failure, false cleanup success, manual OS instructions first

**Single User Instance**:
A runtime rule where only one gateway process may run for a user at a time, with the Runtime State File used as the first signal that an instance may already be active.
_Avoid_: multi-instance gateway, competing PAC state, port-based instance detection

**First-Start Bootstrap**:
A startup behavior where missing default config and Domain List files are created automatically before validation continues, allowing the same start command to continue with an Empty Domain List when no active entries exist yet.
_Avoid_: init command, manual file scaffolding

**Commented Default Config**:
A First-Start Bootstrap behavior where generated configuration includes short comments only for meaningful user-editable settings.
_Avoid_: opaque default config, verbose manual, runtime listener settings

**Start Guidance**:
A start-time user-facing output behavior where successful start output points to the editable Explicit Configuration, Domain List, and managed PAC state instead of runtime listener endpoints.
_Avoid_: listener-first start output, proxy setup instructions, PAC listener summary, control listener summary

**Pending Lifecycle Change**:
A restart-applied Explicit Configuration change detected while the gateway is running and reported to the user without being applied automatically.
_Avoid_: surprise permission prompt, implicit restart

**Restart-Applied Lifecycle**:
A lifecycle behavior where Pending Lifecycle Changes take effect only after the gateway is restarted.
_Avoid_: apply-lifecycle command, hot lifecycle swap

**Explicit Lifecycle Consent**:
A start-time user agreement collected before a Lifecycle Operation changes current-user OS-managed state, combining all needed PAC and CA consent for the current run into one prompt.
_Avoid_: implicit consent, persistent consent, surprise permission prompt, partial lifecycle consent

**Managed PAC Consent**:
An Explicit Lifecycle Consent required when a gateway start would replace existing managed PAC state, showing current managed PAC state and explaining that Runtime Cleanup removes seamless-cors PAC settings without restoring previous PAC state.
_Avoid_: silent proxy replacement, proxy chaining, broad proxy takeover

**Independent PAC Lifecycle**:
A lifecycle boundary where Managed PAC Consent and PAC Routing setup follow gateway start independently of whether the Domain List currently has active entries.
_Avoid_: domain-gated PAC setup, delayed proxy ownership, route-count-based lifecycle

**CA Trust Consent**:
An Explicit Lifecycle Consent required on each gateway start with `ca-trusted: true` before adding Ephemeral User CA trust for HTTPS interception.
_Avoid_: implicit CA trust, persistent CA consent, trust without prompt

**Independent CA Lifecycle**:
A lifecycle boundary where CA Trust Consent and Ephemeral User CA setup follow `ca-trusted` independently of whether the Domain List currently has active entries.
_Avoid_: domain-gated CA trust, implicit CA delay, route-dependent trust setup

**All-Service PAC Management**:
A Managed System Proxy behavior where supported platform adapters apply PAC Routing to every network service they manage, so routing remains consistent when the active network changes during a gateway run.
_Avoid_: active-service-only PAC, partial network setup

**Minimal Command Surface**:
The user-facing command model where normal operation is limited to starting, stopping, and checking the gateway while runtime behavior follows Live Configuration.
_Avoid_: command-heavy configuration, flag-driven operation

**Three Commands**:
The v1 command surface: `start`, `stop`, and `status`.
_Avoid_: init command, trust command, config editing command

**Foreground Start**:
A v1 runtime behavior where `start` runs attached in the foreground rather than launching an official background daemon.
_Avoid_: daemon mode, background start

**Runtime Cleanup**:
An idempotent lifecycle behavior where `start`, `stop`, and gateway shutdown remove seamless-cors-owned PAC settings, CA trust, and runtime files proven by footprint or runtime-file ownership, treating missing resources as already clean. `start` performs Runtime Cleanup only when no active gateway is verified.
_Avoid_: status cleanup, broad cleanup, file-only deletion, crash recovery, restore-based cleanup

**No PAC Restoration**:
A cleanup boundary where Runtime Cleanup removes seamless-cors-owned managed PAC settings without reconstructing previous machine PAC state.
_Avoid_: previous-state rollback, proxy restoration, corporate PAC reconstruction

**Human Status**:
A status output intended for interactive DEV/QA use rather than machine-readable automation.
_Avoid_: JSON status, scripting API

**Read-Only Status**:
A status behavior that reports gateway and cleanup-needed state, including stale Runtime State File detection, without changing proxy settings, CA trust, or runtime files.
_Avoid_: status-triggered cleanup, mutating status command

**Diagnostic Runtime Endpoint**:
An automatically selected listener address shown by status for troubleshooting, not for user proxy setup or configuration.
_Avoid_: setup address, configured listener, manual proxy instruction

**Domain List**:
The user-managed newline-delimited set of upstream hostnames or origins used by PAC Routing to decide which browser requests normally reach the Proxy Listener during DEV/QA work.
_Avoid_: proxy admission list, interception rules, proxy rules

**Domain List Comment**:
A full-line or inline note in the Domain List that is ignored during matching.
_Avoid_: comment-as-entry

**Empty Domain List**:
A valid Domain List state with no active entries, including a file that contains only comments or blank lines; the gateway keeps managed PAC Routing installed and matches no upstreams until valid Domain List Entries are added.
_Avoid_: startup failure for no active entries, proxy-all fallback

**Invalid Domain Startup**:
A startup validation behavior where a Domain List with invalid lines fails before the gateway starts, even when it has no valid entries.
_Avoid_: silent invalid entry, invalid-as-empty, partial startup warning

**Fatal Domain List Error**:
A live configuration behavior where an invalid Domain List edit reports the line-level validation problem, performs Runtime Cleanup, and stops the gateway.
_Avoid_: stale valid routing, partial valid routing, silent invalid entry

**Domain List Entry**:
A single line in the Domain List that may name a full origin or use hostname shorthand for easy DEV/QA configuration.
_Avoid_: rule, matcher expression

**Domain List Routing Policy**:
A runtime interpretation owned by the PAC Routing module that decides whether normalized Domain List Entries send a browser request to the Proxy Listener.
_Avoid_: proxy admission policy, raw string matching, duplicated PAC matchers

**Hostname Shorthand**:
A Domain List Entry that names a host without scheme so it matches that host across schemes and ports.
_Avoid_: default-port-only shorthand, scheme-specific shorthand

**Line-Level Domain Validation**:
A Domain List behavior where each line is validated independently so invalid entries can be reported precisely; any invalid line makes the Domain List invalid for startup and live updates.
_Avoid_: silent invalid entry, partial valid routing, invalid-as-empty

**Exact Domain Match**:
A Domain List matching rule where hostname shorthand matches only the named host unless the entry uses an explicit wildcard.
_Avoid_: implicit subdomain match, broad domain match

**Single-Label Wildcard**:
A Domain List matching rule where `*.example.com` matches exactly one subdomain label and does not match the parent domain or deeper subdomains.
_Avoid_: recursive wildcard, parent-domain wildcard

**Local Target**:
A localhost, loopback, private IP, or plain HTTP upstream entry that may be included in the Domain List for DEV/QA work.
_Avoid_: public-domain-only target, DNS-only target

**IPv6 Full Origin**:
A Domain List Entry for an IPv6 target that must include scheme and bracketed IPv6 host syntax.
_Avoid_: IPv6 hostname shorthand

**Reflective DEV/QA Policy**:
The default cross-origin behavior for traffic that reaches the Proxy Listener, where the gateway reflects the browser request's origin and requested CORS capabilities so credentialed development and testing flows are browser-valid.
_Avoid_: wildcard CORS, production CORS policy, allow-all policy

**Credentialed Reflection**:
A Reflective DEV/QA Policy behavior where Origin-gated responses reaching the Proxy Listener always include `Access-Control-Allow-Credentials: true` with a reflected request origin.
_Avoid_: wildcard credentials, credentialless default

**Null Origin Reflection**:
A Credentialed Reflection behavior where `Origin: null` is reflected for DEV/QA requests reaching the Proxy Listener.
_Avoid_: null origin rejection

**Origin Vary Preservation**:
A Response Repair behavior where existing `Vary` values are preserved and `Origin` is added when absent.
_Avoid_: Vary clobbering, duplicate Origin Vary

**Requested Header Reflection**:
A Reflective DEV/QA Policy behavior where preflight responses echo the browser's requested header list instead of using a wildcard.
_Avoid_: wildcard allow-headers

**Requested Method Reflection**:
A Local Preflight Answer behavior where preflight responses echo the browser's requested method instead of returning a broad method list.
_Avoid_: broad allow-methods

**Global CORS Policy**:
A gateway behavior where every request reaching the Proxy Listener uses the same Reflective DEV/QA Policy instead of per-domain policy settings.
_Avoid_: per-domain CORS policy, domain-specific overrides

**Origin-Gated Rewriting**:
A gateway behavior where cross-origin response changes are applied only when the browser request includes an `Origin` header.
_Avoid_: blanket rewriting, unconditional CORS headers

**Local Preflight Answer**:
A gateway behavior where browser CORS preflight requests that reach the Proxy Listener are answered by the gateway instead of being forwarded upstream.
_Avoid_: upstream preflight, preflight repair

**Private Network Access Reflection**:
A Local Preflight Answer behavior where requests reaching the Proxy Listener that ask for private network access receive `Access-Control-Allow-Private-Network: true`.
_Avoid_: PNA omission for local targets

**Fixed Preflight Cache**:
A Local Preflight Answer behavior that uses a fixed `Access-Control-Max-Age` of 600 seconds.
_Avoid_: configurable preflight cache, indefinite preflight cache

**Response Repair**:
A gateway behavior where real upstream responses for requests that reach the Proxy Listener are adjusted on the way back to satisfy the Reflective DEV/QA Policy.
_Avoid_: request rewrite, upstream configuration

**All-Status Repair**:
A Response Repair behavior where Origin-gated upstream responses reaching the Proxy Listener receive CORS repair regardless of upstream status code.
_Avoid_: success-only repair, hidden API error

**No Request Header Rewriting**:
The product boundary that leaves browser request headers unchanged except for ordinary proxy transport mechanics.
_Avoid_: Origin rewriting, Referer rewriting, auth header mutation

**CORS Header Replacement**:
A Response Repair behavior where existing upstream CORS headers are removed before the gateway writes the Reflective DEV/QA Policy headers.
_Avoid_: CORS header merge, duplicate CORS headers

**Concrete Exposed Headers**:
A Response Repair behavior where `Access-Control-Expose-Headers` lists actual upstream response header names, excluding CORS headers, for maximum browser compatibility.
_Avoid_: wildcard expose-headers

**HTTP CORS Scope**:
The product boundary that focuses gateway repair on browser HTTP/HTTPS CORS requests and leaves WebSocket protocol behavior out of scope.
_Avoid_: WebSocket repair, protocol frame rewriting

**WebSocket Skip**:
A gateway behavior where WebSocket upgrade requests are forwarded without CORS repair or protocol-specific changes.
_Avoid_: WebSocket origin bypass, upgrade repair

**Cookie Out of Scope**:
The product boundary that leaves cookie attributes, cookie values, sessions, and authentication behavior unchanged.
_Avoid_: cookie repair, auth bypass, session rewriting

## Example Dialogue

Developer: "Can I point my browser traffic through seamless-cors for the staging API?"

QA engineer: "Yes, but only for configured upstream domains so unrelated browsing stays outside the gateway."

Developer: "Do I need to configure my browser proxy manually?"

QA engineer: "No, the Managed System Proxy handles that while the gateway is running."

Developer: "Will unrelated browser traffic go through the gateway?"

QA engineer: "No, Selective Managed Proxy uses PAC Routing so only Domain List matches go through the gateway."

Developer: "Will HTTPS domains still route through the gateway when `ca-trusted` is false?"

QA engineer: "No, Trust-Aware PAC Routing sends those HTTPS requests direct because they cannot be repaired."

Developer: "Do I need to maintain the PAC file?"

QA engineer: "No, Generated PAC is derived from Explicit Configuration and the Domain List."

Developer: "How do Domain List changes reach the operating system proxy?"

QA engineer: "The PAC Endpoint serves the current Generated PAC, so routing updates without reinstalling proxy settings."

Developer: "Can I avoid changing my system proxy settings?"

QA engineer: "No, the gateway uses Managed System Proxy so application requests keep their original URLs."

Developer: "What if I decline the Managed PAC Consent prompt?"

QA engineer: "Start stops without changing machine proxy settings because there is no manual proxy fallback."

Developer: "Does the gateway force HTTP/1.1 upstream?"

QA engineer: "No, Default Upstream Negotiation lets Go select the upstream protocol normally."

Developer: "What if upstream TLS verification fails?"

QA engineer: "Explicit Gateway Error makes clear the failure came from the gateway while preserving the upstream TLS problem."

Developer: "Will frontend code be able to read gateway errors?"

QA engineer: "CORS-Readable Gateway Error makes Origin-gated gateway failures visible to the browser client when they reach the Proxy Listener."

Developer: "What format do gateway errors use?"

QA engineer: "JSON Gateway Error keeps failures easy to inspect from frontend API clients."

Developer: "How are gateway failures status-coded?"

QA engineer: "Gateway Error Status Mapping uses `502` for upstream failures, `504` for upstream timeouts, and `500` for gateway failures."

Developer: "Will the gateway impose its own API request timeout?"

QA engineer: "No, No Request Timeout avoids adding an application deadline, while Client Abort Propagation cleans up when the browser cancels."

Developer: "Will the gateway configure Firefox or browser profile certificate stores?"

QA engineer: "No, OS Trust Only keeps certificate trust limited to the current user's operating-system trust store."

Developer: "What happens when `ca-trusted` is false?"

QA engineer: "Trusted HTTPS Interception is disabled, so HTTPS traffic that reaches the Proxy Listener is tunneled without CORS repair."

Developer: "Will the first run automatically trust a CA?"

QA engineer: "No, Opt-In CA Trust makes HTTPS interception an explicit configuration choice."

Developer: "Will the gateway keep reusing the same development CA?"

QA engineer: "No, Ephemeral User CA generates trust material for a gateway run, and Runtime Cleanup removes it."

Developer: "What removes trusted CA material?"

QA engineer: "Runtime Cleanup removes seamless-cors-owned CA trust and runtime CA files."

Developer: "What happens if the gateway crashes before removing its CA?"

QA engineer: "Runtime Cleanup removes leftover Ephemeral User CA trust and files on the next start or stop."

Developer: "Will every operating system have the same managed setup in v1?"

QA engineer: "Every supported platform needs a managed PAC adapter; platforms without one are not supported yet."

Developer: "After I update the Domain List, do I need to restart the gateway?"

QA engineer: "No, Live Configuration applies the newest values to incoming requests."

Developer: "What happens if I save an invalid config file while the gateway is running?"

QA engineer: "Fatal Config Error reports the validation problem, performs Runtime Cleanup, and stops the gateway."

Developer: "What if my config still has removed listener or managed-proxy settings?"

QA engineer: "Lenient Configuration Shape treats them like any other unknown settings, so they do not affect gateway behavior."

Developer: "Do I need a command for every setting?"

QA engineer: "No, the Minimal Command Surface keeps commands rare and lets configuration drive behavior while the gateway is running."

Developer: "Which commands exist in v1?"

QA engineer: "Three Commands: `start`, `stop`, and `status`."

Developer: "Does `start` launch a background service?"

QA engineer: "No, Foreground Start keeps the gateway attached and lets Ctrl-C run Runtime Cleanup in v1."

Developer: "Does Ctrl-C clean up the proxy and CA?"

QA engineer: "Yes, Runtime Cleanup runs on Ctrl-C and the stop command."

Developer: "What if stop finds only cleanup-needed state?"

QA engineer: "Runtime Cleanup finishes removing gateway-owned state before stop exits."

Developer: "Is status intended for scripts?"

QA engineer: "No, Human Status is optimized for interactive understanding."

Developer: "Can status change proxy or CA state?"

QA engineer: "No, Read-Only Status reports state without performing cleanup."

Developer: "Why does status show listener addresses if I cannot configure them?"

QA engineer: "Diagnostic Runtime Endpoint values are shown for troubleshooting, not setup."

Developer: "What if status finds a stale Runtime State File?"

QA engineer: "Read-Only Status reports that the gateway is not running and leaves cleanup to start or stop."

Developer: "Can editing configuration unexpectedly trigger an OS permission prompt?"

QA engineer: "No, lifecycle settings live in Explicit Configuration but become a Pending Lifecycle Change while the gateway is running."

Developer: "What happens if a default listener port is already in use?"

QA engineer: "Automatic Listeners choose available loopback ports at startup."

Developer: "Can other machines use my gateway by default?"

QA engineer: "No, Loopback Default keeps listener endpoints local."

Developer: "Do stop and status go through the proxy listener?"

QA engineer: "No, Control Endpoint keeps command traffic separate from browser proxy traffic."

Developer: "Do I need to know which listener browsers use?"

QA engineer: "No, PAC Routing connects browsers to the Proxy Listener through managed proxy settings."

Developer: "How do pending lifecycle changes take effect?"

QA engineer: "Restart-Applied Lifecycle means they apply on the next gateway start."

Developer: "What happens if the gateway crashes after changing my proxy settings?"

QA engineer: "Runtime Cleanup removes leftover seamless-cors-owned PAC settings before a new start or stop finishes."

Developer: "What if I already use a corporate proxy?"

QA engineer: "Managed PAC Consent shows the current managed PAC state and asks before replacing existing PAC settings for this run."

Developer: "What do I need to configure before starting?"

QA engineer: "Explicit Configuration contains the meaningful user settings, while the Domain List stays as the easy one-domain-per-line file."

Developer: "Where do config files live by default?"

QA engineer: "Home Config Directory keeps them under `.seamless-cors` in the user's home directory."

Developer: "Where do runtime cleanup files live?"

QA engineer: "Runtime State Directory keeps runtime coordination state and product-owned cleanup files under the Home Config Directory."

Developer: "How do stop and status find the running gateway?"

QA engineer: "They read the Runtime State File to find the active Control Endpoint."

Developer: "Does a Runtime State File always mean the gateway is still running?"

QA engineer: "No, Runtime State Verification checks the Control Endpoint before treating it as an active instance."

Developer: "Will cleanup modify state just because it looks suspicious?"

QA engineer: "No, Footprint-Based Cleanup acts only on state proven by a seamless-cors ownership footprint."

Developer: "Can I run two gateways at once?"

QA engineer: "No, Single User Instance uses the Runtime State File to guard active gateway state."

Developer: "Can config paths use `~` or environment variables?"

QA engineer: "Yes, Path Expansion applies to path fields only."

Developer: "Can the gateway start with no domains?"

QA engineer: "Yes, Empty Domain List is valid, so the gateway runs while PAC Routing matches no upstreams until valid Domain List Entries are added."

Developer: "Do I need to run an init command first?"

QA engineer: "No, First-Start Bootstrap creates the default files when needed, and Empty Domain List lets that same start command keep running with no matched upstreams yet."

Developer: "Will generated config explain the settings?"

QA engineer: "Yes, Commented Default Config keeps the generated file short but understandable."

Developer: "Can I write just `api.dev.example.com`?"

QA engineer: "Yes, that Domain List Entry uses hostname shorthand; use a full origin only when scheme or port matters."

Developer: "Can I annotate domains in the Domain List?"

QA engineer: "Yes, Domain List Comment supports full-line and inline comments."

Developer: "Does hostname shorthand include custom ports?"

QA engineer: "Yes, Hostname Shorthand matches the named host across schemes and ports."

Developer: "What if one Domain List line is wrong?"

QA engineer: "Line-Level Domain Validation reports the invalid line, and Invalid Domain Startup or a live validation stop prevents partial routing from a mixed-validity Domain List."

Developer: "What if I save an invalid Domain List while the gateway is running?"

QA engineer: "Fatal Domain List Error reports the line-level validation problem, performs Runtime Cleanup, and stops the gateway."

Developer: "Does `api.dev.example.com` include its subdomains?"

QA engineer: "No, Exact Domain Match requires an explicit wildcard when subdomains should be included."

Developer: "Does `*.example.com` match `deep.api.example.com`?"

QA engineer: "No, Single-Label Wildcard matches only one subdomain label."

Developer: "Can I include my local API or a LAN staging service?"

QA engineer: "Yes, Local Targets are allowed."

Developer: "Can I write IPv6 shorthand in the Domain List?"

QA engineer: "No, IPv6 Full Origin keeps IPv6 entries unambiguous."

Developer: "Can credentialed browser requests work without configuring allowed origins?"

QA engineer: "Yes, the Reflective DEV/QA Policy reflects the request origin instead of using a wildcard."

Developer: "Are credentials allowed for CORS-repaired responses?"

QA engineer: "Yes, Credentialed Reflection always allows credentials with the reflected origin."

Developer: "What if the browser sends `Origin: null`?"

QA engineer: "Null Origin Reflection treats it as a valid DEV/QA origin value."

Developer: "What happens to existing `Vary` headers?"

QA engineer: "Origin Vary Preservation keeps existing values and adds `Origin` once."

Developer: "How are preflight request headers allowed?"

QA engineer: "Requested Header Reflection echoes the browser's requested header list."

Developer: "How are preflight request methods allowed?"

QA engineer: "Requested Method Reflection echoes the browser's requested method."

Developer: "Can each domain have a different CORS policy?"

QA engineer: "No, Global CORS Policy applies the same DEV/QA behavior to every request reaching the Proxy Listener."

Developer: "Will same-origin or non-browser traffic get CORS headers added?"

QA engineer: "No, Origin-Gated Rewriting leaves traffic without an `Origin` header unchanged."

Developer: "Does the upstream server need to handle preflight correctly?"

QA engineer: "No, Local Preflight Answer handles browser preflight and Response Repair handles the real upstream response."

Developer: "Are Private Network Access preflights handled?"

QA engineer: "Yes, Private Network Access Reflection allows PNA preflights that reach the Proxy Listener."

Developer: "Will `401` or `500` upstream responses still be readable by frontend code?"

QA engineer: "Yes, All-Status Repair applies CORS repair to upstream errors too."

Developer: "How long can browsers cache local preflight answers?"

QA engineer: "Fixed Preflight Cache uses 600 seconds."

Developer: "Will the gateway rewrite `Origin` if the upstream rejects it?"

QA engineer: "No, No Request Header Rewriting keeps upstream application checks out of scope."

Developer: "What if the API already returns partial CORS headers?"

QA engineer: "CORS Header Replacement removes them first so the browser sees one consistent DEV/QA policy."

Developer: "How are response headers exposed to frontend code?"

QA engineer: "Concrete Exposed Headers lists the upstream response headers instead of using a wildcard."

Developer: "Will the gateway fix WebSocket origin behavior?"

QA engineer: "No, HTTP CORS Scope keeps WebSocket protocol behavior out of v1."

Developer: "What if the WebSocket domain is in the Domain List?"

QA engineer: "WebSocket Skip still forwards it without gateway repair."

Developer: "Will the gateway rewrite cookies so login works?"

QA engineer: "No, Cookie Out of Scope leaves cookie and authentication behavior unchanged."
