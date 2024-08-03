package main

import (
	"context"

	"github.com/trealla-prolog/go/trealla"
)

func init() {
	prolog, err := trealla.New()

	if err != nil {
		panic(err)
	}

	ctx := context.Background()

	err = prolog.ConsultText(ctx, "user", "foo(bar) :- true.")
	if err != nil {
		panic(err)
	}
}
