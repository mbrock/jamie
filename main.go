package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"
)

var (
	Token string
	logger *log.Logger
)

func init() {
	Token = os.Getenv("DISCORD_TOKEN")
	if Token == "" {
		fmt.Println("No token provided. Please set the DISCORD_TOKEN environment variable.")
		os.Exit(1)
	}

	logger = log.NewWithOptions(os.Stderr, log.Options{
		ReportCaller: true,
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
	dg.AddHandler(voiceStateUpdate)
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

	_, err = s.ChannelVoiceJoin(guildID, voiceChannel.ID, false, false)
	if err != nil {
		return fmt.Errorf("failed to join voice channel: %w", err)
	}

	logger.Info("Joined voice channel", "channel", voiceChannel.Name)
	s.ChannelMessageSend(textChannelID, "Joined voice channel: "+voiceChannel.Name)
	return nil
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

func voiceStateUpdate(s *discordgo.Session, v *discordgo.VoiceStateUpdate) {
	if v.UserID == s.State.User.ID {
		return // Ignore updates for the bot itself
	}

	user, err := s.User(v.UserID)
	if err != nil {
		logger.Error("Error fetching user", "error", err)
		return
	}

	guild, err := s.Guild(v.GuildID)
	if err != nil {
		logger.Error("Error fetching guild", "error", err)
		return
	}

	var action string
	var channelName string

	if v.ChannelID != "" {
		channel, err := s.Channel(v.ChannelID)
		if err != nil {
			logger.Error("Error fetching channel", "error", err)
			return
		}
		channelName = channel.Name
		action = "joined"
	} else {
		action = "left"
	}

	if v.SelfMute {
		action = "muted themselves in"
	} else if v.SelfDeaf {
		action = "deafened themselves in"
	} else if v.Mute {
		action = "was muted in"
	} else if v.Deaf {
		action = "was deafened in"
	} else if v.SelfStream {
		action = "started streaming in"
	} else if v.SelfVideo {
		action = "turned on their camera in"
	}

	logger.Info("Voice state update",
		"user", user.Username,
		"action", action,
		"channel", channelName,
		"guild", guild.Name,
		"timestamp", time.Now().Format(time.RFC3339),
	)
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
			_, err := s.ChannelVoiceJoin(guildID, channel.ID, false, false)
			if err != nil {
				logger.Error("Failed to join voice channel", "channel", channel.Name, "error", err)
			} else {
				logger.Info("Joined voice channel", "channel", channel.Name)
			}
		}
	}

	return nil
}
