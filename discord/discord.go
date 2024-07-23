package discord

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"
	"github.com/google/uuid"

	"jamie/db"
	"jamie/deepgram"
)

type ChannelIdentifier interface {
	GetGuildID() string
	GetChannelID() string
}

type SimpleChannelIdentifier struct {
	GuildID   string
	ChannelID string
}

func (sci SimpleChannelIdentifier) GetGuildID() string {
	return sci.GuildID
}

func (sci SimpleChannelIdentifier) GetChannelID() string {
	return sci.ChannelID
}
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"
	"github.com/google/uuid"

	"jamie/db"
	"jamie/deepgram"
)

type DiscordBot struct {
	logger             *log.Logger
	transcriptChannels sync.Map
	discordToken       string
	session            *discordgo.Session
	deepgramToken      string
}

type VoiceStream struct {
	UserID             string
	StreamID           string
	FirstOpusTimestamp uint32
	FirstReceiveTime   int64
	FirstSequence      uint16
}

type VoiceState struct {
	ssrcToUser   sync.Map
	ssrcToStream sync.Map
	guildID      string
	channelID    string
}

func (bot *DiscordBot) SetLogger(l *log.Logger) {
	bot.logger = l
	deepgram.SetLogger(l)
}

func NewDiscordBot(token string, deepgramToken string) (*DiscordBot, error) {
	bot := &DiscordBot{
		discordToken:  token,
		deepgramToken: deepgramToken,
		logger:        log.New(os.Stderr),
	}

	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("error creating Discord session: %w", err)
	}

	dg.AddHandler(func(s *discordgo.Session, event *discordgo.GuildCreate) {
		bot.guildCreate(s, event)
	})

	err = dg.Open()
	if err != nil {
		return nil, fmt.Errorf("error opening connection: %w", err)
	}

	bot.session = dg
	bot.logger.Info("Bot is now running.")
	return bot, nil
}

func (bot *DiscordBot) Close() error {
	return bot.session.Close()
}

func (bot *DiscordBot) guildCreate(s *discordgo.Session, event *discordgo.GuildCreate) {
	bot.logger.Info("Joined new guild", "guild", event.Guild.Name)
	err := bot.joinAllVoiceChannels(s, SimpleChannelIdentifier{GuildID: event.Guild.ID, ChannelID: ""})
	if err != nil {
		bot.logger.Error("Error joining voice channels", "error", err.Error())
	}
}

func (bot *DiscordBot) joinAllVoiceChannels(s *discordgo.Session, guildID ChannelIdentifier) error {
	channels, err := s.GuildChannels(guildID.GetGuildID())
	if err != nil {
		return fmt.Errorf("error getting guild channels: %w", err)
	}

	for _, channel := range channels {
		if channel.Type == discordgo.ChannelTypeGuildVoice {
			vc, err := s.ChannelVoiceJoin(guildID.GetGuildID(), channel.ID, false, false)
			if err != nil {
				bot.logger.Error("Failed to join voice channel", "channel", channel.Name, "error", err.Error())
			} else {
				bot.logger.Info("Joined voice channel", "channel", channel.Name)
				channelID := SimpleChannelIdentifier{GuildID: guildID.GetGuildID(), ChannelID: channel.ID}
				go func() {
					bot.startDeepgramStream(vc, channelID)
				}()
			}
		}
	}

	return nil
}

func (bot *DiscordBot) voiceStateUpdate(state *VoiceState, _ *discordgo.VoiceConnection, v *discordgo.VoiceSpeakingUpdate) {
	bot.logger.Info("Voice state update", "userID", v.UserID, "speaking", v.Speaking, "SSRC", v.SSRC)
	state.ssrcToUser.Store(v.SSRC, v.UserID)
}

