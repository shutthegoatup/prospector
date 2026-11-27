package main

// The rest change a run only while the server named as its owner still holds it, each
// in one statement, so of two servers only one ever drives a run.

// A lapsed run is ended or taken over only if its lease is still the one that lapsed,
// so of two servers sweeping, one does it.
