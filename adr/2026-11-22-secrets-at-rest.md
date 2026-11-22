# secrets at rest: AES-256-GCM seal, KEK file outside volumes

## Context

Identity signing seeds and later credential-broker values must be
encrypted at rest with one mechanism, not per-service crypto.

## Decision

`shared/seal` seals secrets with AES-256-GCM. AAD binds every value to
`purpose + kid`, so a blob cannot be replayed under another purpose or
key id. The KEK is a 32-byte file created by `hubctl init`, stored on
the host outside Docker volumes and outside backups; services load it
by path. Output is `nonce || ciphertext` and never contains plaintext
or the key.

## Rejected

- libsodium / age: extra dependency for no gain over stdlib GCM here.
- KEK inside a volume or the database: the backup would carry the key
  next to the data it protects.

## Invariants

- adr.secrets-at-rest.gcm-aad — `TestRoundTrip`,
  `TestWrongPurposeOrKidFails`, `TestWrongKeyAndTamperFail`
- adr.secrets-at-rest.no-plaintext — `TestNoPlaintextLeak`
- adr.secrets-at-rest.kek-file — `TestLoadRejectsBadLen`
