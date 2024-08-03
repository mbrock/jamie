package bot

import (
	"context"
	"errors"
	"os"

	"github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"node.town/db"
)

type Bot struct {
	Discord   *discordgo.Session
	Queries   *db.Queries
	SessionID int32
}

func (b *Bot) HandleEvent(_ *discordgo.Session, m *discordgo.Event) {
	log.Info("message", "op", m.Operation, "type", m.Type)
}

func (b *Bot) HandleGuildCreate(
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
	channelID, err := b.Queries.GetLastJoinedChannel(
		context.Background(),
		db.GetLastJoinedChannelParams{
			GuildID:  m.ID,
			BotToken: os.Getenv("DISCORD_TOKEN"),
		},
	)

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
			vc.AddHandler(b.HandleVoiceSpeakingUpdate)
			go b.HandleOpusPackets(vc)
		}
	} else if errors.Is(err, pgx.ErrNoRows) {
		log.Info("No bot voice joins found for guild", "guild", m.ID)
	} else {
		log.Error("Failed to query bot voice joins", "error", err)
	}
}

func (b *Bot) HandleVoiceStateUpdate(
	_ *discordgo.Session,
	m *discordgo.VoiceStateUpdate,
) {
	log.Info("voice", "user", m.UserID, "channel", m.ChannelID)

	err := b.Queries.InsertVoiceStateEvent(
		context.Background(),
		db.InsertVoiceStateEventParams{
			GuildID:    m.GuildID,
			ChannelID:  m.ChannelID,
			UserID:     m.UserID,
			SessionID:  b.SessionID,
			Deaf:       m.Deaf,
			Mute:       m.Mute,
			SelfDeaf:   m.SelfDeaf,
			SelfMute:   m.SelfMute,
			SelfStream: m.SelfStream,
			SelfVideo:  m.SelfVideo,
			Suppress:   m.Suppress,
		},
	)

	if err != nil {
		log.Error("Failed to insert voice state event", "error", err)
	}
}

func (b *Bot) HandleVoiceServerUpdate(
	_ *discordgo.Session,
	m *discordgo.VoiceServerUpdate,
) {
	log.Info("voice", "server", m.Endpoint, "token", m.Token)
}

func (b *Bot) HandleInteractionCreate(
	s *discordgo.Session,
	m *discordgo.InteractionCreate,
) {
	err := s.InteractionRespond(
		m.Interaction,
		&discordgo.InteractionResponse{
			Type: discordgo.InteractionResponsePong,
		},
	)

	if err != nil {
		log.Error("couldn't send response", "err", err)
		return
	}

	vc, err := s.ChannelVoiceJoin(m.GuildID, m.ChannelID, false, false)
	if err != nil {
		log.Error("voice", "error", err)
		return
	}

	vc.AddHandler(b.HandleVoiceSpeakingUpdate)

	go b.HandleOpusPackets(vc)

	// Save or update the channel join information
	err = b.Queries.UpsertBotVoiceJoin(
		context.Background(),
		db.UpsertBotVoiceJoinParams{
			GuildID:   m.GuildID,
			ChannelID: m.ChannelID,
			SessionID: pgtype.Int4{
				Int32: b.SessionID,
				Valid: true,
			},
		},
	)
	if err != nil {
		log.Error("Failed to upsert bot voice join", "error", err)
	}
}

func (b *Bot) HandleVoiceSpeakingUpdate(
	vc *discordgo.VoiceConnection,
	m *discordgo.VoiceSpeakingUpdate,
) {
	err := b.Queries.UpsertSSRCMapping(
		context.Background(),
		db.UpsertSSRCMappingParams{
			GuildID:   vc.GuildID,
			ChannelID: vc.ChannelID,
			UserID:    m.UserID,
			Ssrc:      int64(m.SSRC),
			SessionID: b.SessionID,
		},
	)
	if err != nil {
		log.Error("Failed to insert/update SSRC mapping", "error", err)
	}
}

func (b *Bot) HandleOpusPackets(vc *discordgo.VoiceConnection) {
	for pkt := range vc.OpusRecv {
		err := b.Queries.InsertOpusPacket(
			context.Background(),
			db.InsertOpusPacketParams{
				GuildID:   vc.GuildID,
				ChannelID: vc.ChannelID,
				Ssrc:      int64(pkt.SSRC),
				Sequence:  int32(pkt.Sequence),
				Timestamp: int64(pkt.Timestamp),
				OpusData:  pkt.Opus,
				SessionID: b.SessionID,
			},
		)

		if err != nil {
			log.Error("Failed to insert opus packet", "error", err)
		}
	}
}
