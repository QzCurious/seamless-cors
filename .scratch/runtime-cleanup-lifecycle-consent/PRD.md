Status: ready-for-agent

# Runtime Cleanup and Explicit Lifecycle Consent PRD

## Problem Statement

Users need seamless-cors to leave their machine in a clean, understandable state across normal shutdown, `stop`, Ctrl-C, failed start, and previous gateway crashes. The current implementation relies on in-memory proxy restoration and CA cleanup records, which does not match the clean-break domain model now captured in the glossary and ADRs.

The user wants cleanup to be idempotent and ownership-based. seamless-cors should remove only seamless-cors-owned PAC settings, CA trust, and runtime files, and it should never reconstruct previous machine PAC state. When a lifecycle operation would change current-user OS-managed state, the user should see one clear Explicit Lifecycle Consent prompt before any such change happens.

## Solution

Implement Runtime Cleanup as the single cleanup behavior for `start`, `stop`, gateway shutdown, failed lifecycle mutation, and Ctrl-C. Runtime Cleanup removes seamless-cors-owned resources proven by Managed PAC Footprint, CA Footprint, or runtime-file ownership, treating missing resources as already clean.

`start` first verifies whether an active gateway exists. If an active gateway is verified, `start` exits without running Runtime Cleanup. If no active gateway is verified, `start` runs Runtime Cleanup before reading or validating the new Explicit Configuration. After config and Domain List validation, `start` determines whether Explicit Lifecycle Consent is needed, presents one combined prompt for Managed PAC Consent and CA Trust Consent, and exits without machine changes if declined.

PAC cleanup scans current managed PAC state across all managed network services and removes PAC settings whose loopback HTTP PAC URL path ends in `seamless-cors.pac`. CA cleanup removes current-user CA trust matching the strict CA Footprint and deletes known seamless-cors runtime CA files under the Runtime State Directory. Runtime Cleanup also removes the Runtime State File when no active gateway is verified or after the active gateway has stopped.

## User Stories

