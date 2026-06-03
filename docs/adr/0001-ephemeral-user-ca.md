# Ephemeral User CA

The gateway generates an ephemeral local development certificate authority for each trusted gateway run instead of keeping one persistent CA per user. This may require trust setup more often, but it avoids retaining a CA private key after the gateway stops and makes shutdown cleanup explicit: stopping the gateway removes OS trust and deletes the local CA files.
