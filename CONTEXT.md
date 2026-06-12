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

**Gateway Distribution**:
The installable form of seamless-cors for a specific operating system and CPU architecture.
_Avoid_: cross-platform binary

**Managed Platform**:
A supported operating system where the gateway can configure PAC Routing and user trust on behalf of the user.
_Avoid_: manual platform, manual proxy fallback, all-platform parity without adapters

**Platform Capability Check**:
A preflight lifecycle behavior where the gateway verifies that the current platform can manage required PAC and user trust operations before making lifecycle changes.
_Avoid_: partial platform setup, trust install on unsupported platform, continue-anyway platform prompt

**Capability Check Command**:
A read-only user-facing command that reports whether the current system can run the managed gateway lifecycle before the user attempts installation or start.
_Avoid_: hidden compatibility discovery, lifecycle-only platform errors, mutating compatibility check

**Capability-Only Check**:
A command boundary where `check` reports platform capability without reporting or repairing current Installed User CA state.
_Avoid_: check-as-status, check-triggered CA inspection side effects

**Footprint-Free Check**:
A command boundary where `check` reports platform capability without creating configuration, runtime, CA, or home config files and directories.
_Avoid_: check-time bootstrap, check-time cleanup, check-time CA storage

**Detailed Capability Report**:
A capability reporting behavior that shows both an overall support result and individual platform capability results for managed PAC, CA trust, and cleanup.
_Avoid_: opaque unsupported result, single-error compatibility check

**Best-Effort Stop**:
A stop behavior where cleanup still attempts to remove owned runtime and PAC state even when capability checks are limited or reporting platform problems.
_Avoid_: capability-blocked cleanup, leaving owned runtime state behind

**User-Trusted Development CA**:
A local certificate authority trusted only in the current user's trust store so the gateway can inspect HTTPS traffic for configured upstream domains during DEV/QA work.
_Avoid_: system-wide CA, production CA, shared CA

**OS Trust Only**:
A trust model where the gateway manages only the current user's operating-system trust store and does not inspect or manage browser-specific trust stores.
_Avoid_: browser trust management, profile-specific trust diagnostics

**Installed User CA**:
A long-lived local development certificate authority trusted only in the current user's operating-system trust store and reused across trusted gateway starts until it is removed or replaced.
_Avoid_: per-start CA, system-wide CA, shared CA

**Installed User CA Renewal**:
A lifecycle behavior where an unavailable, invalid, expired, or near-expiry Installed User CA is replaced before trusted HTTPS interception starts.
_Avoid_: expired trust reuse, surprise HTTPS breakage, silent CA replacement

**CA Replacement Rule**:
A CA lifecycle rule where only fully usable Installed User CA state is reused, local permission issues are repaired in place, and every other unusable state is treated by removing owned CA state before installing fresh CA material.
_Avoid_: partial CA reuse, trusting stale local material, special-case invalid states

**CA Ensure**:
A lifecycle behavior where `start` with `ca-trusted: true` and CA Lifecycle Commands verify that usable Installed User CA trust exists, requesting platform approval only when trust must be added or replaced.
_Avoid_: repeated trust prompt, start without usable trust, install-only trust repair

**Single Installed CA State**:
A CA lifecycle invariant where multiple seamless-cors-owned CA trust footprints are treated as repair-needed state and replaced by one usable Installed User CA.
_Avoid_: multiple active development CAs, ambiguous signing identity

**CA Material Integrity**:
A CA lifecycle invariant where current-user CA trust and the local signing key must match; missing or mismatched CA material is treated as repair-needed state.
_Avoid_: trusted cert without signing key, orphaned signing key, mismatched CA pair

**OS-Backed CA Installation**:
A CA lifecycle invariant where Installed User CA state requires current-user operating-system trust; local CA material alone is not installed trust.
_Avoid_: file-only installation, assuming trust from local material

**CA Permission Repair**:
A CA lifecycle behavior where otherwise-valid Installed User CA material with loose local file permissions is tightened in place without replacing trusted CA identity.
_Avoid_: permission-triggered CA rotation, loose CA key permissions

**Leaf Certificate Reuse**:
A runtime behavior where generated per-host HTTPS certificates may be reused within a gateway process until their generation age exceeds the cache reuse limit, but are not persisted as trusted identity across gateway runs.
_Avoid_: persistent leaf certificate inventory, per-request certificate churn, expiry-only cache policy

