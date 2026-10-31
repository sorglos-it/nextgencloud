//! Envelopes for documents signed with Ed25519 keys held by NGC components:
//! API nodes, stores and the release key. A Rust reading of `shared/sign`
//! (sign.go), written to answer one question: what does a second
//! implementation of one contract cost, and does it come out bit for bit the
//! same? It is evidence for a decision paper, not a part of NGC.

use base64::engine::{general_purpose::URL_SAFE_NO_PAD, Engine};
use ed25519_dalek::{Signature, Signer, SigningKey, Verifier, VerifyingKey};
use serde::{Deserialize, Deserializer, Serialize, Serializer};
use sha2::{Digest, Sha256};

// Purposes. A signature always covers message(purpose, payload), so a
// signature made for one purpose never verifies for another.
pub const PURPOSE_CLIENT_CERT: &str = "clientcert/v1";
pub const PURPOSE_API_CERT: &str = "apicert/v1";
pub const PURPOSE_TRUST_LIST: &str = "trustlist/v1";
pub const PURPOSE_TASK: &str = "task/v1";
pub const PURPOSE_CONFIRM: &str = "confirm/v1";
pub const PURPOSE_CATALOG: &str = "catalog/v1";
pub const PURPOSE_LICENSE: &str = "license/v1";
pub const PURPOSE_RELEASE: &str = "release/v1";

/// The largest payload an envelope may carry (Go: sign.MaxPayload).
pub const MAX_PAYLOAD: usize = 4 << 20;

/// The length of a key ID in bytes, before base64url (Go: sign.KeyIDSize).
pub const KEY_ID_SIZE: usize = 16;

/// What the JSON of an envelope holds besides the base64url of its payload:
/// member names, key ID, signature and white space (Go: envelopeSlack).
const ENVELOPE_SLACK: usize = 1 << 10;

const SIG_SIZE: usize = 64;
const PUBLIC_KEY_SIZE: usize = 32;

/// base64url without padding, strict in both directions, as enc.B64 is: the
/// alphabet is checked and trailing bits must be zero, so every byte string
/// has exactly one encoding. The crate's default config refuses trailing
/// bits; it is named here rather than relied on, because a default that
/// changes under us would change what NGC accepts.
pub fn b64_encode(b: &[u8]) -> String {
    URL_SAFE_NO_PAD.encode(b)
}

pub fn b64_decode(s: &str) -> Result<Vec<u8>, Error> {
    URL_SAFE_NO_PAD.decode(s.as_bytes()).map_err(|_| Error::Envelope)
}

fn encoded_len(n: usize) -> usize {
    // base64url without padding: four characters per three bytes, and the
    // remainder of one or two bytes takes two or three.
    n / 3 * 4 + match n % 3 {
        0 => 0,
        1 => 2,
        _ => 3,
    }
}

/// A byte string that encodes as a base64url string in JSON (Go: enc.Bytes).
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Bytes(pub Vec<u8>);

impl Serialize for Bytes {
    fn serialize<S: Serializer>(&self, s: S) -> Result<S::Ok, S::Error> {
        s.serialize_str(&b64_encode(&self.0))
    }
}

impl<'de> Deserialize<'de> for Bytes {
    fn deserialize<D: Deserializer<'de>>(d: D) -> Result<Self, D::Error> {
        let s = String::deserialize(d)?;
        // A string outside the alphabet, with padding, or with bits after its
        // last byte is no byte string. The error is the decoder's own, so it
        // never carries a piece of the input onwards.
        URL_SAFE_NO_PAD
            .decode(s.as_bytes())
            .map(Bytes)
            .map_err(|_| serde::de::Error::custom("enc: not base64url"))
    }
}

/// Every failure of a document from outside is this one error, as it is one
/// error in Go (sign.ErrEnvelope): which rule a document broke is nothing a
/// caller has to tell apart.
#[derive(Debug, PartialEq, Eq)]
pub enum Error {
    Envelope,
    BadSignature,
    WrongKey,
    TooLarge,
}

/// A document signed with an Ed25519 key. Verify the signature before
/// parsing the payload.
#[derive(Clone, Debug, Serialize, Deserialize, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
pub struct Envelope {
    pub payload: Bytes,
    pub kid: String,
    pub sig: Bytes,
}

