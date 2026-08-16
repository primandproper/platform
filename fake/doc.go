/*
Package fake provides generic test data generation utilities for creating fake instances of any type.

Every builder here generates from the same options: numbers start at 1 rather
than 0, and recursion is bounded to [DefaultRecursionDepth]. The bound is a
property of the package rather than of the entry point, so which of BuildFake,
MustBuildFake and BuildFakeForTest a caller reaches for does not change what a
fake costs to build. The ToDepth builders take another bound for callers who
want the nested graph populated.
*/
package fake
