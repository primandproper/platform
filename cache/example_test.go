package cache_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/primandproper/platform/cache"
	"github.com/primandproper/platform/cache/memory"
)

func ExampleCache_setAndGet() {
	ctx := context.Background()
	c, err := memory.NewInMemoryCache[string](nil, nil, nil)
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

func ExampleCache_notFound() {
	ctx := context.Background()
	c, cacheErr := memory.NewInMemoryCache[string](nil, nil, nil)
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
