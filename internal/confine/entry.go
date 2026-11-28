package confine

// Dispatch handles private launch and probe entry points before CLI wiring.
func Dispatch(args []string) (bool, error) { return dispatch(args) }
