package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"

	api "github.com/deepgram/deepgram-go-sdk/pkg/api/listen/v1/websocket/interfaces"
	"github.com/deepgram/deepgram-go-sdk/pkg/audio/microphone"
	interfaces "github.com/deepgram/deepgram-go-sdk/pkg/client/interfaces"
	client "github.com/deepgram/deepgram-go-sdk/pkg/client/listen"
)

var (
	Token  string
	logger *log.Logger
)

func init() {
	Token = os.Getenv("DISCORD_TOKEN")
	if Token == "" {
		fmt.Println("No token provided. Please set the DISCORD_TOKEN environment variable.")
		os.Exit(1)
	}

	logger = log.NewWithOptions(os.Stderr, log.Options{
		ReportCaller:    true,
		ReportTimestamp: true,
	})
}

func main() {
	dg, err := discordgo.New("Bot " + Token)
	if err != nil {
		logger.Fatal("Error creating Discord session", "error", err)
	}

	dg.AddHandler(messageCreate)
	dg.AddHandler(commandHandler)
	dg.AddHandler(guildCreate)

	err = dg.Open()
	if err != nil {
		logger.Fatal("Error opening connection", "error", err)
	}

	logger.Info("Bot is now running. Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	dg.Close()
}

func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	logger.Info("Received message",
		"content", m.Content,
		"author", m.Author.Username,
		"channel", m.ChannelID,
	)
}

func commandHandler(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	if strings.HasPrefix(m.Content, "!join") {
		channelName := strings.TrimSpace(strings.TrimPrefix(m.Content, "!join"))
		err := joinVoiceChannel(s, m.GuildID, m.ChannelID, channelName)
		if err != nil {
			logger.Error("Error joining voice channel", "error", err)
			s.ChannelMessageSend(m.ChannelID, "Error: "+err.Error())
		}
	} else if m.Content == "!listvoice" {
		err := listVoiceChannels(s, m.GuildID, m.ChannelID)
		if err != nil {
			logger.Error("Error listing voice channels", "error", err)
			s.ChannelMessageSend(m.ChannelID, "Error: "+err.Error())
		}
	} else if m.Content == "!invite" {
		sendInviteLink(s, m.ChannelID)
	}
}

func joinVoiceChannel(s *discordgo.Session, guildID, textChannelID, channelName string) error {
	channels, err := s.GuildChannels(guildID)
	if err != nil {
		return fmt.Errorf("error getting guild channels: %w", err)
	}

	var voiceChannel *discordgo.Channel
	for _, channel := range channels {
		if channel.Type == discordgo.ChannelTypeGuildVoice && strings.EqualFold(channel.Name, channelName) {
			voiceChannel = channel
			break
		}
	}

	if voiceChannel == nil {
		return fmt.Errorf("voice channel '%s' not found", channelName)
	}

	vc, err := s.ChannelVoiceJoin(guildID, voiceChannel.ID, false, false)
	if err != nil {
		return fmt.Errorf("failed to join voice channel: %w", err)
	}

	logger.Info("Joined voice channel", "channel", voiceChannel.Name)
	s.ChannelMessageSend(textChannelID, "Joined voice channel: "+voiceChannel.Name)

	go startRecording(vc, guildID, voiceChannel.ID)

	return nil
}

func startRecording(v *discordgo.VoiceConnection, guildID, channelID string) {
	logger.Info("Starting recording", "guild", guildID, "channel", channelID)

	// Create directory structure
	dirPath := filepath.Join("voice", guildID, channelID)
	err := os.MkdirAll(dirPath, 0755)
	if err != nil {
		logger.Error("Failed to create directory", "error", err)
		return
	}

	// Create file with timestamp
	timestamp := time.Now().Format("20060102-150405")
	filePath := filepath.Join(dirPath, timestamp+".opus")
	file, err := os.Create(filePath)
	if err != nil {
		logger.Error("Failed to create file", "error", err)
		return
	}
	defer file.Close()

	// Start receiving audio
	v.Speaking(true)
	defer v.Speaking(false)

	for {
		opus, ok := <-v.OpusRecv
		if !ok {
			logger.Info("Voice channel closed")
			return
		}
		_, err = file.Write(opus.Opus)
		if err != nil {
			logger.Error("Failed to write to file", "error", err)
			return
		}
	}
}

