package discordbot

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"jamie/db"
	"jamie/etc"
	"jamie/llm"
	"jamie/stt"
	"jamie/txt"
	"strings"
	"time"

	discordsdk "github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"
	"github.com/haguro/elevenlabs-go"
	"github.com/pion/webrtc/v4/pkg/media/oggwriter"
	"github.com/tosone/minimp3"
	"layeh.com/gopus"
)

type CommandHandler func(*discordsdk.Session, *discordsdk.MessageCreate, []string) error

type Bot struct {
	log                      *log.Logger
	conn                     *discordsdk.Session
	speechRecognitionService stt.SpeechRecognitionService
	db                       *db.DB
	sessions                 map[string]stt.LiveTranscriptionSession
	openaiAPIKey             string
	commands                 map[string]CommandHandler
	elevenLabsAPIKey         string
}

func NewBot(
	discordToken string,
	speechRecognitionService stt.SpeechRecognitionService,
	logger *log.Logger,
	openaiAPIKey string,
	elevenLabsAPIKey string,
) (*Bot, error) {
	bot := &Bot{
		speechRecognitionService: speechRecognitionService,
		log:                      logger,
		db:                       db.GetDB(),
		sessions: make(
			map[string]stt.LiveTranscriptionSession,
		),
		openaiAPIKey:     openaiAPIKey,
		commands:         make(map[string]CommandHandler),
		elevenLabsAPIKey: elevenLabsAPIKey,
	}

	bot.registerCommands()

	dg, err := discordsdk.New("Bot " + discordToken)
	if err != nil {
		return nil, fmt.Errorf("error creating Discord session: %w", err)
	}

	dg.AddHandler(bot.handleGuildCreate)
	dg.AddHandler(bot.handleVoiceStateUpdate)
	dg.AddHandler(bot.handleMessageCreate)

	err = dg.Open()
	if err != nil {
		return nil, fmt.Errorf("error opening connection: %w", err)
	}

	bot.conn = dg
	bot.log.Info(
		"bot started",
		"username",
		bot.conn.State.User.Username,
	)
	return bot, nil
}

func (bot *Bot) registerCommands() {
	bot.commands["summary"] = bot.handleSummaryCommand
	bot.commands["prompt"] = bot.handlePromptCommand
	bot.commands["listprompts"] = bot.handleListPromptsCommand
	bot.commands["audio"] = bot.handleAudioCommand
}

func (bot *Bot) Close() error {
	return bot.conn.Close()
}

func (bot *Bot) handleGuildCreate(
	_ *discordsdk.Session,
	event *discordsdk.GuildCreate,
) {
	bot.log.Info("joined guild", "guild", event.Guild.Name)
	err := bot.joinAllVoiceChannels(event.Guild.ID)
	if err != nil {
		bot.log.Error(
			"failed to join voice channels",
			"error",
			err.Error(),
		)
	}
}

func (bot *Bot) handleMessageCreate(
	s *discordsdk.Session,
	m *discordsdk.MessageCreate,
) {
	// Ignore messages from the bot itself
	if m.Author.ID == s.State.User.ID {
		return
	}

	// Check if the message starts with the command prefix
	if !strings.HasPrefix(m.Content, "!") {
		return
	}

	// Split the message into command and arguments
	args := strings.Fields(m.Content[1:])
	if len(args) == 0 {
		return
	}

	commandName := args[0]
	handler, exists := bot.commands[commandName]
	if !exists {
		s.ChannelMessageSend(
			m.ChannelID,
			fmt.Sprintf("Unknown command: %s", commandName),
		)
		return
	}

	err := handler(s, m, args[1:])
	if err != nil {
		bot.log.Error(
			"Command execution failed",
			"command",
			commandName,
			"error",
			err.Error(),
		)
		s.ChannelMessageSend(
			m.ChannelID,
			fmt.Sprintf("Error executing command: %s", err.Error()),
		)
	}
}

