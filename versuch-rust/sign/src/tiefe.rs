//! One probe, for the question the sign contract alone cannot answer: Go's
//! enc.DecodeStrict refuses a duplicate member AT ANY DEPTH, also inside a
//! json.RawMessage that is kept byte for byte (shared/enc/enc.go). The bigger
//! contracts - core, store, wire - rest on that. Does serde_json refuse it?
use serde::Deserialize;

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Outer {
    inner: serde_json::Value,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Flat {
    a: u32,
}

fn main() {
    let nested = br#"{"inner":{"a":1,"a":2}}"#;
    match serde_json::from_slice::<Outer>(nested) {
        Ok(o) => println!("nested duplicate: TAKEN, inner = {}", o.inner),
        Err(e) => println!("nested duplicate: refused ({e})"),
    }
    let raw = br#"{"inner":{"a":1,"a":2}}"#;
    match serde_json::from_slice::<&serde_json::value::RawValue>(raw) {
        Ok(v) => println!("raw value: TAKEN, bytes kept = {}", v.get()),
        Err(e) => println!("raw value: refused ({e})"),
    }
    let flat = br#"{"a":1,"a":2}"#;
    match serde_json::from_slice::<Flat>(flat) {
        Ok(f) => println!("flat duplicate on a derived struct: TAKEN, a = {}", f.a),
        Err(e) => println!("flat duplicate on a derived struct: refused ({e})"),
    }
    let utf8 = b"{\"inner\":\"\xff\"}";
    match serde_json::from_slice::<Outer>(utf8) {
        Ok(_) => println!("invalid UTF-8: TAKEN"),
        Err(e) => println!("invalid UTF-8: refused ({e})"),
    }
}