**Per-Host Leaf Certificate**:
A generated HTTPS server certificate for the specific upstream hostname being intercepted, signed by the Installed User CA on demand.
_Avoid_: Domain List-wide leaf certificate, wildcard-first certificate strategy, persisted leaf identity

**Trusted HTTPS Interception**:
A lifecycle behavior where HTTPS traffic that reaches the Proxy Listener is intercepted only when `ca-trusted` is enabled and the Installed User CA is trusted for the current user.
_Avoid_: untrusted HTTPS interception, broken MITM, Domain List gated interception

**Default Trusted HTTPS**:
A lifecycle default where generated configuration starts with `ca-trusted: true`, while Installed User CA trust is still added or replaced only through CA Ensure and required platform approval.
_Avoid_: silent trust installation, disabled-by-default HTTPS interception, config-only trust

**Live Configuration**:
A gateway behavior where hot-applicable user request settings use the newest valid Explicit Configuration and Domain List without requiring the user to manually restart or reload the program.
_Avoid_: manual reload, restart requirement, stale configuration

**All-Or-Nothing Config**:
A configuration behavior where a valid config file is applied as a whole and an invalid config file is rejected without partially changing gateway behavior.
_Avoid_: partial config application, half-applied config

**Lenient Configuration Shape**:
A clean-break configuration behavior where only current settings affect gateway behavior and unknown settings are ignored without aliases, migration, or old-version-specific handling.
_Avoid_: backward-compatible alias, migration, obsolete-setting special case

**Fatal Config Error**:
A configuration behavior where an invalid, missing, or unreadable config file causes the gateway to report the validation problem, perform Runtime Cleanup, and stop.
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
The gateway module behind the Proxy Listener that owns CORS repair, Local Preflight Answer, Response Repair, and Trusted HTTPS Interception behavior for traffic that reaches it.
_Avoid_: Domain List admission module, PAC Routing module, generic proxy

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

**Runtime Coordination**:
A lifecycle behavior that interprets Runtime State File presence, Runtime State Verification results, and Single User Instance decisions for commands without performing Runtime Cleanup itself.
_Avoid_: cleanup module, process supervisor, daemon manager, file-exists-is-running

**Installed CA Storage**:
The durable location under the Home Config Directory for seamless-cors-owned Installed User CA material, kept outside Runtime Cleanup.
_Avoid_: runtime CA storage, temp CA files, stop-owned CA files

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
The strict seamless-cors-owned current-user CA trust identity used to identify Installed User CA trust for lifecycle management.
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
A start-time user-facing output behavior shown only after required lifecycle consent and platform approval have succeeded, pointing to the editable Explicit Configuration, Domain List, and managed PAC state instead of runtime listener endpoints.
_Avoid_: pre-approval running message, listener-first start output, proxy setup instructions, PAC listener summary, control listener summary

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
A platform approval moment required before adding or replacing Installed User CA trust for HTTPS interception, with gateway context shown only when the platform requires approval.
_Avoid_: implicit CA trust, repeated consent for unchanged trust, app-only trust prompt

**Independent CA Lifecycle**:
A lifecycle boundary where CA Trust Consent and Installed User CA availability follow `ca-trusted` independently of whether the Domain List currently has active entries.
_Avoid_: domain-gated CA trust, implicit CA delay, route-dependent trust setup

**Lifecycle Activation Order**:
A startup lifecycle boundary where required CA trust approval and CA Ensure finish before managed PAC state and runtime visibility are established, and Start Guidance is shown only after activation has succeeded.
_Avoid_: pre-validation trust changes, PAC-before-trust startup, degraded trusted mode, pre-approval runtime state, status-visible pending approval, half-started gateway

**All-Service PAC Management**:
A Managed System Proxy behavior where supported platform adapters apply PAC Routing to every network service they manage, so routing remains consistent when the active network changes during a gateway run.
_Avoid_: active-service-only PAC, partial network setup

**Minimal Command Surface**:
The user-facing command model where normal operation is limited to starting, stopping, and checking the gateway while runtime behavior follows Live Configuration.
_Avoid_: command-heavy configuration, flag-driven operation

**CA Lifecycle Commands**:
Top-level user-facing commands that explicitly install, repair, or remove the Installed User CA outside the normal start/stop gateway loop, with removal requiring that no gateway instance is running.
_Avoid_: nested CA command tree, hidden CA removal, per-start CA trust, config editing command, active-runtime CA removal, extra command confirmation

