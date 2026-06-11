# Run cleanup before config validation

`start` checks for an active gateway and, when none is verified, performs Runtime Cleanup before reading or validating the new Explicit Configuration. This keeps cleanup available even when the current config file is invalid, and ensures every new process starts from fresh configuration only after seamless-cors-owned PAC and runtime process state has been cleaned.