impl Envelope {
    /// Checks the structure without the signature: a payload of 1 to
    /// max_payload bytes, a key ID as key_id makes it (22 characters, 16
    /// bytes) and a 64-byte signature (Go: Envelope.Validate).
    pub fn validate(&self, max_payload: usize) -> Result<(), Error> {
        let max = max_payload.min(MAX_PAYLOAD);
        if max < 1
            || self.payload.0.is_empty()
            || self.payload.0.len() > max
            || self.kid.len() != encoded_len(KEY_ID_SIZE)
            || self.sig.0.len() != SIG_SIZE
        {
            return Err(Error::Envelope);
        }
        match b64_decode(&self.kid) {
            Ok(id) if id.len() == KEY_ID_SIZE => Ok(()),
            _ => Err(Error::Envelope),
        }
    }
}

/// Decodes an envelope from outside: strict JSON within a budget of the
/// base64url of max_payload bytes plus 1 KiB, then validate. A max_payload
/// above MAX_PAYLOAD counts as MAX_PAYLOAD. It checks no signature (Go:
/// sign.ParseEnvelope).
pub fn parse_envelope(b: &[u8], max_payload: usize) -> Result<Envelope, Error> {
    let max = max_payload.min(MAX_PAYLOAD);
    if max < 1 {
        return Err(Error::Envelope);
    }
    // An envelope has no array, so the budget only bounds its size.
    if b.len() > encoded_len(max) + ENVELOPE_SLACK {
        return Err(Error::Envelope);
    }
    let env: Envelope = serde_json::from_slice(b).map_err(|_| Error::Envelope)?;
    env.validate(max)?;
    Ok(env)
}

/// The bytes a signature covers: "ngc/" || purpose || 0x00 || payload
/// (Go: sign.Message).
pub fn message(purpose: &str, payload: &[u8]) -> Vec<u8> {
    let mut m = Vec::with_capacity(4 + purpose.len() + 1 + payload.len());
    m.extend_from_slice(b"ngc/");
    m.extend_from_slice(purpose.as_bytes());
    m.push(0);
    m.extend_from_slice(payload);
    m
}

/// Identifies a public key: base64url of the first 16 bytes of its SHA-256
/// (Go: sign.KeyID).
pub fn key_id(pub_key: &[u8]) -> String {
    let h = Sha256::digest(pub_key);
    b64_encode(&h[..KEY_ID_SIZE])
}

/// Signs payload for purpose (Go: sign.Sign).
pub fn sign(priv_key: &SigningKey, purpose: &str, payload: &[u8]) -> Result<Envelope, Error> {
    if payload.len() > MAX_PAYLOAD {
        return Err(Error::TooLarge);
    }
    let pub_key = priv_key.verifying_key();
    Ok(Envelope {
        payload: Bytes(payload.to_vec()),
        kid: key_id(pub_key.as_bytes()),
        sig: Bytes(priv_key.sign(&message(purpose, payload)).to_bytes().to_vec()),
    })
}

/// Checks env against pub_key for purpose (Go: sign.Verify).
pub fn verify(env: &Envelope, pub_key: &[u8], purpose: &str) -> Result<(), Error> {
    if env.payload.0.len() > MAX_PAYLOAD {
        return Err(Error::TooLarge);
    }
    if env.kid != key_id(pub_key) {
        return Err(Error::WrongKey);
    }
    if pub_key.len() != PUBLIC_KEY_SIZE || env.sig.0.len() != SIG_SIZE {
        return Err(Error::BadSignature);
    }
    // Go hands ed25519.Verify a key and a signature of any length and gets
    // false; here both have to become types of their own size first, and a
    // length that does not fit is the same answer, not a panic.
    let key: [u8; PUBLIC_KEY_SIZE] = pub_key.try_into().map_err(|_| Error::BadSignature)?;
    let sig: [u8; SIG_SIZE] = env.sig.0[..].try_into().map_err(|_| Error::BadSignature)?;
    let key = VerifyingKey::from_bytes(&key).map_err(|_| Error::BadSignature)?;
    key.verify(&message(purpose, &env.payload.0), &Signature::from_bytes(&sig))
        .map_err(|_| Error::BadSignature)
}