func (bot *Bot) joinVoiceChannel(guildID, channelID string) error {
	vc, err := bot.conn.ChannelVoiceJoin(guildID, channelID, false, false)
	if err != nil {
		return fmt.Errorf("failed to join voice channel: %w", err)
	}

	bot.log.Info("joined voice channel", "channel", channelID)
	go bot.handleVoiceConnection(vc, guildID, channelID)
	return nil
}

func (bot *Bot) joinAllVoiceChannels(guildID string) error {
	channels, err := bot.conn.GuildChannels(guildID)
	if err != nil {
		return fmt.Errorf("error getting guild channels: %w", err)
	}

	for _, channel := range channels {
		if channel.Type == discordsdk.ChannelTypeGuildVoice {
			err := bot.joinVoiceChannel(guildID, channel.ID)
			if err != nil {
				bot.log.Error(
					"failed to join voice channel",
					"channel",
					channel.Name,
					"error",
					err.Error(),
				)
			}
		}
	}

	return nil
}

func (bot *Bot) handleVoiceConnection(
	vc *discordsdk.VoiceConnection,
	guildID, channelID string,
) {
	vc.AddHandler(bot.handleVoiceSpeakingUpdate)

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	// Generate speech
	mp3Data, err := bot.textToSpeech("hello")
	if err != nil {
		bot.log.Error("Failed to generate speech", "error", err)
		return
	}

	// Convert to Opus packets
	opusPackets, err := convertToOpus(mp3Data)
	if err != nil {
		bot.log.Error("Failed to convert to Opus", "error", err)
		return
	}

	for {
		select {
		case <-ticker.C:

			// Send Opus packets in the background
			go func() {
				vc.Speaking(true)
				defer vc.Speaking(false)
				for _, packet := range opusPackets {
					vc.OpusSend <- packet
				}
			}()

		case packet, ok := <-vc.OpusRecv:
			if !ok {
				bot.log.Info("voice channel closed")
				return
			}

			err := bot.processVoicePacket(packet, guildID, channelID)
			if err != nil {
				bot.log.Error(
					"failed to process voice packet",
					"error",
					err.Error(),
				)
			}
		}
	}
}

func (bot *Bot) handleVoiceSpeakingUpdate(
	_ *discordsdk.VoiceConnection,
	v *discordsdk.VoiceSpeakingUpdate,
) {
	bot.log.Info(
		"speaking update",
		"ssrc", v.SSRC,
		"userID", v.UserID,
		"speaking", v.Speaking,
	)
}

func (bot *Bot) processVoicePacket(
	packet *discordsdk.Packet,
	guildID, channelID string,
) error {
	streamID, err := bot.getOrCreateVoiceStream(packet, guildID, channelID)
	if err != nil {
		return fmt.Errorf("failed to get or create voice stream: %w", err)
	}

	packetID := etc.Gensym()
	err = db.SavePacket(
		packetID,
		streamID,
		int(packet.Sequence),
		int(packet.Timestamp),
		packet.Opus,
	)
	if err != nil {
		return fmt.Errorf(
			"failed to save Discord voice packet to database: %w",
			err,
		)
	}

	session, err := bot.getSpeechRecognitionSession(streamID)
	if err != nil {
		return fmt.Errorf("failed to get speech recognition session: %w", err)
	}

	err = session.SendAudio(packet.Opus)
	if err != nil {
		return fmt.Errorf(
			"failed to send audio to speech recognition service: %w",
			err,
		)
	}

	return nil
}

