package discordbot

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"jamie/db"
	"jamie/etc"
	"jamie/llm"
	"jamie/ogg"
	"jamie/stt"
	"sort"
	"strings"
	"sync"
	"time"

	discordsdk "github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"
	"github.com/haguro/elevenlabs-go"
	"github.com/sashabaranov/go-openai"
)

func (bot *Bot) saveTextMessage(
	channelID, userID, messageID, content string,
	isBot bool,
) error {
	return bot.db.SaveTextMessage(
		context.Background(),
		db.SaveTextMessageParams{
			ID:               etc.Gensym(),
			DiscordChannel:   channelID,
			DiscordUser:      userID,
			DiscordMessageID: messageID,
			Content:          content,
			IsBot:            isBot,
		},
	)
}

type CommandHandler func(*discordsdk.Session, *discordsdk.MessageCreate, []string) error

type Bot struct {
	log                      *log.Logger
	conn                     *discordsdk.Session
	speechRecognitionService stt.SpeechRecognitionService
	db                       *db.Queries
	sessions                 map[string]stt.LiveTranscriptionSession
	openaiAPIKey             string
	commands                 map[string]CommandHandler
	elevenLabsAPIKey         string
	voiceConnections         map[string]*discordsdk.VoiceConnection
	talkModeChannels         map[string]bool
	mu                       sync.Mutex
	voicePacketChan          chan *voicePacket
	audioBuffers             map[string]chan []byte
	voiceStreamCache         map[string]string
	voiceStreamCacheMu       sync.RWMutex
	isSpeaking               bool
	speakingMu               sync.Mutex
}

type voicePacket struct {
	packet    *discordsdk.Packet
	guildID   string
	channelID string
}

