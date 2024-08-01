package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"
	"github.com/jackc/pgx/v4/pgxpool"
)

func main() {
	// Connect to PostgreSQL
	dbpool, err := pgxpool.Connect(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal("Unable to connect to database", "error", err)
	}
	defer dbpool.Close()

	// Read and execute the SQL file to create tables
	sqlFile, err := os.ReadFile("db_init.sql")
	if err != nil {
		log.Fatal("Failed to read db_init.sql", "error", err)
	}

	_, err = dbpool.Exec(context.Background(), string(sqlFile))
	if err != nil {
		log.Fatal("Failed to execute db_init.sql", "error", err)
	}

	discord, err := discordgo.New(
		fmt.Sprintf("Bot %s", os.Getenv("DISCORD_TOKEN")),
	)
	if err != nil {
		panic(err)
	}

	discord.LogLevel = discordgo.LogInformational

	err = discord.Open()
	if err != nil {
		panic(err)
	}

	defer func() {
		err := discord.Close()
		if err != nil {
			log.Error("discord", "close", err)
		}
	}()

	discord.AddHandler(func(s *discordgo.Session, m *discordgo.Event) {
		log.Info(
			"message",
			"op",
			m.Operation,
			"type",
			m.Type,
			"data",
			m.Struct,
		)
	})

	discord.AddHandler(func(s *discordgo.Session, m *discordgo.GuildCreate) {
		log.Info("guild", "id", m.ID, "name", m.Name)
		for _, channel := range m.Guild.Channels {
			log.Info(
				"channel",
				"id",
				channel.ID,
				"name",
				channel.Name,
				"type",
				channel.Type,
			)
		}
		for _, voice := range m.Guild.VoiceStates {
			log.Info("voice", "id", voice.UserID, "channel", voice.ChannelID)
		}

		cmd, err := s.ApplicationCommandCreate(
			discord.State.User.ID,
			m.ID,
			&discordgo.ApplicationCommand{
				Name:        "jamie",
				Description: "Summon Jamie to this channel",
			},
		)
		if err != nil {
			log.Error("command", "error", err)
		}
		log.Info("app command", "id", cmd.ID)
	})

	discord.AddHandler(
		func(s *discordgo.Session, m *discordgo.VoiceStateUpdate) {
			log.Info("voice", "user", m.UserID, "channel", m.ChannelID)
		},
	)

	discord.AddHandler(
		func(s *discordgo.Session, m *discordgo.VoiceServerUpdate) {
			log.Info("voice", "server", m.Endpoint, "token", m.Token)
		},
	)

	discord.AddHandler(
		func(s *discordgo.Session, m *discordgo.InteractionCreate) {
			log.Info(
				"interaction",
				"type",
				m.ApplicationCommandData().CommandType,
				"name",
				m.ApplicationCommandData().Name,
				"channel",
				m.ChannelID,
			)
			s.InteractionRespond(
				m.Interaction,
				&discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: "Hello, world!",
					},
				},
			)

			vc, err := s.ChannelVoiceJoin(
				m.GuildID,
				m.ChannelID,
				false,
				false,
			)
			if err != nil {
				log.Error("voice", "error", err)
			}

			vc.AddHandler(
				func(_ *discordgo.VoiceConnection, m *discordgo.VoiceSpeakingUpdate) {
					log.Info(
						"speaking",
						"user",
						m.UserID,
						"ssrc",
						m.SSRC,
						"speaking",
						m.Speaking,
					)

					_, err := dbpool.Exec(context.Background(),
						"INSERT INTO ssrc_mappings (guild_id, channel_id, user_id, ssrc) VALUES ($1, $2, $3, $4) ON CONFLICT (guild_id, channel_id, ssrc) DO UPDATE SET user_id = $3",
						vc.GuildID, vc.ChannelID, m.UserID, m.SSRC)
					if err != nil {
						log.Error("Failed to insert/update SSRC mapping", "error", err)
					}
				},
			)

			for pkt := range vc.OpusRecv {
				log.Info(
					"opus",
					"token",
					discord.Identify.Token,
					"session",
					vc.SessionID,
					"g",
					vc.GuildID,
					"c",
					vc.ChannelID,
					"ssrc",
					pkt.SSRC,
					"seq",
					pkt.Sequence,
					"ts",
					pkt.Timestamp,
					"n",
					len(pkt.Opus),
				)

				// Insert opus packet into PostgreSQL
				_, err := dbpool.Exec(context.Background(),
					"INSERT INTO opus_packets (guild_id, channel_id, ssrc, sequence, timestamp, opus_data) VALUES ($1, $2, $3, $4, $5, $6)",
					vc.GuildID, vc.ChannelID, pkt.SSRC, pkt.Sequence, pkt.Timestamp, pkt.Opus)

				if err != nil {
					log.Error("Failed to insert opus packet", "error", err)
				}
			}

		},
	)

	log.Info("discord", "status", discord.State.User.Username)

	// wait for CTRL-C
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig
}