1. As a user, I want `start` to avoid cleanup when another gateway is verified as active, so that a second process cannot disrupt the running gateway.
2. As a user, I want `start` to run Runtime Cleanup when no active gateway is verified, so that leftover seamless-cors-owned resources are removed before a new process starts.
3. As a user, I want `start` to run Runtime Cleanup before config validation, so that invalid current configuration does not block cleanup from a previous run.
4. As a user, I want every new gateway process to read fresh Explicit Configuration after cleanup, so that lifecycle behavior is based on the current config.
5. As a user, I want Runtime Cleanup to be idempotent, so that repeated `start`, `stop`, or shutdown cleanup attempts are safe.
6. As a user, I want Runtime Cleanup to treat missing resources as already clean, so that cleanup can be retried without false failures.
7. As a user, I want Runtime Cleanup to remove only seamless-cors-owned resources, so that unrelated machine settings are not modified.
8. As a user, I want Runtime Cleanup to use Footprint-Based Cleanup for managed PAC state, so that PAC cleanup does not depend on previous machine state.
9. As a user, I want managed PAC ownership to be proven by a stable `seamless-cors.pac` footprint, so that cleanup can identify gateway-owned PAC settings across runs.
10. As a user, I want the Managed PAC Footprint to be loopback HTTP scoped, so that cleanup does not remove a corporate or remote PAC that happens to use the same filename.
11. As a user, I want Runtime Cleanup to scan all managed network services for PAC footprints, so that stale PAC settings are found even if the active network changed.
12. As a user, I want Runtime Cleanup to remove seamless-cors PAC settings without restoring previous PAC state, so that cleanup stays simple and ownership-based.
13. As a user, I want Managed PAC Consent before seamless-cors replaces existing managed PAC state, so that I explicitly agree to the clean-break PAC behavior.
14. As a user, I want Managed PAC Consent to show current managed PAC state, so that I know what seamless-cors is about to replace.
15. As a user, I want Managed PAC Consent to explain that previous PAC state will not be restored, so that my consent is informed.
16. As a user, I want Managed PAC Consent only when existing managed PAC state would be replaced, so that seamless-cors does not ask unnecessary questions.
17. As a user, I want no Managed PAC Consent when all managed PAC state is empty or disabled, so that normal starts stay lightweight.
18. As a user, I want no Managed PAC Consent for leftover seamless-cors PAC footprints, so that `start` cleans them first instead of asking me about product-owned state.
19. As a user, I want `start` to exit non-zero with Cleanup Retry Guidance if cleanup fails before consent, so that I know to retry `seamless-cors stop`.
20. As a user, I want CA Trust Consent on each `start` with `ca-trusted: true`, so that adding current-user CA trust is always explicit.
21. As a user, I want Managed PAC Consent and CA Trust Consent combined into one Explicit Lifecycle Consent prompt, so that one start operation has one clear decision.
22. As a user, I want Explicit Lifecycle Consent collected before any OS-managed state changes, so that declining consent leaves my machine unchanged.
23. As a user, I want declining Explicit Lifecycle Consent to stop startup, so that there is no manual proxy fallback or partial lifecycle mode.
24. As a user, I want `start` to write runtime coordination state only after consent, so that declined starts leave no runtime state behind.
25. As a user, I want the Control Endpoint ready before PAC or CA changes are installed, so that `stop` and active-instance checks can coordinate with a partially-started gateway.
26. As a user, I want failed PAC or CA setup after consent to trigger Runtime Cleanup before `start` exits, so that partial lifecycle mutations are removed.
27. As a user, I want cleanup failure after partial setup to exit non-zero, so that false cleanup success is not reported.
28. As a user, I want Cleanup Retry Guidance after cleanup failure, so that I know to resolve OS or permission issues and run `seamless-cors stop` again.
29. As a user, I want `stop` to request active gateway shutdown when one is running, so that the active process can stop serving traffic.
30. As a user, I want `stop` to run Runtime Cleanup after the active gateway stops, so that `stop` only exits once gateway-owned state is clean or a cleanup error is known.
31. As a user, I want `stop` to run Runtime Cleanup even when no Runtime State File exists, so that PAC and CA footprints can still be cleaned.
32. As a user, I want `stop` to clean seamless-cors-owned PAC settings found by footprint, so that a crashed gateway does not leave my machine pointing at a dead PAC endpoint.
33. As a user, I want `stop` to clean seamless-cors-owned CA trust found by CA Footprint, so that an unclean shutdown does not leave trusted development CA material behind.
34. As a user, I want `stop` to delete known runtime CA files, so that local CA keys and certificates do not remain under the Runtime State Directory.
35. As a user, I want `stop` to remove the Runtime State File when no active gateway remains, so that future starts do not see stale coordination state.
36. As a user, I want `status` to stay read-only, so that checking state never changes PAC settings, CA trust, or runtime files.
37. As a user, I want `status` to report not running when no active gateway is verified, so that I understand the gateway lifecycle state.
38. As a user, I want `status` to detect cleanup-needed PAC or CA footprints, so that it can tell me `seamless-cors stop` will clean them.
39. As a user, I want `status` to detect stale Runtime State File data, so that I can understand why `start` or `stop` may perform cleanup.
40. As a user, I want `status` to avoid listing every cleanup resource by default, so that Human Status stays concise.
41. As a user, I want Runtime Cleanup to run during Ctrl-C shutdown, so that foreground use cleans owned PAC, CA, and runtime files.
42. As a user, I want Runtime Cleanup to run on normal gateway termination, so that ordinary exits clean the same resources as `stop`.
43. As a user, I want Runtime Cleanup to run after Fatal Config Error while running, so that invalid live config does not leave lifecycle state behind.
44. As a user, I want cleanup to be protected from active gateways, so that product-owned footprints are not removed while a gateway is legitimately running.
45. As a user, I want Single User Instance to remain based on Runtime State File and Control Endpoint verification, so that PAC footprints do not imply liveness.
46. As a user, I want PAC Endpoint listeners to require no cleanup records, so that process-owned OS resources are not treated as durable cleanup state.
47. As a user, I want Runtime-Owned File Cleanup scoped to known seamless-cors files under the Runtime State Directory, so that user files elsewhere are never deleted.
48. As a user, I want CA Footprint cleanup scoped to current-user OS trust, so that system-wide or browser-specific trust stores are not modified.
49. As a user, I want the CA Footprint stricter than name-contains matching, so that cleanup does not remove unrelated certificates.
50. As a user, I want seamless-cors-owned CA trust to be fully controlled by seamless-cors, so that cleanup may remove matching CA trust without runtime records.
51. As a developer, I want platform adapter behavior expressed through small interfaces, so that PAC state scanning and cleanup can be tested with fake adapters.
52. As a developer, I want footprint matching isolated from OS command execution, so that ownership detection can be tested without mutating real machine settings.
53. As a developer, I want lifecycle orchestration separated from platform mechanics, so that start/stop/status ordering can be tested deterministically.
54. As a developer, I want consent prompt construction separated from terminal input, so that consent requirements and decline behavior are testable.
55. As a developer, I want CA Footprint detection isolated from trust-store mutation, so that trust cleanup rules can be tested safely.
56. As a developer, I want cleanup operations to report partial failures precisely, so that retry guidance can distinguish cleanup-needed state from successful cleanup.
57. As a developer, I want current managed PAC state rendered consistently, so that prompts and status output describe the same machine state.
58. As a developer, I want Runtime Cleanup to be safe under repeated calls, so that start failure, stop, Ctrl-C, and shutdown can all call it without coordination bugs.
59. As a QA engineer, I want tests for declined consent, so that startup cannot mutate PAC, CA, runtime state, or listeners beyond harmless preflight checks.
60. As a QA engineer, I want tests for cleanup before invalid config errors, so that cleanup remains available even when config is broken.

