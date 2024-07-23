package ogg

import (
	"encoding/binary"
	"io"
	"math/rand"
)

const (
	pageHeaderType         = 0
	streamStructureVersion = 0
	continuedPacket        = 1
	beginningOfStream      = 2
	endOfStream            = 4
)

type OggWriter struct {
	w                  io.Writer
	packetCount        int
	granulePosition    int64
	streamSerialNumber uint32
	pageSequenceNumber uint32
	segmentTable       []byte
	payloadData        []byte
}

func NewOggWriter(w io.Writer) *OggWriter {
	return &OggWriter{
		w:                  w,
		streamSerialNumber: rand.Uint32(),
	}
}

func (ow *OggWriter) WritePacket(packet []byte, lastPacket bool) error {
	ow.packetCount++
	ow.granulePosition += int64(len(packet))

	ow.payloadData = append(ow.payloadData, packet...)

	for len(packet) > 255 {
		ow.segmentTable = append(ow.segmentTable, 255)
		packet = packet[255:]
	}
	ow.segmentTable = append(ow.segmentTable, byte(len(packet)))

	if len(ow.payloadData) >= 65025 || lastPacket {
		if err := ow.writePage(lastPacket); err != nil {
			return err
		}
	}

	return nil
}

func (ow *OggWriter) writePage(lastPage bool) error {
	header := make([]byte, 27)
	header[0] = 'O'
	header[1] = 'g'
	header[2] = 'g'
	header[3] = 'S'
	header[4] = streamStructureVersion

	headerType := pageHeaderType
	if lastPage {
		headerType |= endOfStream
	}
	if ow.pageSequenceNumber == 0 {
		headerType |= beginningOfStream
	}
	header[5] = byte(headerType)

	binary.LittleEndian.PutUint64(header[6:14], uint64(ow.granulePosition))
	binary.LittleEndian.PutUint32(header[14:18], ow.streamSerialNumber)
	binary.LittleEndian.PutUint32(header[18:22], ow.pageSequenceNumber)
	ow.pageSequenceNumber++

	// CRC checksum - we'll calculate this later
	binary.LittleEndian.PutUint32(header[22:26], 0)

	header[26] = byte(len(ow.segmentTable))

	if _, err := ow.w.Write(header); err != nil {
		return err
	}

	if _, err := ow.w.Write(ow.segmentTable); err != nil {
		return err
	}

	if _, err := ow.w.Write(ow.payloadData); err != nil {
		return err
	}

	// Reset for next page
	ow.segmentTable = ow.segmentTable[:0]
	ow.payloadData = ow.payloadData[:0]

	return nil
}

func (ow *OggWriter) Close() error {
	if len(ow.payloadData) > 0 {
		return ow.writePage(true)
	}
	return nil
}
