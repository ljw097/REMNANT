package update

// productionKey is the public half of the release signing key. The private
// half exists only as the REMNANT_RELEASE_SIGNING_KEY secret of the release
// workflow. To rotate: generate a new key with remnant-release keygen, ship a
// release that lists both keys (comma-separated) signed with the old key,
// then switch the secret to the new key.
const productionKey = "8qYPsCsnnR5TuOoY7ASWeu0+Txh48WBxA1qmXyZT5IM=" // ggignore: Ed25519 public key; public by design
