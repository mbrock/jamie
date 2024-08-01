package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"encoding/json"

	"github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"
	pgx "github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/spf13/cobra"
)

//go:embed db_init.sql
var sqlFS embed.FS

type Bot struct {
	Discord   *discordgo.Session
	DBPool    *pgxpool.Pool
	SessionID int
}

func (b *Bot) handleEvent(s *discordgo.Session, m *discordgo.Event) {
	log.Info("message", "op", m.Operation, "type", m.Type)
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

	// Check if we should join a voice channel in this guild
	var channelID string
	err = b.DBPool.QueryRow(context.Background(),
		`SELECT bvj.channel_id
		 FROM bot_voice_joins bvj
		 JOIN discord_sessions ds ON bvj.session_id = ds.id
		 WHERE bvj.guild_id = $1 AND ds.bot_token = $2
		 ORDER BY bvj.joined_at DESC
		 LIMIT 1`,
		m.ID, os.Getenv("DISCORD_TOKEN")).Scan(&channelID)

	if err == nil && channelID != "" {
		// We have a record of joining a channel in this guild, so let's join it
		vc, err := s.ChannelVoiceJoin(m.ID, channelID, false, false)
		if err != nil {
			log.Error(
				"Failed to join voice channel",
				"guild",
				m.ID,
				"channel",
				channelID,
				"error",
				err,
			)
		} else {
			log.Info("Rejoined voice channel", "guild", m.ID, "channel", channelID)
			vc.AddHandler(b.handleVoiceSpeakingUpdate)
			go b.handleOpusPackets(vc)
		}
	} else if errors.Is(err, pgx.ErrNoRows) {
		log.Info("No bot voice joins found for guild", "guild", m.ID)
	} else {
		log.Error("Failed to query bot voice joins", "error", err)
	}
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
		b.SessionID,
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
		return
	}

	vc.AddHandler(b.handleVoiceSpeakingUpdate)

	go b.handleOpusPackets(vc)

	// Save or update the channel join information
	_, err = b.DBPool.Exec(
		context.Background(),
		`INSERT INTO bot_voice_joins (guild_id, channel_id, session_id) 
		 VALUES ($1, $2, $3)
		 ON CONFLICT (guild_id, session_id) 
		 DO UPDATE SET channel_id = $2, joined_at = CURRENT_TIMESTAMP`,
		m.GuildID,
		m.ChannelID,
		b.SessionID,
	)
	if err != nil {
		log.Error("Failed to upsert bot voice join", "error", err)
	}
}

func (b *Bot) handleVoiceSpeakingUpdate(
	vc *discordgo.VoiceConnection,
	m *discordgo.VoiceSpeakingUpdate,
) {
	_, err := b.DBPool.Exec(
		context.Background(),
		`INSERT INTO ssrc_mappings (guild_id, channel_id, user_id, ssrc, session_id) 
		VALUES ($1, $2, $3, $4, $5) 
		ON CONFLICT (guild_id, channel_id, ssrc) 
		DO UPDATE SET user_id = $3, session_id = $5`,
		vc.GuildID,
		vc.ChannelID,
		m.UserID,
		m.SSRC,
		b.SessionID,
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
				(guild_id, channel_id, ssrc, sequence, timestamp, opus_data, session_id) 
			VALUES 
				($1, $2, $3, $4, $5, $6, $7)`,
			vc.GuildID,
			vc.ChannelID,
			pkt.SSRC,
			pkt.Sequence,
			pkt.Timestamp,
			pkt.Opus,
			b.SessionID,
		)

		if err != nil {
			log.Error("Failed to insert opus packet", "error", err)
		}
	}
}

var rootCmd = &cobra.Command{
	Use:   "jamie",
	Short: "Jamie is a Discord bot for voice channel interactions",
	Long:  `Jamie is a Discord bot that can join voice channels and perform various operations.`,
}

var listenCmd = &cobra.Command{
	Use:   "listen",
	Short: "Start listening in Discord voice channels",
	Long:  `This command starts the Jamie bot and makes it listen in Discord voice channels.`,
	Run: func(cmd *cobra.Command, args []string) {
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
			log.Fatal("Error creating Discord session", "error", err)
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
			log.Fatal("Error opening Discord session", "error", err)
		}

		defer func() {
			err := discord.Close()
			if err != nil {
				log.Error("discord", "close", err)
			}
		}()

		log.Info("discord", "status", discord.State.User.Username)

		// Insert a record into the discord_sessions table
		var sessionID int
		err = dbpool.QueryRow(
			context.Background(),
			`INSERT INTO discord_sessions (bot_token, user_id) VALUES ($1, $2) RETURNING id`,
			os.Getenv("DISCORD_TOKEN"),
			discord.State.User.ID,
		).Scan(&sessionID)
		if err != nil {
			log.Error("Failed to insert discord session", "error", err)
		}
		bot.SessionID = sessionID

		// wait for CTRL-C
		log.Info("Jamie is now listening. Press CTRL-C to exit.")
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt)
		<-sig
	},
}

var listenPacketsCmd = &cobra.Command{
	Use:   "packets",
	Short: "Listen for new opus packets",
	Long:  `This command listens for new opus packets and prints information about each new packet.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Connect to PostgreSQL
		dbpool, err := pgxpool.Connect(
			context.Background(),
			os.Getenv("DATABASE_URL"),
		)
		if err != nil {
			log.Fatal("Unable to connect to database", "error", err)
		}
		defer dbpool.Close()

		conn, err := dbpool.Acquire(context.Background())
		if err != nil {
			log.Fatal("Error acquiring connection", "error", err)
		}
		defer conn.Release()

		_, err = conn.Exec(context.Background(), "LISTEN new_opus_packet")
		if err != nil {
			log.Fatal("Error listening to channel", "error", err)
		}

		log.Info("Listening for new opus packets. Press CTRL-C to exit.")

		for {
			notification, err := conn.Conn().
				WaitForNotification(context.Background())
			if err != nil {
				log.Error("Error waiting for notification", "error", err)
				continue
			}

			var packet map[string]interface{}
			err = json.Unmarshal([]byte(notification.Payload), &packet)
			if err != nil {
				log.Error("Error unmarshalling payload", "error", err)
				continue
			}

			log.Info("New opus packet",
				"guild_id", packet["guild_id"],
				"channel_id", packet["channel_id"],
				"ssrc", packet["ssrc"],
				"sequence", packet["sequence"],
				"timestamp", packet["timestamp"],
			)
		}
	},
}

func init() {
	rootCmd.AddCommand(listenCmd)
	rootCmd.AddCommand(listenPacketsCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal("Error executing root command", "error", err)
	}
}
