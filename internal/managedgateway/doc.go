// Package managedgateway owns the command-facing gateway lifecycle.
//
// Start, Stop, and Status interpret runtime state, choose cleanup timing,
// activate listeners, wire the Control Endpoint and PAC Endpoint, serve the
// CORS Proxy, watch Live Configuration, and compose read-only status behind
// the Gateway interface.
package managedgateway
