// Argon2id for the password key (Go: proof.DeriveKey over argon2.IDKey).
//
// It runs here and not on the page's own thread, because the derivation is
// meant to be slow - about a second at m=65536, and 64 MiB of working memory
// while it runs - and the window has to keep answering the user meanwhile.
// The typed password lives in one place only: proof.deriveSeed hands its
// buffer over instead of copying it, this file wipes it the moment Argon2id
// has read it, and the worker is terminated right after the seed is on its
// way back, so the working memory goes with it.
//
// Nothing is decided here. proof.validateKDF has held every parameter
// against the contract before the message was sent, and the length of the
// seed comes with it: this file computes and nothing else. Whatever it
// throws reaches deriveSeed as an error event, which answers with BadProof.
import { argon2id } from "../../vendor/noble-hashes/argon2.js";

self.onmessage = (e) => {
  const { password, salt, t, m, p, dkLen } = e.data;
  try {
    self.postMessage(argon2id(password, salt, { t, m, p, dkLen }));
  } finally {
    password.fill(0);
  }
};
