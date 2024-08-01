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

	// Find the most recently active voice channel
	var latestChannel *discordgo.Channel
	var latestTimestamp int64

	for _, channel := range m.Guild.Channels {
		if channel.Type == discordgo.ChannelTypeGuildVoice {
			for _, voice := range m.Guild.VoiceStates {
				if voice.ChannelID == channel.ID {
					// Use the channel with the most recent activity
					if voice.RequestToSpeakTimestamp != nil && voice.RequestToSpeakTimestamp.UnixNano() > latestTimestamp {
						latestChannel = channel
						latestTimestamp = voice.RequestToSpeakTimestamp.UnixNano()
					}
				}
			}
		}
	}

	// Join the latest active channel if found
	if latestChannel != nil {
		vc, err := s.ChannelVoiceJoin(m.ID, latestChannel.ID, false, false)
		if err != nil {
			log.Error("Failed to join voice channel", "guild", m.ID, "channel", latestChannel.ID, "error", err)
		} else {
			log.Info("Joined voice channel", "guild", m.ID, "channel", latestChannel.ID)
			vc.AddHandler(b.handleVoiceSpeakingUpdate)
			go b.handleOpusPackets(vc)

			// Save or update the channel join information
			_, err = b.DBPool.Exec(
				context.Background(),
				`INSERT INTO bot_voice_joins (guild_id, channel_id, session_id) 
				 VALUES ($1, $2, $3)
				 ON CONFLICT (guild_id, session_id) 
				 DO UPDATE SET channel_id = $2, joined_at = CURRENT_TIMESTAMP`,
				m.ID,
				latestChannel.ID,
				b.SessionID,
			)
			if err != nil {
				log.Error("Failed to upsert bot voice join", "error", err)
			}
		}
	} else {
		log.Info("No active voice channels found in guild", "guild", m.ID)
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

		// Rejoin previously joined channels
		bot.rejoinChannels()

		// wait for CTRL-C
		log.Info("Jamie is now listening. Press CTRL-C to exit.")
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt)
		<-sig
	},
}

func (b *Bot) rejoinChannels() {
	rows, err := b.DBPool.Query(context.Background(),
		`SELECT guild_id, channel_id
		 FROM bot_voice_joins
		 WHERE session_id = $1`,
		b.SessionID)
	
	if err != nil {
		log.Error("Failed to query bot voice joins", "error", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var guildID, channelID string
		err := rows.Scan(&guildID, &channelID)
		if err != nil {
			log.Error("Failed to scan bot voice join row", "error", err)
			continue
		}

		vc, err := b.Discord.ChannelVoiceJoin(guildID, channelID, false, false)
		if err != nil {
			log.Error("Failed to rejoin voice channel", "guild", guildID, "channel", channelID, "error", err)
			continue
		}

		vc.AddHandler(b.handleVoiceSpeakingUpdate)
		go b.handleOpusPackets(vc)

		log.Info("Rejoined voice channel", "guild", guildID, "channel", channelID)
	}

	if rows.Err() != nil {
		log.Error("Error iterating over bot voice joins", "error", rows.Err())
	}
}

func init() {
	rootCmd.AddCommand(listenCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal("Error executing root command", "error", err)
	}
}
