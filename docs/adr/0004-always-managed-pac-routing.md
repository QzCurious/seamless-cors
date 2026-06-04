# Always-Managed PAC Routing

The gateway supports only Managed System Proxy operation and removes Manual Proxy Mode, `managed-system-proxy`, and user-configured listener addresses. Supported platforms must provide managed PAC installation, while proxy, PAC, and control listeners are selected automatically on loopback at startup and connected through the Runtime State File. This gives up manual routing control and backward compatibility so normal use has one traffic-capture model, fewer user-facing lifecycle settings, and resilient startup when default ports are occupied.
