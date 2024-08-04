package main

import (
	"context"
	"encoding/base32"
	"fmt"

	"github.com/spf13/viper"
	"github.com/trealla-prolog/go/trealla"
	"node.town/db"
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

func RegisterExample() {
	ctx := context.Background()
	pl, err := trealla.New()
	if err != nil {
		panic(err)
	}

	// Let's add a base32 encoding predicate.
	// To keep it brief, this only handles one mode.
	// base32(+Input, -Output) is det.
	pl.Register(
		ctx,
		"base32",
		2,
		func(_ trealla.Prolog, _ trealla.Subquery, goal0 trealla.Term) trealla.Term {
			// goal is the goal called by Prolog, such as: base32("hello", X).
			// Guaranteed to match up with the registered arity and name.
			goal := goal0.(trealla.Compound)

			// Check the Input argument's type, must be string.
			input, ok := goal.Args[0].(string)
			if !ok {
				// throw(error(type_error(list, X), base32/2)).
				return trealla.Atom("throw").Of(trealla.Atom("error").Of(
					trealla.Atom("type_error").Of("list", goal.Args[0]),
					trealla.Atom("/").Of(trealla.Atom("base32"), 2),
				))
			}

			// Check Output type, must be string or var.
			switch goal.Args[1].(type) {
			case string: // ok
			case trealla.Variable: // ok
			default:
				// throw(error(type_error(list, X), base32/2)).
				// See: terms subpackage for convenience functions to create these errors.
				return trealla.Atom("throw").Of(trealla.Atom("error").Of(
					trealla.Atom("type_error").Of("list", goal.Args[0]),
					trealla.Atom("/").Of(trealla.Atom("base32"), 2),
				))
			}

			// Do the actual encoding work.
			output := base32.StdEncoding.EncodeToString([]byte(input))

			// Return a goal that Trealla will unify with its input:
			// base32(Input, "output_goes_here").
			return trealla.Atom("base32").Of(input, output)
		},
	)

	// Try it out.
	answer, err := pl.QueryOnce(ctx, `base32("hello", Encoded).`)
	if err != nil {
		panic(err)
	}
	fmt.Println(answer.Solution["Encoded"])
}

func RegisterDBQuery(ctx context.Context, queries *db.Queries) {
	pl, err := trealla.New()
	if err != nil {
		panic(err)
	}

	pl.Register(
		ctx,
		"db_query",
		2,
		func(_ trealla.Prolog, _ trealla.Subquery, goal0 trealla.Term) trealla.Term {
			goal := goal0.(trealla.Compound)

			queryType, ok := goal.Args[0].(string)
			if !ok {
				return trealla.Atom("throw").Of(trealla.Atom("error").Of(
					trealla.Atom("type_error").Of("atom", goal.Args[0]),
					trealla.Atom("/").Of(trealla.Atom("db_query"), 2),
				))
			}

			switch queryType {
			case "get_last_joined_channel":
				guildID, ok := goal.Args[1].(string)
				if !ok {
					return trealla.Atom("throw").Of(trealla.Atom("error").Of(
						trealla.Atom("type_error").Of("string", goal.Args[1]),
						trealla.Atom("/").Of(trealla.Atom("db_query"), 2),
					))
				}

				channel, err := queries.GetLastJoinedChannel(ctx, db.GetLastJoinedChannelParams{
					GuildID:  guildID,
					BotToken: viper.GetString("DISCORD_TOKEN"),
				})
				if err != nil {
					return trealla.Atom("throw").Of(trealla.Atom("error").Of(
						trealla.Atom("db_error").Of(err.Error()),
						trealla.Atom("/").Of(trealla.Atom("db_query"), 2),
					))
				}

				return trealla.Atom("db_query").Of(queryType, channel)

			default:
				return trealla.Atom("throw").Of(trealla.Atom("error").Of(
					trealla.Atom("domain_error").Of("query_type", queryType),
					trealla.Atom("/").Of(trealla.Atom("db_query"), 2),
				))
			}
		},
	)

	// Example usage
	answer, err := pl.QueryOnce(ctx, `db_query("get_last_joined_channel", "123456789").`)
	if err != nil {
		fmt.Println("Failed to execute Prolog query:", err)
	} else {
		fmt.Println("Last joined channel:", answer.Solution["_"])
	}
}
