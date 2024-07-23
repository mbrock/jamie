package discord

import (
	"jamie/speech"
)

type Rap struct {
	Uid string
	Rid string
	Era uint32
	Got int64
	Seq uint16
	Owl speech.LiveTranscriptionSession
}
