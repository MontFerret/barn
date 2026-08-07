package registry_test

import (
	"context"

	"github.com/MontFerret/barn/pkg/registry"
)

func ExampleClient_Search() {
	client, err := registry.NewClient()
	if err != nil {
		return
	}

	modules, err := client.Search(context.Background(), registry.SearchOptions{
		Query:    "openai",
		Category: "ai",
	})
	if err != nil {
		return
	}

	_ = modules
}