## Implementation Decisions

- Implement Runtime Cleanup as the single cleanup behavior. Do not keep separate Graceful Cleanup, Stale Runtime Cleanup, Proxy Recovery, CA Recovery, or Full CA Removal concepts in the implementation surface.
- Runtime Cleanup is idempotent. It removes seamless-cors-owned resources proven by footprint or runtime-file ownership, and treats missing resources as already clean.
- Runtime Cleanup does not restore previous managed PAC state. No component should record previous PAC state for restoration.
- Managed PAC ownership is proven by Managed PAC Footprint: a loopback HTTP PAC URL whose path basename is `seamless-cors.pac`. Matching must not depend on the selected listener port.
- Managed PAC cleanup scans all currently managed network services and clears only services whose current managed PAC URL matches the Managed PAC Footprint.
- Managed PAC cleanup must leave non-seamless-cors PAC URLs alone, including remote or corporate PAC URLs that do not match the loopback `seamless-cors.pac` footprint.
- Generated PAC and PAC Endpoint routing should use the `seamless-cors.pac` path so that installed managed PAC settings carry the footprint.
- CA trust ownership is proven by CA Footprint. The CA Footprint must be strict: exact seamless-cors-owned current-user CA trust identity, self-signed CA semantics, and current-user trust scope.
- CA cleanup may remove all current-user CA trust matching the CA Footprint. Manual user or developer creation of CA trust with the same footprint is outside the domain contract.
- Runtime-Owned File Cleanup may remove known seamless-cors runtime files under the Runtime State Directory, including CA cert/key files and runtime coordination files.
- Runtime-Owned File Cleanup must not delete the entire Runtime State Directory blindly and must not delete files outside the known product-owned runtime set.
- Runtime State File remains the active-instance signal. PAC or CA footprints prove cleanup ownership, not liveness.
- Runtime State Verification checks the Control Endpoint from the Runtime State File before treating a gateway as active.
- `start` must not run Runtime Cleanup when an active gateway is verified.
- `start` runs Runtime Cleanup before reading or validating the new Explicit Configuration when no active gateway is verified.
- `start` validates Explicit Configuration and the Domain List after cleanup and before consent.
- `start` determines all required Explicit Lifecycle Consent after validation and before OS-managed state changes.
- Explicit Lifecycle Consent is one combined start-time prompt covering Managed PAC Consent and CA Trust Consent.
- Managed PAC Consent is required only when `start` would replace existing managed PAC state that is not already seamless-cors-owned.
- Managed PAC Consent shows current managed PAC state and explains No PAC Restoration.
- CA Trust Consent is required on every `start` with `ca-trusted: true`.
- Declining Explicit Lifecycle Consent exits startup without changing machine PAC settings, CA trust, runtime state, or serving listeners beyond harmless preflight checks.
- Runtime State File should be written only after consent is granted.
- The Control Endpoint should be ready before installing managed PAC settings or adding CA trust, so `stop` and active-instance verification can coordinate with lifecycle changes.
- If any OS-managed lifecycle mutation succeeds and a later mutation fails, `start` must perform Runtime Cleanup before returning an error.
- Cleanup failures return non-zero and use Cleanup Retry Guidance telling the user to run `seamless-cors stop` again after resolving the OS or permission problem.
- `stop` coordinates active gateway shutdown when possible, then runs Runtime Cleanup and exits only after cleanup is complete or an unrecoverable cleanup error is known.
- `stop` runs Runtime Cleanup even when the Runtime State File is missing.
- `status` remains Read-Only Status. It may scan for cleanup-needed state but must not mutate PAC settings, CA trust, or runtime files.
- `status` should report not running plus a concise hint when cleanup-needed state is present and `seamless-cors stop` can clean it.
- PAC Endpoint/listeners need no cleanup record because loopback listeners are process-owned OS resources.
- All-Service PAC Management remains the supported platform behavior: PAC routing is applied to every managed network service so routing remains consistent when the active network changes.
- Existing platform adapters should move from restore-oriented APIs toward state scanning, footprint cleanup, and installation of the current run's PAC URL.
- Existing CA code should move from record-only trust cleanup toward CA Footprint cleanup plus Runtime-Owned File Cleanup.
- Existing app lifecycle code should be reorganized so active-instance verification, cleanup, config validation, consent, control startup, OS mutation, and serving are explicit steps.