func NewBot(
	discordToken string,
	speechRecognitionService stt.SpeechRecognitionService,
	logger *log.Logger,
	openaiAPIKey string,
	elevenLabsAPIKey string,
	db *db.Queries,
) (*Bot, error) {
	bot := &Bot{
		speechRecognitionService: speechRecognitionService,
		log:                      logger,
		db:                       db,
		sessions: make(
			map[string]stt.LiveTranscriptionSession,
		),
		openaiAPIKey:     openaiAPIKey,
		elevenLabsAPIKey: elevenLabsAPIKey,
		commands:         make(map[string]CommandHandler),
		voiceConnections: make(map[string]*discordsdk.VoiceConnection),
		talkModeChannels: make(map[string]bool),
		voicePacketChan: make(
			chan *voicePacket,
			// three seconds of 20ms frames
			3*1000/20,
		),
		audioBuffers:     make(map[string]chan []byte),
		voiceStreamCache: make(map[string]string),
	}

	bot.registerCommands()
	go bot.processVoicePackets() // Start the goroutine to process voice packets

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
	bot.commands["yo"] = bot.handleYoCommand
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

	// Save the received message
	err := bot.saveTextMessage(
		m.ChannelID,
		m.Author.ID,
		m.ID,
		m.Content,
		m.Author.Bot,
	)
	if err != nil {
		bot.log.Error("Failed to save received message", "error", err.Error())
	}

	// Check if the bot is currently speaking
	bot.speakingMu.Lock()
	if bot.isSpeaking {
		bot.speakingMu.Unlock()
		bot.log.Debug("Ignoring command while speaking")
		return
	}
	bot.speakingMu.Unlock()

	// Check if the channel is in talk mode
	if bot.talkModeChannels[m.ChannelID] {
		if strings.HasPrefix(m.Content, "!yo") {
			// Turn off talk mode
			delete(bot.talkModeChannels, m.ChannelID)
			bot.sendAndSaveMessage(s, m.ChannelID, "Talk mode deactivated.")
		} else {
			// Process the message as a yo command
			bot.handleYoCommand(s, m, strings.Fields(m.Content))
		}
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
	if commandName == "yo" {
		// Turn on talk mode
		bot.talkModeChannels[m.ChannelID] = true
		bot.sendAndSaveMessage(
			s,
			m.ChannelID,
			"Talk mode activated. Type !yo again to deactivate.",
		)
		return
	}

	handler, exists := bot.commands[commandName]
	if !exists {
		bot.sendAndSaveMessage(
			s,
			m.ChannelID,
			fmt.Sprintf("Unknown command: %s", commandName),
		)
		return
	}

	err = handler(s, m, args[1:])
	if err != nil {
		bot.log.Error(
			"Command execution failed",
			"command",
			commandName,
			"error",
			err.Error(),
		)
		bot.sendAndSaveMessage(
			s,
			m.ChannelID,
			fmt.Sprintf("Error executing command: %s", err.Error()),
		)
	}
}

func (bot *Bot) processSegment(
	streamID string,
	segmentDrafts <-chan stt.Result,
) {
	bot.log.Debug("Processing segment", "streamID", streamID)
	var finalResult stt.Result

	for draft := range segmentDrafts {
		finalResult = draft
		bot.log.Info(
			"Received draft result",
			"text",
			draft.Text,
			"confidence",
			draft.Confidence,
		)
	}

	if finalResult.Text != "" {
		bot.log.Info(
			"Final result received",
			"streamID",
			streamID,
			"text",
			finalResult.Text,
		)

		row, err := bot.db.GetChannelAndUsernameForStream(
			context.Background(),
			streamID,
		)
		if err != nil {
			bot.log.Error(
				"Failed to get channel and username",
				"error", err.Error(),
				"streamID", streamID,
			)
			return
		}
		bot.log.Debug(
			"Retrieved channel and username",
			"channel",
			row.DiscordChannel,
			"username",
			row.Username,
		)

		// Check if the channel is in talk mode
		if bot.talkModeChannels[row.DiscordChannel] {
			// Process the speech recognition result as a yo command
			bot.handleYoCommand(bot.conn, &discordsdk.MessageCreate{
				Message: &discordsdk.Message{
					ChannelID: row.DiscordChannel,
					Author: &discordsdk.User{
						Username: row.Username,
					},
					Content: finalResult.Text,
				},
			}, strings.Fields(finalResult.Text))

			// Send the transcription to the Discord channel
			_, err = bot.conn.ChannelMessageSend(
				row.DiscordChannel,
				fmt.Sprintf("%s: %s", row.Username, finalResult.Text),
			)
			if err != nil {
				bot.log.Error(
					"Failed to send transcribed message in talk mode",
					"error", err.Error(),
					"channel", row.DiscordChannel,
				)
			} else {
				bot.log.Info("Sent transcribed message in talk mode", "channel", row.DiscordChannel)
			}
		} else {
			// Send the transcribed message as usual
			_, err = bot.conn.ChannelMessageSend(
				row.DiscordChannel,
				fmt.Sprintf("%s: %s", row.Username, finalResult.Text),
			)

			if err != nil {
				bot.log.Error(
					"Failed to send transcribed message",
					"error", err.Error(),
					"channel", row.DiscordChannel,
				)
			} else {
				bot.log.Info("Sent transcribed message", "channel", row.DiscordChannel)
			}
		}

		recognitionID := etc.Gensym()
		bot.log.Debug(
			"Saving recognition",
			"recognitionID",
			recognitionID,
			"streamID",
			streamID,
		)
		err = bot.db.SaveRecognition(
			context.Background(),
			db.SaveRecognitionParams{
				ID:         recognitionID,
				Stream:     streamID,
				SampleIdx:  int64(finalResult.Start * 48000),
				SampleLen:  int64(finalResult.Duration * 48000),
				Text:       finalResult.Text,
				Confidence: finalResult.Confidence,
			},
		)
		if err != nil {
			bot.log.Error(
				"Failed to save recognition to database",
				"error", err.Error(),
				"recognitionID", recognitionID,
			)
		} else {
			bot.log.Info("Saved recognition to database", "recognitionID", recognitionID)
		}
	} else {
		bot.log.Debug("Received empty final result", "streamID", streamID)
	}
}

func (bot *Bot) sendAndSaveMessage(
	s *discordsdk.Session,
	channelID, content string,
) {
	msg, err := s.ChannelMessageSend(channelID, content)
	if err != nil {
		bot.log.Error("Failed to send message", "error", err.Error())
		return
	}

	err = bot.saveTextMessage(
		channelID,
		s.State.User.ID,
		msg.ID,
		content,
		true,
	)
	if err != nil {
		bot.log.Error("Failed to save sent message", "error", err.Error())
	}
}

func (bot *Bot) joinVoiceChannel(guildID, channelID string) error {
	bot.mu.Lock()
	defer bot.mu.Unlock()

	vc, err := bot.conn.ChannelVoiceJoin(guildID, channelID, false, false)
	if err != nil {
		return fmt.Errorf("failed to join voice channel: %w", err)
	}

	bot.log.Info("joined voice channel", "channel", channelID)

	bot.voiceConnections[channelID] = vc

	go bot.handleVoiceConnection(vc, guildID, channelID)
	return nil
}

func (bot *Bot) sayInVoiceChannel(
	vc *discordsdk.VoiceConnection,
	text string,
) error {
	bot.log.Info("Starting text-to-speech", "text", text)

	// Generate speech
	mp3Data, err := bot.TextToSpeech(text)
	if err != nil {
		bot.log.Error("Failed to generate speech", "error", err)
		return fmt.Errorf("failed to generate speech: %w", err)
	}
	bot.log.Debug("Speech generated successfully", "mp3Size", len(mp3Data))

	// Convert to Opus packets
	opusPackets, err := ogg.ConvertToOpus(mp3Data)
	if err != nil {
		bot.log.Error("Failed to convert to Opus", "error", err)
		return fmt.Errorf("failed to convert to Opus: %w", err)
	}
	bot.log.Debug(
		"Converted to Opus packets",
		"packetCount",
		len(opusPackets),
	)

	// Send Opus packets
	bot.log.Info("Sending audio to voice channel")
	vc.Speaking(true)
	defer vc.Speaking(false)
	for i, packet := range opusPackets {
		vc.OpusSend <- packet
		if i%100 == 0 {
			bot.log.Debug(
				"Sending Opus packets",
				"progress",
				fmt.Sprintf("%d/%d", i+1, len(opusPackets)),
			)
		}
	}

	bot.log.Info("Finished sending audio to voice channel")
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
	go func() {
		vc.AddHandler(bot.handleVoiceSpeakingUpdate)
	}()

	for {
		select {
		case packet, ok := <-vc.OpusRecv:
			if !ok {
				bot.log.Info("voice channel closed")
				return
			}

			select {
			case bot.voicePacketChan <- &voicePacket{packet: packet, guildID: guildID, channelID: channelID}:
				// Packet sent to channel successfully
			default:
				bot.log.Warn("voice packet channel full, dropping packet")
			}
		}
	}
}

func (bot *Bot) handleVoicePacket(
	packet *discordsdk.Packet,
	guildID, channelID string,
) error {
	streamID, err := bot.getOrCreateVoiceStream(
		packet,
		guildID,
		channelID,
	)

	if err != nil {
		bot.log.Error("Failed to get or create voice stream",
			"error", err,
			"guildID", guildID,
			"channelID", channelID,
			"SSRC", packet.SSRC,
		)
		return fmt.Errorf(
			"failed to get or create voice stream: %w",
			err,
		)
	}

	bot.mu.Lock()
	audioBuffer, ok := bot.audioBuffers[streamID]
	if !ok {
		audioBuffer = make(
			chan []byte,
			100,
		) // Adjust buffer size as needed
		bot.audioBuffers[streamID] = audioBuffer
		go bot.processAudioBuffer(streamID, audioBuffer)
	}
	bot.mu.Unlock()

	select {
	case audioBuffer <- packet.Opus:
		return nil
	default:
		bot.log.Warn("Audio buffer full, dropping packet",
			"streamID", streamID,
			"SSRC", packet.SSRC,
		)
		return nil
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

	err := bot.db.UpsertVoiceState(
		context.Background(),
		db.UpsertVoiceStateParams{
			ID:         etc.Gensym(),
			Ssrc:       int64(v.SSRC),
			UserID:     v.UserID,
			IsSpeaking: v.Speaking,
		},
	)

	if err != nil {
		bot.log.Error(
			"failed to upsert voice state",
			"error", err.Error(),
			"ssrc", v.SSRC,
			"userID", v.UserID,
		)
	}
}

func (bot *Bot) getOrCreateVoiceStream(
	packet *discordsdk.Packet,
	guildID, channelID string,
) (string, error) {
	cacheKey := fmt.Sprintf("%d:%s:%s", packet.SSRC, guildID, channelID)

	// Check cache first
	bot.voiceStreamCacheMu.RLock()
	if streamID, ok := bot.voiceStreamCache[cacheKey]; ok {
		bot.voiceStreamCacheMu.RUnlock()
		return streamID, nil
	}
	bot.voiceStreamCacheMu.RUnlock()

	voiceState, err := bot.db.GetVoiceState(
		context.Background(),
		db.GetVoiceStateParams{
			Ssrc:   int64(packet.SSRC),
			UserID: "",
		},
	)
	if err != nil {
		return "", fmt.Errorf("failed to get voice state: %w", err)
	}

	discordID := voiceState.UserID
	username := bot.getUsernameFromID(discordID)

	streamID, err := bot.db.GetStreamForDiscordChannelAndSpeaker(
		context.Background(),
		db.GetStreamForDiscordChannelAndSpeakerParams{
			DiscordGuild:   guildID,
			DiscordChannel: channelID,
			DiscordID:      discordID,
		},
	)

	if errors.Is(err, sql.ErrNoRows) {
		streamID = etc.Gensym()
		speakerID := etc.Gensym()
		err = bot.db.CreateStream(
			context.Background(),
			db.CreateStreamParams{
				ID:              streamID,
				PacketSeqOffset: int64(packet.Sequence),
				SampleIdxOffset: int64(packet.Timestamp),
			},
		)
		if err != nil {
			return "", fmt.Errorf("failed to create new stream: %w", err)
		}

		err := bot.db.CreateDiscordChannelStream(
			context.Background(),
			db.CreateDiscordChannelStreamParams{
				DiscordGuild:   guildID,
				DiscordChannel: channelID,
				Stream:         streamID,
			},
		)

		if err != nil {
			return "", fmt.Errorf(
				"failed to create discord channel stream: %w",
				err,
			)
		}

		err = bot.db.CreateSpeaker(
			context.Background(),
			db.CreateSpeakerParams{
				ID:     speakerID,
				Stream: streamID,
				Emoji:  "", // We're not using emoji anymore
			},
		)

		if err != nil {
			return "", fmt.Errorf(
				"failed to create speaker: %w",
				err,
			)
		}

		err = bot.db.CreateDiscordSpeaker(
			context.Background(),
			db.CreateDiscordSpeakerParams{
				ID:        etc.Gensym(),
				Speaker:   speakerID,
				DiscordID: discordID,
				Ssrc:      int64(packet.SSRC),
				Username:  username,
			},
		)

		if err != nil {
			return "", fmt.Errorf(
				"failed to create discord speaker: %w",
				err,
			)
		}

		bot.log.Info(
			"created new voice stream",
			"streamID", streamID,
			"speakerID", speakerID,
			"discordID", discordID,
			"username", username,
		)
	} else if err != nil {
		return "", fmt.Errorf("failed to query for stream: %w", err)
	}

	// Add to cache
	bot.voiceStreamCacheMu.Lock()
	bot.voiceStreamCache[cacheKey] = streamID
	bot.voiceStreamCacheMu.Unlock()

	return streamID, nil
}

func (bot *Bot) getUsernameFromID(userID string) string {
	user, err := bot.conn.User(userID)
	if err != nil {
		bot.log.Error(
			"Failed to get username",
			"userID",
			userID,
			"error",
			err,
		)
		return "Unknown User"
	}
	return user.Username
}

func (bot *Bot) getSpeechRecognitionSession(
	streamID string,
) (stt.LiveTranscriptionSession, error) {
	bot.mu.Lock()
	defer bot.mu.Unlock()

	session, exists := bot.sessions[streamID]
	if !exists {
		bot.log.Info(
			"Creating new speech recognition session",
			"streamID",
			streamID,
		)
		var err error
		session, err = bot.speechRecognitionService.Start(
			context.Background(),
		)
		if err != nil {
			bot.log.Error(
				"Failed to start speech recognition session",
				"error",
				err,
				"streamID",
				streamID,
			)
			return nil, fmt.Errorf(
				"failed to start speech recognition session: %w",
				err,
			)
		}
		bot.sessions[streamID] = session
		bot.log.Info("Started speech recognition loop", "streamID", streamID)
		go bot.speechRecognitionLoop(streamID, session)
	}
	return session, nil
}

func (bot *Bot) speechRecognitionLoop(
	streamID string,
	session stt.LiveTranscriptionSession,
) {
	bot.log.Info("Starting speech recognition loop", "streamID", streamID)

	for segmentDrafts := range session.Receive() {
		bot.log.Debug("Received segment drafts", "streamID", streamID)
		bot.processSegment(streamID, segmentDrafts)
	}

	bot.log.Info(
		"Speech recognition session closed",
		"streamID",
		streamID,
	)
}

func (bot *Bot) handleVoiceStateUpdate(
	_ *discordsdk.Session,
	v *discordsdk.VoiceStateUpdate,
) {
	if v.UserID == bot.conn.State.User.ID {
		return // Ignore bot's own voice state updates
	}

	// if v.ChannelID == "" {
	// 	// User left a voice channel
	// 	err := bot.db.EndStreamForChannel(v.GuildID, v.ChannelID)
	// 	if err != nil {
	// 		bot.log.Error(
	// 			"failed to update stream end time",
	// 			"error",
	// 			err.Error(),
	// 		)
	// 	}
	// } else {
	// 	// // User joined or moved to a voice channel
	// 	// streamID := etc.Gensym()
	// 	// speakerID := etc.Gensym()
	// 	// discordID := v.UserID
	// 	// emoji := txt.RandomAvatar()
	// 	// err := bot.db.CreateStreamForDiscordChannel(
	// 	// 	streamID,
	// 	// 	v.GuildID,
	// 	// 	v.ChannelID,
	// 	// 	0,
	// 	// 	0,
	// 	// 	speakerID,
	// 	// 	discordID,
	// 	// 	emoji,
	// 	// )
	// 	// if err != nil {
	// 	// 	bot.log.Error("failed to create new stream for user join", "error", err.Error())
	// 	// }
	// }
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
		return fmt.Errorf("usage: !summary <duration> [prompt_name] [speak]")
	}

	timeRange := args[0]
	duration, err := time.ParseDuration(timeRange)
	if err != nil {
		return fmt.Errorf("invalid time range format: %w", err)
	}

	var promptName string
	var speak bool
	if len(args) > 1 {
		promptName = args[1]
	}
	if len(args) > 2 && args[2] == "speak" {
		speak = true
	}

	// Generate summary
	summaryChan, err := llm.SummarizeTranscript(
		bot.db,
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

	err = bot.saveTextMessage(
		m.ChannelID,
		s.State.User.ID,
		message.ID,
		"Generating summary...",
		true,
	)
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

	// Save the final summary message
	err = bot.saveTextMessage(
		m.ChannelID,
		s.State.User.ID,
		message.ID,
		fullSummary.String(),
		true,
	)
	if err != nil {
		bot.log.Error(
			"Failed to save final summary message",
			"error",
			err.Error(),
		)
	}

	if speak {
		err = bot.speakSummary(s, m, fullSummary.String())
		if err != nil {
			return fmt.Errorf("failed to speak summary: %w", err)
		}
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

	err := bot.db.SetSystemPrompt(
		context.Background(),
		db.SetSystemPromptParams{
			Name:   name,
			Prompt: prompt,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to set system prompt: %w", err)
	}

	bot.sendAndSaveMessage(
		s,
		m.ChannelID,
		fmt.Sprintf("System prompt '%s' has been set.", name),
	)

	return nil
}

func (bot *Bot) handleListPromptsCommand(
	s *discordsdk.Session,
	m *discordsdk.MessageCreate,
	args []string,
) error {
	prompts, err := bot.db.ListSystemPrompts(context.Background())
	if err != nil {
		return fmt.Errorf("failed to list system prompts: %w", err)
	}

	if len(prompts) == 0 {
		bot.sendAndSaveMessage(
			s,
			m.ChannelID,
			"No system prompts have been set.",
		)
		return nil
	}

	var message strings.Builder
	message.WriteString("Available system prompts:\n")
	for _, prompt := range prompts {
		message.WriteString(
			fmt.Sprintf("- %s: %s\n", prompt.Name, prompt.Prompt),
		)
	}

	bot.sendAndSaveMessage(s, m.ChannelID, message.String())

	return nil
}

func (bot *Bot) handleYoCommand(
	s *discordsdk.Session,
	m *discordsdk.MessageCreate,
	args []string,
) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: !yo <prompt>")
	}

	prompt := strings.Join(args, " ")

	// Start a goroutine to handle the command asynchronously
	go func() {
		response, err := bot.processYoCommand(s, m, prompt)
		if err != nil {
			bot.log.Error("Failed to process yo command", "error", err)
			bot.sendAndSaveMessage(
				s,
				m.ChannelID,
				fmt.Sprintf("An error occurred: %v", err),
			)
			return
		}

		// Speak the response
		err = bot.speakInChannel(s, m.ChannelID, response)
		if err != nil {
			bot.log.Error("Failed to speak response", "error", err)
			bot.sendAndSaveMessage(
				s,
				m.ChannelID,
				fmt.Sprintf("Failed to speak the response: %v", err),
			)
		}

		// Also send the response as a text message
		bot.sendAndSaveMessage(s, m.ChannelID, response)
	}()

	return nil
}

func (bot *Bot) processYoCommand(
	s *discordsdk.Session,
	m *discordsdk.MessageCreate,
	prompt string,
) (string, error) {
	// Fetch today's text messages and recognitions
	messages, err := bot.db.GetRecentTextMessages(
		context.Background(),
		db.GetRecentTextMessagesParams{
			DiscordChannel: m.ChannelID,
			Limit:          50,
		},
	)
	if err != nil {
		return "", fmt.Errorf("failed to fetch today's messages: %w", err)
	}

	recognitions, err := bot.db.GetRecentRecognitions(
		context.Background(),
		50,
	)
	if err != nil {
		return "", fmt.Errorf("failed to fetch recent recognitions: %w", err)
	}

	// Create context from today's messages and recognitions
	var contextBuilder strings.Builder
	contextBuilder.WriteString(
		"Today's conversation and voice transcriptions:\n",
	)

	// Combine and sort messages and recognitions
	type contextItem struct {
		content   string
		createdAt float64
	}
	var items []contextItem

	for _, msg := range messages {
		sender := "User"
		if msg.IsBot {
			sender = "Bot"
		}
		items = append(items, contextItem{
			content:   fmt.Sprintf("%s: %s", sender, msg.Content),
			createdAt: msg.CreatedAt,
		})
	}

	for _, rec := range recognitions {
		items = append(items, contextItem{
			content:   fmt.Sprintf("%s: %s", rec.Emoji, rec.Text),
			createdAt: rec.CreatedAt,
		})
	}

	// Sort items by createdAt
	sort.Slice(items, func(i, j int) bool {
		return items[i].createdAt < items[j].createdAt
	})

	// Add sorted items to context
	for _, item := range items {
		contextBuilder.WriteString(item.content + "\n")
	}

	contextBuilder.WriteString(
		"\nBased on the conversation and voice transcriptions above, please respond to the following prompt:\n",
	)
	contextBuilder.WriteString(prompt)

	// Create OpenAI client
	client := openai.NewClient(bot.openaiAPIKey)
	ctx := context.Background()

	// Generate response using GPT-4
	resp, err := client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model:     openai.GPT4o,
			MaxTokens: 200,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: "You are a brief, conversational, light, and terse AI assistant. Respond without using any markup or formatting, as your response will be sent to a text-to-speech service.",
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: contextBuilder.String(),
				},
			},
		},
	)

	if err != nil {
		return "", fmt.Errorf("failed to generate GPT-4 response: %w", err)
	}

	return resp.Choices[0].Message.Content, nil
}

