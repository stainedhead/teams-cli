package domain

// Destination looks up a destination. Stub: implemented by WS-A.
func (p Policy) Destination(a Alias) (Destination, bool) { return Destination{}, false }

// CanSend evaluates send permission. Stub: implemented by WS-A.
func (p Policy) CanSend(a Alias) Decision { return Decision{} }

// CanWatch evaluates watch permission. Stub: implemented by WS-A.
func (p Policy) CanWatch(a Alias) Decision { return Decision{} }

// EvalUPN checks the authenticated profile against policy (D14). Stub.
func (p Policy) EvalUPN(me Profile) Decision { return Decision{} }

// Watched returns watch:true destinations sorted by alias. Stub.
func (p Policy) Watched() []Destination { return nil }
