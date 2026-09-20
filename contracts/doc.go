// Package contracts holds the agreements between the parts of NGC: web
// interface, API, controller and database. The sub-packages are the source of
// truth; the web interface implements the same rules in JavaScript and checks
// itself against the vectors in contracts/testdata.
//
// Only the coordinator changes these packages. Licensed under MIT.
package contracts
