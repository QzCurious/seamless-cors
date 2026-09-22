// Package gateway owns the complete gateway lifecycle behind operation-specific
// commands. Callers do not need to know whether an operation is executed in the
// current process or forwarded to an existing foreground owner.
//
// The implementation is organized by responsibility inside this package:
// discovery owns Gateway Ownership and its state cache, transport provides authenticated local
// HTTP, foreground supervises process lifetime, lifecycle coordinates commands,
// lifecycle retains CA and Upstream List facts and composes traffic, the start
// sequence governs activation, and traffic serves the immutable PAC/proxy pair. Those are
// implementation details rather than caller-visible seams.
package gateway
