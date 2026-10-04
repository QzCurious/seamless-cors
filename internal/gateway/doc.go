// Package gateway runs one foreground Gateway per coordination environment.
// Start holds the instance lock, initializes traffic, publishes authenticated
// local control discovery, and waits for stop, a signal, or a serving failure.
// Status, stop, and CA commands address that process while it is running;
// offline commands inspect or mutate local state under the same instance lock.
// Traffic composition and OS PAC delivery remain sequential lifecycle updates.
package gateway
