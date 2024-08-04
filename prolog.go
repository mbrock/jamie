package main

import (
	"context"
	"encoding/base32"
	"fmt"

	"github.com/spf13/viper"
	"github.com/trealla-prolog/go/trealla"
	"github.com/trealla-prolog/go/trealla/terms"
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
				return terms.TypeError(
					trealla.Atom("string"),
					goal.Args[0],
					terms.PI(goal),
				)
			}

			// Check Output type, must be string or var.
			switch goal.Args[1].(type) {
			case string: // ok
			case trealla.Variable: // ok
			default:
				// throw(error(type_error(list, X), base32/2)).
				// See: terms subpackage for convenience functions to create these errors.
				return terms.TypeError(
					trealla.Atom("string"),
					goal.Args[1],
					terms.PI(goal),
				)
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

func RegisterDBQuery(
	pl trealla.Prolog,
	ctx context.Context,
	queries *db.Queries,
) {
	err := pl.Register(
		ctx,
		"last_joined_channel",
		2,
		func(_ trealla.Prolog, _ trealla.Subquery, goal0 trealla.Term) trealla.Term {
			goal := goal0.(trealla.Compound)

			guildID, ok := goal.Args[0].(string)
			if !ok {
				return terms.TypeError(
					trealla.Atom("string"),
					goal.Args[0],
					terms.PI(goal),
				)
			}

			channel, err := queries.GetLastJoinedChannel(
				ctx,
				db.GetLastJoinedChannelParams{
					GuildID:  guildID,
					BotToken: viper.GetString("DISCORD_TOKEN"),
				},
			)
			if err != nil {
				return terms.DomainError(
					trealla.Atom("db_error"),
					trealla.Atom(err.Error()),
					terms.PI(goal),
				)
			}

			return trealla.Atom("last_joined_channel").Of(guildID, channel)
		},
	)
	if err != nil {
		panic(err)
	}

	err = pl.Register(
		ctx,
		"known_guilds",
		1,
		func(_ trealla.Prolog, _ trealla.Subquery, goal0 trealla.Term) trealla.Term {
			goal := goal0.(trealla.Compound)
			guildIDs, err := queries.GetKnownGuildIDs(ctx)
			if err != nil {
				return terms.DomainError(
					trealla.Atom("db_error"),
					trealla.Atom(err.Error()),
					terms.PI(goal),
				)
			}

			guildAtoms := make([]trealla.Term, len(guildIDs))
			for i, id := range guildIDs {
				guildAtoms[i] = trealla.Atom(id)
			}

			return trealla.Atom("known_guilds").Of(guildAtoms)
		},
	)
	if err != nil {
		panic(err)
	}
}