func (bot *DiscordBot) startDeepgramStream(v *discordgo.VoiceConnection, channelID ChannelIdentifier) {
	bot.logger.Info("Starting Deepgram stream", "guild", channelID.GetGuildID(), "channel", channelID.GetChannelID())

	state := &VoiceState{
		guildID:   channelID.GetGuildID(),
		channelID: channelID.GetChannelID(),
	}

	v.AddHandler(func(vc *discordgo.VoiceConnection, vs *discordgo.VoiceSpeakingUpdate) {
		bot.voiceStateUpdate(state, vc, vs)
	})

	dgClient, err := deepgram.NewDeepgramClient(bot.deepgramToken, guildID, channelID, bot.handleTranscript)
	if err != nil {
		bot.logger.Error("Error creating Deepgram client", "error", err.Error())
		return
	}

	bConnected := dgClient.Connect()
	if !bConnected {
		bot.logger.Error("Failed to connect to Deepgram")
		return
	}

	for {
		opus, ok := <-v.OpusRecv
		if !ok {
			bot.logger.Info("Voice channel closed")
			break
		}
		err := dgClient.WriteBinary(opus.Opus)
		if err != nil {
			bot.logger.Error("Failed to send audio to Deepgram", "error", err.Error())
		}

		// Get or create the stream for this SSRC
		streamInterface, exists := state.ssrcToStream.Load(opus.SSRC)
		var stream VoiceStream
		if !exists {
			streamID := uuid.New().String()
			userID, ok := state.ssrcToUser.Load(opus.SSRC)
			if !ok {
				bot.logger.Warn("User ID not found for SSRC", "SSRC", opus.SSRC)
				userID = "unknown"
			}
			stream = VoiceStream{
				UserID:             userID.(string),
				StreamID:           streamID,
				FirstOpusTimestamp: opus.Timestamp,
				FirstReceiveTime:   time.Now().UnixNano(),
				FirstSequence:      opus.Sequence,
			}
			state.ssrcToStream.Store(opus.SSRC, stream)
			err := db.CreateVoiceStream(state.guildID, state.channelID, streamID, userID.(string), opus.SSRC, opus.Timestamp, stream.FirstReceiveTime, stream.FirstSequence)
			if err != nil {
				bot.logger.Error("Failed to create voice stream", "error", err.Error())
				continue
			}
			bot.logger.Info("Created new voice stream", "streamID", streamID, "userID", userID, "SSRC", opus.SSRC)
		} else {
			stream = streamInterface.(VoiceStream)
		}

		// Calculate relative timestamps and sequence
		relativeOpusTimestamp := opus.Timestamp - stream.FirstOpusTimestamp
		relativeSequence := opus.Sequence - stream.FirstSequence
		receiveTime := time.Now().UnixNano()

		// Save the Discord voice packet to the database
		err = db.SaveDiscordVoicePacket(stream.StreamID, opus.Opus, relativeSequence, relativeOpusTimestamp, receiveTime)
		if err != nil {
			bot.logger.Error("Failed to save Discord voice packet to database", "error", err.Error())
		}

		// Print timestamps in seconds and user ID
		timestampSeconds := float64(relativeOpusTimestamp) / 48000.0
		bot.logger.Info("opus", "seq", opus.Sequence, "t", timestampSeconds, "userID", stream.UserID)
	}

	dgClient.Stop()
}

func (bot *DiscordBot) handleTranscript(channelID ChannelIdentifier, transcript string) {
	// Send the transcript to Discord
	_, err := bot.session.ChannelMessageSend(channelID.GetChannelID(), transcript)
	if err != nil {
		bot.logger.Error("Failed to send message to Discord", "error", err.Error())
	}

	// Send the transcript to the channel
	key := fmt.Sprintf("%s:%s", channelID.GetGuildID(), channelID.GetChannelID())
	if ch, ok := bot.transcriptChannels.Load(key); ok {
		ch.(chan string) <- transcript
	}
}

func (bot *DiscordBot) GetTranscriptChannel(channelID ChannelIdentifier) chan string {
	key := fmt.Sprintf("%s:%s", channelID.GetGuildID(), channelID.GetChannelID())
	ch, _ := bot.transcriptChannels.LoadOrStore(key, make(chan string))
	return ch.(chan string)
}

func (state *VoiceState) GetUserIDFromSSRC(ssrc uint32) (string, bool) {
	userID, ok := state.ssrcToUser.Load(ssrc)
	if !ok {
		return "", false
	}
	return userID.(string), true
}

func (state *VoiceState) GetStreamIDFromSSRC(ssrc uint32) (string, bool) {
	stream, ok := state.ssrcToStream.Load(ssrc)
	if !ok {
		return "", false
	}
	return stream.(VoiceStream).StreamID, true
}
