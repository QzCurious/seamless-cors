# Installed User CA

The gateway keeps one long-lived seamless-cors-owned development CA in the current user's operating-system trust store instead of generating a new CA for each trusted gateway run. This retains local CA material so normal `start` and `stop` cycles do not repeatedly require platform trust approval, while explicit CA lifecycle commands and start-time CA Ensure handle installation, repair, renewal, and removal.

The local CA signing key is stored as current-user-readable product state protected by file permissions, not encrypted at rest. Encrypting the key would reintroduce repeated unlock prompts or require a local secret-store dependency, which conflicts with the goal of avoiding trust friction during normal trusted starts.

CA Ensure reuses only a fully usable installed CA: one owned current-user trust identity, matching local CA certificate and key, acceptable validity, and local permissions that can be tightened if needed. Other unusable states are repaired by removing owned CA state and installing fresh CA material, so the lifecycle avoids partial reuse of stale or ambiguous trust material.
