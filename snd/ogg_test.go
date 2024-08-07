package snd

import (
	"math"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/pion/rtp"
	"gopkg.in/hraban/opus.v2"
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
func (m *MockLogger) Warn(msg interface{}, keyvals ...interface{})  {}

func TestOggWritePacket(t *testing.T) {
	mockWriter := &MockOggWriter{}
	mockTime := &MockTimeProvider{currentTime: time.Unix(0, 0).UTC()}
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
	mockTime := &MockTimeProvider{currentTime: time.Unix(0, 0).UTC()}
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
	initialSilenceDuration := 2 * time.Second
	firstPacketTime := startTime.Add(initialSilenceDuration)
	err = ogg.WritePacket(OpusPacket{
		ID:        1,
		Sequence:  1,
		Timestamp: 960, // 20ms at 48kHz
		CreatedAt: firstPacketTime,
		OpusData:  []byte{0x01, 0x02, 0x03},
	})
	if err != nil {
		t.Fatalf("Failed to write first packet: %v", err)
	}

	// Test gap insertion
	gapDuration := 3 * time.Second
	secondPacketTime := firstPacketTime.Add(gapDuration + 20*time.Millisecond)
	totalDuration := initialSilenceDuration + gapDuration + 20*time.Millisecond
	totalPackets := int(totalDuration / (20 * time.Millisecond))
	err = ogg.WritePacket(OpusPacket{
		ID:       2,
		Sequence: 2,
		Timestamp: uint32(
			totalPackets,
		) * 960, // Each packet is 960 samples at 48kHz
		CreatedAt: secondPacketTime,
		OpusData:  []byte{0x04, 0x05, 0x06},
	})
	if err != nil {
		t.Fatalf("Failed to write second packet: %v", err)
	}

	// Calculate expected packets
	initialSilencePackets := int(
		initialSilenceDuration / (20 * time.Millisecond),
	)
	gapSilencePackets := int(gapDuration / (20 * time.Millisecond))
	expectedPackets := initialSilencePackets + 1 + gapSilencePackets + 1 // Initial silence + first packet + gap silence + second packet

	// Verify the written packets
	if len(mockWriter.Packets) != expectedPackets {
		t.Errorf(
			"Expected %d packets, got %d",
			expectedPackets,
			len(mockWriter.Packets),
		)
	}

	// Check initial silence packets
	for i := 0; i < initialSilencePackets; i++ {
		if !issilentPacket(mockWriter.Packets[i]) {
			t.Errorf("Expected silent packet at index %d", i)
		}
	}

	// Check first real packet
	if string(
		mockWriter.Packets[initialSilencePackets].Payload,
	) != string(
		[]byte{0x01, 0x02, 0x03},
	) {
		t.Errorf("First real packet payload mismatch")
	}

	// Check gap silence packets
	for i := initialSilencePackets + 1; i < initialSilencePackets+1+gapSilencePackets; i++ {
		if !issilentPacket(mockWriter.Packets[i]) {
			t.Errorf("Expected silent packet at index %d", i)
		}
	}

	// Check second real packet
	if string(
		mockWriter.Packets[expectedPackets-1].Payload,
	) != string(
		[]byte{0x04, 0x05, 0x06},
	) {
		t.Errorf("Second real packet payload mismatch")
	}
}

func issilentPacket(packet MockRTPPacket) bool {
	return len(packet.Payload) == 3 && packet.Payload[0] == 0xFC &&
		packet.Payload[1] == 0xFD &&
		packet.Payload[2] == 0xFE
}

func TestOggWriteSilentPacketsToFile(t *testing.T) {
	tempFile, err := os.CreateTemp("", "test_ogg_*.ogg")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	//  t.Logf("Created temp file: %s", tempFile.Name())

	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	oggWriter, err := NewOggFile(tempFile.Name())
	if err != nil {
		t.Fatalf("Failed to create OggFile: %v", err)
	}

	mockTime := &MockTimeProvider{currentTime: time.Unix(0, 0).UTC()}
	mockLogger := &MockLogger{}

	startTime := mockTime.Now()
	endTime := startTime.Add(time.Second)

	ogg, err := NewOgg(
		12345,     // ssrc
		startTime, // startTime
		endTime,   // endTime
		oggWriter,
		mockTime,
		mockLogger,
	)
	if err != nil {
		t.Fatalf("Failed to create Ogg: %v", err)
	}

	silentOpusPacket := []byte{0xFC, 0xFD, 0xFE}

	// Write 1 second of silent packets (50 packets of 20ms each)
	for i := 0; i < 50; i++ {
		err = ogg.WritePacket(OpusPacket{
			ID:        i + 1,
			Sequence:  uint16(i + 1),
			Timestamp: uint32((i + 1) * 960),
			CreatedAt: startTime.Add(
				time.Duration(i) * 20 * time.Millisecond,
			),
			OpusData: silentOpusPacket,
		})
		if err != nil {
			t.Fatalf("Failed to write silent packet: %v", err)
		}
	}

	err = ogg.Close()
	if err != nil {
		t.Fatalf("Failed to close Ogg: %v", err)
	}

	// Use opusinfo to verify the Ogg file
	cmd := exec.Command("opusinfo", tempFile.Name())
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("opusinfo failed: %v\nOutput: %s", err, output)
	}
	//	t.Logf("opusinfo output: %s", output)
}

