# Export classification

This template uses only standard, publicly documented cryptographic algorithms and protocols: TLS
1.2/1.3; SSH and SSH certificates (Ed25519, RSA, ECDSA); AES-256-GCM and AES-XTS (disk encryption);
TPM 2.0 sealing; age (X25519, ChaCha20-Poly1305, HKDF and scrypt inside the format); Ed25519,
ECDSA and RSA signatures (update and release signing); Argon2id, HMAC and SHA-256. None of it is
non-standard or proprietary cryptography.

If this repository's source code is public, then under the US Export Administration Regulations
(15 CFR 742.15(b)) that source is not subject to the EAR: no classification, licence or
notification applies to it. Builds and binaries produced from public source carry the same
treatment under 15 CFR 734.3(b)(3) and 734.17.

## A product built from this template must review its own

This file describes the template, as a starting point. It is not the product's statement. A product
copies the template, then adds and removes cryptography and changes whether its source is public,
and either can change the answer. Before its first release, a product reviews its own use against
the list above, and keeps its own `CRYPTO.md` that says what it uses and whether its source is
public. The review is the product's, and so is the responsibility for it.

This file is a factual statement about the cryptography used in the template, not legal advice.