func (bot *Bot) getOrCreateVoiceStream(
	packet *discordsdk.Packet,
	guildID, channelID string,
) (string, error) {
	discordID := fmt.Sprintf(
		"%d",
		packet.SSRC,
	) // Using SSRC as a unique identifier for the Discord user
	streamID, err := db.GetStreamForDiscordChannelAndSpeaker(
		guildID,
		channelID,
		discordID,
	)

	if errors.Is(err, sql.ErrNoRows) {
		streamID = etc.Gensym()
		speakerID := etc.Gensym()
		emoji := txt.RandomAvatar()
		err = db.CreateStreamForDiscordChannel(
			streamID,
			guildID,
			channelID,
			packet.Sequence,
			uint16(packet.Timestamp),
			speakerID,
			discordID,
			emoji,
		)
		if err != nil {
			return "", fmt.Errorf("failed to create new stream: %w", err)
		}

		bot.log.Info(
			"created new voice stream",
			"streamID", streamID,
			"speakerID", speakerID,
			"discordID", discordID,
		)
	} else if err != nil {
		return "", fmt.Errorf("failed to query for stream: %w", err)
	}

	return streamID, nil
}

func (bot *Bot) getSpeechRecognitionSession(
	streamID string,
) (stt.LiveTranscriptionSession, error) {
	session, exists := bot.sessions[streamID]
	if !exists {
		var err error
		session, err = bot.speechRecognitionService.Start(
			context.Background(),
		)
		if err != nil {
			return nil, fmt.Errorf(
				"failed to start speech recognition session: %w",
				err,
			)
		}
		bot.sessions[streamID] = session
		go bot.speechRecognitionLoop(streamID, session)
	}
	return session, nil
}

func (bot *Bot) speechRecognitionLoop(
	streamID string,
	session stt.LiveTranscriptionSession,
) {
	for segmentDrafts := range session.Receive() {
		bot.processSegment(streamID, segmentDrafts)
	}
	bot.log.Info(
		"Speech recognition session closed",
		"streamID",
		streamID,
	)
}

func (bot *Bot) processSegment(streamID string, segmentDrafts <-chan string) {
	var final string

	for draft := range segmentDrafts {
		final = draft
	}

	if final != "" {
		if strings.EqualFold(final, "Change my identity.") {
			bot.handleAvatarChangeRequest(streamID)
			return
		}

		channelID, emoji, err := db.GetChannelAndEmojiForStream(streamID)
		if err != nil {
			bot.log.Error(
				"failed to get channel and emoji",
				"error",
				err.Error(),
			)
			return
		}

		_, err = bot.conn.ChannelMessageSend(
			channelID,
			fmt.Sprintf("%s %s", emoji, final),
		)

		if err != nil {
			bot.log.Error(
				"failed to send transcribed message",
				"error",
				err.Error(),
			)
		}

		recognitionID := etc.Gensym()
		err = db.SaveRecognition(recognitionID, streamID, 0, 0, final, 1.0)
		if err != nil {
			bot.log.Error(
				"failed to save recognition to database",
				"error",
				err.Error(),
			)
		}
	}
}

func (bot *Bot) handleAvatarChangeRequest(streamID string) {
	newEmoji := txt.RandomAvatar()

	err := db.UpdateSpeakerEmoji(streamID, newEmoji)
	if err != nil {
		bot.log.Error("failed to update speaker emoji", "error", err.Error())
		return
	}

	channelID, err := db.GetChannelIDForStream(streamID)
	if err != nil {
		bot.log.Error("failed to get channel ID", "error", err.Error())
		return
	}

	_, err = bot.conn.ChannelMessageSend(
		channelID,
		fmt.Sprintf("You are now %s.", newEmoji),
	)
	if err != nil {
		bot.log.Error(
			"failed to send identity change message",
			"error",
			err.Error(),
		)
	}
}