func (bot *Bot) speakInChannel(
	s *discordsdk.Session,
	channelID string,
	text string,
) error {
	// Set the speaking flag
	bot.speakingMu.Lock()
	bot.isSpeaking = true
	bot.speakingMu.Unlock()
	defer func() {
		bot.speakingMu.Lock()
		bot.isSpeaking = false
		bot.speakingMu.Unlock()
	}()

	// Find the voice channel associated with the text channel
	channel, err := s.Channel(channelID)
	if err != nil {
		return fmt.Errorf("failed to get channel: %w", err)
	}

	guild, err := s.State.Guild(channel.GuildID)
	if err != nil {
		return fmt.Errorf("failed to get guild: %w", err)
	}

	var voiceChannelID string
	for _, vs := range guild.VoiceStates {
		if vs.ChannelID != "" {
			voiceChannelID = vs.ChannelID
			break
		}
	}

	if voiceChannelID == "" {
		return fmt.Errorf("no active voice channel found")
	}

	bot.mu.Lock()
	// Join the voice channel if not already connected
	vc, ok := bot.voiceConnections[voiceChannelID]
	if !ok {
		var err error
		vc, err = s.ChannelVoiceJoin(guild.ID, voiceChannelID, false, true)
		if err != nil {
			bot.mu.Unlock()
			return fmt.Errorf("failed to join voice channel: %w", err)
		}
		bot.voiceConnections[voiceChannelID] = vc
	}
	bot.mu.Unlock()

	// Generate speech
	speechData, err := bot.TextToSpeech(text)
	if err != nil {
		return fmt.Errorf("failed to generate speech: %w", err)
	}

	// Convert to Opus packets
	opusPackets, err := ogg.ConvertToOpus(speechData)
	if err != nil {
		return fmt.Errorf("failed to convert to Opus: %w", err)
	}

	// Send Opus packets
	bot.log.Debug("Starting to send Opus packets")
	vc.Speaking(true)
	bot.log.Debug("Speaking true")
	defer vc.Speaking(false)

	for _, packet := range opusPackets {
		vc.OpusSend <- packet
	}

	bot.log.Debug("Finished sending all Opus packets")

	return nil
}

