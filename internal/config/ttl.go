package config

// ResolveTTL returns the effective TTL in seconds by walking three layers in
// priority order, then clamping the result to the hard ceiling.
//
// Priority:
//  1. requested  — per-request override supplied by the caller (0 means not set)
//  2. bucketDefault — the bucket's own presign_ttl_seconds (0 means not set)
//  3. globalDefault — OPENSTORE_PRESIGN_TTL_DEFAULT from the environment
//
// The result is always clamped to globalMax regardless of which layer provided
// the value. globalMax is OPENSTORE_PRESIGN_TTL_MAX from the environment.
func ResolveTTL(requested, bucketDefault, globalDefault, globalMax int) int {
	ttl := globalDefault

	if bucketDefault > 0 {
		ttl = bucketDefault
	}

	if requested > 0 {
		ttl = requested
	}

	if ttl > globalMax {
		ttl = globalMax
	}

	return ttl
}