**Config-Independent CA Install**:
A CA lifecycle command boundary where installing or repairing the Installed User CA does not require, create, or modify Explicit Configuration, though existing configuration may be read for guidance.
_Avoid_: install-time config bootstrap, install changing `ca-trusted`, config-required CA repair

**Idempotent CA Install**:
A CA lifecycle command behavior where installing reports existing usable CA trust as success without requesting platform approval or changing CA material.
_Avoid_: reinstalling usable CA, noisy no-op install, repeated trust approval

**Config-Independent CA Uninstall**:
A CA lifecycle command boundary where removing the Installed User CA does not modify Explicit Configuration; future trusted starts may reinstall trust when configuration still requests it.
_Avoid_: uninstall changing `ca-trusted`, disabling desired HTTPS interception, config-coupled removal

**Complete CA Uninstall**:
A CA lifecycle invariant where uninstall reports success only after seamless-cors-owned current-user CA trust and local CA material are both absent.
_Avoid_: false uninstall success, trusted CA without local material, leftover private key

**Foreground Start**:
A v1 runtime behavior where `start` runs attached in the foreground rather than launching an official background daemon.
_Avoid_: daemon mode, background start

**Runtime Cleanup**:
An idempotent lifecycle behavior where `start`, `stop`, and gateway shutdown remove seamless-cors-owned PAC settings and runtime process state proven by footprint or runtime-file ownership, treating missing resources as already clean. `start` performs Runtime Cleanup only when no active gateway is verified.
_Avoid_: status cleanup, broad cleanup, CA removal, restore-based cleanup

**No PAC Restoration**:
A cleanup boundary where Runtime Cleanup removes seamless-cors-owned managed PAC settings without reconstructing previous machine PAC state.
_Avoid_: previous-state rollback, proxy restoration, corporate PAC reconstruction

**Human Status**:
A status output intended for interactive DEV/QA use rather than machine-readable automation.
_Avoid_: JSON status, scripting API

**Read-Only Status**:
A status behavior that reports gateway, cleanup-needed, and Installed User CA state, including stale Runtime State File detection, without changing proxy settings, CA trust, local CA material, or runtime files.
_Avoid_: status-triggered cleanup, mutating status command

**CA Health Status**:
A read-only status vocabulary for Installed User CA state using stable values such as usable, missing, expired, expiring-soon, invalid, multiple, mismatched-material, unsupported, and unknown.
_Avoid_: prose-only CA state, status-triggered CA repair

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
A live configuration behavior where an invalid, missing, or unreadable Domain List edit reports the validation problem, performs Runtime Cleanup, and stops the gateway.
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

Developer: "Will the gateway configure Firefox or browser profile certificate stores?"

QA engineer: "No, OS Trust Only keeps certificate trust limited to the current user's operating-system trust store."

Developer: "What happens when `ca-trusted` is false?"

QA engineer: "Trust-Aware PAC Routing does not send matched HTTPS traffic to the Proxy Listener without Trusted HTTPS Interception."

Developer: "Will the first run automatically trust a CA?"

QA engineer: "Default Trusted HTTPS makes HTTPS interception the generated configuration default, but CA Ensure still requires platform approval before adding user trust."

Developer: "Will the gateway keep reusing the same development CA?"

QA engineer: "Yes. Installed User CA reuses trusted CA material across trusted gateway starts until it is removed or replaced."

Developer: "What removes trusted CA material?"

QA engineer: "CA lifecycle commands remove seamless-cors-owned CA trust and local CA material."

Developer: "What happens if the gateway crashes before removing its CA?"

QA engineer: "Installed User CA remains available for the next trusted gateway start unless the user removes it."

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

QA engineer: "`start`, `stop`, and `status` manage the gateway runtime; CA Lifecycle Commands manage Installed User CA trust."

Developer: "Does `start` launch a background service?"

QA engineer: "No, Foreground Start keeps the gateway attached and lets Ctrl-C run Runtime Cleanup in v1."

Developer: "Does Ctrl-C clean up the proxy and CA?"

QA engineer: "Ctrl-C runs Runtime Cleanup for gateway runtime state, but Installed User CA trust remains until a CA Lifecycle Command removes it."

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
