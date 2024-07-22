package discord

import (
	"fmt"
	"sync"

	"github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"
	"github.com/google/uuid"

	"jamie/db"
	"jamie/deepgram"
)

var (
	logger             *log.Logger
	transcriptChannels sync.Map
	discordToken       string
)

type VoiceStream struct {
	UserID   string
	StreamID string
}

type VoiceState struct {
	ssrcToStream sync.Map
	guildID      string
	channelID    string
}

func SetLogger(l *log.Logger) {
	logger = l
	deepgram.SetLogger(l)
}

func StartBot(token string, deepgramToken string) (*discordgo.Session, error) {
	discordToken = token
	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("error creating Discord session: %w", err)
	}

	dg.AddHandler(func(s *discordgo.Session, event *discordgo.GuildCreate) {
		guildCreate(s, event, deepgramToken)
	})

	err = dg.Open()
	if err != nil {
		return nil, fmt.Errorf("error opening connection: %w", err)
	}

	logger.Info("Bot is now running.")
	return dg, nil
}

func guildCreate(s *discordgo.Session, event *discordgo.GuildCreate, deepgramToken string) {
	logger.Info("Joined new guild", "guild", event.Guild.Name)
	err := joinAllVoiceChannels(s, event.Guild.ID, deepgramToken)
	if err != nil {
		logger.Error("Error joining voice channels", "error", err.Error())
	}
}

func joinAllVoiceChannels(s *discordgo.Session, guildID, deepgramToken string) error {
	channels, err := s.GuildChannels(guildID)
	if err != nil {
		return fmt.Errorf("error getting guild channels: %w", err)
	}

	for _, channel := range channels {
		if channel.Type == discordgo.ChannelTypeGuildVoice {
			vc, err := s.ChannelVoiceJoin(guildID, channel.ID, false, false)
			if err != nil {
				logger.Error("Failed to join voice channel", "channel", channel.Name, "error", err.Error())
			} else {
				logger.Info("Joined voice channel", "channel", channel.Name)
				channelID := channel.ID
				go func() {
					startDeepgramStream(vc, guildID, channelID, deepgramToken)
				}()
			}
		}
	}

	return nil
}

func voiceStateUpdate(state *VoiceState, _ *discordgo.VoiceConnection, v *discordgo.VoiceSpeakingUpdate) {
	logger.Info("Voice state update", "userID", v.UserID, "speaking", v.Speaking, "SSRC", v.SSRC)

	_, exists := state.ssrcToStream.Load(v.SSRC)
	if !exists {
		streamID := uuid.New().String()
		err := db.CreateVoiceStream(state.guildID, state.channelID, streamID, v.UserID, uint32(v.SSRC))
		if err != nil {
			logger.Error("Failed to create voice stream", "error", err.Error())
		} else {
			logger.Info("Created new voice stream", "streamID", streamID, "userID", v.UserID, "SSRC", v.SSRC)
			state.ssrcToStream.Store(uint32(v.SSRC), VoiceStream{
				UserID:   v.UserID,
				StreamID: streamID,
			})
		}
	}
}

func startDeepgramStream(v *discordgo.VoiceConnection, guildID, channelID, deepgramToken string) {
	logger.Info("Starting Deepgram stream", "guild", guildID, "channel", channelID)

	state := &VoiceState{
		guildID:   guildID,
		channelID: channelID,
	}
	v.AddHandler(func(vc *discordgo.VoiceConnection, vs *discordgo.VoiceSpeakingUpdate) {
		voiceStateUpdate(state, vc, vs)
	})

	dgClient, err := deepgram.NewDeepgramClient(deepgramToken, guildID, channelID, handleTranscript)
	if err != nil {
		logger.Error("Error creating Deepgram client", "error", err.Error())
		return
	}

	bConnected := dgClient.Connect()
	if !bConnected {
		logger.Error("Failed to connect to Deepgram")
		return
	}

	for {
		opus, ok := <-v.OpusRecv
		if !ok {
			logger.Info("Voice channel closed")
			break
		}
		err := dgClient.WriteBinary(opus.Opus)
		if err != nil {
			logger.Error("Failed to send audio to Deepgram", "error", err.Error())
		}

		// Get the stream ID for this SSRC
		streamInterface, exists := state.ssrcToStream.Load(opus.SSRC)
		if !exists {
			logger.Error("Failed to find stream for SSRC", "SSRC", opus.SSRC)
			continue
		}
		stream := streamInterface.(VoiceStream)
		streamID := stream.StreamID

		// Save the Discord voice packet to the database
		err = db.SaveDiscordVoicePacket(streamID, opus.Opus, opus.Sequence)
		if err != nil {
			logger.Error("Failed to save Discord voice packet to database", "error", err.Error())
		}

		// Print timestamps in seconds and user ID
		timestampSeconds := float64(opus.Timestamp) / 48000.0
		userID, _ := state.GetUserIDFromSSRC(opus.SSRC)
		logger.Info("opus", "seq", opus.Sequence, "t", timestampSeconds, "userID", userID)
	}

	dgClient.Stop()
}

func handleTranscript(guildID, channelID, transcript string) {
	// Send the transcript to Discord
	s, err := discordgo.New("Bot " + discordToken)
	if err != nil {
		logger.Error("Failed to create Discord session", "error", err.Error())
		return
	}
	defer s.Close()

	_, err = s.ChannelMessageSend(channelID, transcript)
	if err != nil {
		logger.Error("Failed to send message to Discord", "error", err.Error())
	}

	// Send the transcript to the channel
	key := fmt.Sprintf("%s:%s", guildID, channelID)
	if ch, ok := transcriptChannels.Load(key); ok {
		ch.(chan string) <- transcript
	}
}

func GetTranscriptChannel(guildID, channelID string) chan string {
	key := fmt.Sprintf("%s:%s", guildID, channelID)
	ch, _ := transcriptChannels.LoadOrStore(key, make(chan string))
	return ch.(chan string)
}

func (state *VoiceState) GetUserIDFromSSRC(ssrc uint32) (string, bool) {
	stream, ok := state.ssrcToStream.Load(ssrc)
	if !ok {
		return "", false
	}
	return stream.(VoiceStream).UserID, true
}

func (state *VoiceState) GetStreamIDFromSSRC(ssrc uint32) (string, bool) {
	stream, ok := state.ssrcToStream.Load(ssrc)
	if !ok {
		return "", false
	}
	return stream.(VoiceStream).StreamID, true
}
