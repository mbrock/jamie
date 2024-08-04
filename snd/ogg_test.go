package snd

import (
	"testing"
	"time"

	"github.com/pion/rtp"
)

type MockTimeProvider struct {
	currentTime time.Time
}

func (m *MockTimeProvider) Now() time.Time {
	return m.currentTime
}

type MockOggWriter struct {
	Packets []MockRTPPacket
}

type MockRTPPacket struct {
	SequenceNumber uint16
	Timestamp      uint32
	SSRC           uint32
	Payload        []byte
}

func (m *MockOggWriter) WriteRTP(packet *rtp.Packet) error {
	m.Packets = append(m.Packets, MockRTPPacket{
		SequenceNumber: packet.SequenceNumber,
		Timestamp:      packet.Timestamp,
		SSRC:           packet.SSRC,
		Payload:        packet.Payload,
	})
	return nil
}

func (m *MockOggWriter) Close() error {
	return nil
}

type MockLogger struct{}

func (m *MockLogger) Info(msg interface{}, keyvals ...interface{})  {}
func (m *MockLogger) Error(msg interface{}, keyvals ...interface{}) {}
func (m *MockLogger) Debug(msg interface{}, keyvals ...interface{}) {}

func TestOggWritePacket(t *testing.T) {
	mockWriter := &MockOggWriter{}
	mockTime := &MockTimeProvider{currentTime: time.Now()}
	mockLogger := &MockLogger{}

	startTime := mockTime.Now()
	endTime := startTime.Add(time.Minute)

	ogg, err := NewOgg(
		12345,     // ssrc
		startTime, // startTime
		endTime,   // endTime
		mockWriter,
		mockTime,
		mockLogger,
	)
	if err != nil {
		t.Fatalf("Failed to create Ogg: %v", err)
	}

	// Test writing a packet
	err = ogg.WritePacket(OpusPacket{
		ID:        1,
		Sequence:  1,
		Timestamp: 960,
		CreatedAt: startTime,
		OpusData:  []byte{0x01, 0x02, 0x03},
	})
	if err != nil {
		t.Fatalf("Failed to write packet: %v", err)
	}

	// Verify the written packet
	if len(mockWriter.Packets) != 1 {
		t.Fatalf("Expected 1 packet, got %d", len(mockWriter.Packets))
	}
	packet := mockWriter.Packets[0]
	if packet.SequenceNumber != 1 {
		t.Errorf("Expected sequence number 1, got %d", packet.SequenceNumber)
	}
	if packet.Timestamp != 960 {
		t.Errorf("Expected timestamp 960, got %d", packet.Timestamp)
	}
	if packet.SSRC != 12345 {
		t.Errorf("Expected SSRC 12345, got %d", packet.SSRC)
	}
	if string(packet.Payload) != string([]byte{0x01, 0x02, 0x03}) {
		t.Errorf("Payload mismatch")
	}
}

func TestOggSilenceAndGapInsertion(t *testing.T) {
	mockWriter := &MockOggWriter{}
	mockTime := &MockTimeProvider{currentTime: time.Now()}
	mockLogger := &MockLogger{}

	startTime := mockTime.Now()
	endTime := startTime.Add(time.Minute)

	ogg, err := NewOgg(
		12345,     // ssrc
		startTime, // startTime
		endTime,   // endTime
		mockWriter,
		mockTime,
		mockLogger,
	)
	if err != nil {
		t.Fatalf("Failed to create Ogg: %v", err)
	}

	// Test initial silence insertion
	firstPacketTime := startTime.Add(2 * time.Second)
	err = ogg.WritePacket(OpusPacket{
		ID:        1,
		Sequence:  1,
		Timestamp: 960,
		CreatedAt: firstPacketTime,
		OpusData:  []byte{0x01, 0x02, 0x03},
	})
	if err != nil {
		t.Fatalf("Failed to write first packet: %v", err)
	}

	// Test gap insertion
	secondPacketTime := firstPacketTime.Add(3 * time.Second)
	err = ogg.WritePacket(OpusPacket{
		ID:        2,
		Sequence:  4, // Simulating a gap of 2 packets
		Timestamp: 3840,
		CreatedAt: secondPacketTime,
		OpusData:  []byte{0x04, 0x05, 0x06},
	})
	if err != nil {
		t.Fatalf("Failed to write second packet: %v", err)
	}

	// Calculate expected packets
	initialSilencePackets := 2 * 50 // 2 seconds / 20ms per packet
	gapSilencePackets := 3 * 50     // 3 seconds / 20ms per packet
	expectedPackets := initialSilencePackets + 1 + gapSilencePackets + 1 // Initial silence + first packet + gap silence + second packet

	// Verify the written packets
	if len(mockWriter.Packets) != expectedPackets {
		t.Fatalf("Expected %d packets, got %d", expectedPackets, len(mockWriter.Packets))
	}

	// Check initial silence packets
	for i := 0; i < initialSilencePackets; i++ {
		if !issilentPacket(mockWriter.Packets[i]) {
			t.Errorf("Expected silent packet at index %d", i)
		}
	}

	// Check first real packet
	if string(mockWriter.Packets[initialSilencePackets].Payload) != string([]byte{0x01, 0x02, 0x03}) {
		t.Errorf("First real packet payload mismatch")
	}

	// Check gap silence packets
	for i := initialSilencePackets + 1; i < initialSilencePackets + 1 + gapSilencePackets; i++ {
		if !issilentPacket(mockWriter.Packets[i]) {
			t.Errorf("Expected silent packet at index %d", i)
		}
	}

	// Check second real packet
	if string(mockWriter.Packets[expectedPackets-1].Payload) != string([]byte{0x04, 0x05, 0x06}) {
		t.Errorf("Second real packet payload mismatch")
	}
}

func issilentPacket(packet MockRTPPacket) bool {
	return len(packet.Payload) == 3 && packet.Payload[0] == 0xf8 && packet.Payload[1] == 0xff && packet.Payload[2] == 0xfe
}
