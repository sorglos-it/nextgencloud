//! Proves the Rust reading of shared/sign against the Go one, on the vectors
//! Go and JavaScript already share (shared/testdata/sign.json) and on the
//! rejection table of shared/sign/sign_test.go. Every line it prints is a
//! case: OK or FAIL. It writes nothing into shared/.

use ngc_sign_versuch::*;
use ed25519_dalek::SigningKey;
use serde::{Deserialize, Serialize};
use std::process::ExitCode;

// The vector file, member for member and in the order Go writes it
// (shared/sign/sign_test.go: signVector).
#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct Vector {
    seed: Bytes,
    public: Bytes,
    purpose: String,
    message: Bytes,
    envelope: Envelope,
}

struct Report {
    ok: usize,
    bad: usize,
}

impl Report {
    fn check(&mut self, name: &str, pass: bool) {
        if pass {
            self.ok += 1;
            println!("OK   {name}");
        } else {
            self.bad += 1;
            println!("FAIL {name}");
        }
    }
}

fn main() -> ExitCode {
    let path = std::env::args().nth(1).unwrap_or_else(|| {
        "../../shared/testdata/sign.json".to_string()
    });
    let raw = std::fs::read(&path).expect("read the vector file");
    let v: Vector = serde_json::from_slice(&raw).expect("parse the vector file");
    let mut r = Report { ok: 0, bad: 0 };

    let seed: [u8; 32] = v.seed.0[..].try_into().expect("a 32-byte seed");
    let key = SigningKey::from_bytes(&seed);
    let public = key.verifying_key().to_bytes().to_vec();

    // 1. The key the seed makes, the id it gets, the bytes a signature covers
    //    and the signature itself - each against the vector, byte for byte.
    r.check("public key from the seed", public == v.public.0);
    r.check("key id", key_id(&public) == v.envelope.kid);
    r.check("message bytes", message(&v.purpose, &v.envelope.payload.0) == v.message.0);

    let made = sign(&key, &v.purpose, &v.envelope.payload.0).expect("sign");
    r.check("signature bytes", made.sig == v.envelope.sig);
    r.check("whole envelope", made == v.envelope);

    // 2. The whole vector file, rebuilt from Rust and compared with the file
    //    Go's golden test wrote. This is the bit-equality claim in one line.
    let rebuilt = Vector {
        seed: v.seed.clone(),
        public: Bytes(public.clone()),
        purpose: v.purpose.clone(),
        message: Bytes(message(&v.purpose, &v.envelope.payload.0)),
        envelope: made.clone(),
    };
    let mut out = serde_json::to_vec_pretty(&rebuilt).expect("encode");
    out.push(b'\n'); // golden.Check appends one, Go's MarshalIndent does not.
    r.check("the vector file rebuilt byte for byte", out == raw);
    if out != raw {
        println!("     own bytes:\n{}", String::from_utf8_lossy(&out));
    }

    // 3. Verify: the envelope as it stands, and every way it can be wrong.
    r.check("verify takes the envelope", verify(&v.envelope, &public, &v.purpose).is_ok());
    let mut flipped = v.envelope.clone();
    flipped.sig.0[0] ^= 1;
    r.check("a flipped signature is refused",
        verify(&flipped, &public, &v.purpose) == Err(Error::BadSignature));
    let mut other = v.envelope.clone();
    other.payload.0[0] ^= 1;
    r.check("other bytes under the same signature are refused",
        verify(&other, &public, &v.purpose) == Err(Error::BadSignature));
    r.check("another purpose is refused",
        verify(&v.envelope, &public, PURPOSE_CATALOG) == Err(Error::BadSignature));
    let mut strange = public.clone();
    strange[0] ^= 1;
    r.check("another key is refused by its id",
        verify(&v.envelope, &strange, &v.purpose) == Err(Error::WrongKey));
    r.check("a key of 31 bytes is refused",
        verify(&v.envelope, &public[..31], &v.purpose) == Err(Error::WrongKey));

    // 4. The rejection table of shared/sign/sign_test.go (TestParseEnvelope),
    //    case for case, over an envelope of this contract's own vector. The
    //    payload is 15 bytes here, where the Go test signs 7, so the bound
    //    cases count from 15.
    let n = v.envelope.payload.0.len();
    let payload = format!("\"payload\":\"{}\"", b64_encode(&v.envelope.payload.0));
    let kid = format!("\"kid\":\"{}\"", v.envelope.kid);
    let sig = format!("\"sig\":\"{}\"", b64_encode(&v.envelope.sig.0));
    let doc = |members: &[&str]| -> Vec<u8> { format!("{{{}}}", members.join(",")).into_bytes() };
    let good = doc(&[&payload, &kid, &sig]);

    for (name, input) in [
        ("members in another order", doc(&[&sig, &payload, &kid])),
        ("white space", format!(" {{\n{payload} , {kid},\t{sig}}}\r\n").into_bytes()),
    ] {
        r.check(&format!("taken: {name}"), parse_envelope(&input, n).is_ok());
    }

    let kid_short = format!("\"kid\":\"{}\"", &v.envelope.kid[1..]);
    let kid_bits = format!("\"kid\":\"{}B\"", "A".repeat(21));
    let kid_alpha = format!("\"kid\":\"{}\"", "+".repeat(22));
    let sig_63 = format!("\"sig\":\"{}\"", b64_encode(&v.envelope.sig.0[..63]));
    let payload_pad = format!("\"payload\":\"{}=\"", b64_encode(&v.envelope.payload.0));
    let kid_case = format!("\"Kid\":\"{}\"", v.envelope.kid);
    let mut trailing = good.clone();
    trailing.extend_from_slice(b"{}");
    let mut too_big = good.clone();
    too_big.extend(std::iter::repeat(b' ').take(encoded_len_pub(n) + 1024));

    let cases: Vec<(&str, Vec<u8>, usize)> = vec![
        ("payload one byte over the bound", good.clone(), n - 1),
        ("bound below 1", good.clone(), 0),
        ("empty payload", doc(&["\"payload\":\"\"", &kid, &sig]), n),
        ("no payload", doc(&[&kid, &sig]), n),
        ("no key ID", doc(&[&payload, &sig]), n),
        ("no signature", doc(&[&payload, &kid]), n),
        ("key ID one character short", doc(&[&payload, &kid_short, &sig]), n),
        ("key ID with bits after its 16 bytes", doc(&[&payload, &kid_bits, &sig]), n),
        ("key ID outside the alphabet", doc(&[&payload, &kid_alpha, &sig]), n),
        ("signature of 63 bytes", doc(&[&payload, &kid, &sig_63]), n),
        ("payload with padding", doc(&[&payload_pad, &kid, &sig]), n),
        ("unknown member", doc(&[&payload, &kid, &sig, "\"x\":\"y\""]), n),
        ("duplicate member", doc(&[&payload, &kid, &kid, &sig]), n),
        ("member name in another case", doc(&[&payload, &kid_case, &sig]), n),
        ("null", doc(&[&payload, &kid, "\"sig\":null"]), n),
        ("trailing data", trailing, n),
        ("not an object", b"[]".to_vec(), n),
        ("over the size of its budget", too_big, n),
    ];
    for (name, input, max) in cases {
        r.check(&format!("refused: {name}"),
            parse_envelope(&input, max) == Err(Error::Envelope));
    }

    println!("\n{} OK, {} FAIL", r.ok, r.bad);
    if r.bad == 0 { ExitCode::SUCCESS } else { ExitCode::FAILURE }
}

// encoded_len is module-private in the library, as it is in Go; the budget
// case needs the same number here.
fn encoded_len_pub(n: usize) -> usize {
    n / 3 * 4 + match n % 3 { 0 => 0, 1 => 2, _ => 3 }
}
