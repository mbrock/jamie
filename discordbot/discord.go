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

	dis "github.com/bwmarrin/discordgo"
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

type CommandHandler func(*dis.Session, *dis.MessageCreate, []string) error

type VoiceChannel struct {
	Conn                *dis.VoiceConnection
	TalkMode            bool
	InboundAudioPackets chan *voicePacket
}

type Bot struct {
	mu   sync.Mutex
	db   *db.Queries
	log  *log.Logger
	conn *dis.Session

	openaiAPIKey     string
	elevenLabsAPIKey string

	speechRecognition stt.SpeechRecognition
	speechRecognizers map[string]stt.SpeechRecognizer // streamID
	audioBuffers      map[string]chan []byte          // streamID

	commands map[string]CommandHandler // command name

	voiceChannels map[string]*VoiceChannel // channelID

	streamIdCache   map[string]string // cacheKey -> streamID
	streamIdCacheMu sync.RWMutex

	isSpeaking bool
	speakingMu sync.Mutex
}

type voicePacket struct {
	packet    *dis.Packet
	guildID   string
	channelID string
}

func NewBot(
	discordToken string,
	speechRecognitionService stt.SpeechRecognition,
	logger *log.Logger,
	openaiAPIKey string,
	elevenLabsAPIKey string,
	db *db.Queries,
) (*Bot, error) {
	bot := &Bot{
		db:                db,
		log:               logger,
		openaiAPIKey:      openaiAPIKey,
		elevenLabsAPIKey:  elevenLabsAPIKey,
		commands:          make(map[string]CommandHandler),
		voiceChannels:     make(map[string]*VoiceChannel),
		audioBuffers:      make(map[string]chan []byte),
		speechRecognition: speechRecognitionService,
		speechRecognizers: make(
			map[string]stt.SpeechRecognizer,
		),
		streamIdCache: make(map[string]string),
	}

	bot.registerCommands()

	dg, err := dis.New("Bot " + discordToken)
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
	bot.log.Info("bot", "username", bot.conn.State.User.Username)
	return bot, nil
}

func (bot *Bot) registerCommands() {
	bot.commands["summary"] = bot.handleSummaryCommand
	bot.commands["prompt"] = bot.handlePromptCommand
	bot.commands["listprompts"] = bot.handleListPromptsCommand
	bot.commands["talk"] = bot.handleTalkCommand
}

func (bot *Bot) Close() error {
	return bot.conn.Close()
}