func (bot *Bot) handleVoiceStateUpdate(
	_ *discordsdk.Session,
	v *discordsdk.VoiceStateUpdate,
) {
	if v.UserID == bot.conn.State.User.ID {
		return // Ignore bot's own voice state updates
	}

	if v.ChannelID == "" {
		// User left a voice channel
		err := db.EndStreamForChannel(v.GuildID, v.ChannelID)
		if err != nil {
			bot.log.Error(
				"failed to update stream end time",
				"error",
				err.Error(),
			)
		}
	} else {
		// User joined or moved to a voice channel
		streamID := etc.Gensym()
		speakerID := etc.Gensym()
		discordID := v.UserID
		emoji := txt.RandomAvatar()
		err := db.CreateStreamForDiscordChannel(streamID, v.GuildID, v.ChannelID, 0, uint16(0), speakerID, discordID, emoji)
		if err != nil {
			bot.log.Error("failed to create new stream for user join", "error", err.Error())
		}
	}
}

func (bot *Bot) handleSummaryCommand(
	s *discordsdk.Session,
	m *discordsdk.MessageCreate,
	args []string,
) error {
	bot.log.Info(
		"Summary command received",
		"channel",
		m.ChannelID,
		"args",
		args,
	)

	if len(args) < 1 {
		return fmt.Errorf("usage: !summary <duration> [prompt_name]")
	}

	timeRange := args[0]
	duration, err := time.ParseDuration(timeRange)
	if err != nil {
		return fmt.Errorf("invalid time range format: %w", err)
	}

	var promptName string
	if len(args) > 1 {
		promptName = args[1]
	}

	// Generate summary
	summaryChan, err := llm.SummarizeTranscript(
		bot.openaiAPIKey,
		duration,
		promptName,
	)
	if err != nil {
		return fmt.Errorf("failed to start summary generation: %w", err)
	}

	// Send initial message
	message, err := s.ChannelMessageSend(m.ChannelID, "Generating summary...")
	if err != nil {
		return fmt.Errorf("failed to send initial message: %w", err)
	}

	// Accumulate and update summary
	var fullSummary strings.Builder
	updateTicker := time.NewTicker(2 * time.Second)
	defer updateTicker.Stop()

	for {
		select {
		case chunk, ok := <-summaryChan:
			if !ok {
				// Channel closed, summary generation complete
				goto DONE
			}
			fullSummary.WriteString(chunk)
		case <-updateTicker.C:
			if fullSummary.Len() > 0 {
				_, err = s.ChannelMessageEdit(
					m.ChannelID,
					message.ID,
					fullSummary.String(),
				)
				if err != nil {
					bot.log.Error(
						"failed to update summary message",
						"error",
						err,
					)
				}
			}
		}
	}

DONE:

	// Send final summary
	_, err = s.ChannelMessageEdit(
		m.ChannelID,
		message.ID,
		fullSummary.String(),
	)
	if err != nil {
		return fmt.Errorf("failed to send final summary message: %w", err)
	}

	return nil
}

func (bot *Bot) handlePromptCommand(
	s *discordsdk.Session,
	m *discordsdk.MessageCreate,
	args []string,
) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: !prompt <name> <prompt text>")
	}

	name := args[0]
	prompt := strings.Join(args[1:], " ")

	err := bot.db.SetSystemPrompt(name, prompt)
	if err != nil {
		return fmt.Errorf("failed to set system prompt: %w", err)
	}

	_, err = s.ChannelMessageSend(
		m.ChannelID,
		fmt.Sprintf("System prompt '%s' has been set.", name),
	)
	if err != nil {
		return fmt.Errorf("failed to send confirmation message: %w", err)
	}

	return nil
}

func (bot *Bot) handleListPromptsCommand(
	s *discordsdk.Session,
	m *discordsdk.MessageCreate,
	args []string,
) error {
	prompts, err := bot.db.ListSystemPrompts()
	if err != nil {
		return fmt.Errorf("failed to list system prompts: %w", err)
	}

	if len(prompts) == 0 {
		_, err = s.ChannelMessageSend(
			m.ChannelID,
			"No system prompts have been set.",
		)
		if err != nil {
			return fmt.Errorf("failed to send message: %w", err)
		}
		return nil
	}

	var message strings.Builder
	message.WriteString("Available system prompts:\n")
	for name, prompt := range prompts {
		message.WriteString(fmt.Sprintf("- %s: %s\n", name, prompt))
	}

	_, err = s.ChannelMessageSend(m.ChannelID, message.String())
	if err != nil {
		return fmt.Errorf("failed to send prompts list: %w", err)
	}

	return nil
}

