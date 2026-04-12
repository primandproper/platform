package routing

// ContextKey represents strings to be used in Context objects. From the docs:
//
//	"The provided key must be comparable and should not be of type string or
//	 any other built-in type to avoid collisions between packages using context."
type ContextKey string