func (bot *Bot) handleGuildCreate(_ *dis.Session, event *dis.GuildCreate) {
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
	s *dis.Session,
	m *dis.MessageCreate,
) {
	if m.Author.ID == s.State.User.ID {
		return
	}

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

	bot.mu.Lock()
	voiceChannel, ok := bot.voiceChannels[m.ChannelID]
	bot.mu.Unlock()

	if ok && voiceChannel.TalkMode {
		if strings.HasPrefix(m.Content, "!talk") {
			// Turn off talk mode
			voiceChannel.TalkMode = false
			bot.sendAndSaveMessage(s, m.ChannelID, "Talk mode deactivated.")
		} else {
			// Process the message as a talk command
			bot.handleTalkCommand(s, m, strings.Fields(m.Content))
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
	if commandName == "talk" {
		// Turn on talk mode
		if ok {
			voiceChannel.TalkMode = true
			bot.sendAndSaveMessage(
				s,
				m.ChannelID,
				"Talk mode activated. Type !talk again to deactivate.",
			)
		} else {
			bot.sendAndSaveMessage(
				s,
				m.ChannelID,
				"You must be in a voice channel to activate talk mode.",
			)
		}
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
	var finalResult stt.Result

	for draft := range segmentDrafts {
		finalResult = draft
	}

	if finalResult.Text != "" {
		bot.log.Info(
			"heard",
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

		// Check if the channel is in talk mode
		bot.mu.Lock()
		voiceChannel, ok := bot.voiceChannels[row.DiscordChannel]
		bot.mu.Unlock()
		if ok && voiceChannel.TalkMode {
			bot.speakingMu.Lock()
			isSpeaking := bot.isSpeaking
			bot.speakingMu.Unlock()

			if !isSpeaking {
				bot.speakingMu.Lock()
				bot.isSpeaking = true
				bot.speakingMu.Unlock()
				// Process the speech recognition result as a yo command
				bot.handleTalkCommand(bot.conn, &dis.MessageCreate{
					Message: &dis.Message{
						ChannelID: row.DiscordChannel,
						Author: &dis.User{
							Username: row.Username,
						},
						Content: finalResult.Text,
					},
				},
					strings.Fields(finalResult.Text),
				)

				// Send the transcription to the Discord channel
				_, err = bot.conn.ChannelMessageSend(
					row.DiscordChannel,
					fmt.Sprintf("> %s: %s", row.Username, finalResult.Text),
				)
				if err != nil {
					bot.log.Error(
						"Failed to send transcribed message in talk mode",
						"error", err.Error(),
						"channel", row.DiscordChannel,
					)
				}
			}
		} else {
			// Send the transcribed message as usual
			_, err = bot.conn.ChannelMessageSend(
				row.DiscordChannel,
				fmt.Sprintf("> %s: %s", row.Username, finalResult.Text),
			)

			if err != nil {
				bot.log.Error(
					"Failed to send transcribed message",
					"error", err.Error(),
					"channel", row.DiscordChannel,
				)
			}
		}

		recognitionID := etc.Gensym()

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
		}
	}
}

func (bot *Bot) sendAndSaveMessage(
	s *dis.Session,
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

	packetChan := make(
		chan *voicePacket,
		3*1000/20,
	) // three seconds of 20ms frames
	voiceChannel := &VoiceChannel{
		Conn:                vc,
		TalkMode:            false,
		InboundAudioPackets: packetChan,
	}
	bot.voiceChannels[channelID] = voiceChannel

	go bot.processVoicePackets(packetChan)
	go bot.handleVoiceConnection(voiceChannel, guildID, channelID)
	return nil
}

func (bot *Bot) joinAllVoiceChannels(guildID string) error {
	channels, err := bot.conn.GuildChannels(guildID)
	if err != nil {
		return fmt.Errorf("error getting guild channels: %w", err)
	}

	for _, channel := range channels {
		if channel.Type == dis.ChannelTypeGuildVoice {
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
	voiceChannel *VoiceChannel,
	guildID, channelID string,
) {
	go func() {
		voiceChannel.Conn.AddHandler(bot.handleVoiceSpeakingUpdate)
	}()

	for packet := range voiceChannel.Conn.OpusRecv {
		select {
		case voiceChannel.InboundAudioPackets <- &voicePacket{packet: packet, guildID: guildID, channelID: channelID}:
			// Packet sent to channel successfully
		default:
			bot.log.Warn(
				"voice packet channel full, dropping packet",
				"channelID",
				channelID,
			)
		}
	}
}

func (bot *Bot) handleVoicePacket(
	packet *dis.Packet,
	guildID, channelID string,
) error {
	streamID, err := bot.ensureVoiceStream(
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
	_ *dis.VoiceConnection,
	v *dis.VoiceSpeakingUpdate,
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

func (bot *Bot) ensureVoiceStream(
	packet *dis.Packet,
	guildID, channelID string,
) (string, error) {
	cacheKey := fmt.Sprintf("%d:%s:%s", packet.SSRC, guildID, channelID)

	if streamID, ok := bot.getCachedVoiceStream(cacheKey); ok {
		return streamID, nil
	}

	streamID, err := bot.findOrSaveVoiceStream(packet, guildID, channelID)
	if err != nil {
		return "", err
	}

	bot.streamIdCacheMu.Lock()
	bot.streamIdCache[cacheKey] = streamID
	bot.streamIdCacheMu.Unlock()

	return streamID, nil
}

func (bot *Bot) getCachedVoiceStream(cacheKey string) (string, bool) {
	bot.streamIdCacheMu.RLock()
	streamID, ok := bot.streamIdCache[cacheKey]
	bot.streamIdCacheMu.RUnlock()
	return streamID, ok
}

func (bot *Bot) findOrSaveVoiceStream(
	packet *dis.Packet,
	guildID, channelID string,
) (string, error) {
	discordID, username, streamID, err := bot.findVoiceStream(
		packet,
		guildID,
		channelID,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			streamID, err = bot.createNewVoiceStream(
				packet,
				guildID,
				channelID,
				discordID,
				username,
			)
			if err != nil {
				return "", fmt.Errorf(
					"failed to create new voice stream: %w",
					err,
				)
			}
		} else {
			return "", fmt.Errorf("failed to find voice stream: %w", err)
		}
	}

	return streamID, nil
}

func (bot *Bot) findVoiceStream(
	packet *dis.Packet,
	guildID string,
	channelID string,
) (string, string, string, error) {
	voiceState, err := bot.db.GetVoiceState(
		context.Background(),
		db.GetVoiceStateParams{
			Ssrc:   int64(packet.SSRC),
			UserID: "",
		},
	)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to get voice state: %w", err)
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
	if err != nil {
		return discordID, username, "", err
	}

	return discordID, username, streamID, nil
}

func (bot *Bot) createNewVoiceStream(
	packet *dis.Packet,
	guildID, channelID, discordID, username string,
) (string, error) {
	streamID := etc.Gensym()
	speakerID := etc.Gensym()

	err := bot.db.CreateStream(
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

	err = bot.db.CreateDiscordChannelStream(
		context.Background(),
		db.CreateDiscordChannelStreamParams{
			ID:             etc.Gensym(),
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
		return "", fmt.Errorf("failed to create speaker: %w", err)
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
		return "", fmt.Errorf("failed to create discord speaker: %w", err)
	}

	bot.log.Info(
		"created new voice stream",
		"streamID", streamID,
		"speakerID", speakerID,
		"discordID", discordID,
		"username", username,
	)

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
) (stt.SpeechRecognizer, error) {
	bot.mu.Lock()
	defer bot.mu.Unlock()

	session, exists := bot.speechRecognizers[streamID]
	if !exists {
		var err error
		session, err = bot.speechRecognition.Start(
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
		bot.speechRecognizers[streamID] = session
		go bot.speechRecognitionLoop(streamID, session)
	}
	return session, nil
}

func (bot *Bot) speechRecognitionLoop(
	streamID string,
	session stt.SpeechRecognizer,
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

func (bot *Bot) handleVoiceStateUpdate(
	_ *dis.Session,
	v *dis.VoiceStateUpdate,
) {
	if v.UserID == bot.conn.State.User.ID {
		return
	}
}

func (bot *Bot) handleSummaryCommand(
	s *dis.Session,
	m *dis.MessageCreate,
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
		return fmt.Errorf("usage: !summary [prompt_name] [speak]")
	}

	var promptName string
	var speak bool
	if len(args) > 0 {
		promptName = args[0]
	}
	if len(args) > 1 && args[1] == "speak" {
		speak = true
	}

	// Generate summary
	summaryChan, err := llm.SummarizeTranscript(
		bot.db,
		bot.openaiAPIKey,
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

	// Update the message with the summary
	fullSummary := bot.updateMessageWithSummary(
		s,
		m.ChannelID,
		message.ID,
		summaryChan,
	)

	// Save the final summary message
	err = bot.saveTextMessage(
		m.ChannelID,
		s.State.User.ID,
		message.ID,
		fullSummary,
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
		err = bot.speakSummary(s, m, fullSummary)
		if err != nil {
			return fmt.Errorf("failed to speak summary: %w", err)
		}
	}

	return nil
}

func (bot *Bot) updateMessageWithSummary(
	s *dis.Session,
	channelID string,
	messageID string,
	summaryChan <-chan string,
) string {
	var fullSummary strings.Builder
	updateTicker := time.NewTicker(2 * time.Second)
	defer updateTicker.Stop()

	for {
		select {
		case chunk, ok := <-summaryChan:
			if !ok {
				// Channel closed, summary generation complete
				return fullSummary.String()
			}
			fullSummary.WriteString(chunk)
		case <-updateTicker.C:
			if fullSummary.Len() > 0 {
				_, err := s.ChannelMessageEdit(
					channelID,
					messageID,
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
}

func (bot *Bot) handlePromptCommand(
	s *dis.Session,
	m *dis.MessageCreate,
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
	s *dis.Session,
	m *dis.MessageCreate,
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

func (bot *Bot) handleTalkCommand(
	s *dis.Session,
	m *dis.MessageCreate,
	args []string,
) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: !talk <prompt>")
	}

	prompt := strings.Join(args, " ")

	// Start a goroutine to handle the command asynchronously
	go func() {
		response, err := bot.processTalkCommand(s, m, prompt)
		if err != nil {
			bot.log.Error("Failed to process talk command", "error", err)
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

func (bot *Bot) processTalkCommand(
	_ *dis.Session,
	m *dis.MessageCreate,
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
		"\nBased on the conversation and voice transcriptions above, react to the following prompt. Remember, you are a brief, terse, stoner noir, weird interlocutor named Jamie. You never offer to help. You improvise together. Respond without using any markup or formatting, as your response will be sent to a text-to-speech service.\n",
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
			MaxTokens: 300,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: "You never offer to help. You hang back. Write terse, weird analyses without formatting. Do not react, just write.",
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
	s *dis.Session,
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
	voiceChannel, ok := bot.voiceChannels[voiceChannelID]
	if !ok {
		vc, err := s.ChannelVoiceJoin(guild.ID, voiceChannelID, false, true)
		if err != nil {
			bot.mu.Unlock()
			return fmt.Errorf("failed to join voice channel: %w", err)
		}
		packetChan := make(chan *voicePacket, 3*1000/20)
		voiceChannel = &VoiceChannel{
			Conn:                vc,
			TalkMode:            false,
			InboundAudioPackets: packetChan,
		}
		bot.voiceChannels[voiceChannelID] = voiceChannel
		go bot.processVoicePackets(packetChan)
		go bot.handleVoiceConnection(voiceChannel, guild.ID, voiceChannelID)
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
	voiceChannel.Conn.Speaking(true)
	bot.log.Debug("Speaking true")
	defer voiceChannel.Conn.Speaking(false)

	for _, packet := range opusPackets {
		voiceChannel.Conn.OpusSend <- packet
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
	bot.log.Info("speaking", "text", text)
	elevenlabs.SetAPIKey(bot.elevenLabsAPIKey)

	ttsReq := elevenlabs.TextToSpeechRequest{
		Text:    text,
		ModelID: "eleven_turbo_v2_5",
	}

	audio, err := elevenlabs.TextToSpeech("XB0fDUnXU5powFXDhCwa", ttsReq)
	if err != nil {
		bot.log.Error(
			"Failed to generate speech from ElevenLabs",
			"error",
			err,
		)
		return nil, fmt.Errorf("failed to generate speech: %w", err)
	}

	return audio, nil
}

func (bot *Bot) speakSummary(
	s *dis.Session,
	m *dis.MessageCreate,
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
	bot.mu.Lock()
	voiceChannel, ok := bot.voiceChannels[voiceChannelID]
	if !ok {
		vc, err := s.ChannelVoiceJoin(m.GuildID, voiceChannelID, false, true)
		if err != nil {
			bot.mu.Unlock()
			return fmt.Errorf("failed to join voice channel: %w", err)
		}
		voiceChannel = &VoiceChannel{
			Conn:                vc,
			TalkMode:            false,
			InboundAudioPackets: make(chan *voicePacket, 3*1000/20),
		}
		bot.voiceChannels[voiceChannelID] = voiceChannel
		go bot.processVoicePackets(voiceChannel.InboundAudioPackets)
		go bot.handleVoiceConnection(voiceChannel, m.GuildID, voiceChannelID)
	}
	bot.mu.Unlock()

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
	voiceChannel.Conn.Speaking(true)
	defer voiceChannel.Conn.Speaking(false)

	for _, packet := range opusPackets {
		voiceChannel.Conn.OpusSend <- packet
	}

	return nil
}

func (bot *Bot) processVoicePackets(packetChan <-chan *voicePacket) {
	for packet := range packetChan {
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
