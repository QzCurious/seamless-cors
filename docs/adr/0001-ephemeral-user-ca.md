# Ephemeral User CA

The gateway generates an ephemeral local development certificate authority for each trusted gateway run instead of keeping one persistent CA per user. This may require trust setup more often, but it avoids retaining a CA private key after the gateway stops and keeps CA ownership explicit: Runtime Cleanup removes seamless-cors-owned CA trust and local CA files by footprint and runtime-file ownership.
