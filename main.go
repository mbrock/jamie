package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func main() {
	// Connect to NATS
	nc, err := nats.Connect(nats.DefaultURL)
	if err != nil {
		log.Fatal("Failed to connect to NATS", "error", err)
	}
	defer nc.Close()

	// Create JetStream context
	js, err := jetstream.New(nc)
	if err != nil {
		log.Fatal("Failed to create JetStream context", "error", err)
	}

	// Create a stream for opus packets
	_, err = js.CreateStream(
		context.Background(),
		jetstream.StreamConfig{
			Name:     "DISCORD",
			Subjects: []string{"discord.>"},
		},
	)
	if err != nil {
		log.Fatal("Failed to create stream", "error", err)
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

					ack, err := js.Publish(
						context.Background(),
						fmt.Sprintf(
							"discord.ssrc.%s.%s",
							vc.GuildID,
							vc.ChannelID,
						),
						[]byte(fmt.Sprintf("%s:%d", m.UserID, m.SSRC)),
					)
					if err != nil {
						log.Error("Failed to publish ssrc", "error", err)
					}
					log.Info("ack", "id", ack.Sequence)
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

				// Publish opus packet to NATS JetStream
				subject := fmt.Sprintf(
					"discord.opus.%s.%s.%d",
					vc.GuildID,
					vc.ChannelID,
					pkt.SSRC,
				)

				_, err := js.Publish(
					context.Background(),
					subject,
					pkt.Opus,
				)

				if err != nil {
					log.Error("Failed to publish opus packet", "error", err)
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
