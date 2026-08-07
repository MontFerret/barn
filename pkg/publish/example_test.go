package publish_test

import (
	"context"

	"github.com/MontFerret/barn/pkg/publish"
)

func ExamplePrepare() {
	result, err := publish.Prepare(context.Background(), publish.Request{
		Directory: ".",
		Tag:       "v1.2.3",
	})
	if err != nil {
		return
	}

	_ = result.Files
}
