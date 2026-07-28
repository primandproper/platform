package cache_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/primandproper/platform-go/v8/cache"
	"github.com/primandproper/platform-go/v8/cache/memory"
)

func ExampleCache_setAndGet() {
	ctx := context.Background()
	c, err := memory.NewInMemoryCache[string](0, nil, nil, nil)
	if err != nil {
		panic(err)
	}

	value := "cached-value"
	if err = c.Set(ctx, "my-key", &value); err != nil {
		panic(err)
	}

	result, err := c.Get(ctx, "my-key")
	if err != nil {
		panic(err)
	}

	fmt.Println(*result)
	// Output: cached-value
}

func ExampleCache_batch() {
	ctx := context.Background()
	c, err := memory.NewInMemoryCache[string](0, nil, nil, nil)
	if err != nil {
		panic(err)
	}

	// Batched reads and writes are part of Cache itself — no assertion needed.
	one, two := "one", "two"
	if err = c.SetMany(ctx, map[string]*string{"k1": &one, "k2": &two}); err != nil {
		panic(err)
	}

	// Missing keys are simply absent from the result.
	results, err := c.GetMany(ctx, []string{"k1", "k2", "missing"})
	if err != nil {
		panic(err)
	}

	fmt.Println(len(results))
	fmt.Println(*results["k1"])
	// Output:
	// 2
	// one
}

// ExampleNewGobCodec shows the codec the serializing providers use by default.
// Consumers reach for Codec only to replace it — redis.WithCodec accepts any
// implementation whose Decode round-trips its own Encode. Note the migration
// caveat: values written under one codec are unreadable through another.
func ExampleNewGobCodec() {
	type session struct {
		UserID string
		Roles  []string
	}

	codec := cache.NewGobCodec[session]()

	encoded, err := codec.Encode(&session{UserID: "u-1", Roles: []string{"admin", "auditor"}})
	if err != nil {
		panic(err)
	}

	decoded, err := codec.Decode(encoded)
	if err != nil {
		panic(err)
	}

	fmt.Println(decoded.UserID, decoded.Roles)
	// Output: u-1 [admin auditor]
}

func ExampleCache_notFound() {
	ctx := context.Background()
	c, cacheErr := memory.NewInMemoryCache[string](0, nil, nil, nil)
	if cacheErr != nil {
		panic(cacheErr)
	}

	_, err := c.Get(ctx, "nonexistent")
	fmt.Println(err)
	fmt.Println(errors.Is(err, cache.ErrNotFound))
	// Output:
	// not found
	// true
}