func TestOggWriteSineWave(t *testing.T) {
	tempFile, err := os.CreateTemp("", "test_sine_ogg_*.ogg")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	oggWriter, err := NewOggFile(tempFile.Name())
	if err != nil {
		t.Fatalf("Failed to create OggFile: %v", err)
	}

	mockTime := &MockTimeProvider{currentTime: time.Unix(0, 0).UTC()}
	mockLogger := &MockLogger{}

	startTime := mockTime.Now()
	endTime := startTime.Add(time.Second)

	ogg, err := NewOgg(
		12345,     // ssrc
		startTime, // startTime
		endTime,   // endTime
		oggWriter,
		mockTime,
		mockLogger,
	)
	if err != nil {
		t.Fatalf("Failed to create Ogg: %v", err)
	}

	// Create Opus encoder
	enc, err := opus.NewEncoder(48000, 1, opus.AppVoIP)
	if err != nil {
		t.Fatalf("Failed to create Opus encoder: %v", err)
	}

	// Generate sine wave and encode to Opus packets
	sampleRate := 48000
	duration := time.Second
	frequency := 440.0     // A4 note
	samplesPerFrame := 960 // 20ms at 48kHz

	for i := 0; i < int(duration.Seconds()*float64(sampleRate)); i += samplesPerFrame {
		pcm := make([]int16, samplesPerFrame)
		for j := 0; j < samplesPerFrame; j++ {
			sample := int16(
				32767 * math.Sin(
					2*math.Pi*frequency*float64(i+j)/float64(sampleRate),
				),
			)
			pcm[j] = sample
		}

		data := make([]byte, 1000)
		n, err := enc.Encode(pcm, data)
		if err != nil {
			t.Fatalf("Failed to encode PCM to Opus: %v", err)
		}
		opusPacket := data[:n]

		err = ogg.WritePacket(OpusPacket{
			ID:        i/samplesPerFrame + 1,
			Sequence:  uint16(i/samplesPerFrame + 1),
			Timestamp: uint32(i + samplesPerFrame),
			CreatedAt: startTime.Add(
				time.Duration(i) * time.Second / time.Duration(sampleRate),
			),
			OpusData: opusPacket,
		})
		if err != nil {
			t.Fatalf("Failed to write packet: %v", err)
		}
	}

	err = ogg.Close()
	if err != nil {
		t.Fatalf("Failed to close Ogg: %v", err)
	}

	// Use opusinfo to verify the Ogg file
	cmd := exec.Command("opusinfo", tempFile.Name())
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("opusinfo failed: %v\nOutput: %s", err, output)
	}
	t.Logf("opusinfo output:\n%s", output)

	// Use ffprobe to analyze the audio content
	cmd = exec.Command(
		"ffprobe",
		"-v",
		"error",
		"-show_entries",
		"stream=codec_name,channels,sample_rate",
		"-of",
		"default=noprint_wrappers=1",
		tempFile.Name(),
	)
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ffprobe failed: %v\nOutput: %s", err, output)
	}
	t.Logf("FFprobe output:\n%s", output)
}