func (bot *Bot) GenerateOggOpusBlob(
	streamID string,
	startSample, endSample int,
) ([]byte, error) {
	return ogg.GenerateOggOpusBlob(
		bot.db,
		streamID,
		int64(startSample),
		int64(endSample),
	)
}

func (bot *Bot) TextToSpeech(text string) ([]byte, error) {
	bot.log.Info("Starting text-to-speech generation", "text", text)
	elevenlabs.SetAPIKey(bot.elevenLabsAPIKey)

	ttsReq := elevenlabs.TextToSpeechRequest{
		Text:    text,
		ModelID: "eleven_monolingual_v1",
	}

	bot.log.Debug("Sending request to ElevenLabs API")
	audio, err := elevenlabs.TextToSpeech("pNInz6obpgDQGcFmaJgB", ttsReq)
	if err != nil {
		bot.log.Error(
			"Failed to generate speech from ElevenLabs",
			"error",
			err,
		)
		return nil, fmt.Errorf("failed to generate speech: %w", err)
	}

	bot.log.Info(
		"Text-to-speech generation successful",
		"audioSize",
		len(audio),
	)
	return audio, nil
}

func (bot *Bot) speakSummary(
	s *discordsdk.Session,
	m *discordsdk.MessageCreate,
	summary string,
) error {
	// Find the voice channel the user is in
	guild, err := s.State.Guild(m.GuildID)
	if err != nil {
		return fmt.Errorf("failed to get guild: %w", err)
	}

	var voiceChannelID string
	for _, vs := range guild.VoiceStates {
		if vs.UserID == m.Author.ID {
			voiceChannelID = vs.ChannelID
			break
		}
	}

	if voiceChannelID == "" {
		return fmt.Errorf("user is not in a voice channel")
	}

	// Join the voice channel if not already connected
	vc, ok := bot.voiceConnections[voiceChannelID]
	if !ok {
		vc, err = s.ChannelVoiceJoin(m.GuildID, voiceChannelID, false, true)
		if err != nil {
			return fmt.Errorf("failed to join voice channel: %w", err)
		}
		bot.voiceConnections[voiceChannelID] = vc
	}

	// Generate speech
	speechData, err := bot.TextToSpeech(summary)
	if err != nil {
		return fmt.Errorf("failed to generate speech: %w", err)
	}

	// Convert to Opus packets
	opusPackets, err := ogg.ConvertToOpus(speechData)
	if err != nil {
		return fmt.Errorf("failed to convert to Opus: %w", err)
	}

	// Send Opus packets
	vc.Speaking(true)
	defer vc.Speaking(false)

	for _, packet := range opusPackets {
		vc.OpusSend <- packet
	}

	return nil
}

func (bot *Bot) processVoicePackets() {
	for packet := range bot.voicePacketChan {
		err := bot.handleVoicePacket(
			packet.packet,
			packet.guildID,
			packet.channelID,
		)
		if err != nil {
			bot.log.Error(
				"failed to process voice packet",
				"error", err.Error(),
				"guildID", packet.guildID,
				"channelID", packet.channelID,
			)
		}
	}
}

func (bot *Bot) processAudioBuffer(
	streamID string,
	audioBuffer <-chan []byte,
) {
	session, err := bot.getSpeechRecognitionSession(streamID)
	if err != nil {
		bot.log.Error("Failed to get speech recognition session",
			"error", err,
			"streamID", streamID,
		)
		return
	}

	for audioData := range audioBuffer {
		err := session.SendAudio(audioData)
		if err != nil {
			bot.log.Error(
				"Failed to send audio to speech recognition service",
				"error",
				err,
				"streamID",
				streamID,
			)
		}
	}
}