Major modules to build or modify:

- Lifecycle orchestrator: owns `start`, `stop`, status ordering, active-instance guard, cleanup-before-config behavior, consent-before-mutation behavior, and cleanup-after-partial-failure behavior.
- Runtime cleanup module: a deep module exposing an idempotent cleanup operation over PAC footprints, CA footprints, runtime-owned files, and Runtime State File removal.
- Managed PAC platform adapter: exposes current managed PAC state, installs the current Generated PAC URL, and clears managed PAC settings matching the Managed PAC Footprint.
- Managed PAC footprint matcher: parses PAC URLs and determines whether they are loopback HTTP URLs whose path basename is `seamless-cors.pac`.
- Lifecycle consent module: determines whether Managed PAC Consent and CA Trust Consent are required, renders one combined prompt, and returns an explicit proceed/decline result.
- CA trust adapter: exposes CA Footprint cleanup for current-user OS trust and keeps real OS trust mutations behind testable interfaces.
- Runtime file ownership module: identifies known seamless-cors runtime files that Runtime Cleanup may delete.
- Human status module: reports active gateway state and cleanup-needed state without mutation.

## Testing Decisions

- Tests should verify external behavior and domain contracts, not internal call ordering unless that ordering is externally visible through mutation/no-mutation outcomes.
- Runtime Cleanup tests should use fake PAC, CA, and runtime-file adapters to verify idempotency, missing-resource success, partial failure behavior, and retry guidance.
- Managed PAC Footprint tests should cover loopback HTTP URLs ending in `seamless-cors.pac`, different ports, nested paths ending in the footprint filename, localhost and loopback IPs, non-loopback hosts, HTTPS URLs, remote corporate PAC URLs, and filenames that merely contain the footprint as a substring.
- Managed PAC cleanup tests should cover all-service scanning, cleaning only seamless-cors-owned PAC settings, leaving non-seamless-cors PAC settings untouched, service disappearance, OS query failure, and repeated cleanup.
- Managed PAC install tests should verify the generated PAC URL uses the `seamless-cors.pac` path.
- CA Footprint tests should use fake trust stores. They should verify exact seamless-cors current-user CA trust cleanup, no name-contains cleanup, no system-wide cleanup, already-absent trust success, and repeated cleanup.
- Runtime-Owned File Cleanup tests should verify deletion of known runtime files, no deletion outside the Runtime State Directory, no broad directory deletion, missing file success, and partial file deletion failure handling.
- Lifecycle `start` tests should cover verified active gateway exits without cleanup, no active gateway runs cleanup before config validation, cleanup failure prevents config validation and start, invalid config after successful cleanup, and fresh config read after cleanup.
- Lifecycle `start` consent tests should cover no consent needed, Managed PAC Consent only, CA Trust Consent only, combined consent, decline exits without OS mutation, and consent granted proceeds to runtime state and OS mutation.
- Lifecycle partial failure tests should cover PAC install succeeds then CA trust fails, CA trust succeeds then later serving setup fails if applicable, cleanup success after partial mutation, and cleanup failure with non-zero exit plus Cleanup Retry Guidance.
- `stop` tests should cover active gateway shutdown followed by cleanup, missing Runtime State File cleanup, stale Runtime State File cleanup, cleanup failure non-zero, and repeated stop becoming a clean success.
- `status` tests should cover active gateway status, not running with no cleanup-needed state, not running with PAC footprint, not running with CA footprint, not running with runtime files, stale Runtime State File detection, and no mutation of fake adapters.
- Consent prompt tests should assert that current managed PAC state is displayed when Managed PAC Consent is required and that No PAC Restoration is stated.
- Prior art already exists in the repository for app lifecycle tests with fake adapters, PAC generation tests, platform adapter tests, CA tests with fake trust adapters, and control/status tests. Extend those patterns rather than requiring real OS changes.