func (bot *Bot) handleAudioCommand(
	s *discordsdk.Session,
	m *discordsdk.MessageCreate,
	args []string,
) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: !audio <stream_id> <duration>")
	}

	streamID := args[0]
	durationStr := args[1]

	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		return fmt.Errorf("invalid duration format: %w", err)
	}

	endTime := time.Now()
	startTime := endTime.Add(-duration)

	oggData, err := bot.GenerateOggOpusBlob(streamID, startTime, endTime)
	if err != nil {
		return fmt.Errorf("failed to generate OGG Opus blob: %w", err)
	}

	// Send the OGG Opus blob as a file
	_, err = s.ChannelFileSendWithMessage(
		m.ChannelID,
		"audio.ogg",
		bytes.NewReader(oggData),
		m.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to send audio file: %w", err)
	}

	return nil
}

func (bot *Bot) GenerateOggOpusBlob(
	streamID string,
	startTime, endTime time.Time,
) ([]byte, error) {
	// Fetch packets from the database
	packets, err := bot.db.GetPacketsForStreamInTimeRange(
		streamID,
		startTime,
		endTime,
	)
	if err != nil {
		return nil, fmt.Errorf("fetch packets: %w", err)
	}

	// Create a buffer to store the OGG Opus data
	var oggBuffer bytes.Buffer

	// Create an OGG writer
	// Assuming 48kHz sample rate and 2 channels (stereo) for Opus
	oggWriter, err := oggwriter.NewWith(&oggBuffer, 48000, 2)
	if err != nil {
		return nil, fmt.Errorf("create OGG writer: %w", err)
	}

	// Write packets to the OGG writer
	for _, packet := range packets {
		if err := oggWriter.WriteOpus(packet); err != nil {
			return nil, fmt.Errorf("write Opus packet: %w", err)
		}
	}

	// Close the OGG writer to finalize the file
	if err := oggWriter.Close(); err != nil {
		return nil, fmt.Errorf("close OGG writer: %w", err)
	}

	return oggBuffer.Bytes(), nil
}

func (bot *Bot) textToSpeech(text string) ([]byte, error) {
	elevenlabs.SetAPIKey(bot.elevenLabsAPIKey)

	ttsReq := elevenlabs.TextToSpeechRequest{
		Text:    text,
		ModelID: "eleven_monolingual_v1",
	}

	audio, err := elevenlabs.TextToSpeech("pNInz6obpgDQGcFmaJgB", ttsReq)
	if err != nil {
		return nil, fmt.Errorf("failed to generate speech: %w", err)
	}

	return audio, nil
}

func convertToOpus(mp3Data []byte) ([][]byte, error) {
	encoder, err := gopus.NewEncoder(48000, 2, gopus.Audio)
	if err != nil {
		return nil, fmt.Errorf("failed to create Opus encoder: %w", err)
	}

	decoder, err := minimp3.NewDecoder(bytes.NewReader(mp3Data))
	if err != nil {
		return nil, fmt.Errorf("failed to create MP3 decoder: %w", err)
	}

	var opusPackets [][]byte
	for {
		var pcm = make([]byte, 1024)
		_, err := decoder.Read(pcm)
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}

		pcmInt16 := make([]int16, len(pcm)/2)
		for i := 0; i < len(pcm); i += 2 {
			pcmInt16[i/2] = int16(pcm[i]) | int16(pcm[i+1])<<8
		}

		opusData, err := encoder.Encode(pcmInt16, 960, 32000)
		if err != nil {
			return nil, fmt.Errorf("failed to encode Opus: %w", err)
		}
		opusPackets = append(opusPackets, opusData)
	}

	return opusPackets, nil
}
