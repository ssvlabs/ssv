package obft

// OperatorInCluster reports whether `id` is in `operators`. Shared
// helper consumed by both base and twoab validation paths (the cluster-
// membership check is identical across protocols since both use the
// same []OperatorID shape for the cluster roster).
//
// Linear scan is intentional: typical cluster sizes are n ≤ 13 in SSV,
// so the constant overhead of a map allocation would dwarf the linear-
// scan cost. If clusters grow past ~50 operators in the future, this
// can be replaced with a map-backed lookup via a Cluster type that
// caches the membership set.
func OperatorInCluster(id OperatorID, operators []OperatorID) bool {
	for _, op := range operators {
		if op == id {
			return true
		}
	}
	return false
}