func listVoiceChannels(s *discordgo.Session, guildID, textChannelID string) error {
	channels, err := s.GuildChannels(guildID)
	if err != nil {
		return fmt.Errorf("error getting guild channels: %w", err)
	}

	var voiceChannels []string
	for _, channel := range channels {
		if channel.Type == discordgo.ChannelTypeGuildVoice {
			voiceChannels = append(voiceChannels, channel.Name)
		}
	}

	if len(voiceChannels) == 0 {
		s.ChannelMessageSend(textChannelID, "No voice channels found in this server.")
		return nil
	}

	message := "Available voice channels:\n" + strings.Join(voiceChannels, "\n")
	s.ChannelMessageSend(textChannelID, message)
	return nil
}

func sendInviteLink(s *discordgo.Session, channelID string) {
	inviteLink := fmt.Sprintf("https://discord.com/api/oauth2/authorize?client_id=%s&permissions=3145728&scope=bot", s.State.User.ID)
	message := fmt.Sprintf("To invite me to your server, use this link:\n%s", inviteLink)
	s.ChannelMessageSend(channelID, message)
}

func voiceStateUpdate(s *discordgo.VoiceConnection, v *discordgo.VoiceSpeakingUpdate) {
	logger.Info("Voice state update", "userID", v.UserID, "speaking", v.Speaking)
}

func guildCreate(s *discordgo.Session, event *discordgo.GuildCreate) {
	logger.Info("Joined new guild", "guild", event.Guild.Name)
	err := joinAllVoiceChannels(s, event.Guild.ID)
	if err != nil {
		logger.Error("Error joining voice channels", "error", err)
	}
}

func joinAllVoiceChannels(s *discordgo.Session, guildID string) error {
	channels, err := s.GuildChannels(guildID)
	if err != nil {
		return fmt.Errorf("error getting guild channels: %w", err)
	}

	for _, channel := range channels {
		if channel.Type == discordgo.ChannelTypeGuildVoice {
			vc, err := s.ChannelVoiceJoin(guildID, channel.ID, false, false)
			if err != nil {
				logger.Error("Failed to join voice channel", "channel", channel.Name, "error", err)
			} else {
				logger.Info("Joined voice channel", "channel", channel.Name)
				go startRecording(vc, guildID, channel.ID)
			}

			vc.AddHandler(voiceStateUpdate)
		}
	}

	return nil
}

// Copyright 2023-2024 Deepgram SDK contributors. All Rights Reserved.
// Use of this source code is governed by a MIT license that can be found in the LICENSE file.
// SPDX-License-Identifier: MIT

// Implement your own callback
type MyCallback struct {
	sb *strings.Builder
}

func (c MyCallback) Message(mr *api.MessageResponse) error {
	// handle the message
	sentence := strings.TrimSpace(mr.Channel.Alternatives[0].Transcript)

	if len(mr.Channel.Alternatives) == 0 || len(sentence) == 0 {
		return nil
	}

	if mr.IsFinal {
		c.sb.WriteString(sentence)
		c.sb.WriteString(" ")

		if mr.SpeechFinal {
			fmt.Printf("[------- Is Final]: %s\n", c.sb.String())
			c.sb.Reset()
		}
	} else {
		fmt.Printf("[Interm Result]: %s\n", sentence)
	}

	return nil
}

func (c MyCallback) Open(ocr *api.OpenResponse) error {
	// handle the open
	fmt.Printf("\n[Open] Received\n")
	return nil
}

func (c MyCallback) Metadata(md *api.MetadataResponse) error {
	// handle the metadata
	fmt.Printf("\n[Metadata] Received\n")
	fmt.Printf("Metadata.RequestID: %s\n", strings.TrimSpace(md.RequestID))
	fmt.Printf("Metadata.Channels: %d\n", md.Channels)
	fmt.Printf("Metadata.Created: %s\n\n", strings.TrimSpace(md.Created))
	return nil
}

