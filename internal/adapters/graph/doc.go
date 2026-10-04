// Package graph is the Microsoft Graph adapter: it implements usecase.Graph
// over core httpx. Every request shape is an assumption (UA-1, unverified
// against a real tenant), each pinned by a TestAssumed* test.
//
// The adapter never sees a token: authorization is injected through
// httpx.TokenRefresher. Reads are marked safe to retry, writes never are.
// Errors never contain response bodies, ids beyond path templates, or tokens.
package graph
