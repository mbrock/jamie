package discord

import (
	"jamie/speech"
)

type VoiceStream struct {
	UserID             string
	StreamID           string
	FirstOpusTimestamp uint32
	FirstReceiveTime   int64
	FirstSequence      uint16
	DeepgramSession    speech.LiveTranscriptionSession
}
