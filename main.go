package main

import (
	"context"
	"embed"
	"fmt"
	"os"
	"os/signal"

	"github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"
	"github.com/jackc/pgx/v4/pgxpool"
)

//go:embed db_init.sql
var sqlFS embed.FS

type Bot struct {
	Discord *discordgo.Session
	DBPool  *pgxpool.Pool
}

func (b *Bot) handleEvent(s *discordgo.Session, m *discordgo.Event) {
	log.Info(
		"message",
		"op",
		m.Operation,
		"type",
		m.Type,
		"data",
		m.Struct,
	)
}

func (b *Bot) handleGuildCreate(
	s *discordgo.Session,
	m *discordgo.GuildCreate,
) {
	log.Info("guild", "id", m.ID, "name", m.Name)
	for _, channel := range m.Guild.Channels {
		log.Info("channel", "id", channel.ID, "name", channel.Name)
	}
	for _, voice := range m.Guild.VoiceStates {
		log.Info("voice", "id", voice.UserID, "channel", voice.ChannelID)
	}

	cmd, err := s.ApplicationCommandCreate(
		b.Discord.State.User.ID,
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
}

func (b *Bot) handleVoiceStateUpdate(
	s *discordgo.Session,
	m *discordgo.VoiceStateUpdate,
) {
	log.Info("voice", "user", m.UserID, "channel", m.ChannelID)

	_, err := b.DBPool.Exec(
		context.Background(),
		`INSERT INTO voice_state_events (
			guild_id, channel_id, user_id, session_id, 
			deaf, mute, self_deaf, self_mute, 
			self_stream, self_video, suppress, 
			request_to_speak_timestamp
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		m.GuildID,
		m.ChannelID,
		m.UserID,
		m.SessionID,
		m.Deaf,
		m.Mute,
		m.SelfDeaf,
		m.SelfMute,
		m.SelfStream,
		m.SelfVideo,
		m.Suppress,
		m.RequestToSpeakTimestamp,
	)

	if err != nil {
		log.Error("Failed to insert voice state event", "error", err)
	}
}

func (b *Bot) handleVoiceServerUpdate(
	s *discordgo.Session,
	m *discordgo.VoiceServerUpdate,
) {
	log.Info("voice", "server", m.Endpoint, "token", m.Token)
}

func (b *Bot) handleInteractionCreate(
	s *discordgo.Session,
	m *discordgo.InteractionCreate,
) {
	s.InteractionRespond(
		m.Interaction,
		&discordgo.InteractionResponse{
			Type: discordgo.InteractionResponsePong,
		},
	)

	vc, err := s.ChannelVoiceJoin(m.GuildID, m.ChannelID, false, false)
	if err != nil {
		log.Error("voice", "error", err)
	}

	vc.AddHandler(b.handleVoiceSpeakingUpdate)

	go b.handleOpusPackets(vc)
}

func (b *Bot) handleVoiceSpeakingUpdate(
	vc *discordgo.VoiceConnection,
	m *discordgo.VoiceSpeakingUpdate,
) {
	_, err := b.DBPool.Exec(
		context.Background(),
		`INSERT INTO ssrc_mappings (guild_id, channel_id, user_id, ssrc) 
		VALUES ($1, $2, $3, $4) 
		ON CONFLICT (guild_id, channel_id, ssrc) 
		DO UPDATE SET user_id = $3`,
		vc.GuildID,
		vc.ChannelID,
		m.UserID,
		m.SSRC,
	)
	if err != nil {
		log.Error("Failed to insert/update SSRC mapping", "error", err)
	}
}

func (b *Bot) handleOpusPackets(vc *discordgo.VoiceConnection) {
	for pkt := range vc.OpusRecv {
		_, err := b.DBPool.Exec(
			context.Background(),
			`INSERT INTO opus_packets 
				(guild_id, channel_id, ssrc, sequence, timestamp, opus_data) 
			VALUES 
				($1, $2, $3, $4, $5, $6)`,
			vc.GuildID,
			vc.ChannelID,
			pkt.SSRC,
			pkt.Sequence,
			pkt.Timestamp,
			pkt.Opus,
		)

		if err != nil {
			log.Error("Failed to insert opus packet", "error", err)
		}
	}
}

func main() {
	// Connect to PostgreSQL
	dbpool, err := pgxpool.Connect(
		context.Background(),
		os.Getenv("DATABASE_URL"),
	)
	if err != nil {
		log.Fatal("Unable to connect to database", "error", err)
	}
	defer dbpool.Close()

	// Read and execute the embedded SQL file to create tables
	sqlFile, err := sqlFS.ReadFile("db_init.sql")
	if err != nil {
		log.Fatal("Failed to read embedded db_init.sql", "error", err)
	}

	_, err = dbpool.Exec(context.Background(), string(sqlFile))
	if err != nil {
		log.Fatal("Failed to execute embedded db_init.sql", "error", err)
	}

	discord, err := discordgo.New(
		fmt.Sprintf("Bot %s", os.Getenv("DISCORD_TOKEN")),
	)
	if err != nil {
		panic(err)
	}

	discord.LogLevel = discordgo.LogInformational

	bot := &Bot{
		Discord: discord,
		DBPool:  dbpool,
	}

	discord.AddHandler(bot.handleEvent)
	discord.AddHandler(bot.handleGuildCreate)
	discord.AddHandler(bot.handleVoiceStateUpdate)
	discord.AddHandler(bot.handleVoiceServerUpdate)
	discord.AddHandler(bot.handleInteractionCreate)

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

	log.Info("discord", "status", discord.State.User.Username)

	// wait for CTRL-C
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig
}
