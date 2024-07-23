package discord

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"
	"github.com/google/uuid"

	"jamie/db"
	"jamie/speech"
)

type Vox struct {
	guildID   string
	channelID string
	log       *log.Logger
	srcuid    *sync.Map
	srcrid    *sync.Map
	asr       speech.ASR
	api       *discordgo.Session
}

func Hear(
	guildID, channelID string,
	log *log.Logger,
	asr speech.ASR,
	api *discordgo.Session,
) *Vox {
	return &Vox{
		guildID:   guildID,
		channelID: channelID,
		log:       log,
		srcuid:    &sync.Map{},
		srcrid:    &sync.Map{},
		asr:       asr,
		api:       api,
	}
}

func (vox *Vox) Recv(
	pkt *discordgo.Packet,
) error {
	stream, err := vox.rapForPkt(pkt)
	if err != nil {
		return fmt.Errorf("failed to get or create stream: %w", err)
	}

	relativeOpusTimestamp := pkt.Timestamp - stream.Era
	relativeSequence := pkt.Sequence - stream.Seq
	receiveTime := time.Now().UnixNano()

	err = db.SaveDiscordVoicePacket(
		stream.Rid,
		pkt.Opus,
		relativeSequence,
		relativeOpusTimestamp,
		receiveTime,
	)
	if err != nil {
		return fmt.Errorf(
			"failed to save Discord voice packet to database: %w",
			err,
		)
	}

	err = stream.Owl.SendAudio(pkt.Opus)
	if err != nil {
		return fmt.Errorf("failed to send audio to Deepgram: %w", err)
	}

	return nil
}

func (vox *Vox) rapForPkt(
	pkt *discordgo.Packet,
) (*Rap, error) {
	streamInterface, yes := vox.srcrid.Load(pkt.SSRC)
	if yes {
		return streamInterface.(*Rap), nil
	}

	rid := uuid.New().String()
	uid, ok := vox.srcuid.Load(pkt.SSRC)
	if !ok {
		vox.log.Debug("user id not found", "ssrc", int(pkt.SSRC))
		uid = ""
	}

	owl, err := vox.asr.Start(
		context.Background(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to start Deepgram session: %w", err)
	}

	rap := &Rap{
		Uid: uid.(string),
		Rid: rid,
		Era: pkt.Timestamp,
		Got: time.Now().UnixNano(),
		Seq: pkt.Sequence,
		Owl: owl,
	}

	vox.srcrid.Store(pkt.SSRC, rap)

	err = db.CreateVoiceStream(
		vox.guildID,
		vox.channelID,
		rid,
		uid.(string),
		pkt.SSRC,
		pkt.Timestamp,
		rap.Got,
		rap.Seq,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create voice stream: %w", err)
	}

	vox.log.Info("talk",
		"src", int(pkt.SSRC),
		"uid", uid.(string),
		"rid", rid,
	)

	go vox.hype(rap)

	return rap, nil
}

func (vox *Vox) hype(rap *Rap) {
	pic := txtpic(rap.Rid)

	for bit := range rap.Owl.Read() {
		var txt string

		for txt = range bit {
			vox.log.Info("yap", "txt", txt)
		}

		if txt != "" {
			txt = strings.TrimSpace(txt)

			if strings.EqualFold(txt, "Change my identity.") {
				pic = newpic(pic)
				_, err := vox.api.ChannelMessageSend(
					vox.channelID,
					fmt.Sprintf(
						"You are now %s.",
						pic,
					),
				)
				if err != nil {
					vox.log.Error(
						"send identity change message",
						"error",
						err.Error(),
					)
				}
				continue
			}

			_, err := vox.api.ChannelMessageSend(
				vox.channelID,
				fmt.Sprintf(
					"%s %s",
					pic,
					txt,
				),
			)

			if err != nil {
				vox.log.Error("send new message", "error", err.Error())
			}

			err = db.SaveTranscript(
				vox.guildID,
				vox.channelID,
				txt,
			)

			if err != nil {
				vox.log.Error(
					"save transcript to database",
					"error",
					err.Error(),
				)
			}

		}
	}
}

func newpic(currentEmoji string) string {
	emojis := []string{
		"😀",
		"😎",
		"🤖",
		"👽",
		"🐱",
		"🐶",
		"🦄",
		"🐸",
		"🦉",
		"🦋",
		"🌈",
		"🌟",
		"🍎",
		"🍕",
		"🎸",
		"🚀",
		"🧙",
		"🧛",
		"🧜",
		"🧚",
		"🧝",
		"🦸",
		"🦹",
		"🥷",
		"👨‍🚀",
		"👩‍🔬",
		"🕵️",
		"👨‍🍳",
		"🧑‍🎨",
		"👩‍🏫",
		"🧑‍🌾",
		"🧑‍🏭",
	}

	currentIndex := -1
	for i, emoji := range emojis {
		if emoji == currentEmoji {
			currentIndex = i
			break
		}
	}

	newIndex := (currentIndex + 1) % len(emojis)
	return emojis[newIndex]
}

// Helper function to generate a consistent emoji based on the stream ID
func txtpic(streamID string) string {
	// List of emojis to choose from
	emojis := []string{
		"😀",
		"😎",
		"🤖",
		"👽",
		"🐱",
		"🐶",
		"🦄",
		"🐸",
		"🦉",
		"🦋",
		"🌈",
		"🌟",
		"🍎",
		"🍕",
		"🎸",
		"🚀",
		"🧙",
		"🧛",
		"🧜",
		"🧚",
		"🧝",
		"🦸",
		"🦹",
		"🥷",
		"👨‍🚀",
		"👩‍🔬",
		"🕵️",
		"👨‍🍳",
		"🧑‍🎨",
		"👩‍🏫",
		"🧑‍🌾",
		"🧑‍🏭",
	}

	// Use the first 4 characters of the stream ID to generate a consistent index
	index := 0
	for i := 0; i < 4 && i < len(streamID); i++ {
		index += int(streamID[i])
	}

	// Use modulo to ensure the index is within the range of the emojis slice
	return emojis[index%len(emojis)]
}

func (vox *Vox) Know(
	v *discordgo.VoiceSpeakingUpdate,
) {
	vox.log.Info("talk",
		"src", v.SSRC,
		"guy", v.UserID,
		"yap", v.Speaking,
	)
	vox.srcuid.Store(uint32(v.SSRC), v.UserID)
}

func (vox *Vox) Whom(
	ssrc uint32,
) (string, bool) {
	userID, ok := vox.srcuid.Load(ssrc)
	if !ok {
		return "", false
	}
	return userID.(string), true
}

func (vox *Vox) Find(
	ssrc uint32,
) (string, bool) {
	stream, ok := vox.srcrid.Load(ssrc)
	if !ok {
		return "", false
	}
	return stream.(*Rap).Rid, true
}
