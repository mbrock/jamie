package discordbot

import "github.com/bwmarrin/discordgo"

type Discord interface {
	AddHandler(handler interface{}) func()
	Open() error
	UpdateStreamingStatus(idle int, name string, url string) (err error)
	UpdateListeningStatus(name string) (err error)
	ChannelVoiceJoin(
		gID, cID string,
		mute, deaf bool,
	) (voice *discordgo.VoiceConnection, err error)
	Close() error
	ChannelTyping(
		channelID string,
		options ...discordgo.RequestOption,
	) (err error)
	ChannelMessages(
		channelID string,
		limit int,
		beforeID, afterID, aroundID string,
		options ...discordgo.RequestOption,
	) (st []*discordgo.Message, err error)
	ChannelMessageSend(
		channelID string,
		content string,
		options ...discordgo.RequestOption,
	) (*discordgo.Message, error)
	ChannelMessageSendReply(
		channelID string,
		content string,
		reference *discordgo.MessageReference,
		options ...discordgo.RequestOption,
	) (*discordgo.Message, error)
	ChannelMessageEdit(
		channelID, messageID, content string,
		options ...discordgo.RequestOption,
	) (*discordgo.Message, error)
	ChannelMessageDelete(
		channelID, messageID string,
		options ...discordgo.RequestOption,
	) (err error)
	MessageReactionAdd(
		channelID, messageID, emojiID string,
		options ...discordgo.RequestOption,
	) error
	MessageReactions(
		channelID, messageID, emojiID string,
		limit int,
		beforeID, afterID string,
		options ...discordgo.RequestOption,
	) (st []*discordgo.User, err error)
	User(
		userID string,
		options ...discordgo.RequestOption,
	) (*discordgo.User, error)
	GuildChannels(
		guildID string,
		options ...discordgo.RequestOption,
	) (st []*discordgo.Channel, err error)
	Channel(
		channelID string,
		options ...discordgo.RequestOption,
	) (st *discordgo.Channel, err error)
	MyUserID() (userID string, err error)
	GuildVoiceStates(guildID string) ([]*discordgo.VoiceState, error)
}
