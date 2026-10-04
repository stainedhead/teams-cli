// Package graphtest is an httptest fake of the Microsoft Graph routes that
// teams-cli uses (data-dictionary "Graph request/response mapping"). It is
// shared by the graph adapter tests and by the cli and cmd integration tests.
//
// Every shape here is an assumption (UA-1, unverified against a real tenant).
// The fake checks that an Authorization header is present and never records or
// validates its value.
package graphtest
