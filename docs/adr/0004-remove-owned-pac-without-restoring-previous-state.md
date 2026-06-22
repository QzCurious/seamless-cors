# Remove owned PAC without restoring previous state

Gateway Footprint Cleanup removes managed PAC settings that carry the Managed PAC Ownership Marker, but it does not record or restore previous machine PAC state. This trades exact rollback for a clean-break model: replacing existing PAC settings requires Explicit Lifecycle Consent, and cleanup stays idempotent by removing only resources with seamless-cors Ownership Markers.