## Out of Scope

- Restoring previous machine PAC state.
- Proxy Chaining.
- Proxy Recovery.
- CA Recovery records.
- Runtime Resource Records for PAC or CA cleanup.
- Manual proxy fallback when consent is declined.
- Persistent lifecycle consent stored in configuration.
- Persistent reusable CA.
- System-wide CA trust cleanup.
- Browser-specific trust store cleanup or diagnostics.
- Cleanup of arbitrary files under the home config directory.
- Cleanup of PAC URLs that are not loopback HTTP URLs ending in `seamless-cors.pac`.
- Treating PAC or CA footprints as evidence of a live gateway.
- Changing CORS response repair behavior.
- Changing Domain List matching behavior.
- Introducing JSON status or machine-readable cleanup reports.
- Supporting multiple simultaneous gateway instances for one user.

## Further Notes

- This PRD follows the updated glossary in `CONTEXT.md` and ADRs for Ephemeral User CA, PAC-Based Selective Managed Proxy, cleanup before config validation, and removing owned PAC without restoring previous state.
- The existing broad seamless-cors PRD is older and still contains decisions that conflict with the current domain model, including proxy chaining, proxy recovery, marker-based recovery, manual proxy mode, and fixed listener expectations. This PRD should be treated as the source of truth for Runtime Cleanup and Explicit Lifecycle Consent.
- Implementation should keep real OS trust and proxy mutations behind fakeable adapters. Automated tests must not mutate the real OS proxy settings or trust store.