func (c MyCallback) SpeechStarted(ssr *api.SpeechStartedResponse) error {
	fmt.Printf("\n[SpeechStarted] Received\n")
	return nil
}

func (c MyCallback) UtteranceEnd(ur *api.UtteranceEndResponse) error {
	utterance := strings.TrimSpace(c.sb.String())
	if len(utterance) > 0 {
		fmt.Printf("[------- UtteranceEnd]: %s\n", utterance)
		c.sb.Reset()
	} else {
		fmt.Printf("\n[UtteranceEnd] Received\n")
	}

	return nil
}

func (c MyCallback) Close(ocr *api.CloseResponse) error {
	// handle the close
	fmt.Printf("\n[Close] Received\n")
	return nil
}

func (c MyCallback) Error(er *api.ErrorResponse) error {
	// handle the error
	fmt.Printf("\n[Error] Received\n")
	fmt.Printf("Error.Type: %s\n", er.Type)
	fmt.Printf("Error.ErrCode: %s\n", er.ErrCode)
	fmt.Printf("Error.Description: %s\n\n", er.Description)
	return nil
}

func (c MyCallback) UnhandledEvent(byData []byte) error {
	// handle the unhandled event
	fmt.Printf("\n[UnhandledEvent] Received\n")
	fmt.Printf("UnhandledEvent: %s\n\n", string(byData))
	return nil
}

func foo() {
	// init library
	microphone.Initialize()

	// print instructions
	fmt.Print("\n\nPress ENTER to exit!\n\n")

	/*
		DG Streaming API
	*/
	// init library
	client.Init(client.InitLib{
		LogLevel: client.LogLevelDefault, // LogLevelDefault, LogLevelFull, LogLevelDebug, LogLevelTrace
	})

	// Go context
	ctx := context.Background()

	// client options
	cOptions := &interfaces.ClientOptions{
		EnableKeepAlive: true,
	}

	// set the Transcription options
	tOptions := &interfaces.LiveTranscriptionOptions{
		Model:       "nova-2",
		Language:    "en-US",
		Punctuate:   true,
		Encoding:    "linear16",
		Channels:    1,
		SampleRate:  16000,
		SmartFormat: true,
		VadEvents:   true,
		// To get UtteranceEnd, the following must be set:
		InterimResults: true,
		UtteranceEndMs: "1000",
		// End of UtteranceEnd settings
	}

	// example on how to send a custom parameter
	// params := make(map[string][]string, 0)
	// params["dictation"] = []string{"true"}
	// ctx = interfaces.WithCustomParameters(ctx, params)

	// implement your own callback
	callback := MyCallback{
		sb: &strings.Builder{},
	}

	// create a Deepgram client
	dgClient, err := client.NewWebSocket(ctx, "", cOptions, tOptions, callback)
	if err != nil {
		fmt.Println("ERROR creating LiveTranscription connection:", err)
		return
	}

	// connect the websocket to Deepgram
	bConnected := dgClient.Connect()
	if !bConnected {
		fmt.Println("Client.Connect failed")
		os.Exit(1)
	}

	/*
		Microphone package
	*/
	// mic stuf
	mic, err := microphone.New(microphone.AudioConfig{
		InputChannels: 1,
		SamplingRate:  16000,
	})
	if err != nil {
		fmt.Printf("Initialize failed. Err: %v\n", err)
		os.Exit(1)
	}

	// start the mic
	err = mic.Start()
	if err != nil {
		fmt.Printf("mic.Start failed. Err: %v\n", err)
		os.Exit(1)
	}

	go func() {
		// feed the microphone stream to the Deepgram client (this is a blocking call)
		mic.Stream(dgClient)
	}()

	// wait for user input to exit
	input := bufio.NewScanner(os.Stdin)
	input.Scan()

	// close mic stream
	err = mic.Stop()
	if err != nil {
		fmt.Printf("mic.Stop failed. Err: %v\n", err)
		os.Exit(1)
	}

	// teardown library
	microphone.Teardown()

	// close DG client
	dgClient.Stop()

	fmt.Printf("\n\nProgram exiting...\n")
	// time.Sleep(120 * time.Second)
}
