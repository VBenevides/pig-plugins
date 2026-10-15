package hashline

// WithScope attaches a session identity without depending on the host SDK.
// An Engine retains only the active scope; switching away revokes all reads.
func WithScope(request Cancellation, scope string) Cancellation {
	return scopedCancellation{Cancellation: request, scope: scope}
}

type scopedCancellation struct {
	Cancellation
	scope string
}

func (request scopedCancellation) ReadScope() string { return request.scope }
